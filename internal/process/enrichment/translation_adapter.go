package enrichment

import (
	"context"
	"fmt"
	"strings"

	"github.com/lueurxax/telegram-digest-bot/internal/core/llm"
)

type translationAdapter struct {
	llmClient llm.Client
}

// NewTranslationAdapter creates a new translation adapter.
// The model parameter is deprecated and ignored - the LLM registry handles task-specific model selection.
func NewTranslationAdapter(llmClient llm.Client, _ string) TranslationClient {
	return &translationAdapter{
		llmClient: llmClient,
	}
}

func (a *translationAdapter) Translate(ctx context.Context, text string, targetLanguage string) (string, error) {
	// Pass empty model to let the LLM registry handle task-specific model selection
	// via LLM_TRANSLATE_MODEL env var or default task config
	res, err := a.llmClient.TranslateText(ctx, text, targetLanguage, "")
	if err != nil {
		return "", fmt.Errorf(fmtErrTranslateTo, targetLanguage, err)
	}

	return cleanTranslation(res), nil
}

func cleanTranslation(text string) string {
	text = strings.TrimSpace(text)

	// Remove internal newlines for search queries
	text = strings.ReplaceAll(text, "\n", " ")
	text = strings.ReplaceAll(text, "\r", " ")
	text = strings.Trim(text, `"'`)

	prefixes := []string{
		"translation:", "translated:", "query:", "translated query:",
		"перевод:", "переведенный запрос:", "запрос:",
		"greek translation:", "english translation:",
	}

	for {
		changed := false
		text = strings.Trim(text, `"' `)
		lowerText := strings.ToLower(text)

		for _, p := range prefixes {
			if strings.HasPrefix(lowerText, p) {
				text = strings.TrimSpace(text[len(p):])
				lowerText = strings.ToLower(text)
				changed = true
			}
		}

		if !changed {
			break
		}
	}

	return strings.Trim(text, `"'`)
}
