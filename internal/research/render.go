package research

import (
	"embed"
	"fmt"
	"html/template"
	"io"
	"strings"
	"time"
)

const fmtDateISO = "2006-01-02"

//go:embed templates/*.html
var templateFS embed.FS

var templateFuncs = template.FuncMap{
	"formatTime": func(t time.Time) string {
		if t.IsZero() {
			return "-"
		}
		return t.Format("2006-01-02 15:04")
	},
	"formatDate": func(t time.Time) string {
		if t.IsZero() {
			return "-"
		}
		return t.Format(fmtDateISO)
	},
	"formatDatePtr": func(t *time.Time) string {
		if t == nil || t.IsZero() {
			return ""
		}
		return t.Format(fmtDateISO)
	},
	"formatFloat": func(val float64) string {
		return fmt.Sprintf(fmtFloat2, val)
	},
	"formatFloat32": func(val float32) string {
		return fmt.Sprintf(fmtFloat2, val)
	},
	"truncate": func(s string, max int) string {
		if max <= 0 {
			return s
		}
		runes := []rune(s)
		if len(runes) <= max {
			return s
		}
		return string(runes[:max]) + "..."
	},
	"join": strings.Join,
}

// Renderer renders research HTML templates.
type Renderer struct {
	tmpl *template.Template
}

// NewRenderer creates a renderer for research templates.
func NewRenderer() (*Renderer, error) {
	tmpl, err := template.New("research").
		Funcs(templateFuncs).
		ParseFS(templateFS, "templates/*.html")
	if err != nil {
		return nil, fmt.Errorf("parse research templates: %w", err)
	}

	return &Renderer{tmpl: tmpl}, nil
}

// Render renders a named template.
func (r *Renderer) Render(w io.Writer, name string, data any) error {
	if err := r.tmpl.ExecuteTemplate(w, name, data); err != nil {
		return fmt.Errorf("execute template %s: %w", name, err)
	}

	return nil
}
