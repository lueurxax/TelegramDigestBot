package telegrambot

import (
	"strings"
	"testing"
)

func TestIsNumericWeight(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"0.1", true},
		{"1.0", true},
		{"1.5", true},
		{"2.0", true},
		{"0.5", true},
		{"0.0", false},  // below min
		{"0.09", false}, // below min
		{"2.1", false},  // above max
		{"3.0", false},  // above max
		{"auto", false},
		{"abc", false},
		{"", false},
		{"-1.0", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := isNumericWeight(tt.input)

			if got != tt.expected {
				t.Errorf("isNumericWeight(%q) = %v, want %v", tt.input, got, tt.expected)
			}
		})
	}
}

func TestFormatChannelDisplay(t *testing.T) {
	tests := []struct {
		name       string
		username   string
		title      string
		identifier string
		want       string
	}{
		{
			name:       "prefer username",
			username:   "testchannel",
			title:      "Test Channel",
			identifier: "123456",
			want:       "<code>@testchannel</code>",
		},
		{
			name:       "fallback to title",
			username:   "",
			title:      "Test Channel",
			identifier: "123456",
			want:       "<b>Test Channel</b>",
		},
		{
			name:       "fallback to identifier",
			username:   "",
			title:      "",
			identifier: "123456",
			want:       "<code>123456</code>",
		},
		{
			name:       "escape html in username",
			username:   "test<>channel",
			title:      "",
			identifier: "",
			want:       "<code>@test&lt;&gt;channel</code>",
		},
		{
			name:       "escape html in title",
			username:   "",
			title:      "Test <b>Channel</b>",
			identifier: "",
			want:       "<b>Test &lt;b&gt;Channel&lt;/b&gt;</b>",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatChannelDisplay(tt.username, tt.title, tt.identifier)

			if got != tt.want {
				t.Errorf("formatChannelDisplay(%q, %q, %q) = %q, want %q",
					tt.username, tt.title, tt.identifier, got, tt.want)
			}
		})
	}
}

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
				// Note: blockquote tags might add a few chars over limit, which is acceptable
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
