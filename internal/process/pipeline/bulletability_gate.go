package pipeline

import (
	"context"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/rs/zerolog"

	"github.com/lueurxax/telegram-digest-bot/internal/core/llm"
	"github.com/lueurxax/telegram-digest-bot/internal/platform/observability"
)

const (
	bulletabilityHighThreshold = 0.65
	bulletabilityLowThreshold  = 0.35
	bulletabilityFallbackScore = 0.5

	bulletabilityClassifierTimeout  = 1200 * time.Millisecond
	bulletabilityClassifierMaxRunes = 900
	bulletabilityShortLineMaxRunes  = 120
	bulletabilityMinHeadingPatterns = 2

	bulletabilitySourceDeterministic = "deterministic"
	bulletabilitySourceLLM           = "llm"

	bulletabilityResultBulletable    = "bulletable"
	bulletabilityResultNotBulletable = "not_bulletable"

	bulletabilityShortLineWeight = 0.2
	bulletabilityNewlineWeight   = 0.2

	bulletabilityScoreTextPartsCap     = 3
	bulletabilityClassifierSectionsCap = 4
)

const bulletabilityClassifierPrompt = `You are a format suitability classifier for digest bullets.
Decide if the provided content should be split into multiple bullets.
Return ONLY strict JSON: {"decision":"relevant|irrelevant","confidence":0-1,"reason":"..."}.

Mapping:
- decision="relevant" means bulletable=true (multiple independent points).
- decision="irrelevant" means bulletable=false (single narrative point).

Rules:
- Use relevant only when there are multiple distinct points suitable for separate bullets.
- Use irrelevant for single narrative updates, single-claim reports, or one continuous story.
- If uncertain, choose irrelevant.
`

var (
	bulletabilityMarkerLineRegex = regexp.MustCompile(`^\s*(?:[-*•—]\s+|\d+[\.\)]\s+)`)
	bulletabilityHeadingRegex    = regexp.MustCompile(`^[\p{L}\p{N}][\p{L}\p{N}\s]{2,60}[:：]$`)
)

type bulletabilityDecision struct {
	bulletable bool
	source     string
	score      float64
}

func (p *Pipeline) evaluateBulletability(ctx context.Context, logger zerolog.Logger, candidate llm.MessageInput, summary string, s *pipelineSettings) bulletabilityDecision {
	linkContext, _ := p.buildLinkContext(candidate, s)
	scoreText := buildBulletabilityScoreText(candidate.Text, candidate.PreviewText, linkContext)
	score := computeBulletabilityScore(scoreText)
	observability.BulletabilityScore.Observe(score)

	if score >= bulletabilityHighThreshold {
		return p.recordBulletabilityDecision(true, bulletabilitySourceDeterministic, score)
	}

	if score <= bulletabilityLowThreshold {
		return p.recordBulletabilityDecision(false, bulletabilitySourceDeterministic, score)
	}

	classifierInput := buildBulletabilityClassifierInput(candidate, summary, linkContext)
	if bulletable, ok := p.classifyBulletabilityWithLLM(ctx, logger, classifierInput); ok {
		return p.recordBulletabilityDecision(bulletable, bulletabilitySourceLLM, score)
	}

	return p.recordBulletabilityDecision(score >= bulletabilityFallbackScore, bulletabilitySourceDeterministic, score)
}

func (p *Pipeline) recordBulletabilityDecision(bulletable bool, source string, score float64) bulletabilityDecision {
	result := bulletabilityResultNotBulletable
	if bulletable {
		result = bulletabilityResultBulletable
	} else {
		observability.BulletSingleModeTotal.Inc()
	}

	observability.BulletabilityDecisionTotal.WithLabelValues(result, source).Inc()

	return bulletabilityDecision{
		bulletable: bulletable,
		source:     source,
		score:      score,
	}
}

