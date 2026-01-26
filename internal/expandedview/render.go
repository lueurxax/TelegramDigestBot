package expandedview

import (
	"embed"
	"fmt"
	"html/template"
	"io"
	"net/url"
	"strings"
	"time"

	db "github.com/lueurxax/telegram-digest-bot/internal/storage"
)

//go:embed templates/*.html
var templateFS embed.FS

// Template function helpers.
var templateFuncs = template.FuncMap{
	"mul": func(a float32, b float64) float64 {
		return float64(a) * b
	},
}

// Renderer handles HTML template rendering.
type Renderer struct {
	expandedTmpl *template.Template
	errorTmpl    *template.Template
}

// NewRenderer creates a new template renderer.
func NewRenderer() (*Renderer, error) {
	expandedTmpl, err := template.New("expanded.html").
		Funcs(templateFuncs).
		ParseFS(templateFS, "templates/expanded.html")
	if err != nil {
		return nil, fmt.Errorf("parse expanded template: %w", err)
	}

	errorTmpl, err := template.New("error.html").
		ParseFS(templateFS, "templates/error.html")
	if err != nil {
		return nil, fmt.Errorf("parse error template: %w", err)
	}

	return &Renderer{
		expandedTmpl: expandedTmpl,
		errorTmpl:    errorTmpl,
	}, nil
}

// ExpandedViewData contains all data for rendering the expanded view.
type ExpandedViewData struct {
	Item         *db.ItemDebugDetail
	Evidence     []db.ItemEvidenceWithSource
	ClusterItems []ClusterItemView
	ChatGPTLink  string
	GeneratedAt  time.Time
}

// ClusterItemView is a simplified view of a cluster item.
type ClusterItemView struct {
	ID              string
	Summary         string
	ChannelUsername string
}

// ErrorData contains data for rendering error pages.
type ErrorData struct {
	Code    int
	Title   string
	Message string
}

// RenderExpanded renders the expanded view page.
func (r *Renderer) RenderExpanded(w io.Writer, data *ExpandedViewData) error {
	if err := r.expandedTmpl.Execute(w, data); err != nil {
		return fmt.Errorf("execute expanded template: %w", err)
	}

	return nil
}

// RenderError renders an error page.
func (r *Renderer) RenderError(w io.Writer, data *ErrorData) error {
	if err := r.errorTmpl.Execute(w, data); err != nil {
		return fmt.Errorf("execute error template: %w", err)
	}

	return nil
}

const (
	maxTextLen         = 1000
	maxEvidenceSources = 3
)

// BuildChatGPTLink constructs a ChatGPT deep link with a pre-filled prompt.
func BuildChatGPTLink(item *db.ItemDebugDetail, evidence []db.ItemEvidenceWithSource) string {
	var sb strings.Builder

	sb.WriteString("I'm reading about this topic:\n\n")
	sb.WriteString("Topic: ")
	sb.WriteString(item.Topic)
	sb.WriteString("\n\nSummary: ")
	sb.WriteString(item.Summary)
	sb.WriteString("\n\nOriginal text:\n")

	// Truncate text to avoid overly long prompts
	text := item.Text

	if len(text) > maxTextLen {
		text = text[:maxTextLen] + "..."
	}

	sb.WriteString(text)

	// Add evidence context if available
	if len(evidence) > 0 {
		sb.WriteString("\n\nRelated sources:\n")

		for i, ev := range evidence {
			if i >= maxEvidenceSources {
				break
			}

			sb.WriteString("- ")

			if ev.Source.Title != "" {
				sb.WriteString(ev.Source.Title)
			} else {
				sb.WriteString(ev.Source.URL)
			}

			sb.WriteString("\n")
		}
	}

	sb.WriteString("\n\nPlease explain the background and significance of this topic. ")
	sb.WriteString("What are the key facts I should know?")

	// ChatGPT doesn't have a direct deep link with prompt, so we link to the main page
	// The user can paste the prompt from clipboard (future: add copy-to-clipboard JS)
	return "https://chat.openai.com/?q=" + url.QueryEscape(sb.String())
}
