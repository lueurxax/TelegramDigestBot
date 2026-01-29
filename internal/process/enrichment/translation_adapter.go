package enrichment

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/lueurxax/telegram-digest-bot/internal/core/llm"
)

var (
	errTranslationRefused       = errors.New("LLM refused to translate")
	errTranslationWrongLanguage = errors.New("translation in wrong language")
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

	cleaned := cleanTranslation(res)

	// Validate the translation isn't a refusal or garbage
	if isLLMRefusal(cleaned) {
		return "", errTranslationRefused
	}

	// Validate the translation is in the expected language
	if !isLikelyTargetLanguage(cleaned, targetLanguage) {
		return "", errTranslationWrongLanguage
	}

	return cleaned, nil
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

// llmRefusalPatterns contains common LLM refusal/error patterns.
var llmRefusalPatterns = []string{
	"i am not able to",
	"i'm not able to",
	"i cannot",
	"i can't",
	"i'm sorry",
	"i am sorry",
	"sorry, i",
	"unable to provide",
	"cannot provide",
	"not able to provide",
	"cannot translate",
	"unable to translate",
	"i don't have",
	"i do not have",
	"as an ai",
	"as a language model",
	"i'm an ai",
	"i am an ai",
	"please provide",
	"could you please",
	"i need more",
	"the text appears",
	"this text appears",
	"it seems like",
	"it appears that",
}

// isLLMRefusal detects if the translation response is an LLM refusal or error message.
func isLLMRefusal(text string) bool {
	if text == "" {
		return true
	}

	lower := strings.ToLower(text)

	// Check for common refusal patterns
	for _, pattern := range llmRefusalPatterns {
		if strings.Contains(lower, pattern) {
			return true
		}
	}

	// If response is very long (>300 chars) for a query, it's likely an explanation/refusal
	if len(text) > maxRefusalLength {
		return true
	}

	return false
}

const maxRefusalLength = 300

// scriptCounts holds character counts for different scripts.
type scriptCounts struct {
	latin, cyrillic, greek, total int
}

// countScripts counts characters by script type in the text.
func countScripts(text string) scriptCounts {
	var counts scriptCounts

	for _, r := range text {
		if !isLetter(r) {
			continue
		}

		counts.total++

		switch {
		case isGreekChar(r):
			counts.greek++
		case isCyrillicChar(r):
			counts.cyrillic++
		case isLatinChar(r):
			counts.latin++
		}
	}

	return counts
}

// expectedScript maps language codes to their expected script type.
var expectedScript = map[string]string{
	"el": "greek", "greek": "greek",
	"ru": "cyrillic", "russian": "cyrillic", "uk": "cyrillic", "ukrainian": "cyrillic",
	"en": "latin", "english": "latin", "de": "latin", "german": "latin",
	"fr": "latin", "french": "latin", "es": "latin", "spanish": "latin",
	"it": "latin", "italian": "latin", "pt": "latin", "portuguese": "latin",
}

// isLikelyTargetLanguage checks if the text appears to be in the target language.
// Uses simple script detection for languages with distinct scripts.
func isLikelyTargetLanguage(text, targetLang string) bool {
	if text == "" {
		return false
	}

	counts := countScripts(text)
	if counts.total == 0 {
		return true // Can't determine, assume OK
	}

	script, known := expectedScript[targetLang]
	if !known {
		return true // Unknown language, assume OK
	}

	return hasExpectedScript(counts, script)
}

// hasExpectedScript checks if the script ratio meets the minimum threshold.
func hasExpectedScript(counts scriptCounts, script string) bool {
	var scriptCount int

	switch script {
	case "greek":
		scriptCount = counts.greek
	case "cyrillic":
		scriptCount = counts.cyrillic
	case "latin":
		scriptCount = counts.latin
	}

	return float64(scriptCount)/float64(counts.total) >= minScriptRatio
}

const minScriptRatio = 0.3 // At least 30% of characters should be in expected script

func isLetter(r rune) bool {
	return isLatinChar(r) || isGreekChar(r) || isCyrillicChar(r)
}

func isGreekChar(r rune) bool {
	return (r >= 0x0370 && r <= 0x03FF) || (r >= 0x1F00 && r <= 0x1FFF)
}

func isCyrillicChar(r rune) bool {
	return r >= 0x0400 && r <= 0x052F
}

func isLatinChar(r rune) bool {
	return (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') ||
		(r >= 0x00C0 && r <= 0x024F)
}
