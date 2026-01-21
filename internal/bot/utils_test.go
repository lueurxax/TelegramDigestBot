package bot

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/lueurxax/telegram-digest-bot/internal/output/digest"
	db "github.com/lueurxax/telegram-digest-bot/internal/storage"
)

const (
	errIsNumericWeightFmt           = "isNumericWeight(%q) = %v, want %v"
	errFormatRatingsChannelNameFmt  = "formatRatingsChannelName() = %q, want %q"
	errFindChannelNilFmt            = "findChannelByIdentifier() = %v, want nil"
	errFindChannelNonNil            = "findChannelByIdentifier() = nil, want non-nil"
	errFindChannelIDFmt             = "findChannelByIdentifier().ID = %q, want %q"
	errFormatDiscoveryIdentifierFmt = "formatDiscoveryIdentifier() = %q, want %q"
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
				t.Errorf(errIsNumericWeightFmt, tt.input, got, tt.expected)
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
				//nolint:goconst // test literal
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

func TestFormatLink(t *testing.T) {
	tests := []struct {
		name     string
		username string
		peerID   int64
		msgID    int64
		label    string
		want     string
	}{
		{
			name:     "public channel with username",
			username: "testchannel",
			peerID:   123456,
			msgID:    42,
			label:    "Link",
			want:     `<a href="https://t.me/testchannel/42">Link</a>`,
		},
		{
			name:     "private channel without username",
			username: "",
			peerID:   123456789,
			msgID:    99,
			label:    "Private Link",
			want:     `<a href="https://t.me/c/123456789/99">Private Link</a>`,
		},
		{
			name:     "escapes username and label",
			username: "test<channel>",
			peerID:   123,
			msgID:    1,
			label:    "Click <here>",
			want:     `<a href="https://t.me/test&lt;channel&gt;/1">Click &lt;here&gt;</a>`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatLink(tt.username, tt.peerID, tt.msgID, tt.label)

			if got != tt.want {
				t.Errorf("FormatLink() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseRatingsDaysLimit(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		wantDays  int
		wantLimit int
	}{
		{
			name:      "no args uses defaults",
			args:      []string{},
			wantDays:  DefaultRatingsDays,
			wantLimit: DefaultRatingsLimit,
		},
		{
			name:      "first arg sets days",
			args:      []string{"7"},
			wantDays:  7,
			wantLimit: DefaultRatingsLimit,
		},
		{
			name:      "both args set days and limit",
			args:      []string{"14", "50"},
			wantDays:  14,
			wantLimit: 50,
		},
		{
			name:      "invalid first arg uses default days",
			args:      []string{"abc"},
			wantDays:  DefaultRatingsDays,
			wantLimit: DefaultRatingsLimit,
		},
		{
			name:      "negative days uses default",
			args:      []string{"-5"},
			wantDays:  DefaultRatingsDays,
			wantLimit: DefaultRatingsLimit,
		},
		{
			name:      "zero days uses default",
			args:      []string{"0"},
			wantDays:  DefaultRatingsDays,
			wantLimit: DefaultRatingsLimit,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			days, limit := parseRatingsDaysLimit(tt.args)

			if days != tt.wantDays {
				t.Errorf("parseRatingsDaysLimit() days = %d, want %d", days, tt.wantDays)
			}

			if limit != tt.wantLimit {
				t.Errorf("parseRatingsDaysLimit() limit = %d, want %d", limit, tt.wantLimit)
			}
		})
	}
}

func TestComputeRatingTotals(t *testing.T) {
	tests := []struct {
		name           string
		summaries      []db.RatingSummary
		wantGood       int
		wantBad        int
		wantIrrelevant int
		wantTotal      int
	}{
		{
			name:           "empty summaries",
			summaries:      nil,
			wantGood:       0,
			wantBad:        0,
			wantIrrelevant: 0,
			wantTotal:      0,
		},
		{
			name: "single summary",
			summaries: []db.RatingSummary{
				{GoodCount: 10, BadCount: 5, IrrelevantCount: 2, TotalCount: 17},
			},
			wantGood:       10,
			wantBad:        5,
			wantIrrelevant: 2,
			wantTotal:      17,
		},
		{
			name: "multiple summaries",
			summaries: []db.RatingSummary{
				{GoodCount: 10, BadCount: 5, IrrelevantCount: 2, TotalCount: 17},
				{GoodCount: 20, BadCount: 3, IrrelevantCount: 1, TotalCount: 24},
			},
			wantGood:       30,
			wantBad:        8,
			wantIrrelevant: 3,
			wantTotal:      41,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			good, bad, irrelevant, total := computeRatingTotals(tt.summaries)

			if good != tt.wantGood {
				t.Errorf("good = %d, want %d", good, tt.wantGood)
			}

			if bad != tt.wantBad {
				t.Errorf("bad = %d, want %d", bad, tt.wantBad)
			}

			if irrelevant != tt.wantIrrelevant {
				t.Errorf("irrelevant = %d, want %d", irrelevant, tt.wantIrrelevant)
			}

			if total != tt.wantTotal {
				t.Errorf("total = %d, want %d", total, tt.wantTotal)
			}
		})
	}
}

func TestFormatRatingsChannelName(t *testing.T) {
	tests := []struct {
		name      string
		channelID string
		username  string
		title     string
		want      string
	}{
		{
			name:      "prefer username",
			channelID: "123",
			username:  "testchannel",
			title:     "Test Channel",
			want:      "@testchannel",
		},
		{
			name:      "fallback to title",
			channelID: "123",
			username:  "",
			title:     "Test Channel",
			want:      "Test Channel",
		},
		{
			name:      "fallback to channelID",
			channelID: "123456789",
			username:  "",
			title:     "",
			want:      "123456789",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatRatingsChannelName(tt.channelID, tt.username, tt.title)

			if got != tt.want {
				t.Errorf(errFormatRatingsChannelNameFmt, got, tt.want)
			}
		})
	}
}

func TestIsRelevanceKeyword(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"auto", true},
		{"manual", true},
		{"off", true},
		{"on", true},
		{"enable", false},
		{"disable", false},
		{"", false},
		{"AUTO", false}, // case-sensitive
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := isRelevanceKeyword(tt.input)

			if got != tt.want {
				t.Errorf("isRelevanceKeyword(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestFindChannelByIdentifier(t *testing.T) {
	channels := []db.Channel{
		{ID: "1", Username: "testchannel", TGPeerID: 123456},
		{ID: "2", Username: "otherchannel", TGPeerID: 789012},
		{ID: "3", Username: "", TGPeerID: 555555},
	}

	tests := []struct {
		name       string
		identifier string
		wantID     string
		wantNil    bool
	}{
		{
			name:       "find by username",
			identifier: "testchannel",
			wantID:     "1",
		},
		{
			name:       "find by @username",
			identifier: "@otherchannel",
			wantID:     "2",
		},
		{
			name:       "find by peer ID",
			identifier: "123456",
			wantID:     "1",
		},
		{
			name:       "find private channel by peer ID",
			identifier: "555555",
			wantID:     "3",
		},
		{
			name:       "not found",
			identifier: "nonexistent",
			wantNil:    true,
		},
		{
			name:       "empty identifier",
			identifier: "",
			wantNil:    true,
		},
		{
			name:       "@ only",
			identifier: "@",
			wantNil:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := findChannelByIdentifier(channels, tt.identifier)

			if tt.wantNil {
				if got != nil {
					t.Errorf(errFindChannelNilFmt, got)
				}

				return
			}

			require.NotNil(t, got, errFindChannelNonNil)

			if got.ID != tt.wantID {
				t.Errorf(errFindChannelIDFmt, got.ID, tt.wantID)
			}
		})
	}
}

func TestFormatDiscoveryIdentifier(t *testing.T) {
	tests := []struct {
		name      string
		discovery db.DiscoveredChannel
		want      string
	}{
		{
			name: "prefer username",
			discovery: db.DiscoveredChannel{
				Username:   "testchannel",
				TGPeerID:   123456,
				InviteLink: "https://t.me/+abc",
			},
			want: "@testchannel",
		},
		{
			name: "fallback to peer ID",
			discovery: db.DiscoveredChannel{
				Username:   "",
				TGPeerID:   123456,
				InviteLink: "https://t.me/+abc",
			},
			want: "ID:123456",
		},
		{
			name: "fallback to invite link indicator",
			discovery: db.DiscoveredChannel{
				Username:   "",
				TGPeerID:   0,
				InviteLink: "https://t.me/+abc",
			},
			want: "[invite link]",
		},
		{
			name: "empty when nothing available",
			discovery: db.DiscoveredChannel{
				Username:   "",
				TGPeerID:   0,
				InviteLink: "",
			},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatDiscoveryIdentifier(tt.discovery)

			if got != tt.want {
				t.Errorf(errFormatDiscoveryIdentifierFmt, got, tt.want)
			}
		})
	}
}

