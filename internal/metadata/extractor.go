package metadata

import (
	"crypto/md5"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/ca-srg/ragent/internal/types"
	"gopkg.in/yaml.v3"
)

// Type alias for DocumentMetadata
type DocumentMetadata = types.DocumentMetadata

// MetadataExtractor implements the MetadataExtractor interface
type MetadataExtractor struct{}

// NewMetadataExtractor creates a new MetadataExtractor instance
func NewMetadataExtractor() *MetadataExtractor {
	return &MetadataExtractor{}
}

// ExtractMetadata extracts metadata from a file's content and path
func (e *MetadataExtractor) ExtractMetadata(filePath string, content string) (*DocumentMetadata, error) {
	metadata := &DocumentMetadata{
		FilePath:     filePath,
		CustomFields: make(map[string]interface{}),
	}

	// Parse front matter if present
	frontMatter, cleanContent, err := e.ParseFrontMatter(content)
	if err != nil {
		return nil, fmt.Errorf("failed to parse front matter: %w", err)
	}

	// Extract title from front matter or filename
	metadata.Title = e.extractTitle(frontMatter, filePath, cleanContent)

	// Extract category from front matter or file path
	metadata.Category = e.extractCategory(frontMatter, filePath)

	// Extract tags from front matter
	metadata.Tags = e.extractTags(frontMatter)

	// Extract author from front matter
	metadata.Author = e.extractAuthor(frontMatter)

	// Extract reference from front matter
	metadata.Reference = e.extractReference(frontMatter)

	// Extract dates from front matter or file system
	metadata.CreatedAt, metadata.UpdatedAt = e.extractDates(frontMatter, filePath)

	// Set source as filename
	metadata.Source = filepath.Base(filePath)

	// Calculate word count
	metadata.WordCount = e.calculateWordCount(cleanContent)

	// Store all front matter fields in CustomFields
	for key, value := range frontMatter {
		// Skip fields that we've already extracted to specific fields
		if !e.isReservedField(key) {
			metadata.CustomFields[key] = value
		}
	}

	return metadata, nil
}

// ParseFrontMatter extracts YAML front matter from markdown content
func (e *MetadataExtractor) ParseFrontMatter(content string) (map[string]interface{}, string, error) {
	frontMatter := make(map[string]interface{})

	// Check if content starts with YAML front matter
	if strings.HasPrefix(content, "---") {
		// Find the end of front matter
		lines := strings.Split(content, "\n")
		var endIndex = -1

		for i := 1; i < len(lines); i++ {
			if strings.TrimSpace(lines[i]) == "---" {
				endIndex = i
				break
			}
		}

		if endIndex != -1 {
			// Extract front matter YAML
			frontMatterLines := lines[1:endIndex]
			frontMatterYAML := strings.Join(frontMatterLines, "\n")

			// Parse YAML
			if len(frontMatterYAML) > 0 {
				err := yaml.Unmarshal([]byte(frontMatterYAML), &frontMatter)
				if err != nil {
					return frontMatter, content, fmt.Errorf("invalid YAML front matter: %w", err)
				}
			}

			// Return clean content without front matter
			cleanContent := strings.Join(lines[endIndex+1:], "\n")
			return frontMatter, cleanContent, nil
		}
	}

	// Try to parse markdown-style metadata section
	markdownMetadata, cleanContent := e.parseMarkdownMetadata(content)

	// Merge markdown metadata into frontMatter
	for key, value := range markdownMetadata {
		frontMatter[key] = value
	}

	return frontMatter, cleanContent, nil
}

// parseMarkdownMetadata parses markdown-style metadata sections
// Looks for "## メタデータ" section and extracts "- **field**: value" format
func (e *MetadataExtractor) parseMarkdownMetadata(content string) (map[string]interface{}, string) {
	metadata := make(map[string]interface{})
	lines := strings.Split(content, "\n")

	var metadataStartIndex = -1
	var metadataEndIndex = -1

	// Find "## メタデータ" section
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.Contains(trimmed, "## メタデータ") || strings.Contains(trimmed, "## metadata") {
			metadataStartIndex = i
			break
		}
	}

	if metadataStartIndex == -1 {
		// No metadata section found, return original content
		return metadata, content
	}

	// Find the end of metadata section (next heading or "---")
	for i := metadataStartIndex + 1; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		if strings.HasPrefix(trimmed, "#") || trimmed == "---" {
			metadataEndIndex = i
			break
		}
	}

	if metadataEndIndex == -1 {
		metadataEndIndex = len(lines)
	}

	// Parse metadata lines in format: - **field**: value
	for i := metadataStartIndex + 1; i < metadataEndIndex; i++ {
		line := strings.TrimSpace(lines[i])
		if strings.HasPrefix(line, "- **") && strings.Contains(line, "**:") {
			// Extract field name and value
			parts := strings.SplitN(line, "**:", 2)
			if len(parts) == 2 {
				fieldName := strings.TrimSpace(strings.TrimPrefix(parts[0], "- **"))
				value := strings.TrimSpace(parts[1])

				if fieldName != "" && value != "" {
					metadata[fieldName] = value
				}
			}
		}
	}

	// Remove metadata section from content
	var cleanLines []string
	if metadataStartIndex > 0 {
		cleanLines = append(cleanLines, lines[:metadataStartIndex]...)
	}
	if metadataEndIndex < len(lines) {
		cleanLines = append(cleanLines, lines[metadataEndIndex:]...)
	}

	cleanContent := strings.Join(cleanLines, "\n")
	return metadata, cleanContent
}

