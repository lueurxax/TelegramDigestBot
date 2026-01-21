package enrichment

import (
	"context"
	"fmt"
	"strings"

	"github.com/lueurxax/telegram-digest-bot/internal/core/llm"
)

type translationAdapter struct {
	llmClient llm.Client
	model     string
}

func NewTranslationAdapter(llmClient llm.Client, model string) TranslationClient {
	return &translationAdapter{
		llmClient: llmClient,
		model:     model,
	}
}

func (a *translationAdapter) Translate(ctx context.Context, text string, targetLanguage string) (string, error) {
	res, err := a.llmClient.TranslateText(ctx, text, targetLanguage, a.model)
	if err != nil {
		return "", fmt.Errorf(fmtErrTranslateTo, targetLanguage, err)
	}

	return cleanTranslation(res), nil
}

func cleanTranslation(text string) string {
	text = strings.TrimSpace(text)
	text = strings.Trim(text, `"'`)

	prefixes := []string{
		"translation:", "translated:", "query:", "translated query:",
		"перевод:", "переведенный запрос:", "запрос:",
	}

	lowerText := strings.ToLower(text)
	for _, p := range prefixes {
		if strings.HasPrefix(lowerText, p) {
			text = strings.TrimSpace(text[len(p):])
			lowerText = strings.ToLower(text)
		}
	}

	return strings.Trim(text, `"'`)
}