func TestGetImageFileName(t *testing.T) {
	tests := []struct {
		mimeType string
		want     string
	}{
		{"image/jpeg", "cover.jpg"},
		{"image/png", "cover.png"},
		{"image/webp", "cover.webp"},
		{"image/gif", ""},       // GIFs should be skipped
		{"video/mp4", ""},       // Videos should be skipped
		{"application/pdf", ""}, // Non-images should be skipped
		{"text/html", ""},       // Non-images should be skipped
		{"", ""},                // Empty should be skipped
		{"unknown/type", ""},    // Unknown types should be skipped
	}

	for _, tt := range tests {
		t.Run(tt.mimeType, func(t *testing.T) {
			got := getImageFileName(tt.mimeType)

			if got != tt.want {
				t.Errorf("getImageFileName(%q) = %q, want %q", tt.mimeType, got, tt.want)
			}
		})
	}
}

func TestGetTopicEmoji(t *testing.T) {
	tests := []struct {
		topic string
		want  string
	}{
		{"Technology", "💻"},
		{"Finance", "💰"},
		{"Politics", "⚖️"},
		{"Sports", "🏆"},
		{"Entertainment", "🎬"},
		{"Science", "🔬"},
		{"Health", "🏥"},
		{"Business", "📊"},
		{"World News", "🌍"},
		{"Local News", "📍"},
		{"Culture", "🎨"},
		{"Education", "📚"},
		{"Humor", "😂"},
		{"Unknown Topic", "•"}, // Unknown should return bullet
		{"", "•"},              // Empty should return bullet
	}

	for _, tt := range tests {
		t.Run(tt.topic, func(t *testing.T) {
			got := getTopicEmoji(tt.topic)

			if got != tt.want {
				t.Errorf("getTopicEmoji(%q) = %q, want %q", tt.topic, got, tt.want)
			}
		})
	}
}