// GenerateKey creates a unique key for the document
func (e *MetadataExtractor) GenerateKey(metadata *DocumentMetadata) string {
	// Use MD5 hash of file path and title for uniqueness
	keySource := fmt.Sprintf("%s:%s", metadata.FilePath, metadata.Title)
	hash := md5.Sum([]byte(keySource))
	return fmt.Sprintf("%x", hash)
}

// extractTitle gets title from front matter, filename, or content
func (e *MetadataExtractor) extractTitle(frontMatter map[string]interface{}, filePath, content string) string {
	// Try front matter first
	if title, ok := frontMatter["title"].(string); ok && title != "" {
		return title
	}

	// Try to extract from first heading in content
	if title := e.extractTitleFromContent(content); title != "" {
		return title
	}

	// Fall back to filename without extension
	filename := filepath.Base(filePath)
	ext := filepath.Ext(filename)
	return strings.TrimSuffix(filename, ext)
}

// extractTitleFromContent extracts title from the first heading
func (e *MetadataExtractor) extractTitleFromContent(content string) string {
	// Look for first # heading
	re := regexp.MustCompile(`(?m)^#\s+(.+)$`)
	matches := re.FindStringSubmatch(content)
	if len(matches) > 1 {
		return strings.TrimSpace(matches[1])
	}
	return ""
}

// extractCategory gets category from front matter or file path
func (e *MetadataExtractor) extractCategory(frontMatter map[string]interface{}, filePath string) string {
	// Try front matter first
	if category, ok := frontMatter["category"].(string); ok && category != "" {
		return category
	}

	// Try to infer from directory structure
	dir := filepath.Dir(filePath)
	parts := strings.Split(dir, string(filepath.Separator))

	// Get the last directory name as category
	if len(parts) > 0 {
		lastDir := parts[len(parts)-1]
		if lastDir != "." && lastDir != "" {
			return lastDir
		}
	}

	return "general"
}

// extractTags gets tags from front matter
func (e *MetadataExtractor) extractTags(frontMatter map[string]interface{}) []string {
	var tags []string

	// Handle different tag formats
	if tagValue, ok := frontMatter["tags"]; ok {
		switch v := tagValue.(type) {
		case []interface{}:
			for _, tag := range v {
				if tagStr, ok := tag.(string); ok {
					tags = append(tags, strings.TrimSpace(tagStr))
				}
			}
		case []string:
			for _, tag := range v {
				tags = append(tags, strings.TrimSpace(tag))
			}
		case string:
			// Handle comma-separated tags
			for _, tag := range strings.Split(v, ",") {
				tags = append(tags, strings.TrimSpace(tag))
			}
		}
	}

	return tags
}

// extractAuthor gets author from front matter
func (e *MetadataExtractor) extractAuthor(frontMatter map[string]interface{}) string {
	if author, ok := frontMatter["author"].(string); ok {
		return author
	}
	return ""
}

// extractReference extracts reference URL from front matter
func (e *MetadataExtractor) extractReference(frontMatter map[string]interface{}) string {
	if ref, ok := frontMatter["reference"]; ok {
		if refStr, ok := ref.(string); ok {
			return strings.TrimSpace(refStr)
		}
	}
	return ""
}

// extractDates gets creation and modification dates
func (e *MetadataExtractor) extractDates(frontMatter map[string]interface{}, filePath string) (time.Time, time.Time) {
	var createdAt, updatedAt time.Time

	// Try to get dates from front matter
	if dateValue, ok := frontMatter["date"]; ok {
		if dateStr, ok := dateValue.(string); ok {
			if parsed, err := e.parseDate(dateStr); err == nil {
				createdAt = parsed
			}
		}
	}

	if updateValue, ok := frontMatter["updated"]; ok {
		if updateStr, ok := updateValue.(string); ok {
			if parsed, err := e.parseDate(updateStr); err == nil {
				updatedAt = parsed
			}
		}
	}

	// Fall back to file system dates if not found in front matter
	if createdAt.IsZero() || updatedAt.IsZero() {
		if _, err := filepath.Abs(filePath); err == nil {
			// Use current time as fallback since we can't access file stats here
			if updatedAt.IsZero() {
				updatedAt = time.Now()
			}
			if createdAt.IsZero() {
				createdAt = updatedAt
			}
		}
	}

	// If still zero, use current time
	if createdAt.IsZero() {
		createdAt = time.Now()
	}
	if updatedAt.IsZero() {
		updatedAt = createdAt
	}

	return createdAt, updatedAt
}

