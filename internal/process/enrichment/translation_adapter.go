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

func (a *translationAdapter) Translate(ctx context.Context, text string, targetLanguage string) (string, error) {
	res, err := a.llmClient.TranslateText(ctx, text, targetLanguage, a.model)
	if err != nil {
		return "", fmt.Errorf(fmtErrTranslateTo, targetLanguage, err)
	}

	return res, nil
}
