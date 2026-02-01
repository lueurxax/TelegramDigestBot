package pipeline

import (
	"strings"
	"time"

	"github.com/lueurxax/telegram-digest-bot/internal/core/domain"
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
	if len(s.summaryStripPhrasesDefault) == 0 {
		s.summaryStripPhrasesDefault = defaultSummaryStripPhrases
	}

	if s.summaryStripPhrasesByLang == nil {
		s.summaryStripPhrasesByLang = make(map[string][]string)
	}

	for _, lang := range []string{"ru", "uk", "en"} {
		if len(s.summaryStripPhrasesByLang[lang]) == 0 {
			s.summaryStripPhrasesByLang[lang] = s.summaryStripPhrasesDefault
		}
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

func parseCSVList(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}

	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))

	for _, part := range parts {
		part = strings.TrimSpace(strings.ToLower(part))
		if part == "" {
			continue
		}

		out = append(out, part)
	}

	return out
}

func (s *pipelineSettings) normalizeLinkSettings() {
	if s.linkPrimaryMinWords <= 0 {
		s.linkPrimaryMinWords = 200
	}

	if s.linkPrimaryShortMsgChars <= 0 {
		s.linkPrimaryShortMsgChars = domain.ShortMessageThreshold
	}

	if s.linkPrimaryMaxLinks <= 0 {
		s.linkPrimaryMaxLinks = s.maxLinks
	}
}

func (s *pipelineSettings) summaryStripPhrasesFor(lang string) []string {
	lang = strings.ToLower(strings.TrimSpace(lang))
	if lang != "" {
		if phrases, ok := s.summaryStripPhrasesByLang[lang]; ok && len(phrases) > 0 {
			return phrases
		}
	}

	return s.summaryStripPhrasesDefault
}
