package enrichment

import (
	"context"
	"fmt"

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
	res, err := a.llmClient.TranslateText(ctx, text, "en", a.model)
	if err != nil {
		return "", fmt.Errorf("translate text: %w", err)
	}

	return res, nil
}
