package digest

import (
	"strings"
	"testing"

	db "github.com/lueurxax/telegram-digest-bot/internal/storage"
)

func TestFormatItemLabel(t *testing.T) {
	tests := []struct {
		name string
		item db.Item
		want string
	}{
		{
			name: "with source channel",
			item: db.Item{SourceChannel: "testchannel"},
			want: "@testchannel",
		},
		{
			name: "with channel title only",
			item: db.Item{SourceChannelTitle: "Test Channel"},
			want: "Test Channel",
		},
		{
			name: "default label",
			item: db.Item{},
			want: DefaultSourceLabel,
		},
		{
			name: "prefers channel over title",
			item: db.Item{
				SourceChannel:      "testchannel",
				SourceChannelTitle: "Test Channel",
			},
			want: "@testchannel",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatItemLabel(tt.item); got != tt.want {
				t.Errorf("formatItemLabel() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestGroupItemsBySummary(t *testing.T) {
	t.Run("groups duplicate summaries", func(t *testing.T) {
		items := []db.Item{
			{ID: "1", Summary: "Summary A", ImportanceScore: 0.5},
			{ID: "2", Summary: "Summary B", ImportanceScore: 0.6},
			{ID: "3", Summary: "Summary A", ImportanceScore: 0.7},
		}

		groups := groupItemsBySummary(items, make(map[string]bool))

		if len(groups) != 2 {
			t.Fatalf("expected 2 groups, got %d", len(groups))
		}

		// First group should be Summary A with 2 items
		if groups[0].summary != "Summary A" {
			t.Errorf("expected first group to be 'Summary A', got %q", groups[0].summary)
		}

		if len(groups[0].items) != 2 {
			t.Errorf("expected 2 items in first group, got %d", len(groups[0].items))
		}

		// Should have higher importance score
		if groups[0].importanceScore != 0.7 {
			t.Errorf("expected importance 0.7, got %f", groups[0].importanceScore)
		}
	})

	t.Run("skips seen summaries", func(t *testing.T) {
		items := []db.Item{
			{ID: "1", Summary: "Summary A"},
			{ID: "2", Summary: "Summary B"},
		}
		seen := map[string]bool{"Summary A": true}

		groups := groupItemsBySummary(items, seen)

		if len(groups) != 1 {
			t.Fatalf("expected 1 group, got %d", len(groups))
		}

		if groups[0].summary != "Summary B" {
			t.Errorf("expected 'Summary B', got %q", groups[0].summary)
		}
	})

	t.Run("empty input returns empty", func(t *testing.T) {
		groups := groupItemsBySummary(nil, make(map[string]bool))
		if len(groups) != 0 {
			t.Errorf("expected 0 groups, got %d", len(groups))
		}
	})
}

func TestFormatSummaryLine(t *testing.T) {
	tests := []struct {
		name         string
		group        summaryGroup
		includeTopic bool
		prefix       string
		summary      string
		wantContains string
	}{
		{
			name: "without topic",
			group: summaryGroup{
				items: []db.Item{{Topic: ""}},
			},
			includeTopic: false,
			prefix:       "📝",
			summary:      "Test summary",
			wantContains: "📝 Test summary",
		},
		{
			name: "with topic included",
			group: summaryGroup{
				items: []db.Item{{Topic: "Technology"}},
			},
			includeTopic: true,
			prefix:       "📝",
			summary:      "Test summary",
			wantContains: "<b>Technology</b>",
		},
		{
			name: "topic disabled even when present",
			group: summaryGroup{
				items: []db.Item{{Topic: "Technology"}},
			},
			includeTopic: false,
			prefix:       "📝",
			summary:      "Test summary",
			wantContains: "📝 Test summary",
		},
		{
			name: "unknown topic uses bullet",
			group: summaryGroup{
				items: []db.Item{{Topic: "Unknown Topic"}},
			},
			includeTopic: true,
			prefix:       "📝",
			summary:      "Test summary",
			wantContains: "•",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatSummaryLine(tt.group, tt.includeTopic, tt.prefix, tt.summary)
			if !strings.Contains(got, tt.wantContains) {
				t.Errorf("formatSummaryLine() = %q, want to contain %q", got, tt.wantContains)
			}
		})
	}
}

func TestTopicEmojis(t *testing.T) {
	knownTopics := []string{
		"Technology", "Finance", "Politics", "Sports",
		"Entertainment", "Science", "Health", "Business",
	}

	for _, topic := range knownTopics {
		if emoji, ok := topicEmojis[topic]; !ok || emoji == "" {
			t.Errorf("topic %q should have an emoji", topic)
		}
	}
}
