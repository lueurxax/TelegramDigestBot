// Package filters implements message content filtering.
//
// The package provides configurable filters to exclude unwanted content:
//   - Minimum length filter
//   - Emoji-only message detection
//   - Boilerplate/CTA detection
//   - Ad keyword filtering
//   - Allow/deny pattern matching
//
// Filters can operate in mixed, allowlist, or denylist mode.
package filters

import (
	"strings"

	"golang.org/x/text/cases"

	db "github.com/lueurxax/telegram-digest-bot/internal/storage"
)

const (
	filterModeAllowlist = "allowlist"
	filterModeMixed     = "mixed"
	filterModeDenylist  = "denylist"

	ReasonMinLength    = "filter_min_length"
	ReasonEmojiOnly    = "filter_emoji_only"
	ReasonBoilerplate  = "filter_boilerplate"
	ReasonForwardShell = "filter_forward_shell"
	ReasonAds          = "filter_ads"
	ReasonAdsComments  = "filter_ads_comments_disabled"
	ReasonDeny         = "filter_deny"
	ReasonAllowMiss    = "filter_allow_miss"
)

// Filterer applies content filters to determine if messages should be excluded.
type Filterer struct {
	adsEnabled  bool
	minLength   int
	adsKeywords []string
	filters     []db.Filter
	mode        string // mixed, allowlist, denylist
	caser       cases.Caser
}

// New creates a new Filterer with the given configuration.
func New(filters []db.Filter, adsEnabled bool, minLength int, adsKeywords []string, mode string) *Filterer {
	if minLength <= 0 {
		minLength = 20
	}

	if len(adsKeywords) == 0 {
		adsKeywords = []string{"#ad", "sponsored", "promo", "подпишись", "купи", "зарабатывай", "выигрывай"}
	}

	if mode == "" {
		mode = filterModeMixed
	}

	return &Filterer{
		filters:     filters,
		adsEnabled:  adsEnabled,
		minLength:   minLength,
		adsKeywords: adsKeywords,
		mode:        mode,
		caser:       cases.Fold(),
	}
}

// IsFiltered returns true if the text should be excluded.
func (f *Filterer) IsFiltered(text string) bool {
	filtered, _ := f.FilterReason(text)
	return filtered
}

// FilterReason returns whether the text is filtered and the reason code.
func (f *Filterer) FilterReason(text string) (bool, string) {
	return f.FilterReasonWithMinLength(text, f.minLength)
}

// FilterReasonWithMinLength checks filters with a custom minimum length.
func (f *Filterer) FilterReasonWithMinLength(text string, minLength int) (bool, string) {
	if minLength > 0 && len(text) < minLength {
		return true, ReasonMinLength
	}

	lowerText := f.caser.String(text)

	if f.containsAds(lowerText) {
		return true, ReasonAds
	}

	if f.matchesDenyFilter(lowerText) {
		return true, ReasonDeny
	}

	if f.failsAllowFilter(lowerText) {
		return true, ReasonAllowMiss
	}

	return false, ""
}

func (f *Filterer) containsAds(lowerText string) bool {
	if !f.adsEnabled {
		return false
	}

	for _, kw := range f.adsKeywords {
		if strings.Contains(lowerText, f.caser.String(kw)) {
			return true
		}
	}

	return false
}

func (f *Filterer) matchesDenyFilter(lowerText string) bool {
	if f.mode != filterModeDenylist && f.mode != filterModeMixed {
		return false
	}

	for _, filter := range f.filters {
		if filter.Type == "deny" && strings.Contains(lowerText, f.caser.String(filter.Pattern)) {
			return true
		}
	}

	return false
}

func (f *Filterer) failsAllowFilter(lowerText string) bool {
	if f.mode != filterModeAllowlist && f.mode != filterModeMixed {
		return false
	}

	hasAllowFilters := false
	matchedAllow := false

	for _, filter := range f.filters {
		if filter.Type == "allow" {
			hasAllowFilters = true

			if strings.Contains(lowerText, f.caser.String(filter.Pattern)) {
				matchedAllow = true
			}
		}
	}

	return hasAllowFilters && !matchedAllow
}
