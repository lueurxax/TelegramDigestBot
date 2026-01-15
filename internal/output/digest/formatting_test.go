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

func TestClusterMaxImportance(t *testing.T) {
	tests := []struct {
		name string
		c    db.ClusterWithItems
		want float32
	}{
		{
			name: "single item",
			c:    db.ClusterWithItems{Items: []db.Item{{ImportanceScore: 0.5}}},
			want: 0.5,
		},
		{
			name: "multiple items returns max",
			c: db.ClusterWithItems{Items: []db.Item{
				{ImportanceScore: 0.5},
				{ImportanceScore: 0.9},
				{ImportanceScore: 0.6},
			}},
			want: 0.9,
		},
		{
			name: "empty cluster returns 0",
			c:    db.ClusterWithItems{Items: []db.Item{}},
			want: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := clusterMaxImportance(tt.c); got != tt.want {
				t.Errorf("clusterMaxImportance() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCategorizeItems(t *testing.T) {
	items := []db.Item{
		{ID: "1", ImportanceScore: 0.9}, // breaking >= 0.8
		{ID: "2", ImportanceScore: 0.7}, // notable >= 0.6
		{ID: "3", ImportanceScore: 0.5}, // also < 0.6
		{ID: "4", ImportanceScore: 0.8}, // breaking exactly
		{ID: "5", ImportanceScore: 0.6}, // notable exactly
	}

	breaking, notable, also := categorizeItems(items)

	if len(breaking.items) != 2 {
		t.Errorf("breaking items count = %d, want 2", len(breaking.items))
	}

	if len(notable.items) != 2 {
		t.Errorf("notable items count = %d, want 2", len(notable.items))
	}

	if len(also.items) != 1 {
		t.Errorf("also items count = %d, want 1", len(also.items))
	}

	// Verify correct items in each category
	if breaking.items[0].ID != "1" && breaking.items[1].ID != "1" {
		t.Error("expected item 1 in breaking")
	}

	if also.items[0].ID != "3" {
		t.Errorf("expected item 3 in also, got %s", also.items[0].ID)
	}
}

func TestCategorizeClusters(t *testing.T) {
	clusters := []db.ClusterWithItems{
		{Topic: "A", Items: []db.Item{{ImportanceScore: 0.9}}},  // breaking
		{Topic: "B", Items: []db.Item{{ImportanceScore: 0.65}}}, // notable
		{Topic: "C", Items: []db.Item{{ImportanceScore: 0.4}}},  // also
	}

	breaking, notable, also := categorizeClusters(clusters)

	if len(breaking.clusters) != 1 {
		t.Errorf("breaking clusters = %d, want 1", len(breaking.clusters))
	}

	if len(notable.clusters) != 1 {
		t.Errorf("notable clusters = %d, want 1", len(notable.clusters))
	}

	if len(also.clusters) != 1 {
		t.Errorf("also clusters = %d, want 1", len(also.clusters))
	}

	if breaking.clusters[0].Topic != "A" {
		t.Errorf("breaking cluster topic = %s, want A", breaking.clusters[0].Topic)
	}
}

func TestCountDistinctTopics(t *testing.T) {
	tests := []struct {
		name  string
		items []db.Item
		want  int
	}{
		{
			name:  "empty items",
			items: nil,
			want:  0,
		},
		{
			name: "all same topic",
			items: []db.Item{
				{Topic: "Technology"},
				{Topic: "Technology"},
			},
			want: 1,
		},
		{
			name: "distinct topics",
			items: []db.Item{
				{Topic: "Technology"},
				{Topic: "Finance"},
				{Topic: "Politics"},
			},
			want: 3,
		},
		{
			name: "mix of same and different",
			items: []db.Item{
				{Topic: "Technology"},
				{Topic: "Finance"},
				{Topic: "Technology"},
				{Topic: "Finance"},
			},
			want: 2,
		},
		{
			name: "empty topic strings not counted",
			items: []db.Item{
				{Topic: ""},
				{Topic: ""},
			},
			want: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := countDistinctTopics(tt.items); got != tt.want {
				t.Errorf("countDistinctTopics() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestExtractTopicsFromDigest(t *testing.T) {
	tests := []struct {
		name     string
		items    []db.Item
		clusters []db.ClusterWithItems
		want     int
	}{
		{
			name:     "empty items and clusters",
			items:    nil,
			clusters: nil,
			want:     0,
		},
		{
			name: "items only",
			items: []db.Item{
				{Topic: "Technology"},
				{Topic: "Finance"},
				{Topic: "Technology"},
			},
			clusters: nil,
			want:     2,
		},
		{
			name:  "clusters only",
			items: nil,
			clusters: []db.ClusterWithItems{
				{Topic: "Politics"},
				{Topic: "Sports"},
			},
			want: 2,
		},
		{
			name: "items and clusters with overlap",
			items: []db.Item{
				{Topic: "Technology"},
				{Topic: "Finance"},
			},
			clusters: []db.ClusterWithItems{
				{Topic: "Technology"},
				{Topic: "Sports"},
			},
			want: 3,
		},
		{
			name: "empty topics not counted",
			items: []db.Item{
				{Topic: ""},
				{Topic: "Science"},
			},
			clusters: []db.ClusterWithItems{
				{Topic: ""},
			},
			want: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractTopicsFromDigest(tt.items, tt.clusters)

			if len(got) != tt.want {
				t.Errorf("extractTopicsFromDigest() returned %d topics, want %d. Topics: %v", len(got), tt.want, got)
			}
		})
	}
}

func TestCountItemsWithMedia(t *testing.T) {
	tests := []struct {
		name  string
		items []db.ItemWithMedia
		want  int
	}{
		{
			name:  "empty items",
			items: nil,
			want:  0,
		},
		{
			name: "all items have media",
			items: []db.ItemWithMedia{
				{MediaData: []byte{1, 2, 3}},
				{MediaData: []byte{4, 5, 6}},
			},
			want: 2,
		},
		{
			name: "no items have media",
			items: []db.ItemWithMedia{
				{MediaData: nil},
				{MediaData: []byte{}},
			},
			want: 0,
		},
		{
			name: "mixed items",
			items: []db.ItemWithMedia{
				{MediaData: []byte{1, 2, 3}},
				{MediaData: nil},
				{MediaData: []byte{4, 5}},
				{MediaData: []byte{}},
			},
			want: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := countItemsWithMedia(tt.items); got != tt.want {
				t.Errorf("countItemsWithMedia() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestExtractDigestHeader(t *testing.T) {
	tests := []struct {
		name         string
		text         string
		wantContains string
		wantMissing  string
	}{
		{
			name:         "basic header",
			text:         "━━━━━━\nDigest Title\n12:00 - 13:00\n🔴 Breaking news",
			wantContains: "Digest Title",
			wantMissing:  "🔴",
		},
		{
			name:         "header with notable marker",
			text:         "Header\n📌 Notable item",
			wantContains: "Header",
			wantMissing:  "📌",
		},
		{
			name:         "header with standard marker",
			text:         "First line\nSecond line\n📝 Standard item",
			wantContains: "Second line",
			wantMissing:  "📝",
		},
		{
			name:         "header with topic border",
			text:         "Digest\n┌─────\n│ Topic content",
			wantContains: "Digest",
			wantMissing:  "┌",
		},
		{
			name:         "empty text",
			text:         "",
			wantContains: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractDigestHeader(tt.text)

			if tt.wantContains != "" && !strings.Contains(got, tt.wantContains) {
				t.Errorf("extractDigestHeader() = %q, want to contain %q", got, tt.wantContains)
			}

			if tt.wantMissing != "" && strings.Contains(got, tt.wantMissing) {
				t.Errorf("extractDigestHeader() = %q, should not contain %q", got, tt.wantMissing)
			}
		})
	}
}
