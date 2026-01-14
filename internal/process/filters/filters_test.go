package filters

import (
	"testing"

	"github.com/lueurxax/telegram-digest-bot/internal/storage"
)

func TestFilterer_IsFiltered(t *testing.T) {
	tests := []struct {
		name       string
		adsEnabled bool
		filters    []db.Filter
		text       string
		expected   bool
	}{
		{
			name:     "short text",
			text:     "too short",
			expected: true,
		},
		{
			name:       "ad keyword",
			adsEnabled: true,
			text:       "Check out this promo for our new product which is very long and detailed!",
			expected:   true,
		},
		{
			name:       "ads disabled",
			adsEnabled: false,
			text:       "Check out this promo for our new product which is very long and detailed!",
			expected:   false,
		},
		{
			name: "deny filter",
			filters: []db.Filter{
				{Type: "deny", Pattern: "spam"},
			},
			text:     "This message contains spam and should be filtered because it is long enough.",
			expected: true,
		},
		{
			name: "allow filter - matched",
			filters: []db.Filter{
				{Type: "allow", Pattern: "important"},
			},
			text:     "This is an important message that should be allowed because it contains the keyword.",
			expected: false,
		},
		{
			name: "allow filter - not matched",
			filters: []db.Filter{
				{Type: "allow", Pattern: "important"},
			},
			text:     "This is a regular message that does not contain the special keyword and should be filtered.",
			expected: true,
		},
		{
			name: "mixed filters - deny wins",
			filters: []db.Filter{
				{Type: "allow", Pattern: "important"},
				{Type: "deny", Pattern: "spam"},
			},
			text:     "This is an important message but it also has spam and should be denied.",
			expected: true,
		},
		{
			name:     "no filters - normal text",
			text:     "This is just a normal message about some news that is definitely long enough to pass.",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := New(tt.filters, tt.adsEnabled, 20, nil, "mixed")
			if got := f.IsFiltered(tt.text); got != tt.expected {
				t.Errorf("IsFiltered() = %v, want %v", got, tt.expected)
			}
		})
	}
}