func TestFormatDigestItemCaption(t *testing.T) {
	tests := []struct {
		name         string
		item         digest.RichDigestItem
		wantContains []string
	}{
		{
			name: "basic item",
			item: digest.RichDigestItem{
				Summary:    "Test summary",
				Topic:      "Technology",
				Importance: 0.5,
				Channel:    "testchannel",
				ChannelID:  123456,
				MsgID:      42,
			},
			wantContains: []string{"💻", "Test summary", "@testchannel"},
		},
		{
			name: "breaking news item",
			item: digest.RichDigestItem{
				Summary:    "Breaking news!",
				Topic:      "Politics",
				Importance: 0.9,
				Channel:    "newschannel",
			},
			wantContains: []string{"⚖️", "🔴", "Breaking news!"},
		},
		{
			name: "notable item",
			item: digest.RichDigestItem{
				Summary:    "Notable event",
				Topic:      "Sports",
				Importance: 0.7,
				Channel:    "sportschannel",
			},
			wantContains: []string{"🏆", "📌", "Notable event"},
		},
		{
			name: "item without topic",
			item: digest.RichDigestItem{
				Summary:    "Simple update",
				Topic:      "",
				Importance: 0.3,
				Channel:    "channel",
			},
			wantContains: []string{"Simple update", "@channel"},
		},
		{
			name: "item with link",
			item: digest.RichDigestItem{
				Summary:   "Link test",
				Channel:   "linkchannel",
				ChannelID: 789,
				MsgID:     100,
			},
			wantContains: []string{"Link test", "https://t.me/linkchannel/100"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatDigestItemCaption(tt.item)

			for _, s := range tt.wantContains {
				if !strings.Contains(got, s) {
					t.Errorf("formatDigestItemCaption() = %q, want to contain %q", got, s)
				}
			}
		})
	}
}
