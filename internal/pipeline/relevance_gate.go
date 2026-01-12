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
		return gateDecision{decision: DecisionIrrelevant, confidence: ConfidenceEmpty, reason: ReasonEmpty}
	}

	withoutLinks := strings.TrimSpace(gateURLRegex.ReplaceAllString(trimmed, ""))
	if withoutLinks == "" {
		return gateDecision{decision: DecisionIrrelevant, confidence: ConfidenceLinkOnly, reason: ReasonLinkOnly}
	}

	if !hasAlphaNum(withoutLinks) {
		return gateDecision{decision: DecisionIrrelevant, confidence: ConfidenceNoText, reason: ReasonNoText}
	}

	return gateDecision{decision: DecisionRelevant, confidence: ConfidencePassed, reason: ReasonPassed}
}

func hasAlphaNum(s string) bool {
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			return true
		}
	}

	return false
}
