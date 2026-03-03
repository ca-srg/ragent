package webui

import (
	"embed"
	"fmt"
	"html/template"
	"io"
	"time"
)

//go:embed templates/*.html templates/partials/*.html
var templatesFS embed.FS

//go:embed static/*
var staticFiles embed.FS

// TemplateManager manages HTML templates
type TemplateManager struct {
	templates *template.Template
}

// NewTemplateManager creates a new template manager
func NewTemplateManager() (*TemplateManager, error) {
	funcMap := template.FuncMap{
		"formatSize":     formatSize,
		"formatDuration": formatDurationTemplate,
		"formatTime":     formatTime,
		"formatPercent":  formatPercent,
		"statusClass":    statusClass,
		"sub":            func(a, b int) int { return a - b },
		"add":            func(a, b int) int { return a + b },
	}

	tmpl, err := template.New("").Funcs(funcMap).ParseFS(
		templatesFS, "templates/*.html", "templates/partials/*.html")
	if err != nil {
		return nil, fmt.Errorf("failed to parse templates: %w", err)
	}

	return &TemplateManager{
		templates: tmpl,
	}, nil
}

// Render renders a template to the writer
func (tm *TemplateManager) Render(w io.Writer, name string, data interface{}) error {
	return tm.templates.ExecuteTemplate(w, name, data)
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
