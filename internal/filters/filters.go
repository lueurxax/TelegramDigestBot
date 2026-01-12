package filters

import (
	"strings"

	"golang.org/x/text/cases"

	"github.com/lueurxax/telegram-digest-bot/internal/db"
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
		mode = "mixed"
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
	// Basic length filter
	if len(text) < f.minLength {
		return true
	}

	lowerText := f.caser.String(text)

	// Ads filter (heuristic)
	if f.adsEnabled {
		for _, kw := range f.adsKeywords {
			if strings.Contains(lowerText, f.caser.String(kw)) {
				return true
			}
		}
	}

	hasAllowFilters := false
	matchedAllow := false

	for _, filter := range f.filters {
		lowerPattern := f.caser.String(filter.Pattern)

		if filter.Type == "deny" && (f.mode == "denylist" || f.mode == "mixed") {
			if strings.Contains(lowerText, lowerPattern) {
				return true
			}
		} else if filter.Type == "allow" && (f.mode == "allowlist" || f.mode == "mixed") {
			hasAllowFilters = true

			if strings.Contains(lowerText, lowerPattern) {
				matchedAllow = true
			}
		}
	}

	if hasAllowFilters && !matchedAllow && (f.mode == "allowlist" || f.mode == "mixed") {
		return true
	}

	return false
}