// parseDate attempts to parse various date formats
func (e *MetadataExtractor) parseDate(dateStr string) (time.Time, error) {
	formats := []string{
		"2006-01-02",
		"2006-01-02T15:04:05Z",
		"2006-01-02T15:04:05-07:00",
		"2006-01-02 15:04:05",
		"January 2, 2006",
		"Jan 2, 2006",
		"2006/01/02",
	}

	for _, format := range formats {
		if t, err := time.Parse(format, dateStr); err == nil {
			return t, nil
		}
	}

	return time.Time{}, fmt.Errorf("unable to parse date: %s", dateStr)
}

// calculateWordCount counts words in content
func (e *MetadataExtractor) calculateWordCount(content string) int {
	// Remove markdown syntax for more accurate word count
	cleaned := e.cleanMarkdown(content)

	// Split by whitespace and count non-empty strings
	words := strings.Fields(cleaned)
	return len(words)
}

// cleanMarkdown removes markdown syntax for word counting
func (e *MetadataExtractor) cleanMarkdown(content string) string {
	// Remove code blocks
	re := regexp.MustCompile("(?s)```.*?```")
	content = re.ReplaceAllString(content, "")

	// Remove inline code
	re = regexp.MustCompile("`[^`]*`")
	content = re.ReplaceAllString(content, "")

	// Remove links but keep text
	re = regexp.MustCompile(`\[([^\]]*)\]\([^\)]*\)`)
	content = re.ReplaceAllString(content, "$1")

	// Remove image syntax
	re = regexp.MustCompile(`!\[([^\]]*)\]\([^\)]*\)`)
	content = re.ReplaceAllString(content, "$1")

	// Remove headings markup
	re = regexp.MustCompile(`(?m)^#+\s+`)
	content = re.ReplaceAllString(content, "")

	// Remove emphasis markers
	content = strings.ReplaceAll(content, "**", "")
	content = strings.ReplaceAll(content, "*", "")
	content = strings.ReplaceAll(content, "__", "")
	content = strings.ReplaceAll(content, "_", "")

	return content
}

// isReservedField checks if a field name is reserved for specific metadata fields
func (e *MetadataExtractor) isReservedField(fieldName string) bool {
	reserved := []string{
		"title", "category", "tags", "author", "date", "updated",
		"created", "created_at", "updated_at", "reference",
	}

	fieldLower := strings.ToLower(fieldName)
	for _, reserved := range reserved {
		if fieldLower == reserved {
			return true
		}
	}

	return false
}

func (e *MetadataExtractor) ExtractGitHubMetadata(repoOwner, repoName, repoRelativePath, content string) (*DocumentMetadata, error) {
	metadata := &DocumentMetadata{
		FilePath:     fmt.Sprintf("github://%s/%s/%s", repoOwner, repoName, repoRelativePath),
		CustomFields: make(map[string]interface{}),
	}

	frontMatter, cleanContent, err := e.ParseFrontMatter(content)
	if err != nil {
		return nil, fmt.Errorf("failed to parse front matter: %w", err)
	}

	metadata.Title = e.extractTitle(frontMatter, repoRelativePath, cleanContent)

	if category, ok := frontMatter["category"].(string); ok && category != "" {
		metadata.Category = category
	} else {
		dir := filepath.Dir(repoRelativePath)
		lastDir := filepath.Base(dir)
		if lastDir == "." || lastDir == "" {
			metadata.Category = "general"
		} else {
			metadata.Category = lastDir
		}
	}

	if author, ok := frontMatter["author"].(string); ok && author != "" {
		metadata.Author = author
	} else {
		metadata.Author = repoOwner
	}

	if source, ok := frontMatter["source"].(string); ok && source != "" {
		metadata.Source = source
	} else {
		metadata.Source = repoName
	}

	metadata.Reference = fmt.Sprintf("https://github.com/%s/%s/blob/main/%s", repoOwner, repoName, repoRelativePath)

	tags := e.extractTags(frontMatter)
	if len(tags) > 0 {
		metadata.Tags = tags
	} else {
		metadata.Tags = []string{repoOwner, repoName}
	}

	metadata.CreatedAt, metadata.UpdatedAt = e.extractDates(frontMatter, repoRelativePath)
	metadata.WordCount = e.calculateWordCount(cleanContent)

	for key, value := range frontMatter {
		if !e.isReservedField(key) {
			metadata.CustomFields[key] = value
		}
	}

	return metadata, nil
}
