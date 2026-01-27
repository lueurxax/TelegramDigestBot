package expandedview

import (
	"embed"
	"encoding/base64"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/lueurxax/telegram-digest-bot/internal/core/links/linkextract"
	db "github.com/lueurxax/telegram-digest-bot/internal/storage"
)

//go:embed templates/*.html
var templateFS embed.FS

// Template function helpers.
var templateFuncs = template.FuncMap{
	"mul": func(a float32, b float64) float64 {
		return float64(a) * b
	},
	// safeHTML marks a string as safe HTML (for LLM-generated summaries with basic formatting)
	"safeHTML": func(s string) template.HTML {
		return template.HTML(s) //nolint:gosec // summaries are LLM-generated, admin-only page
	},
	// isImageMedia checks if the binary data is a valid image that can be displayed
	"isImageMedia": func(data []byte) bool {
		if len(data) == 0 {
			return false
		}
		mimeType := http.DetectContentType(data)
		return strings.HasPrefix(mimeType, "image/")
	},
	// mediaDataURL converts binary image data to a base64 data URL for inline display
	"mediaDataURL": func(data []byte) string {
		if len(data) == 0 {
			return ""
		}
		// Detect MIME type from magic bytes
		mimeType := http.DetectContentType(data)
		return fmt.Sprintf("data:%s;base64,%s", mimeType, base64.StdEncoding.EncodeToString(data))
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
	Item            *db.ItemDebugDetail
	Evidence        []db.ItemEvidenceWithSource
	ClusterItems    []ClusterItemView
	ChatGPTPrompt   string // Full prompt text for clipboard copy (no truncation)
	OriginalMsgLink string // Telegram link to original message
	GeneratedAt     time.Time

	// Apple Shortcuts integration
	ShortcutEnabled   bool   // Whether shortcut button should be shown
	ShortcutURL       string // shortcuts://run-shortcut URL with encoded prompt
	ShortcutICloudURL string // iCloud link to install the shortcut
}

// ClusterItemView is a simplified view of a cluster item.
type ClusterItemView struct {
	ID              string
	Summary         string
	Text            string // Full message text for maximum context
	ChannelUsername string
	ChannelPeerID   int64
	MessageID       int64
}

// ErrorData contains data for rendering error pages.
type ErrorData struct {
	Code        int
	Title       string
	Message     string
	BotUsername string
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

// Default prompt size limits (can be overridden by config).
const (
	defaultMaxPromptChars   = 12000
	maxEvidenceSources      = 5
	maxCorroborationItems   = 5
	maxOriginalLinks        = 10
	maxDescriptionLen       = 200
	fmtBulletItem           = "- %s\n"
	fmtBulletItemWithSource = "- @%s: %s\n"
)

// PromptBuilderConfig holds configuration for building the ChatGPT prompt.
type PromptBuilderConfig struct {
	MaxChars int
}

// BuildChatGPTPrompt constructs the full prompt text for ChatGPT Q&A.
// This follows the proposal's "maximum context" approach: raw text, links,
// corroboration text, original links, capped by prompt limits.
func BuildChatGPTPrompt(item *db.ItemDebugDetail, evidence []db.ItemEvidenceWithSource, clusterItems []ClusterItemView, cfg PromptBuilderConfig) string {
	var sb strings.Builder

	writePromptHeader(&sb, item)
	writePromptSource(&sb, item)
	writePromptText(&sb, item)
	writePromptOriginalLinks(&sb, item)
	writePromptCorroboration(&sb, clusterItems)
	writePromptEvidence(&sb, evidence)
	writePromptQuestions(&sb)

	result := sb.String()

	// MaxChars <= 0 means no truncation (full prompt for clipboard)
	if cfg.MaxChars > 0 {
		result = truncatePrompt(result, cfg.MaxChars)
	}

	return result
}

func writePromptHeader(sb *strings.Builder, item *db.ItemDebugDetail) {
	sb.WriteString("I'm reading about this topic and want to understand it better.\n\n")
	sb.WriteString("## Topic\n")
	sb.WriteString(item.Topic)
	sb.WriteString("\n\n## Summary\n")
	sb.WriteString(item.Summary)
}

func writePromptSource(sb *strings.Builder, item *db.ItemDebugDetail) {
	sb.WriteString("\n\n## Original Source\n")

	if item.ChannelUsername != "" {
		fmt.Fprintf(sb, "Telegram: https://t.me/%s/%d\n", item.ChannelUsername, item.MessageID)
		fmt.Fprintf(sb, "Channel: @%s (%s)\n", item.ChannelUsername, item.ChannelTitle)
	} else {
		fmt.Fprintf(sb, "Telegram: https://t.me/c/%d/%d\n", item.ChannelPeerID, item.MessageID)
		fmt.Fprintf(sb, "Channel: %s\n", item.ChannelTitle)
	}
}

func writePromptText(sb *strings.Builder, item *db.ItemDebugDetail) {
	sb.WriteString("\n## Original Text\n")
	sb.WriteString(item.Text)

	if item.PreviewText != "" && item.PreviewText != item.Text {
		sb.WriteString("\n\n## Preview/Link Content\n")
		sb.WriteString(item.PreviewText)
	}
}

func writePromptOriginalLinks(sb *strings.Builder, item *db.ItemDebugDetail) {
	// Extract URLs from message entities and media JSON
	urls := linkextract.ExtractAllURLs(item.Text, item.EntitiesJSON, item.MediaJSON)
	if len(urls) == 0 {
		return
	}

	sb.WriteString("\n\n## Links in Message\n")
	sb.WriteString("URLs referenced in the original message:\n")

	for i, url := range urls {
		if i >= maxOriginalLinks {
			fmt.Fprintf(sb, "... and %d more links\n", len(urls)-maxOriginalLinks)

			break
		}

		fmt.Fprintf(sb, fmtBulletItem, url)
	}
}

func writePromptCorroboration(sb *strings.Builder, clusterItems []ClusterItemView) {
	if len(clusterItems) == 0 {
		return
	}

	sb.WriteString("\n\n## Duplicate/Related Messages (Corroboration)\n")
	sb.WriteString("Other sources reporting on the same topic:\n")

	for i, ci := range clusterItems {
		if i >= maxCorroborationItems {
			break
		}

		// Write source attribution with link
		if ci.ChannelUsername != "" {
			fmt.Fprintf(sb, "\n### @%s\n", ci.ChannelUsername)
			fmt.Fprintf(sb, "Link: https://t.me/%s/%d\n", ci.ChannelUsername, ci.MessageID)
		} else if ci.ChannelPeerID != 0 {
			fmt.Fprintf(sb, "\n### Channel %d\n", ci.ChannelPeerID)
			fmt.Fprintf(sb, "Link: https://t.me/c/%d/%d\n", ci.ChannelPeerID, ci.MessageID)
		}

		// Write full message text (not just summary)
		if ci.Text != "" {
			fmt.Fprintf(sb, "Message:\n%s\n", ci.Text)
		} else if ci.Summary != "" {
			fmt.Fprintf(sb, "Summary: %s\n", ci.Summary)
		}
	}
}

func writePromptEvidence(sb *strings.Builder, evidence []db.ItemEvidenceWithSource) {
	if len(evidence) == 0 {
		return
	}

	sb.WriteString("\n\n## External Sources (Evidence)\n")

	for i, ev := range evidence {
		if i >= maxEvidenceSources {
			break
		}

		writeEvidenceItem(sb, ev)
	}
}

func writeEvidenceItem(sb *strings.Builder, ev db.ItemEvidenceWithSource) {
	title := ev.Source.Title
	if title == "" {
		title = ev.Source.Domain
	}

	fmt.Fprintf(sb, fmtBulletItem, title)

	if ev.Source.URL != "" {
		fmt.Fprintf(sb, "  URL: %s\n", ev.Source.URL)
	}

	if ev.Source.Description != "" {
		desc := ev.Source.Description
		if len(desc) > maxDescriptionLen {
			desc = desc[:maxDescriptionLen] + "..."
		}

		fmt.Fprintf(sb, "  Excerpt: %s\n", desc)
	}
}

func writePromptQuestions(sb *strings.Builder) {
	sb.WriteString("\n\n## Questions\n")
	sb.WriteString("1. What is the background and context of this topic?\n")
	sb.WriteString("2. What are the key facts I should know?\n")
	sb.WriteString("3. Are there any important nuances or caveats?\n")
	sb.WriteString("4. What are different perspectives on this topic?\n")
}

func truncatePrompt(s string, maxChars int) string {
	if len(s) > maxChars {
		return s[:maxChars-3] + "..."
	}

	return s
}

// BuildOriginalMsgLink constructs the Telegram link to the original message.
func BuildOriginalMsgLink(item *db.ItemDebugDetail) string {
	if item.ChannelUsername != "" {
		return fmt.Sprintf("https://t.me/%s/%d", item.ChannelUsername, item.MessageID)
	}

	return fmt.Sprintf("https://t.me/c/%d/%d", item.ChannelPeerID, item.MessageID)
}

// ShortcutConfig holds configuration for building the Apple Shortcuts URL.
type ShortcutConfig struct {
	Enabled      bool
	ShortcutName string
	ICloudURL    string
	MaxChars     int
}

// Default shortcut URL limits.
const (
	defaultShortcutMaxChars   = 2000
	shortcutURLTruncateSuffix = "...\n\n[Full prompt: use Copy Prompt button]"
)

// BuildShortcutURL constructs the shortcuts:// URL for Apple Shortcuts integration.
// The prompt is truncated to fit within URL length limits.
func BuildShortcutURL(shortcutName, fullPrompt string, maxChars int) string {
	if maxChars <= 0 {
		maxChars = defaultShortcutMaxChars
	}

	// Truncate prompt for URL, leaving room for the suffix
	prompt := fullPrompt
	suffixLen := len(shortcutURLTruncateSuffix)

	if len(prompt) > maxChars-suffixLen {
		prompt = prompt[:maxChars-suffixLen] + shortcutURLTruncateSuffix
	}

	// URL-encode the prompt and shortcut name
	encodedName := url.QueryEscape(shortcutName)
	encodedPrompt := url.QueryEscape(prompt)

	return fmt.Sprintf("shortcuts://run-shortcut?name=%s&input=text&text=%s", encodedName, encodedPrompt)
}
