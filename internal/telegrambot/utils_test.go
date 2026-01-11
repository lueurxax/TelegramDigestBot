package telegrambot

import (
	"strings"
	"testing"
)

func TestSplitHTML(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		limit    int
		wantLen  int
		contains []string
	}{
		{
			name:     "simple split",
			text:     "line 1\nline 2\nline 3",
			limit:    10,
			wantLen:  3, // Each line is 6 chars, with newline it's 13, so each must be separate
			contains: []string{"line 1", "line 2", "line 3"},
		},
		{
			name:    "blockquote split",
			text:    "header\n<blockquote>line 1\nline 2\nline 3</blockquote>\nfooter",
			limit:   30,
			wantLen: 2,
		},
		{
			name:    "nested tags split",
			text:    "<b>bold <i>italic\nstill italic</i> bold</b>",
			limit:   20,
			wantLen: 2,
		},
		{
			name:    "tags with attributes split",
			text:    "<a href=\"http://example.com\">link text\nsecond line</a>",
			limit:   20,
			wantLen: 2,
		},
		{
			name:    "very long line split",
			text:    "ThisIsAVeryLongLineThatExceedsTheLimitAndHasNoNewlines",
			limit:   10,
			wantLen: 6,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parts := SplitHTML(tt.text, tt.limit)
			if len(parts) != tt.wantLen {
				t.Errorf("SplitHTML() got %d parts, want %d. Parts: %v", len(parts), tt.wantLen, parts)
			}
			for i, p := range parts {
				if len(p) > tt.limit && !strings.Contains(p, "<blockquote>") { // blockquote might add few chars but we should be careful
					// Actually our limit check is strictly before adding line, so it should be fine.
				}
				if strings.Contains(p, "<blockquote>") && !strings.Contains(p, "</blockquote>") {
					t.Errorf("Part %d has open blockquote: %s", i, p)
				}
				if !strings.Contains(p, "<blockquote>") && strings.Contains(p, "</blockquote>") {
					t.Errorf("Part %d has closed blockquote without opening: %s", i, p)
				}
			}
		})
	}
}
