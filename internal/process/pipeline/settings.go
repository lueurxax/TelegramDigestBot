package pipeline

import (
	"strings"
	"time"
)

func (s *pipelineSettings) normalizeMinLengthSettings() {
	if s.minLengthDefault <= 0 {
		s.minLengthDefault = DefaultMinLength
	}

	if s.minLengthByLang == nil {
		s.minLengthByLang = make(map[string]int)
	}

	for lang, val := range s.minLengthByLang {
		if val <= 0 {
			s.minLengthByLang[lang] = s.minLengthDefault
		}
	}
}

func (s *pipelineSettings) minLengthForLanguage(lang string) int {
	lang = strings.ToLower(strings.TrimSpace(lang))
	if lang == "" {
		return s.minLengthDefault
	}

	if val, ok := s.minLengthByLang[lang]; ok && val > 0 {
		return val
	}

	return s.minLengthDefault
}

func (s *pipelineSettings) normalizeSummarySettings() {
	if s.summaryMaxChars <= 0 {
		s.summaryMaxChars = 220
	}

	if len(s.summaryStripPhrases) == 0 {
		s.summaryStripPhrases = defaultSummaryStripPhrases
	}
}

func (s *pipelineSettings) normalizeDedupWindows() {
	if s.dedupWindow <= 0 {
		s.dedupWindow = DefaultDedupWindowHours * time.Hour
	}

	if s.dedupSameChannelWindow <= 0 {
		s.dedupSameChannelWindow = DefaultDedupSameChannelWindowHours * time.Hour
	}
}

func parseDomainList(raw string) map[string]struct{} {
	out := make(map[string]struct{})
	if strings.TrimSpace(raw) == "" {
		return out
	}

	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(strings.ToLower(part))
		if part != "" {
			out[part] = struct{}{}
		}
	}

	return out
}
