package llm

import (
	"strings"
	"testing"
)

func TestBuildCoverPrompt(t *testing.T) {
	tests := []struct {
		name         string
		topics       []string
		narrative    string
		wantContains []string
		wantMissing  []string
	}{
		{
			name:      "empty topics and narrative",
			topics:    nil,
			narrative: "",
			wantContains: []string{
				"abstract editorial illustration for a news digest",
				"modern conceptual art",
				"Absolutely no text",
			},
			wantMissing: []string{"current events", "news digest covering"},
		},
		{
			name:      "with topics only",
			topics:    []string{"Technology", "Finance"},
			narrative: "",
			wantContains: []string{
				"news digest covering: Technology, Finance",
				"Absolutely no text",
			},
			wantMissing: []string{"current events"},
		},
		{
			name:      "with narrative only",
			topics:    nil,
			narrative: "Breaking news about tech startups",
			wantContains: []string{
				"representing these current events: Breaking news about tech startups",
				"Absolutely no text",
			},
			wantMissing: []string{"news digest covering"},
		},
		{
			name:      "with both topics and narrative",
			topics:    []string{"Politics", "World News"},
			narrative: "Important summit meeting",
			wantContains: []string{
				// When narrative is present, it takes precedence over topics
				"representing these current events: Important summit meeting",
				"Absolutely no text",
			},
			wantMissing: []string{"Politics", "World News"},
		},
		{
			name:      "long narrative is truncated",
			topics:    nil,
			narrative: strings.Repeat("This is a very long narrative. ", 20),
			wantContains: []string{
				"current events",
				"...", // Truncation indicator
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildCoverPrompt(tt.topics, tt.narrative)

			for _, s := range tt.wantContains {
				if !strings.Contains(got, s) {
					t.Errorf("buildCoverPrompt() = %q, want to contain %q", got, s)
				}
			}

			for _, s := range tt.wantMissing {
				if strings.Contains(got, s) {
					t.Errorf("buildCoverPrompt() = %q, should not contain %q", got, s)
				}
			}
		})
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		input string
		max   int
		want  string
	}{
		{"hello", 10, "hello"},
		{"hello", 5, "hello"},
		{"hello", 3, "hel..."},        // Truncated with ellipsis
		{"hello world", 5, "hello..."}, // Truncated with ellipsis
		{"", 10, ""},
		{"test", 0, "..."},            // Edge case: max 0 adds ellipsis
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := truncate(tt.input, tt.max)

			if got != tt.want {
				t.Errorf("truncate(%q, %d) = %q, want %q", tt.input, tt.max, got, tt.want)
			}
		})
	}
}
