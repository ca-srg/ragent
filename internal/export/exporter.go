package export

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ca-srg/kiberag/internal/kibela"
)

// CategoryNotFoundError represents an error when category cannot be determined for a note
type CategoryNotFoundError struct {
	NoteID    string
	NoteTitle string
	NoteURL   string
}

func (e CategoryNotFoundError) Error() string {
	return fmt.Sprintf("category could not be determined for note: ID=%s, Title=%s, URL=%s", e.NoteID, e.NoteTitle, e.NoteURL)
}

type Exporter struct {
	client *kibela.Client
}

func New(client *kibela.Client) *Exporter {
	return &Exporter{
		client: client,
	}
}

func (e *Exporter) ExportAllNotes(outputDir string) error {
	ctx := context.Background()

	fmt.Println("Fetching all notes from Kibela...")
	notes, err := e.client.GetAllNotes(ctx)
	if err != nil {
		return fmt.Errorf("failed to get notes: %w", err)
	}

	fmt.Printf("Found %d notes. Starting export...\n", len(notes))

	successCount := 0
	skipCount := 0

	for i, note := range notes {
		err := e.saveNoteAsMarkdown(note, outputDir)
		if err != nil {
			var categoryErr CategoryNotFoundError
			if errors.As(err, &categoryErr) {
				// カテゴリ決定エラーの場合はスキップして継続
				fmt.Printf("Warning: Skipping note '%s' - %v\n", note.Title, categoryErr)
				skipCount++
			} else {
				// その他のエラーの場合は処理停止
				return fmt.Errorf("export stopped due to error for note %s: %w", note.Title, err)
			}
		} else {
			successCount++
		}

		if (i+1)%10 == 0 {
			fmt.Printf("Processed %d/%d notes (exported: %d, skipped: %d)...\n", i+1, len(notes), successCount, skipCount)
		}
	}

	fmt.Printf("Export completed. Total: %d, Exported: %d, Skipped: %d\n", len(notes), successCount, skipCount)
	return nil
}

func (e *Exporter) saveNoteAsMarkdown(note kibela.Note, outputDir string) error {
	filename := e.generateFilename(note)
	filePath := filepath.Join(outputDir, filename)

	markdown, err := e.convertToMarkdown(note)
	if err != nil {
		return fmt.Errorf("failed to convert note to markdown: %w", err)
	}

	err = os.WriteFile(filePath, []byte(markdown), 0644)
	if err != nil {
		return fmt.Errorf("failed to write file %s: %w", filePath, err)
	}

	return nil
}

func (e *Exporter) generateFilename(note kibela.Note) string {
	// より包括的なファイル名サニタイズ
	// macOS、Linux、Windowsで問題となる文字を除去
	reg := regexp.MustCompile(`[<>:"/\\|?*\x00-\x1f\x7f-\x9f]`)
	cleanTitle := reg.ReplaceAllString(note.Title, "_")

	// 連続するアンダースコアを単一にする
	reg = regexp.MustCompile(`_+`)
	cleanTitle = reg.ReplaceAllString(cleanTitle, "_")

	// 前後のアンダースコアと空白を除去
	cleanTitle = strings.Trim(cleanTitle, "_ ")

	// UTF-8文字境界を考慮した安全な長さ制限
	if len(cleanTitle) > 100 {
		cleanTitle = e.truncateUTF8(cleanTitle, 100)
	}

	if cleanTitle == "" {
		cleanTitle = "untitled"
	}

	publishedAt, err := time.Parse(time.RFC3339, note.PublishedAt)
	if err != nil {
		publishedAt = time.Now()
	}

	datePrefix := publishedAt.Format("2006-01-02")

	return fmt.Sprintf("%s_%s.md", datePrefix, cleanTitle)
}

// truncateUTF8 はUTF-8文字境界を考慮して文字列を安全に切り詰める
func (e *Exporter) truncateUTF8(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}

	// バイト数で切り詰め、その後UTF-8文字境界まで戻る
	truncated := s[:maxBytes]
	for len(truncated) > 0 && !utf8.ValidString(truncated) {
		truncated = truncated[:len(truncated)-1]
	}

	return truncated
}

