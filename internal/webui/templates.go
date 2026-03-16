package webui

import (
	"bytes"
	"embed"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"path/filepath"
	"time"
)

//go:embed templates/*.html templates/partials/*.html
var templatesFS embed.FS

//go:embed static/*
var staticFiles embed.FS

// TemplateManager manages HTML templates.
// Each page template gets its own isolated template set to prevent
// {{define "content"}} blocks from colliding across pages.
type TemplateManager struct {
	templates map[string]*template.Template
}

// NewTemplateManager creates a new template manager with no base path prefix.
func NewTemplateManager() (*TemplateManager, error) {
	return NewTemplateManagerWithBasePath("")
}

// NewTemplateManagerWithBasePath creates a new template manager with a URL base path prefix.
// basePath is prepended to all URL references in templates (e.g., "/dashboard").
// Pass "" for standalone mode where the server owns the root path.
func NewTemplateManagerWithBasePath(basePath string) (*TemplateManager, error) {
	funcMap := template.FuncMap{
		"formatSize":     formatSize,
		"formatDuration": formatDurationTemplate,
		"formatTime":     formatTime,
		"formatPercent":  formatPercent,
		"statusClass":    statusClass,
		"sub":            func(a, b int) int { return a - b },
		"add":            func(a, b int) int { return a + b },
		"basePath":       func() string { return basePath },
	}

	shared, err := template.New("").Funcs(funcMap).ParseFS(
		templatesFS, "templates/base.html", "templates/partials/*.html")
	if err != nil {
		return nil, fmt.Errorf("failed to parse shared templates: %w", err)
	}

	templates := make(map[string]*template.Template)

	// Each page gets its own clone so {{define "content"}} blocks don't collide.
	// Without this, the last-parsed page's "content" overwrites all others.
	pageFiles, err := fs.Glob(templatesFS, "templates/*.html")
	if err != nil {
		return nil, fmt.Errorf("failed to glob page templates: %w", err)
	}
	for _, pf := range pageFiles {
		name := filepath.Base(pf)
		if name == "base.html" {
			continue
		}
		clone, err := shared.Clone()
		if err != nil {
			return nil, fmt.Errorf("failed to clone shared templates for %s: %w", name, err)
		}
		if _, err := clone.ParseFS(templatesFS, pf); err != nil {
			return nil, fmt.Errorf("failed to parse page template %s: %w", name, err)
		}
		templates[name] = clone
	}

	partialFiles, err := fs.Glob(templatesFS, "templates/partials/*.html")
	if err != nil {
		return nil, fmt.Errorf("failed to glob partial templates: %w", err)
	}
	for _, pf := range partialFiles {
		name := filepath.Base(pf)
		templates[name] = shared
	}

	return &TemplateManager{
		templates: templates,
	}, nil
}

// Render renders a template to the writer.
// Output is buffered internally to prevent partial writes on error,
// which avoids superfluous WriteHeader calls when used with http.ResponseWriter.
func (tm *TemplateManager) Render(w io.Writer, name string, data interface{}) error {
	t, ok := tm.templates[name]
	if !ok {
		return fmt.Errorf("template %q not found", name)
	}
	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, name, data); err != nil {
		return err
	}
	_, err := buf.WriteTo(w)
	return err
}

// formatSize formats file size for display
func formatSize(size int64) string {
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
	)

	switch {
	case size >= GB:
		return fmt.Sprintf("%.1f GB", float64(size)/GB)
	case size >= MB:
		return fmt.Sprintf("%.1f MB", float64(size)/MB)
	case size >= KB:
		return fmt.Sprintf("%.1f KB", float64(size)/KB)
	default:
		return fmt.Sprintf("%d B", size)
	}
}

// formatDurationTemplate formats duration for templates
func formatDurationTemplate(d time.Duration) string {
	if d == 0 {
		return "-"
	}
	return formatDuration(d)
}

// formatTime formats time for display
func formatTime(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.Format("2006-01-02 15:04:05")
}

// formatPercent formats a percentage
func formatPercent(p float64) string {
	return fmt.Sprintf("%.1f%%", p)
}

// statusClass returns CSS class based on status
func statusClass(status VectorizeStatus) string {
	switch status {
	case StatusRunning:
		return "status-running"
	case StatusError:
		return "status-error"
	case StatusStopping:
		return "status-stopping"
	default:
		return "status-idle"
	}
}