func (p *Pipeline) classifyBulletabilityWithLLM(ctx context.Context, logger zerolog.Logger, input string) (bool, bool) {
	if strings.TrimSpace(input) == "" {
		return false, false
	}

	classifierCtx, cancel := context.WithTimeout(ctx, bulletabilityClassifierTimeout)
	defer cancel()

	res, err := p.llmClient.RelevanceGate(classifierCtx, input, "", bulletabilityClassifierPrompt)
	if err != nil {
		logger.Debug().Err(err).Msg("bulletability classifier failed")
		return false, false
	}

	switch strings.ToLower(strings.TrimSpace(res.Decision)) {
	case DecisionRelevant:
		return true, true
	case DecisionIrrelevant:
		return false, true
	default:
		logger.Debug().Str(LogFieldDecision, res.Decision).Msg("bulletability classifier returned invalid decision")
		return false, false
	}
}

func computeBulletabilityScore(text string) float64 {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return 0
	}

	lines := splitNonEmptyLines(trimmed)
	if len(lines) == 0 {
		return 0
	}

	markerLines := countMarkerLines(lines)
	shortLines := countShortLines(lines, bulletabilityShortLineMaxRunes)
	newlineDensity := calculateNewlineDensity(trimmed)

	score := 0.0
	if markerLines >= 3 {
		score += 0.5
	} else if markerLines == 2 {
		score += 0.35
	}

	if shortLines >= 3 && markerLines != 2 {
		score += bulletabilityShortLineWeight
	}

	if newlineDensity >= 0.04 {
		score += bulletabilityNewlineWeight
	}

	if hasHeadingSeparatorPattern(lines) {
		score += 0.1
	}

	if score > 1 {
		score = 1
	}

	return score
}

func buildBulletabilityScoreText(text, previewText, linkContext string) string {
	parts := make([]string, 0, bulletabilityScoreTextPartsCap)

	message := strings.TrimSpace(text)
	if message != "" {
		parts = append(parts, message)
	}

	preview := strings.TrimSpace(previewText)
	if preview != "" && preview != message {
		parts = append(parts, preview)
	}

	link := strings.TrimSpace(linkContext)
	if link != "" {
		parts = append(parts, link)
	}

	return strings.Join(parts, "\n")
}

func buildBulletabilityClassifierInput(candidate llm.MessageInput, summary, linkContext string) string {
	sections := make([]string, 0, bulletabilityClassifierSectionsCap)

	if text := strings.TrimSpace(candidate.Text); text != "" {
		sections = append(sections, "Message:\n"+text)
	}

	preview := strings.TrimSpace(candidate.PreviewText)
	if preview != "" && preview != strings.TrimSpace(candidate.Text) {
		sections = append(sections, "Preview:\n"+preview)
	}

	if link := strings.TrimSpace(linkContext); link != "" {
		sections = append(sections, "Link context:\n"+link)
	}

	if s := strings.TrimSpace(summary); s != "" {
		sections = append(sections, "Summary:\n"+s)
	}

	input := strings.Join(sections, "\n\n")
	if utf8.RuneCountInString(input) <= bulletabilityClassifierMaxRunes {
		return input
	}

	runes := []rune(input)

	return string(runes[:bulletabilityClassifierMaxRunes])
}

func splitNonEmptyLines(text string) []string {
	raw := strings.Split(text, "\n")
	lines := make([]string, 0, len(raw))

	for _, line := range raw {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		lines = append(lines, trimmed)
	}

	return lines
}

func countMarkerLines(lines []string) int {
	count := 0

	for _, line := range lines {
		if bulletabilityMarkerLineRegex.MatchString(line) {
			count++
		}
	}

	return count
}

func countShortLines(lines []string, maxRunes int) int {
	count := 0

	for _, line := range lines {
		if utf8.RuneCountInString(line) <= maxRunes {
			count++
		}
	}

	return count
}

func calculateNewlineDensity(text string) float64 {
	runes := utf8.RuneCountInString(text)
	if runes == 0 {
		return 0
	}

	return float64(strings.Count(text, "\n")) / float64(runes)
}

func hasHeadingSeparatorPattern(lines []string) bool {
	count := 0

	for _, line := range lines {
		if bulletabilityHeadingRegex.MatchString(line) || strings.HasSuffix(line, "—") {
			count++
		}
	}

	return count >= bulletabilityMinHeadingPatterns
}
