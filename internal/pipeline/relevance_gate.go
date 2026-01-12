package pipeline

import (
	"regexp"
	"strings"
	"unicode"
)

const (
	gateModel   = "heuristic"
	gateVersion = "v1"
)

type gateDecision struct {
	decision   string
	confidence float32
	reason     string
}

var gateURLRegex = regexp.MustCompile(`(?i)\bhttps?://\S+|\bt\.me/\S+`)

func evaluateRelevanceGate(text string) gateDecision {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return gateDecision{decision: DecisionIrrelevant, confidence: 1.0, reason: ReasonEmpty}
	}

	withoutLinks := strings.TrimSpace(gateURLRegex.ReplaceAllString(trimmed, ""))
	if withoutLinks == "" {
		return gateDecision{decision: DecisionIrrelevant, confidence: 0.9, reason: ReasonLinkOnly}
	}

	if !hasAlphaNum(withoutLinks) {
		return gateDecision{decision: DecisionIrrelevant, confidence: 0.8, reason: ReasonNoText}
	}

	return gateDecision{decision: DecisionRelevant, confidence: 0.6, reason: ReasonPassed}
}

func hasAlphaNum(s string) bool {
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			return true
		}
	}

	return false
}