// extractCategoryFromFolderPath はフォルダパスから第3階層のカテゴリを抽出します
// 例: "フォルダ/サーバー/設計/ストダビ" → "設計"
func (e *Exporter) extractCategoryFromFolderPath(folderPath string) string {
	fmt.Printf("Debug: extractCategoryFromFolderPath input: '%s'\n", folderPath)

	if folderPath == "" {
		fmt.Printf("Debug: Empty folder path, returning empty category\n")
		return ""
	}

	// パスを "/" で分割
	parts := strings.Split(folderPath, "/")
	fmt.Printf("Debug: Split parts: %v (length: %d)\n", parts, len(parts))

	// 第1階層が「個人メモ」「日報」などの意味のあるカテゴリの場合のみ使用
	if len(parts) >= 1 && parts[0] != "" {
		firstLevel := parts[0]
		priorityCategories := []string{"個人メモ", "日報", "手順", "データ", "技術", "会議", "企画", "プロジェクト"}
		for _, priority := range priorityCategories {
			if firstLevel == priority {
				fmt.Printf("Debug: Found priority category at index 0: '%s'\n", firstLevel)
				return firstLevel
			}
		}
	}

	// 特定のフォルダを専用カテゴリにマッピング
	if len(parts) >= 1 && parts[0] != "" {
		firstLevel := parts[0]
		categoryMapping := map[string]string{
			"施策仕様書": "仕様書",
		}
		if mappedCategory, exists := categoryMapping[firstLevel]; exists {
			fmt.Printf("Debug: Found mapped category for '%s': '%s'\n", firstLevel, mappedCategory)
			return mappedCategory
		}
	}

	// 第3階層（インデックス2）が存在する場合のみ使用
	if len(parts) >= 3 && parts[2] != "" {
		category := parts[2]
		fmt.Printf("Debug: Found category at index 2: '%s'\n", category)
		return category
	}

	fmt.Printf("Debug: No valid category found, returning empty\n")
	return ""
}

func (e *Exporter) convertToMarkdown(note kibela.Note) (string, error) {
	var builder strings.Builder

	builder.WriteString(fmt.Sprintf("# %s\n\n", note.Title))

	builder.WriteString("## メタデータ\n\n")
	builder.WriteString(fmt.Sprintf("- **title**: %s\n", note.Title))
	builder.WriteString(fmt.Sprintf("- **id**: %s\n", note.ID))
	builder.WriteString(fmt.Sprintf("- **author**: %s\n", note.Author.Account))

	publishedAt, err := time.Parse(time.RFC3339, note.PublishedAt)
	if err == nil {
		builder.WriteString(fmt.Sprintf("- **date**: %s\n", publishedAt.Format("2006-01-02 15:04:05")))
	}

	// デバッグ情報：フォルダ情報をログ出力
	fmt.Printf("Debug: Note ID=%s, Title=%s\n", note.ID, note.Title)
	fmt.Printf("Debug: Folders count: %d\n", len(note.Folders.Nodes))
	for i, folder := range note.Folders.Nodes {
		fmt.Printf("Debug: Folder[%d]: FullName='%s'\n", i, folder.FullName)
	}

	// カテゴリを追加
	var category string
	if len(note.Folders.Nodes) > 0 {
		category = e.extractCategoryFromFolderPath(note.Folders.Nodes[0].FullName)
		fmt.Printf("Debug: Extracted category='%s' from path='%s'\n", category, note.Folders.Nodes[0].FullName)
	} else {
		fmt.Printf("Debug: No folders found for note\n")
	}

	// カテゴリが取得できない場合は専用エラーで処理停止
	if category == "" {
		noteURL := e.client.GetNoteURL(note)
		return "", CategoryNotFoundError{
			NoteID:    note.ID,
			NoteTitle: note.Title,
			NoteURL:   noteURL,
		}
	}

	builder.WriteString(fmt.Sprintf("- **category**: %s\n", category))

	// Kibelaページへの参照URLを追加
	pageURL := e.client.GetNoteURL(note)
	builder.WriteString(fmt.Sprintf("- **reference**: %s\n", pageURL))

	builder.WriteString("\n---\n\n")

	builder.WriteString("## 本文\n\n")
	builder.WriteString(note.Content)

	return builder.String(), nil
}
