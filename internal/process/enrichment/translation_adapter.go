package enrichment

import (
	"context"

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

func (a *translationAdapter) TranslateToEnglish(ctx context.Context, text string) (string, error) {
	return a.llmClient.TranslateText(ctx, text, "en", a.model)
}
