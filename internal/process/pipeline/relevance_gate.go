package pipeline

import (
	"context"
	"regexp"
	"strings"
	"unicode"

	"github.com/rs/zerolog"
)

const (
	gateModeHeuristic = "heuristic"
	gateModeLLM       = "llm"
	gateModeHybrid    = "hybrid"

	gateModelHeuristic   = "heuristic"
	gateModelLLM         = "llm"
	gateVersionHeuristic = "v1"

	gatePromptActiveKey     = "prompt:relevance_gate:active"
	gatePromptVersionPrefix = "prompt:relevance_gate:"
	gatePromptDefaultVer    = "v1"
)

const defaultGatePrompt = `You are a relevance gate for a Telegram digest pipeline.
Decide if the message should be summarized for a news digest.
Return ONLY JSON with keys: decision ("relevant" or "irrelevant"), confidence (0-1), reason (short_snake_case).

Rubric:
- Relevant if it contains a factual update, news, or meaningful information likely to matter to readers.
- Irrelevant if it is spam, pure promotion, link-only, empty, or non-informational chatter.
- If unsure, choose "relevant" with low confidence.
`

type gateDecision struct {
	decision   string
	confidence float32
	reason     string
	model      string
	version    string
}

var gateURLRegex = regexp.MustCompile(`(?i)\bhttps?://\S+|\bt\.me/\S+`)

func (p *Pipeline) evaluateRelevanceGate(ctx context.Context, logger zerolog.Logger, text string, s *pipelineSettings) gateDecision {
	mode := strings.ToLower(strings.TrimSpace(s.relevanceGateMode))
	if mode == "" {
		mode = gateModeHeuristic
	}

	heuristic := evaluateRelevanceGateHeuristic(text)

	switch mode {
	case gateModeLLM:
		if decision, ok := p.evaluateGateLLM(ctx, logger, text, s); ok {
			return decision
		}
	case gateModeHybrid:
		if heuristic.decision == DecisionIrrelevant {
			return heuristic
		}

		if decision, ok := p.evaluateGateLLM(ctx, logger, text, s); ok {
			return decision
		}
	}

	return heuristic
}

func (p *Pipeline) evaluateGateLLM(ctx context.Context, logger zerolog.Logger, text string, s *pipelineSettings) (gateDecision, bool) {
	prompt, version := p.loadGatePrompt(ctx, logger)

	model := s.relevanceGateModel
	if model == "" {
		model = p.cfg.LLMModel
	}

	// Pass model to llmClient, if empty it will let the LLM registry handle task-specific model selection
	// via LLM_RELEVANCE_GATE_MODEL env var or default task config
	result, err := p.llmClient.RelevanceGate(ctx, text, s.relevanceGateModel, prompt)
	if err != nil {
		logger.Warn().Err(err).Str(LogFieldTask, dropReasonRelevanceGate).Msg("relevance gate LLM call failed")
		return gateDecision{}, false
	}

	decision := strings.ToLower(strings.TrimSpace(result.Decision))
	if decision != DecisionRelevant && decision != DecisionIrrelevant {
		logger.Warn().Str(LogFieldTask, dropReasonRelevanceGate).Str("decision", result.Decision).Msg("invalid relevance gate decision")
		return gateDecision{}, false
	}

	confidence := result.Confidence
	if confidence < 0 {
		confidence = 0
	} else if confidence > 1 {
		confidence = 1
	}

	return gateDecision{
		decision:   decision,
		confidence: confidence,
		reason:     strings.TrimSpace(result.Reason),
		model:      model,
		version:    version,
	}, true
}

func (p *Pipeline) loadGatePrompt(ctx context.Context, logger zerolog.Logger) (string, string) {
	version := gatePromptDefaultVer

	var active string
	if err := p.database.GetSetting(ctx, gatePromptActiveKey, &active); err == nil {
		if strings.TrimSpace(active) != "" {
			version = strings.TrimSpace(active)
		}
	}

	var override string
	if err := p.database.GetSetting(ctx, gatePromptVersionPrefix+version, &override); err == nil {
		if strings.TrimSpace(override) != "" {
			return override, version
		}
	} else {
		logger.Debug().Err(err).Msg("failed to load relevance gate prompt override")
	}

	return defaultGatePrompt, version
}

func evaluateRelevanceGateHeuristic(text string) gateDecision {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return gateDecision{decision: DecisionIrrelevant, confidence: ConfidenceEmpty, reason: ReasonEmpty, model: gateModelHeuristic, version: gateVersionHeuristic}
	}

	withoutLinks := strings.TrimSpace(gateURLRegex.ReplaceAllString(trimmed, ""))
	if withoutLinks == "" {
		return gateDecision{decision: DecisionIrrelevant, confidence: ConfidenceLinkOnly, reason: ReasonLinkOnly, model: gateModelHeuristic, version: gateVersionHeuristic}
	}

	if !hasAlphaNum(withoutLinks) {
		return gateDecision{decision: DecisionIrrelevant, confidence: ConfidenceNoText, reason: ReasonNoText, model: gateModelHeuristic, version: gateVersionHeuristic}
	}

	return gateDecision{decision: DecisionRelevant, confidence: ConfidencePassed, reason: ReasonPassed, model: gateModelHeuristic, version: gateVersionHeuristic}
}

func hasAlphaNum(s string) bool {
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			return true
		}
	}

	return false
}
