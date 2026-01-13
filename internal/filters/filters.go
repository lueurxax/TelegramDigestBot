package filters

import (
	"strings"

	"golang.org/x/text/cases"

	"github.com/lueurxax/telegram-digest-bot/internal/db"
)

const (
	filterModeAllowlist = "allowlist"
	filterModeMixed     = "mixed"
	filterModeDenylist  = "denylist"
)

type Filterer struct {
	adsEnabled  bool
	minLength   int
	adsKeywords []string
	filters     []db.Filter
	mode        string // mixed, allowlist, denylist
	caser       cases.Caser
}

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

func (f *Filterer) IsFiltered(text string) bool {
	if len(text) < f.minLength {
		return true
	}

	lowerText := f.caser.String(text)

	if f.containsAds(lowerText) {
		return true
	}

	if f.matchesDenyFilter(lowerText) {
		return true
	}

	return f.failsAllowFilter(lowerText)
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
