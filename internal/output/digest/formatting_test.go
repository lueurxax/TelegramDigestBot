package digest

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/lueurxax/telegram-digest-bot/internal/platform/config"
	db "github.com/lueurxax/telegram-digest-bot/internal/storage"
)

// Test constants for formatting tests
const (
	testErrFormatItemLabel = "formatItemLabel() = %q, want %q"

	testErrExpected2Groups = "expected 2 groups, got %d"

	testErrExpected1Group = "expected 1 group, got %d"

	testErrExpected0Groups = "expected 0 groups, got %d"

	testErrClusterMaxImportance = "clusterMaxImportance() = %v, want %v"

	testErrCountItemsWithMedia = "countItemsWithMedia() = %d, want %d"

	testErrTruncateForLog = "truncateForLog() = %q, want %q"

	testErrClampThreshold = "clampThreshold(%v, %v, %v) = %v, want %v"

	testNameSkipsSeenSummaries = "skips seen summaries"

	testSummaryFirst = "First"

	testNameEmptyItems = "empty items"

	testErrEmptyCategories = "expected all empty categories for empty input"

	testErrExpected3AlsoItems = "expected 3 also items, got %d"

	testErrExpected1BreakingItem = "expected 1 breaking item, got %d"

	testErrExpected1AlsoItem = "expected 1 also item, got %d"

	testErrExpected1AlsoCluster = "expected 1 also cluster, got %d"

	testErrExpected1NotableItem = "expected 1 notable item, got %d"

	testTopicBorder = "│"

	testErrExpected1Summary = "expected 1 summary, got %d"

	testPrefixLabel = "prefix"

	testStartNotZero = "start should not be zero"

	testErrItemsLength2 = "Items length = %d, want 2"

	testDigestForHeader = "Digest for"

	testTopicContent = "Topic content"

	testErrNoTopicContent = "should not contain topic content, got %q"

	testErrExpected2Summaries = "expected 2 summaries, got %d"

	testErrExpected3Summaries = "expected 3 summaries, got %d"

	testErrExpected1Breaking = "expected 1 breaking cluster, got %d"

	testErrExpected1Notable = "expected 1 notable cluster, got %d"

	testSummaryLabel = "summary"

	testErrExpected2Entries = "expected 2 entries, got %d"

	test3Items = "3 items"

	testTime1000 = "10:00"

	testTime1100 = "11:00"

	testAtChannel = "@testchannel"

	testNameEmptyLabelChannel = "empty label uses channel name"

	testMyTitle = "My Title"

	testSourceTitle = "Title"

	testErrExpected0Links = "expected 0 links for empty items, got %d"

	testErrIndexWant5 = "index = %d, want 5"

	testLinkLabel = "Label"

	testHeaderLines = "Header Line 1\nHeader Line 2"

	testBreakingWord = "Breaking"

	testNameRespectsMaxLimit = "respects max limit"

	testErrExpected5Summaries = "expected 5 summaries, got %d"
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
				t.Errorf(testErrFormatItemLabel, got, tt.want)
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
			t.Fatalf(testErrExpected2Groups, len(groups))
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

	t.Run(testNameSkipsSeenSummaries, func(t *testing.T) {
		items := []db.Item{
			{ID: "1", Summary: "Summary A"},
			{ID: "2", Summary: "Summary B"},
		}

		seen := map[string]bool{"Summary A": true}

		groups := groupItemsBySummary(items, seen)

		if len(groups) != 1 {
			t.Fatalf(testErrExpected1Group, len(groups))
		}

		if groups[0].summary != "Summary B" {
			t.Errorf("expected 'Summary B', got %q", groups[0].summary)
		}
	})

	t.Run("empty input returns empty", func(t *testing.T) {
		groups := groupItemsBySummary(nil, make(map[string]bool))

		if len(groups) != 0 {
			t.Errorf(testErrExpected0Groups, len(groups))
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
			prefix:       EmojiStandard,
			summary:      testSummaryText,
			wantContains: EmojiStandard + " " + testSummaryText,
		},
		{
			name: "with topic included",
			group: summaryGroup{
				items: []db.Item{{Topic: "Technology"}},
			},
			includeTopic: true,
			prefix:       EmojiStandard,
			summary:      testSummaryText,
			wantContains: "<b>" + testTopicTechnology + "</b>",
		},
		{
			name: "topic disabled even when present",
			group: summaryGroup{
				items: []db.Item{{Topic: "Technology"}},
			},
			includeTopic: false,
			prefix:       EmojiStandard,
			summary:      testSummaryText,
			wantContains: EmojiStandard + " " + testSummaryText,
		},
		{
			name: "unknown topic uses bullet",
			group: summaryGroup{
				items: []db.Item{{Topic: "Unknown Topic"}},
			},
			includeTopic: true,
			prefix:       EmojiStandard,
			summary:      testSummaryText,
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
				t.Errorf(testErrClusterMaxImportance, got, tt.want)
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
				t.Errorf(testErrCountItemsWithMedia, got, tt.want)
			}
		})
	}
}

func TestTruncateForLog(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "short string unchanged",
			input: "Short text",
			want:  "Short text",
		},
		{
			name:  "exact length unchanged",
			input: strings.Repeat("a", LogTruncateLength),
			want:  strings.Repeat("a", LogTruncateLength),
		},
		{
			name:  "long string truncated",
			input: strings.Repeat("a", LogTruncateLength+10),
			want:  strings.Repeat("a", LogTruncateLength) + "...",
		},
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := truncateForLog(tt.input); got != tt.want {
				t.Errorf(testErrTruncateForLog, got, tt.want)
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
			text:         "Digest\n┌─────\n" + testTopicBorder + " " + testTopicContent,
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

func TestBuildWindowsFromScheduleTimes(t *testing.T) {
	base := time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name     string
		times    []time.Time
		prev     time.Time
		minStart time.Time
		wantLen  int
	}{
		{
			name:     "empty times",
			times:    []time.Time{},
			prev:     base,
			minStart: base.Add(-24 * time.Hour),
			wantLen:  0,
		},
		{
			name:     "single window",
			times:    []time.Time{base.Add(time.Hour)},
			prev:     base,
			minStart: base.Add(-24 * time.Hour),
			wantLen:  1,
		},
		{
			name:     "multiple windows",
			times:    []time.Time{base.Add(time.Hour), base.Add(2 * time.Hour), base.Add(3 * time.Hour)},
			prev:     base,
			minStart: base.Add(-24 * time.Hour),
			wantLen:  3,
		},
		{
			name:     "times before prev are skipped",
			times:    []time.Time{base.Add(-time.Hour), base.Add(time.Hour)},
			prev:     base,
			minStart: base.Add(-24 * time.Hour),
			wantLen:  1,
		},
		{
			name:     "minStart constrains window start",
			times:    []time.Time{base.Add(2 * time.Hour)},
			prev:     base.Add(-time.Hour),
			minStart: base,
			wantLen:  1,
		},
		{
			name:     "equal prev and time produces no window",
			times:    []time.Time{base},
			prev:     base,
			minStart: base.Add(-24 * time.Hour),
			wantLen:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			windows := buildWindowsFromScheduleTimes(tt.times, tt.prev, tt.minStart)

			if len(windows) != tt.wantLen {
				t.Errorf("buildWindowsFromScheduleTimes() returned %d windows, want %d", len(windows), tt.wantLen)
			}
		})
	}
}

func TestBuildWindowsFromScheduleTimesContent(t *testing.T) {
	base := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)

	times := []time.Time{base.Add(time.Hour), base.Add(2 * time.Hour)}

	prev := base

	minStart := base.Add(-24 * time.Hour)

	windows := buildWindowsFromScheduleTimes(times, prev, minStart)

	if len(windows) != 2 {
		t.Fatalf("expected 2 windows, got %d", len(windows))
	}

	// First window: prev -> times[0]

	if !windows[0].start.Equal(base) {
		t.Errorf("first window start = %v, want %v", windows[0].start, base)
	}

	if !windows[0].end.Equal(base.Add(time.Hour)) {
		t.Errorf("first window end = %v, want %v", windows[0].end, base.Add(time.Hour))
	}

	// Second window: times[0] -> times[1]

	if !windows[1].start.Equal(base.Add(time.Hour)) {
		t.Errorf("second window start = %v, want %v", windows[1].start, base.Add(time.Hour))
	}

	if !windows[1].end.Equal(base.Add(2 * time.Hour)) {
		t.Errorf("second window end = %v, want %v", windows[1].end, base.Add(2*time.Hour))
	}
}

func TestClampThreshold(t *testing.T) {
	tests := []struct {
		name   string
		value  float32
		minVal float32
		maxVal float32
		want   float32
	}{
		{name: "within range", value: 0.5, minVal: 0.0, maxVal: 1.0, want: 0.5},
		{name: "at min", value: 0.0, minVal: 0.0, maxVal: 1.0, want: 0.0},
		{name: "at max", value: 1.0, minVal: 0.0, maxVal: 1.0, want: 1.0},
		{name: "below min", value: -0.5, minVal: 0.0, maxVal: 1.0, want: 0.0},
		{name: "above max", value: 1.5, minVal: 0.0, maxVal: 1.0, want: 1.0},
		{name: "typical threshold range", value: 0.3, minVal: 0.1, maxVal: 0.9, want: 0.3},
		{name: "small values", value: 0.05, minVal: 0.1, maxVal: 0.9, want: 0.1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := clampThreshold(tt.value, tt.minVal, tt.maxVal); got != tt.want {
				t.Errorf(testErrClampThreshold, tt.value, tt.minVal, tt.maxVal, got, tt.want)
			}
		})
	}
}

func TestCollectClusterSummaries(t *testing.T) {
	tests := []struct {
		name     string
		clusters []db.ClusterWithItems
		maxItems int
		wantLen  int
	}{
		{
			name:     "empty clusters",
			clusters: []db.ClusterWithItems{},
			maxItems: 10,
			wantLen:  0,
		},
		{
			name: "single cluster with topic",
			clusters: []db.ClusterWithItems{
				{Topic: "Tech", Items: []db.Item{{Summary: testSummaryText}}},
			},
			maxItems: 10,
			wantLen:  1,
		},
		{
			name: "multiple clusters takes first item each",
			clusters: []db.ClusterWithItems{
				{Topic: "Tech", Items: []db.Item{{Summary: "Summary 1"}, {Summary: "Summary 2"}}},
				{Topic: "Finance", Items: []db.Item{{Summary: "Summary 3"}}},
			},
			maxItems: 10,
			wantLen:  2, // Only first item from each cluster
		},
		{
			name: "respects maxItems limit",
			clusters: []db.ClusterWithItems{
				{Topic: "A", Items: []db.Item{{Summary: "S1"}}},
				{Topic: "B", Items: []db.Item{{Summary: "S2"}}},
				{Topic: "C", Items: []db.Item{{Summary: "S3"}}},
				{Topic: "D", Items: []db.Item{{Summary: "S4"}}},
			},
			maxItems: 2,
			wantLen:  2,
		},
		{
			name: "skips clusters without topic",
			clusters: []db.ClusterWithItems{
				{Topic: "", Items: []db.Item{{Summary: "No topic"}}},
				{Topic: "Tech", Items: []db.Item{{Summary: "With topic"}}},
			},
			maxItems: 10,
			wantLen:  1,
		},
		{
			name: "skips clusters without items",
			clusters: []db.ClusterWithItems{
				{Topic: "Tech", Items: []db.Item{}},
				{Topic: "Finance", Items: []db.Item{{Summary: "Has item"}}},
			},
			maxItems: 10,
			wantLen:  1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := collectClusterSummaries(tt.clusters, tt.maxItems)

			if len(got) != tt.wantLen {
				t.Errorf("collectClusterSummaries() returned %d summaries, want %d", len(got), tt.wantLen)
			}
		})
	}
}

func TestAppendItemSummaries(t *testing.T) {
	tests := []struct {
		name     string
		existing []string
		items    []db.Item
		maxItems int
		wantLen  int
	}{
		{
			name:     "empty items",
			existing: []string{},
			items:    []db.Item{},
			maxItems: 10,
			wantLen:  0,
		},
		{
			name:     "append to empty",
			existing: []string{},
			items:    []db.Item{{Summary: "New item"}},
			maxItems: 10,
			wantLen:  1,
		},
		{
			name:     "append to existing",
			existing: []string{"Existing"},
			items:    []db.Item{{Summary: "New item"}},
			maxItems: 10,
			wantLen:  2,
		},
		{
			name:     "respects max limit",
			existing: []string{"One", "Two"},
			items:    []db.Item{{Summary: "Three"}, {Summary: "Four"}},
			maxItems: 3,
			wantLen:  3,
		},
		{
			name:     "already at max",
			existing: []string{"One", "Two", "Three"},
			items:    []db.Item{{Summary: "Four"}},
			maxItems: 3,
			wantLen:  3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := appendItemSummaries(tt.existing, tt.items, tt.maxItems)

			if len(got) != tt.wantLen {
				t.Errorf("appendItemSummaries() returned %d summaries, want %d", len(got), tt.wantLen)
			}
		})
	}
}

func TestCountDistinctTopicsEdgeCases(t *testing.T) {
	t.Run("case insensitivity", func(t *testing.T) {
		items := []db.Item{
			{Topic: "Technology"},
			{Topic: "technology"},
			{Topic: "TECHNOLOGY"},
		}
		// These are treated as the same topic (case insensitive)

		got := countDistinctTopics(items)

		if got != 1 {
			t.Errorf("countDistinctTopics() = %d, want 1 (case insensitive)", got)
		}
	})

	t.Run("whitespace only topics", func(t *testing.T) {
		items := []db.Item{
			{Topic: " "},
			{Topic: "  "},
			{Topic: "\t"},
		}
		// Whitespace-only topics are treated as empty/not counted

		got := countDistinctTopics(items)

		if got != 0 {
			t.Errorf("countDistinctTopics() = %d, want 0 for whitespace topics", got)
		}
	})

	t.Run("mixed valid and whitespace topics", func(t *testing.T) {
		items := []db.Item{
			{Topic: "Technology"},
			{Topic: ""},
			{Topic: "Finance"},
			{Topic: " "},
		}

		got := countDistinctTopics(items)

		if got != 2 {
			t.Errorf("countDistinctTopics() = %d, want 2", got)
		}
	})
}

func TestGetImportancePrefixEdgeCases(t *testing.T) {
	tests := []struct {
		score float32
		want  string
	}{
		{1.0, EmojiBreaking},
		{0.8, EmojiBreaking},
		{0.79, EmojiNotable},
		{0.6, EmojiNotable},
		{0.59, EmojiStandard},
		{0.4, EmojiStandard},
		{0.39, EmojiBullet},
		{0.0, EmojiBullet},
		{-0.1, EmojiBullet},
	}

	for _, tt := range tests {
		got := getImportancePrefix(tt.score)

		if got != tt.want {
			t.Errorf("getImportancePrefix(%v) = %s, want %s", tt.score, got, tt.want)
		}
	}
}

func TestGroupItemsBySummaryEdgeCases(t *testing.T) {
	t.Run("multiple duplicates", func(t *testing.T) {
		items := []db.Item{
			{ID: "1", Summary: "Same", ImportanceScore: 0.3},
			{ID: "2", Summary: "Same", ImportanceScore: 0.5},
			{ID: "3", Summary: "Same", ImportanceScore: 0.8},
			{ID: "4", Summary: "Same", ImportanceScore: 0.2},
		}

		groups := groupItemsBySummary(items, make(map[string]bool))

		if len(groups) != 1 {
			t.Fatalf(testErrExpected1Group, len(groups))
		}

		if len(groups[0].items) != 4 {
			t.Errorf("expected 4 items in group, got %d", len(groups[0].items))
		}
		// Should have the highest importance score

		if groups[0].importanceScore != testSimilarityThresholdDefault {
			t.Errorf("expected importance 0.8, got %f", groups[0].importanceScore)
		}
	})

	t.Run("all seen summaries filtered", func(t *testing.T) {
		items := []db.Item{
			{ID: "1", Summary: "A"},
			{ID: "2", Summary: "B"},
		}

		seen := map[string]bool{"A": true, "B": true}

		groups := groupItemsBySummary(items, seen)

		if len(groups) != 0 {
			t.Errorf(testErrExpected0Groups, len(groups))
		}
	})

	t.Run("preserves order of first occurrence", func(t *testing.T) {
		items := []db.Item{
			{ID: "1", Summary: testSummaryFirst},
			{ID: "2", Summary: "Second"},
			{ID: "3", Summary: "Third"},
		}

		groups := groupItemsBySummary(items, make(map[string]bool))

		if len(groups) != 3 {
			t.Fatalf("expected 3 groups, got %d", len(groups))
		}

		if groups[0].summary != testSummaryFirst {
			t.Errorf("expected first summary 'First', got %q", groups[0].summary)
		}

		if groups[2].summary != "Third" {
			t.Errorf("expected third summary 'Third', got %q", groups[2].summary)
		}
	})
}

//nolint:gocyclo // table-driven test with many cases
func TestCategorizeItemsEdgeCases(t *testing.T) {
	t.Run(testNameEmptyItems, func(t *testing.T) {
		breaking, notable, also := categorizeItems([]db.Item{})

		if len(breaking.items) != 0 || len(notable.items) != 0 || len(also.items) != 0 {
			t.Error(testErrEmptyCategories)
		}
	})

	t.Run("all breaking", func(t *testing.T) {
		items := []db.Item{
			{ID: "1", ImportanceScore: 0.9},
			{ID: "2", ImportanceScore: 0.85},
			{ID: "3", ImportanceScore: 0.95},
		}

		breaking, notable, also := categorizeItems(items)

		if len(breaking.items) != 3 {
			t.Errorf("expected 3 breaking items, got %d", len(breaking.items))
		}

		if len(notable.items) != 0 || len(also.items) != 0 {
			t.Error("expected no notable or also items")
		}
	})

	t.Run("all also", func(t *testing.T) {
		items := []db.Item{
			{ID: "1", ImportanceScore: 0.1},
			{ID: "2", ImportanceScore: 0.2},
			{ID: "3", ImportanceScore: 0.3},
		}

		breaking, notable, also := categorizeItems(items)

		if len(also.items) != 3 {
			t.Errorf(testErrExpected3AlsoItems, len(also.items))
		}

		if len(breaking.items) != 0 || len(notable.items) != 0 {
			t.Error("expected no breaking or notable items")
		}
	})

	t.Run("boundary values", func(t *testing.T) {
		items := []db.Item{
			{ID: "1", ImportanceScore: ImportanceScoreBreaking},        // exactly 0.8 = breaking
			{ID: "2", ImportanceScore: ImportanceScoreNotable},         // exactly 0.6 = notable
			{ID: "3", ImportanceScore: ImportanceScoreStandard},        // exactly 0.4 = also
			{ID: "4", ImportanceScore: ImportanceScoreBreaking - 0.01}, // 0.79 = notable
		}

		breaking, notable, also := categorizeItems(items)

		if len(breaking.items) != 1 {
			t.Errorf(testErrExpected1BreakingItem, len(breaking.items))
		}

		if len(notable.items) != 2 {
			t.Errorf("expected 2 notable items, got %d", len(notable.items))
		}

		if len(also.items) != 1 {
			t.Errorf(testErrExpected1AlsoItem, len(also.items))
		}
	})
}

func TestCategorizeClustersEdgeCases(t *testing.T) {
	t.Run("empty clusters", func(t *testing.T) {
		breaking, notable, also := categorizeClusters([]db.ClusterWithItems{})

		if len(breaking.clusters) != 0 || len(notable.clusters) != 0 || len(also.clusters) != 0 {
			t.Error(testErrEmptyCategories)
		}
	})

	t.Run("cluster with multiple items uses max importance", func(t *testing.T) {
		clusters := []db.ClusterWithItems{
			{
				Topic: "Tech",
				Items: []db.Item{
					{ImportanceScore: 0.5},
					{ImportanceScore: 0.9}, // Max
					{ImportanceScore: 0.3},
				},
			},
		}

		breaking, notable, also := categorizeClusters(clusters)

		if len(breaking.clusters) != 1 {
			t.Errorf(testErrExpected1Breaking, len(breaking.clusters))
		}

		if len(notable.clusters) != 0 || len(also.clusters) != 0 {
			t.Error("expected no notable or also clusters")
		}
	})

	t.Run("cluster with empty items", func(t *testing.T) {
		clusters := []db.ClusterWithItems{
			{Topic: "Empty", Items: []db.Item{}},
		}

		_, _, also := categorizeClusters(clusters)

		// Empty items = max importance 0, goes to also

		if len(also.clusters) != 1 {
			t.Errorf(testErrExpected1AlsoCluster, len(also.clusters))
		}
	})
}

func TestClusterMaxImportanceEdgeCases(t *testing.T) {
	t.Run("single item", func(t *testing.T) {
		c := db.ClusterWithItems{Items: []db.Item{{ImportanceScore: testReliability075}}}

		if got := clusterMaxImportance(c); got != testReliability075 {
			t.Errorf(testErrClusterMaxImportance, got, testReliability075)
		}
	})

	t.Run("negative importance scores", func(t *testing.T) {
		c := db.ClusterWithItems{Items: []db.Item{
			{ImportanceScore: -0.1},
			{ImportanceScore: -0.5},
		}}
		// Zero is the floor since we start at 0

		if got := clusterMaxImportance(c); got != 0 {
			t.Errorf("clusterMaxImportance() = %v, want 0", got)
		}
	})

	t.Run("all same importance", func(t *testing.T) {
		c := db.ClusterWithItems{Items: []db.Item{
			{ImportanceScore: 0.5},
			{ImportanceScore: 0.5},
			{ImportanceScore: 0.5},
		}}

		if got := clusterMaxImportance(c); got != testHalfValue {
			t.Errorf("clusterMaxImportance() = %v, want 0.5", got)
		}
	})
}

func TestFormatItemLabelEdgeCases(t *testing.T) {
	t.Run("whitespace channel", func(t *testing.T) {
		// SourceChannel is checked first, if empty string, it falls through
		item := db.Item{SourceChannel: "", SourceChannelTitle: testSourceTitle}

		got := formatItemLabel(item)

		if got != testSourceTitle {
			t.Errorf(testErrFormatItemLabel, got, testSourceTitle)
		}
	})

	t.Run("both empty", func(t *testing.T) {
		item := db.Item{}

		got := formatItemLabel(item)

		if got != DefaultSourceLabel {
			t.Errorf(testErrFormatItemLabel, got, DefaultSourceLabel)
		}
	})
}

func TestExtractDigestHeaderEdgeCases(t *testing.T) {
	t.Run("only items no header", func(t *testing.T) {
		text := "🔴 First breaking item"

		got := extractDigestHeader(text)

		if got != "" {
			t.Errorf("extractDigestHeader() = %q, want empty", got)
		}
	})

	t.Run("multiline header with separator", func(t *testing.T) {
		text := "━━━━━━\nDigest Title\n12:00 - 13:00\nStats line\n🔴 Breaking"

		got := extractDigestHeader(text)

		if !strings.Contains(got, "Digest Title") {
			t.Errorf("extractDigestHeader() should contain 'Digest Title', got %q", got)
		}

		if !strings.Contains(got, "Stats line") {
			t.Errorf("extractDigestHeader() should contain 'Stats line', got %q", got)
		}
	})

	t.Run("pipe border stops extraction", func(t *testing.T) {
		text := "Header\n" + testTopicBorder + " " + testTopicContent

		got := extractDigestHeader(text)

		if strings.Contains(got, testTopicBorder) {
			t.Errorf("extractDigestHeader() should not contain pipe border, got %q", got)
		}
	})
}

func TestBuildWindowsFromScheduleTimesMinStartConstraint(t *testing.T) {
	base := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)

	t.Run("minStart after prev constrains window start", func(t *testing.T) {
		times := []time.Time{base.Add(2 * time.Hour)}

		prev := base.Add(-2 * time.Hour) // 08:00

		minStart := base // 10:00 - after prev

		windows := buildWindowsFromScheduleTimes(times, prev, minStart)

		if len(windows) != 1 {
			t.Fatalf("expected 1 window, got %d", len(windows))
		}
		// Window start should be constrained to minStart

		if !windows[0].start.Equal(base) {
			t.Errorf("window start = %v, want %v (minStart)", windows[0].start, base)
		}
	})

	t.Run("unsorted times handled correctly", func(t *testing.T) {
		// Times that are not after prev get skipped
		times := []time.Time{
			base.Add(-time.Hour), // before prev
			base.Add(time.Hour),  // after prev
		}

		prev := base

		minStart := base.Add(-2 * time.Hour)

		windows := buildWindowsFromScheduleTimes(times, prev, minStart)

		if len(windows) != 1 {
			t.Errorf("expected 1 window (skipping time before prev), got %d", len(windows))
		}
	})
}

func TestCollectClusterSummariesEdgeCases(t *testing.T) {
	t.Run("nil clusters", func(t *testing.T) {
		got := collectClusterSummaries(nil, 5)

		if len(got) != 0 {
			t.Errorf("expected 0 summaries for nil clusters, got %d", len(got))
		}
	})

	t.Run("clusters with empty items slice", func(t *testing.T) {
		clusters := []db.ClusterWithItems{
			{Topic: "Tech", Items: []db.Item{}},
			{Topic: "Finance", Items: []db.Item{{Summary: "Has content"}}},
		}

		got := collectClusterSummaries(clusters, 5)
		// First cluster should be skipped (empty items)

		if len(got) != 1 {
			t.Errorf(testErrExpected1Summary, len(got))
		}
	})

	t.Run("zero maxItems returns empty", func(t *testing.T) {
		clusters := []db.ClusterWithItems{
			{Topic: "Tech", Items: []db.Item{{Summary: "Content"}}},
		}

		got := collectClusterSummaries(clusters, 0)

		if len(got) != 0 {
			t.Errorf("expected 0 summaries for maxItems=0, got %d", len(got))
		}
	})
}

func TestAppendItemSummariesEdgeCases(t *testing.T) {
	t.Run("nil items", func(t *testing.T) {
		got := appendItemSummaries([]string{"existing"}, nil, 5)

		if len(got) != 1 {
			t.Errorf(testErrExpected1Summary, len(got))
		}
	})

	t.Run("items with empty summaries skipped", func(t *testing.T) {
		items := []db.Item{
			{Summary: ""},
			{Summary: "Valid"},
			{Summary: ""},
		}

		got := appendItemSummaries([]string{}, items, 5)

		if len(got) != 1 {
			t.Errorf("expected 1 summary (empty ones skipped), got %d", len(got))
		}
	})
}

func TestExtractTopicsFromDigestEdgeCases(t *testing.T) {
	t.Run("duplicate topics across items and clusters", func(t *testing.T) {
		items := []db.Item{
			{Topic: "Technology"},
			{Topic: "Technology"},
		}

		clusters := []db.ClusterWithItems{
			{Topic: "Technology"},
			{Topic: "Finance"},
		}

		got := extractTopicsFromDigest(items, clusters)

		if len(got) != 2 {
			t.Errorf("expected 2 unique topics, got %d", len(got))
		}
	})

	t.Run("all empty topics", func(t *testing.T) {
		items := []db.Item{{Topic: ""}, {Topic: ""}}

		clusters := []db.ClusterWithItems{{Topic: ""}}

		got := extractTopicsFromDigest(items, clusters)

		if len(got) != 0 {
			t.Errorf("expected 0 topics for all empty, got %d", len(got))
		}
	})
}

func TestCountItemsWithMediaEdgeCases(t *testing.T) {
	t.Run("nil slice", func(t *testing.T) {
		if got := countItemsWithMedia(nil); got != 0 {
			t.Errorf("countItemsWithMedia(nil) = %d, want 0", got)
		}
	})
}

func TestTruncateForLogEdgeCases(t *testing.T) {
	t.Run("exactly at limit", func(t *testing.T) {
		input := strings.Repeat("x", LogTruncateLength)

		got := truncateForLog(input)

		if got != input {
			t.Errorf("expected exact input, got different string")
		}
	})

	t.Run("one over limit", func(t *testing.T) {
		input := strings.Repeat("x", LogTruncateLength+1)

		got := truncateForLog(input)

		expected := strings.Repeat("x", LogTruncateLength) + "..."

		if got != expected {
			t.Errorf(testErrTruncateForLog, got, expected)
		}
	})

	t.Run("unicode characters", func(t *testing.T) {
		// Note: This tests byte truncation, not rune truncation
		input := strings.Repeat("x", LogTruncateLength-1) + "test"

		got := truncateForLog(input)

		if !strings.HasSuffix(got, "...") {
			t.Errorf("expected truncation with ..., got %q", got)
		}
	})
}

func TestFormatSummaryLineMultipleTopics(t *testing.T) {
	t.Run("topic with known emoji", func(t *testing.T) {
		group := summaryGroup{
			items: []db.Item{{Topic: "Technology"}},
		}

		got := formatSummaryLine(group, true, testPrefixLabel, testSummaryLabel)

		if !strings.Contains(got, testTopicTechnology) {
			t.Errorf("formatSummaryLine() should contain topic, got %q", got)
		}
	})

	t.Run("empty topic in items", func(t *testing.T) {
		group := summaryGroup{
			items: []db.Item{{Topic: ""}},
		}

		got := formatSummaryLine(group, true, testPrefixLabel, testSummaryLabel)
		// Should not include topic formatting when topic is empty

		if strings.Contains(got, "<b>") {
			t.Errorf("formatSummaryLine() should not have bold topic when empty, got %q", got)
		}
	})
}

func TestScheduleWindowStruct(t *testing.T) {
	now := time.Now()

	w := scheduleWindow{
		start: now,
		end:   now.Add(time.Hour),
	}

	if w.start.IsZero() {
		t.Error(testStartNotZero)
	}

	if !w.end.After(w.start) {
		t.Error("end should be after start")
	}
}

func TestDigestProcessConfigStruct(t *testing.T) {
	cfg := digestProcessConfig{
		window:                      time.Hour,
		targetChatID:                12345,
		importanceThreshold:         0.5,
		catchupWindow:               24 * time.Hour,
		anomalyNotificationsEnabled: true,
	}

	if cfg.window != time.Hour {
		t.Errorf("window = %v, want 1h", cfg.window)
	}

	if cfg.targetChatID != 12345 {
		t.Errorf("targetChatID = %d, want 12345", cfg.targetChatID)
	}

	if cfg.importanceThreshold != testHalfValue {
		t.Errorf("importanceThreshold = %v, want 0.5", cfg.importanceThreshold)
	}

	if cfg.catchupWindow != 24*time.Hour {
		t.Errorf("catchupWindow = %v, want 24h", cfg.catchupWindow)
	}

	if !cfg.anomalyNotificationsEnabled {
		t.Error("anomalyNotificationsEnabled should be true")
	}
}

func TestAnomalyInfoStruct(t *testing.T) {
	now := time.Now()

	a := anomalyInfo{
		start:       now,
		end:         now.Add(time.Hour),
		totalItems:  100,
		readyItems:  50,
		threshold:   0.5,
		isBacklog:   true,
		backlogSize: 200,
	}

	if a.start.IsZero() {
		t.Error(testStartNotZero)
	}

	if a.totalItems != 100 {
		t.Errorf("totalItems = %d, want 100", a.totalItems)
	}

	if a.readyItems != 50 {
		t.Errorf("readyItems = %d, want 50", a.readyItems)
	}

	if a.threshold != testHalfValue {
		t.Errorf("threshold = %v, want %v", a.threshold, testHalfValue)
	}

	if !a.isBacklog {
		t.Error("isBacklog should be true")
	}

	if a.backlogSize != 200 {
		t.Errorf("backlogSize = %d, want 200", a.backlogSize)
	}
}

func TestRichDigestItemStruct(t *testing.T) {
	item := RichDigestItem{
		Summary:    testSummaryText,
		Topic:      testTopicTechnology,
		Importance: ImportanceScoreBreaking,
		Channel:    "testchannel",
		ChannelID:  12345,
		MsgID:      100,
		MediaData:  []byte{1, 2, 3},
	}

	if item.Summary != testSummaryText {
		t.Errorf("Summary = %q, want %q", item.Summary, testSummaryText)
	}

	if item.Topic != testTopicTechnology {
		t.Errorf("Topic = %q, want %q", item.Topic, testTopicTechnology)
	}

	if item.Importance != ImportanceScoreBreaking {
		t.Errorf("Importance = %v, want %v", item.Importance, ImportanceScoreBreaking)
	}

	if item.Channel != "testchannel" {
		t.Errorf("Channel = %q, want 'testchannel'", item.Channel)
	}

	if item.ChannelID != 12345 {
		t.Errorf("ChannelID = %d, want 12345", item.ChannelID)
	}

	if item.MsgID != 100 {
		t.Errorf("MsgID = %d, want 100", item.MsgID)
	}

	if len(item.MediaData) != 3 {
		t.Errorf("MediaData length = %d, want 3", len(item.MediaData))
	}
}

func TestRichDigestContentStruct(t *testing.T) {
	content := RichDigestContent{
		Header:   "Test Header",
		DigestID: "abc123",
		Items: []RichDigestItem{
			{Summary: "Item 1"},
			{Summary: "Item 2"},
		},
	}

	if content.Header != "Test Header" {
		t.Errorf("Header = %q, want 'Test Header'", content.Header)
	}

	if content.DigestID != "abc123" {
		t.Errorf("DigestID = %q, want 'abc123'", content.DigestID)
	}

	if len(content.Items) != 2 {
		t.Errorf(testErrItemsLength2, len(content.Items))
	}
}

func TestClusterGroupStruct(t *testing.T) {
	group := clusterGroup{
		clusters: []db.ClusterWithItems{
			{Topic: "Tech"},
		},
		items: []db.Item{
			{ID: "1"},
		},
	}

	if len(group.clusters) != 1 {
		t.Errorf("clusters length = %d, want 1", len(group.clusters))
	}

	if len(group.items) != 1 {
		t.Errorf("items length = %d, want 1", len(group.items))
	}
}

func TestFormatSummaryLineAllTopics(t *testing.T) {
	// Test all known topics have emojis
	topics := []string{
		"Technology", "Finance", "Politics", "Sports",
		"Entertainment", "Science", "Health", "Business",
		"World News", "Local News", "Culture", "Education", "Humor",
	}

	for _, topic := range topics {
		t.Run(topic, func(t *testing.T) {
			group := summaryGroup{
				items: []db.Item{{Topic: topic}},
			}

			got := formatSummaryLine(group, true, EmojiStandard, "test summary")

			// Should contain the topic name

			if !strings.Contains(got, topic) {
				t.Errorf("formatSummaryLine() for %q should contain topic name, got %q", topic, got)
			}
		})
	}
}

func TestExtractDigestHeaderComplexCases(t *testing.T) {
	t.Run("header with multiple separators", func(t *testing.T) {
		text := "━━━━━━━━━━━━━━━━━━━━━━\n" + testDigestForHeader + " 10:00 - 11:00\n━━━━━━━━━━━━━━━━━━━━━━\n📊 Stats\n🔴 Breaking news"

		got := extractDigestHeader(text)

		if !strings.Contains(got, testDigestForHeader) {
			t.Errorf("should contain header, got %q", got)
		}

		if strings.Contains(got, EmojiBreaking) {
			t.Errorf("should not contain breaking marker, got %q", got)
		}
	})

	t.Run("header with nested content stops at border", func(t *testing.T) {
		text := "Header line 1\nHeader line 2\n┌─────────────────────\n" + testTopicBorder + " " + testTopicContent

		got := extractDigestHeader(text)

		if !strings.Contains(got, "Header line 1") {
			t.Errorf("should contain first line, got %q", got)
		}

		if strings.Contains(got, testTopicContent) {
			t.Errorf(testErrNoTopicContent, got)
		}
	})
}

func TestBuildWindowsFromScheduleTimesEdgeCases(t *testing.T) {
	base := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)

	t.Run("all times before prev", func(t *testing.T) {
		times := []time.Time{
			base.Add(-3 * time.Hour),
			base.Add(-2 * time.Hour),
			base.Add(-1 * time.Hour),
		}

		prev := base

		minStart := base.Add(-24 * time.Hour)

		windows := buildWindowsFromScheduleTimes(times, prev, minStart)

		if len(windows) != 0 {
			t.Errorf("expected 0 windows when all times before prev, got %d", len(windows))
		}
	})

	t.Run("minStart after all times", func(t *testing.T) {
		times := []time.Time{
			base.Add(1 * time.Hour),
			base.Add(2 * time.Hour),
		}

		prev := base

		minStart := base.Add(3 * time.Hour) // After all times

		windows := buildWindowsFromScheduleTimes(times, prev, minStart)
		// No valid windows since minStart is after all scheduled times
		// But the logic adds windows where start < end, so it depends on implementation
		// Based on code: start = prev if prev >= minStart, else minStart
		// For first time (1h), start = 3h, end = 1h => skipped since start >= end
		// For second time (2h), start = 1h, end = 2h => would be added but wait...
		// Actually prev is updated to 1h after first iteration, but since 1h is not after base, it's skipped
		// So no windows should be created

		if len(windows) != 0 {
			t.Errorf("expected 0 windows, got %d", len(windows))
		}
	})

	t.Run("consecutive windows", func(t *testing.T) {
		times := []time.Time{
			base.Add(1 * time.Hour),
			base.Add(2 * time.Hour),
			base.Add(3 * time.Hour),
		}

		prev := base

		minStart := base.Add(-1 * time.Hour)

		windows := buildWindowsFromScheduleTimes(times, prev, minStart)

		if len(windows) != 3 {
			t.Errorf("expected 3 consecutive windows, got %d", len(windows))
		}

		// Verify windows are consecutive

		for i := 1; i < len(windows); i++ {
			if !windows[i].start.Equal(windows[i-1].end) {
				t.Errorf("window %d start should equal window %d end", i, i-1)
			}
		}
	})
}

func TestCollectClusterSummariesContentVerification(t *testing.T) {
	clusters := []db.ClusterWithItems{
		{Topic: "Tech", Items: []db.Item{{Summary: "First tech item"}, {Summary: "Second tech item"}}},
		{Topic: "Finance", Items: []db.Item{{Summary: "Finance news"}}},
	}

	summaries := collectClusterSummaries(clusters, 10)

	if len(summaries) != 2 {
		t.Fatalf(testErrExpected2Summaries, len(summaries))
	}

	// Should contain first item summary from each cluster

	if summaries[0] != "First tech item" {
		t.Errorf("first summary = %q, want 'First tech item'", summaries[0])
	}

	if summaries[1] != "Finance news" {
		t.Errorf("second summary = %q, want 'Finance news'", summaries[1])
	}
}

func TestAppendItemSummariesContentVerification(t *testing.T) {
	existing := []string{"Existing summary"}

	items := []db.Item{
		{Summary: "New item 1"},
		{Summary: "New item 2"},
	}

	result := appendItemSummaries(existing, items, 10)

	if len(result) != 3 {
		t.Fatalf(testErrExpected3Summaries, len(result))
	}

	if result[0] != "Existing summary" {
		t.Errorf("first should be existing, got %q", result[0])
	}

	if result[1] != "New item 1" {
		t.Errorf("second should be 'New item 1', got %q", result[1])
	}

	if result[2] != "New item 2" {
		t.Errorf("third should be 'New item 2', got %q", result[2])
	}
}

func TestDigestRenderContextCategorizeWithMixedClusters(t *testing.T) {
	rc := &digestRenderContext{
		settings: digestSettings{topicsEnabled: true},
		clusters: []db.ClusterWithItems{
			{Topic: "Breaking", Items: []db.Item{
				{ImportanceScore: 0.85},
				{ImportanceScore: 0.6}, // Max in cluster is 0.85
			}},
			{Topic: "Notable", Items: []db.Item{
				{ImportanceScore: 0.65},
			}},
			{Topic: "Standard", Items: []db.Item{
				{ImportanceScore: 0.45},
			}},
		},
	}

	breaking, notable, also := rc.categorizeByImportance()

	if len(breaking.clusters) != 1 {
		t.Errorf(testErrExpected1Breaking, len(breaking.clusters))
	}

	if len(notable.clusters) != 1 {
		t.Errorf(testErrExpected1Notable, len(notable.clusters))
	}

	if len(also.clusters) != 1 {
		t.Errorf(testErrExpected1AlsoCluster, len(also.clusters))
	}
}

func TestCreateDigestEntriesWithComplexClusters(t *testing.T) {
	s := &Scheduler{}

	clusters := []db.ClusterWithItems{
		{
			Topic: "Multi-Source Story",
			Items: []db.Item{
				{Summary: "Item A", SourceChannel: "ch1", SourceMsgID: 1},
				{Summary: "Item B", SourceChannel: "ch2", SourceMsgID: 2},
				{Summary: "Item C", SourceChannel: "ch3", SourceMsgID: 3},
			},
		},
		{
			Topic: "Single Item",
			Items: []db.Item{
				{Summary: "Solo", SourceChannel: "solo", SourceMsgID: 100},
			},
		},
	}

	entries := s.createDigestEntries(nil, clusters)

	if len(entries) != 2 {
		t.Fatalf(testErrExpected2Entries, len(entries))
	}

	// First entry should have 3 sources

	if len(entries[0].Sources) != 3 {
		t.Errorf("first entry should have 3 sources, got %d", len(entries[0].Sources))
	}

	// Second entry should have 1 source

	if len(entries[1].Sources) != 1 {
		t.Errorf("second entry should have 1 source, got %d", len(entries[1].Sources))
	}

	// Verify body contains bullet points

	if !strings.Contains(entries[0].Body, "Item A") {
		t.Errorf("body should contain 'Item A', got %q", entries[0].Body)
	}
}

func TestFormatLinkHTMLEscaping(t *testing.T) {
	s := &Scheduler{}

	tests := []struct {
		name    string
		item    db.Item
		label   string
		wantNot string // Should NOT contain this (unescaped)
	}{
		{
			name:    "channel with special chars",
			item:    db.Item{SourceChannel: "test<>channel", SourceMsgID: 1},
			label:   "Test<>Label",
			wantNot: "<>",
		},
		{
			name:    "ampersand in channel",
			item:    db.Item{SourceChannel: "test&channel", SourceMsgID: 1},
			label:   "Test&Label",
			wantNot: "\"Test&Label\"", // Should be escaped
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := s.formatLink(tt.item, tt.label)
			// Should contain the escaped version, not raw HTML

			if strings.Contains(got, tt.wantNot) && !strings.Contains(got, "&amp;") && !strings.Contains(got, "&lt;") {
				t.Errorf("formatLink() should escape HTML chars, got %q", got)
			}
		})
	}
}

func TestDigestRenderContextBuildMetadataSectionSingleChannel(t *testing.T) {
	rc := &digestRenderContext{
		settings: digestSettings{topicsEnabled: false},
		items: []db.Item{
			{SourceChannel: "singleChannel"},
			{SourceChannel: "singleChannel"},
			{SourceChannel: "singleChannel"},
		},
	}

	var sb strings.Builder
	rc.buildMetadataSection(&sb)

	got := sb.String()

	if !strings.Contains(got, test3Items) {
		t.Errorf("should contain '3 items', got %q", got)
	}

	if !strings.Contains(got, "1 channel") {
		t.Errorf("should contain '1 channel', got %q", got)
	}
}

func TestGetImportancePrefixAllThresholds(t *testing.T) {
	// Test exact boundary values
	tests := []struct {
		score float32
		want  string
	}{
		// Breaking boundary
		{ImportanceScoreBreaking, EmojiBreaking},
		{ImportanceScoreBreaking + 0.1, EmojiBreaking},
		{ImportanceScoreBreaking - 0.01, EmojiNotable},

		// Notable boundary
		{ImportanceScoreNotable, EmojiNotable},
		{ImportanceScoreNotable + 0.1, EmojiNotable},
		{ImportanceScoreNotable - 0.01, EmojiStandard},

		// Standard boundary
		{ImportanceScoreStandard, EmojiStandard},
		{ImportanceScoreStandard + 0.1, EmojiStandard},
		{ImportanceScoreStandard - 0.01, EmojiBullet},

		// Edge cases
		{0.0, EmojiBullet},
		{1.0, EmojiBreaking},
	}

	for _, tt := range tests {
		got := getImportancePrefix(tt.score)

		if got != tt.want {
			t.Errorf("getImportancePrefix(%v) = %q, want %q", tt.score, got, tt.want)
		}
	}
}

func TestSchedulerNew(t *testing.T) {
	cfg := &config.Config{}

	s := New(cfg, nil, nil, nil, nil)

	require.NotNil(t, s, "New() returned nil")
	require.Equal(t, cfg, s.cfg, "config not set correctly")
}

func TestGetLockIDConsistency(t *testing.T) {
	s := &Scheduler{
		cfg: &config.Config{LeaderElectionLeaseName: "test-consistent-lease"},
	}

	// Get ID multiple times

	ids := make([]int64, 10)

	for i := range ids {
		ids[i] = s.getLockID()
	}

	// All should be the same

	for i := 1; i < len(ids); i++ {
		if ids[i] != ids[0] {
			t.Errorf("getLockID() not consistent: %d != %d", ids[i], ids[0])
		}
	}
}

func TestGetLockIDDifferentLeases(t *testing.T) {
	leases := []string{"lease-a", "lease-b", "lease-c", "production", "staging"}

	ids := make(map[int64]string)

	for _, lease := range leases {
		s := &Scheduler{cfg: &config.Config{LeaderElectionLeaseName: lease}}

		id := s.getLockID()

		if existing, ok := ids[id]; ok {
			t.Errorf("collision: %q and %q both produce ID %d", lease, existing, id)
		}

		ids[id] = lease
	}
}

func TestDigestRenderContextGetHeader(t *testing.T) {
	tests := []struct {
		name     string
		language string
		want     string
	}{
		{name: "english default", language: "en", want: testDigestForHeader},
		{name: "russian", language: "ru", want: "Дайджест за"},
		{name: "german", language: "de", want: "Digest für"},
		{name: "spanish", language: "es", want: "Resumen para"},
		{name: "french", language: "fr", want: "Résumé pour"},
		{name: "italian", language: "it", want: "Riassunto per"},
		{name: "uppercase language", language: "RU", want: "Дайджест за"},
		{name: "unknown language", language: "xx", want: testDigestForHeader},
		{name: "empty language", language: "", want: testDigestForHeader},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rc := &digestRenderContext{
				settings: digestSettings{digestLanguage: tt.language},
			}

			got := rc.getHeader()

			if got != tt.want {
				t.Errorf("getHeader() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDigestRenderContextGetSectionTitles(t *testing.T) {
	tests := []struct {
		name         string
		language     string
		wantBreaking string
		wantNotable  string
		wantAlso     string
	}{
		{name: "english default", language: "en", wantBreaking: "Breaking", wantNotable: "Notable", wantAlso: "Also"},
		{name: "russian", language: "ru", wantBreaking: "Срочно", wantNotable: "Важное", wantAlso: "Остальное"},
		{name: "german", language: "de", wantBreaking: "Eilmeldung", wantNotable: "Wichtig", wantAlso: "Weiteres"},
		{name: "spanish", language: "es", wantBreaking: "Última hora", wantNotable: "Destacado", wantAlso: "Otros"},
		{name: "french", language: "fr", wantBreaking: "Flash info", wantNotable: "Important", wantAlso: "Autres"},
		{name: "italian", language: "it", wantBreaking: "Ultime notizie", wantNotable: "Importante", wantAlso: "Altro"},
		{name: "uppercase", language: "FR", wantBreaking: "Flash info", wantNotable: "Important", wantAlso: "Autres"},
		{name: "unknown", language: "zh", wantBreaking: "Breaking", wantNotable: "Notable", wantAlso: "Also"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rc := &digestRenderContext{
				settings: digestSettings{digestLanguage: tt.language},
			}

			breaking, notable, also := rc.getSectionTitles()

			if breaking != tt.wantBreaking {
				t.Errorf("getSectionTitles() breaking = %q, want %q", breaking, tt.wantBreaking)
			}

			if notable != tt.wantNotable {
				t.Errorf("getSectionTitles() notable = %q, want %q", notable, tt.wantNotable)
			}

			if also != tt.wantAlso {
				t.Errorf("getSectionTitles() also = %q, want %q", also, tt.wantAlso)
			}
		})
	}
}

func TestDigestRenderContextBuildHeaderSection(t *testing.T) {
	base := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)

	rc := &digestRenderContext{
		settings:     digestSettings{digestLanguage: "en"},
		displayStart: base,
		displayEnd:   base.Add(time.Hour),
	}

	var sb strings.Builder
	rc.buildHeaderSection(&sb)

	got := sb.String()

	if !strings.Contains(got, DigestSeparatorLine) {
		t.Error("buildHeaderSection() should contain separator line")
	}

	if !strings.Contains(got, testTime1000) {
		t.Errorf("buildHeaderSection() should contain start time, got %q", got)
	}

	if !strings.Contains(got, testTime1100) {
		t.Errorf("buildHeaderSection() should contain end time, got %q", got)
	}

	if !strings.Contains(got, testDigestForHeader) {
		t.Errorf("buildHeaderSection() should contain header text, got %q", got)
	}
}

func TestDigestRenderContextBuildMetadataSection(t *testing.T) {
	rc := &digestRenderContext{
		settings: digestSettings{topicsEnabled: true},
		items: []db.Item{
			{SourceChannel: "channel1", Topic: "Tech"},
			{SourceChannel: "channel1", Topic: "Tech"},
			{SourceChannel: "channel2", Topic: "Finance"},
		},
		clusters: []db.ClusterWithItems{},
	}

	var sb strings.Builder
	rc.buildMetadataSection(&sb)

	got := sb.String()

	// Should contain item count

	if !strings.Contains(got, test3Items) {
		t.Errorf("buildMetadataSection() should contain '3 items', got %q", got)
	}
	// Should contain channel count (2 unique)

	if !strings.Contains(got, "2 channels") {
		t.Errorf("buildMetadataSection() should contain '2 channels', got %q", got)
	}
}

func TestDigestRenderContextCategorizeByImportance(t *testing.T) {
	t.Run("with clusters enabled", func(t *testing.T) {
		rc := &digestRenderContext{
			settings: digestSettings{topicsEnabled: true},
			clusters: []db.ClusterWithItems{
				{Topic: "A", Items: []db.Item{{ImportanceScore: 0.9}}},
				{Topic: "B", Items: []db.Item{{ImportanceScore: 0.5}}},
			},
		}

		breaking, notable, also := rc.categorizeByImportance()

		if len(breaking.clusters) != 1 {
			t.Errorf(testErrExpected1Breaking, len(breaking.clusters))
		}

		if len(also.clusters) != 1 {
			t.Errorf(testErrExpected1AlsoCluster, len(also.clusters))
		}

		if len(notable.clusters) != 0 {
			t.Errorf("expected 0 notable clusters, got %d", len(notable.clusters))
		}
	})

	t.Run("with items when topics disabled", func(t *testing.T) {
		rc := &digestRenderContext{
			settings: digestSettings{topicsEnabled: false},
			items: []db.Item{
				{ID: "1", ImportanceScore: 0.85},
				{ID: "2", ImportanceScore: 0.65},
			},
		}

		breaking, notable, _ := rc.categorizeByImportance()

		if len(breaking.items) != 1 {
			t.Errorf(testErrExpected1BreakingItem, len(breaking.items))
		}

		if len(notable.items) != 1 {
			t.Errorf(testErrExpected1NotableItem, len(notable.items))
		}
	})

	t.Run("with items when clusters empty", func(t *testing.T) {
		rc := &digestRenderContext{
			settings: digestSettings{topicsEnabled: true},
			clusters: []db.ClusterWithItems{}, // Empty clusters
			items: []db.Item{
				{ID: "1", ImportanceScore: 0.85},
			},
		}

		breaking, _, _ := rc.categorizeByImportance()

		if len(breaking.items) != 1 {
			t.Errorf(testErrExpected1BreakingItem, len(breaking.items))
		}
	})
}

func TestCreateDigestEntries(t *testing.T) {
	s := &Scheduler{}

	t.Run("with clusters", func(t *testing.T) {
		clusters := []db.ClusterWithItems{
			{
				Topic: "Technology",
				Items: []db.Item{
					{Summary: "First item", SourceChannel: "ch1", SourceMsgID: 1},
					{Summary: "Second item", SourceChannel: "ch2", SourceMsgID: 2},
				},
			},
		}

		entries := s.createDigestEntries(nil, clusters)

		if len(entries) != 1 {
			t.Fatalf("expected 1 entry, got %d", len(entries))
		}

		if entries[0].Title != testTopicTechnology {
			t.Errorf("expected title 'Technology', got %q", entries[0].Title)
		}

		if len(entries[0].Sources) != 2 {
			t.Errorf("expected 2 sources, got %d", len(entries[0].Sources))
		}
	})

	t.Run("without clusters", func(t *testing.T) {
		items := []db.Item{
			{Summary: "Summary 1", SourceChannel: "ch1", SourceMsgID: 1},
			{Summary: "Summary 2", SourceChannel: "ch2", SourceMsgID: 2},
		}

		entries := s.createDigestEntries(items, nil)

		if len(entries) != 2 {
			t.Fatalf(testErrExpected2Entries, len(entries))
		}

		for i, entry := range entries {
			if len(entry.Sources) != 1 {
				t.Errorf("entry %d expected 1 source, got %d", i, len(entry.Sources))
			}
		}
	})

	t.Run("empty input", func(t *testing.T) {
		entries := s.createDigestEntries(nil, nil)

		if len(entries) != 0 {
			t.Errorf("expected 0 entries for empty input, got %d", len(entries))
		}
	})
}

func TestFormatLink(t *testing.T) {
	s := &Scheduler{}

	t.Run("public channel with username", func(t *testing.T) {
		item := db.Item{
			SourceChannel:   "testchannel",
			SourceMsgID:     123,
			SourceChannelID: 12345,
		}

		got := s.formatLink(item, testAtChannel)

		if !strings.Contains(got, "https://t.me/testchannel/123") {
			t.Errorf("formatLink() = %q, want to contain public channel link", got)
		}

		if !strings.Contains(got, testAtChannel) {
			t.Errorf("formatLink() = %q, want to contain label", got)
		}
	})

	t.Run("private channel without username", func(t *testing.T) {
		item := db.Item{
			SourceChannel:   "",
			SourceMsgID:     456,
			SourceChannelID: 987654,
		}

		got := s.formatLink(item, "Private Channel")

		if !strings.Contains(got, "https://t.me/c/987654/456") {
			t.Errorf("formatLink() = %q, want to contain private channel link", got)
		}
	})

	t.Run(testNameEmptyLabelChannel, func(t *testing.T) {
		item := db.Item{
			SourceChannel:   "fallback",
			SourceMsgID:     789,
			SourceChannelID: 111,
		}

		got := s.formatLink(item, "")

		if !strings.Contains(got, "fallback") {
			t.Errorf("formatLink() with empty label should use channel name, got %q", got)
		}
	})

	t.Run("empty label and channel uses title", func(t *testing.T) {
		item := db.Item{
			SourceChannel:      "",
			SourceChannelTitle: "My Title",
			SourceMsgID:        100,
			SourceChannelID:    222,
		}

		got := s.formatLink(item, "")

		if !strings.Contains(got, testMyTitle) {
			t.Errorf("formatLink() should use title when channel empty, got %q", got)
		}
	})

	t.Run("all empty uses default", func(t *testing.T) {
		item := db.Item{
			SourceChannel:      "",
			SourceChannelTitle: "",
			SourceMsgID:        100,
			SourceChannelID:    333,
		}

		got := s.formatLink(item, "")

		if !strings.Contains(got, DefaultSourceLabel) {
			t.Errorf("formatLink() should use default label, got %q", got)
		}
	})

	t.Run("escapes HTML in label", func(t *testing.T) {
		item := db.Item{
			SourceChannel: "channel",
			SourceMsgID:   1,
		}

		got := s.formatLink(item, "<script>alert('xss')</script>")

		if strings.Contains(got, "<script>") {
			t.Errorf("formatLink() should escape HTML, got %q", got)
		}

		if !strings.Contains(got, "&lt;script&gt;") {
			t.Errorf("formatLink() should contain escaped HTML, got %q", got)
		}
	})
}

func TestFormatItemLinks(t *testing.T) {
	s := &Scheduler{}
	rc := &digestRenderContext{scheduler: s}

	t.Run("multiple items", func(t *testing.T) {
		items := []db.Item{
			{SourceChannel: "ch1", SourceMsgID: 1},
			{SourceChannel: "ch2", SourceMsgID: 2},
			{SourceChannel: "ch3", SourceMsgID: 3},
		}

		links := rc.formatItemLinks(items)

		if len(links) != 3 {
			t.Errorf("expected 3 links, got %d", len(links))
		}

		for i, link := range links {
			if !strings.Contains(link, "<a href=") {
				t.Errorf("link %d should be HTML anchor, got %q", i, link)
			}
		}
	})

	t.Run(testNameEmptyItems, func(t *testing.T) {
		links := rc.formatItemLinks([]db.Item{})

		if len(links) != 0 {
			t.Errorf(testErrExpected0Links, len(links))
		}
	})
}

func TestClampThresholdEdgeCases(t *testing.T) {
	tests := []struct {
		name   string
		value  float32
		minVal float32
		maxVal float32
		want   float32
	}{
		{name: "equal min and max", value: 0.5, minVal: 0.5, maxVal: 0.5, want: 0.5},
		{name: "value at both bounds", value: 0.5, minVal: 0.5, maxVal: 0.5, want: 0.5},
		{name: "negative min", value: -0.5, minVal: -1.0, maxVal: 0.0, want: -0.5},
		{name: "very small range", value: 0.55, minVal: 0.5, maxVal: 0.51, want: 0.51},
		{name: "zero value clamped up", value: 0.0, minVal: 0.1, maxVal: 0.9, want: 0.1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := clampThreshold(tt.value, tt.minVal, tt.maxVal)

			if got != tt.want {
				t.Errorf(testErrClampThreshold, tt.value, tt.minVal, tt.maxVal, got, tt.want)
			}
		})
	}
}

func TestTopicEmojisMap(t *testing.T) {
	// Ensure all expected topics have emojis
	expectedTopics := []string{
		"Technology", "Finance", "Politics", "Sports",
		"Entertainment", "Science", "Health", "Business",
		"World News", "Local News", "Culture", "Education", "Humor",
	}

	for _, topic := range expectedTopics {
		emoji, ok := topicEmojis[topic]

		if !ok {
			t.Errorf("topic %q missing from topicEmojis map", topic)
		}

		if emoji == "" {
			t.Errorf("topic %q has empty emoji", topic)
		}
	}
}

func TestDigestRenderContextBuildMetadataSectionWithClusters(t *testing.T) {
	rc := &digestRenderContext{
		settings: digestSettings{topicsEnabled: true},
		items: []db.Item{
			{SourceChannel: "channel1"},
		},
		clusters: []db.ClusterWithItems{
			{Topic: "Tech"},
			{Topic: "Finance"},
			{Topic: "Tech"}, // Duplicate, but clusters count unique
		},
	}

	var sb strings.Builder
	rc.buildMetadataSection(&sb)

	got := sb.String()

	// Should count clusters as topics

	if !strings.Contains(got, "3 topics") {
		t.Errorf("buildMetadataSection() with clusters should show cluster count as topics, got %q", got)
	}
}

func TestDigestRenderContextBuildMetadataSectionTopicsDisabled(t *testing.T) {
	rc := &digestRenderContext{
		settings: digestSettings{topicsEnabled: false},
		items: []db.Item{
			{SourceChannel: "channel1", Topic: "Tech"},
			{SourceChannel: "channel2", Topic: "Finance"},
		},
	}

	var sb strings.Builder
	rc.buildMetadataSection(&sb)

	got := sb.String()

	// Topics disabled should show 0 topics

	if !strings.Contains(got, "0 topics") {
		t.Errorf("buildMetadataSection() with topics disabled should show 0 topics, got %q", got)
	}
}

func TestSchedulerGetLockID(t *testing.T) {
	t.Run("deterministic for same name", func(t *testing.T) {
		s := &Scheduler{
			cfg: &config.Config{LeaderElectionLeaseName: "test-lease"},
		}

		id1 := s.getLockID()

		id2 := s.getLockID()

		if id1 != id2 {
			t.Errorf("getLockID() should be deterministic: %d != %d", id1, id2)
		}
	})

	t.Run("different for different names", func(t *testing.T) {
		s1 := &Scheduler{cfg: &config.Config{LeaderElectionLeaseName: "lease-1"}}

		s2 := &Scheduler{cfg: &config.Config{LeaderElectionLeaseName: "lease-2"}}

		id1 := s1.getLockID()

		id2 := s2.getLockID()

		if id1 == id2 {
			t.Errorf("getLockID() should be different for different names: %d == %d", id1, id2)
		}
	})

	t.Run("empty name produces zero", func(t *testing.T) {
		s := &Scheduler{cfg: &config.Config{LeaderElectionLeaseName: ""}}

		id := s.getLockID()

		if id != 0 {
			t.Errorf("getLockID() with empty name = %d, want 0", id)
		}
	})
}

func TestSchedulerFormatItems(t *testing.T) {
	s := &Scheduler{}

	t.Run("formats multiple items", func(t *testing.T) {
		items := []db.Item{
			{ID: "1", Summary: "Summary one", SourceChannel: "ch1", SourceMsgID: 1, ImportanceScore: 0.85},
			{ID: "2", Summary: "Summary two", SourceChannel: "ch2", SourceMsgID: 2, ImportanceScore: 0.65},
		}

		rc := &digestRenderContext{scheduler: s, seenSummaries: make(map[string]bool)}
		got := rc.formatItems(items, false)

		if !strings.Contains(got, "Summary one") {
			t.Errorf("formatItems() should contain first summary, got %q", got)
		}

		if !strings.Contains(got, "Summary two") {
			t.Errorf("formatItems() should contain second summary, got %q", got)
		}
	})

	t.Run(testNameSkipsSeenSummaries, func(t *testing.T) {
		items := []db.Item{
			{ID: "1", Summary: "Already seen", SourceChannel: "ch1", SourceMsgID: 1},
			{ID: "2", Summary: "New summary", SourceChannel: "ch2", SourceMsgID: 2},
		}

		seen := map[string]bool{"Already seen": true}
		rc := &digestRenderContext{scheduler: s, seenSummaries: seen}

		got := rc.formatItems(items, false)

		if strings.Contains(got, "Already seen") {
			t.Errorf("formatItems() should skip seen summary, got %q", got)
		}

		if !strings.Contains(got, "New summary") {
			t.Errorf("formatItems() should contain new summary, got %q", got)
		}
	})

	t.Run(testNameEmptyItems, func(t *testing.T) {
		rc := &digestRenderContext{scheduler: s, seenSummaries: make(map[string]bool)}
		got := rc.formatItems([]db.Item{}, false)

		if got != "" {
			t.Errorf("formatItems() with empty items = %q, want empty", got)
		}
	})

	t.Run("groups same summaries", func(t *testing.T) {
		items := []db.Item{
			{ID: "1", Summary: "Same news", SourceChannel: "ch1", SourceMsgID: 1, ImportanceScore: 0.5},
			{ID: "2", Summary: "Same news", SourceChannel: "ch2", SourceMsgID: 2, ImportanceScore: 0.7},
		}

		rc := &digestRenderContext{scheduler: s, seenSummaries: make(map[string]bool)}

		got := rc.formatItems(items, false)

		// Should only show the summary once

		count := strings.Count(got, "Same news")

		if count > 1 {
			t.Errorf("formatItems() should group same summaries, found %d occurrences", count)
		}
		// But should show both source channels

		if !strings.Contains(got, "ch1") || !strings.Contains(got, "ch2") {
			t.Errorf("formatItems() should show both sources, got %q", got)
		}
	})

	t.Run("includes topic when enabled", func(t *testing.T) {
		items := []db.Item{
			{ID: "1", Summary: "Tech news", SourceChannel: "ch1", SourceMsgID: 1, Topic: "Technology"},
		}

		rc := &digestRenderContext{scheduler: s, seenSummaries: make(map[string]bool)}
		got := rc.formatItems(items, true)

		if !strings.Contains(got, testTopicTechnology) {
			t.Errorf("formatItems() with includeTopic=true should show topic, got %q", got)
		}
	})
}

func TestFormatSummaryGroupWithMultipleSources(t *testing.T) {
	s := &Scheduler{}
	rc := &digestRenderContext{scheduler: s}

	group := summaryGroup{
		summary: "Multi-source news",
		items: []db.Item{
			{SourceChannel: "channel1", SourceMsgID: 1},
			{SourceChannel: "channel2", SourceMsgID: 2},
			{SourceChannel: "channel3", SourceMsgID: 3},
		},
		importanceScore: 0.75,
	}

	var sb strings.Builder
	rc.formatSummaryGroup(&sb, group, false)

	got := sb.String()

	// Should contain all three sources separated by the source separator

	if !strings.Contains(got, "channel1") {
		t.Errorf("formatSummaryGroup() should contain channel1, got %q", got)
	}

	if !strings.Contains(got, "channel2") {
		t.Errorf("formatSummaryGroup() should contain channel2, got %q", got)
	}

	if !strings.Contains(got, "channel3") {
		t.Errorf("formatSummaryGroup() should contain channel3, got %q", got)
	}
}

func TestDigestRenderContextBuildSections(t *testing.T) {
	base := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)

	t.Run("full render with breaking notable and also", func(t *testing.T) {
		rc := &digestRenderContext{
			settings:     digestSettings{digestLanguage: "en", topicsEnabled: false},
			displayStart: base,
			displayEnd:   base.Add(time.Hour),
			scheduler:    &Scheduler{},
			items: []db.Item{
				{ID: "1", Summary: "Breaking news", SourceChannel: "ch1", SourceMsgID: 1, ImportanceScore: 0.9},
				{ID: "2", Summary: "Notable news", SourceChannel: "ch2", SourceMsgID: 2, ImportanceScore: 0.7},
				{ID: "3", Summary: "Other news", SourceChannel: "ch3", SourceMsgID: 3, ImportanceScore: 0.3},
			},
		}

		var sb strings.Builder
		rc.buildHeaderSection(&sb)
		rc.buildMetadataSection(&sb)

		got := sb.String()

		if !strings.Contains(got, testDigestForHeader) {
			t.Error("should contain header")
		}

		if !strings.Contains(got, test3Items) {
			t.Errorf("should contain item count, got %q", got)
		}
	})
}

func TestDigestSettingsStruct(t *testing.T) {
	s := digestSettings{
		topicsEnabled:               true,
		digestLanguage:              "ru",
		othersAsNarrative:           true,
		freshnessDecayHours:         12,
		freshnessFloor:              0.5,
		consolidatedClustersEnabled: true,
	}

	if !s.topicsEnabled {
		t.Error("topicsEnabled should be true")
	}

	if s.digestLanguage != "ru" {
		t.Errorf("digestLanguage = %q, want 'ru'", s.digestLanguage)
	}

	if !s.othersAsNarrative {
		t.Error("othersAsNarrative should be true")
	}

	if s.freshnessDecayHours != 12 {
		t.Errorf("freshnessDecayHours = %d, want 12", s.freshnessDecayHours)
	}
}

func TestSummaryGroupStruct(t *testing.T) {
	group := summaryGroup{
		summary: testSummaryText,
		items: []db.Item{
			{ID: "1", Summary: testSummaryText},
			{ID: "2", Summary: testSummaryText},
		},
		importanceScore: testReliability075,
	}

	if group.summary != testSummaryText {
		t.Errorf("summary = %q, want %q", group.summary, testSummaryText)
	}

	if len(group.items) != 2 {
		t.Errorf("items length = %d, want 2", len(group.items))
	}

	if group.importanceScore != testReliability075 {
		t.Errorf("importanceScore = %v, want %v", group.importanceScore, testReliability075)
	}
}

func TestTopicBalanceResultStruct(t *testing.T) {
	result := topicBalanceResult{
		Items:           []db.Item{{ID: "1"}, {ID: "2"}},
		TopicsAvailable: 5,
		TopicsSelected:  3,
		Relaxed:         true,
		MaxPerTopic:     2,
	}

	if len(result.Items) != 2 {
		t.Errorf(testErrItemsLength2, len(result.Items))
	}

	if result.TopicsAvailable != 5 {
		t.Errorf("TopicsAvailable = %d, want 5", result.TopicsAvailable)
	}

	if result.TopicsSelected != 3 {
		t.Errorf("TopicsSelected = %d, want 3", result.TopicsSelected)
	}

	if !result.Relaxed {
		t.Error("Relaxed should be true")
	}
}

func TestConstantsExist(t *testing.T) {
	// Verify key constants have expected values
	if DefaultTopic == "" {
		t.Error("DefaultTopic should not be empty")
	}

	if DefaultSourceLabel == "" {
		t.Error("DefaultSourceLabel should not be empty")
	}

	if DigestSeparatorLine == "" {
		t.Error("DigestSeparatorLine should not be empty")
	}

	if TimeFormatHourMinute == "" {
		t.Error("TimeFormatHourMinute should not be empty")
	}

	if ClusterMaxItemsLimit <= 0 {
		t.Errorf("ClusterMaxItemsLimit = %d, should be positive", ClusterMaxItemsLimit)
	}

	if LogTruncateLength <= 0 {
		t.Errorf("LogTruncateLength = %d, should be positive", LogTruncateLength)
	}
}

func TestEmojis(t *testing.T) {
	// Verify importance emoji constants are non-empty
	if EmojiBreaking == "" {
		t.Error("EmojiBreaking should not be empty")
	}

	if EmojiNotable == "" {
		t.Error("EmojiNotable should not be empty")
	}

	if EmojiStandard == "" {
		t.Error("EmojiStandard should not be empty")
	}

	if EmojiBullet == "" {
		t.Error("EmojiBullet should not be empty")
	}
}

func TestImportanceScoreThresholds(t *testing.T) {
	// Verify threshold ordering
	if ImportanceScoreBreaking <= ImportanceScoreNotable {
		t.Errorf("ImportanceScoreBreaking (%v) should be > ImportanceScoreNotable (%v)",
			ImportanceScoreBreaking, ImportanceScoreNotable)
	}

	if ImportanceScoreNotable <= ImportanceScoreStandard {
		t.Errorf("ImportanceScoreNotable (%v) should be > ImportanceScoreStandard (%v)",
			ImportanceScoreNotable, ImportanceScoreStandard)
	}
}

func TestRenderRepresentativeCluster(t *testing.T) {
	s := &Scheduler{}

	rc := &digestRenderContext{
		scheduler:     s,
		seenSummaries: make(map[string]bool),
	}

	t.Run("renders cluster with topic", func(t *testing.T) {
		cluster := db.ClusterWithItems{
			Topic: "Technology",
			Items: []db.Item{
				{Summary: testSummaryText, SourceChannel: "ch1", SourceMsgID: 1, ImportanceScore: 0.75},
			},
		}

		var sb strings.Builder

		rendered := rc.renderRepresentativeCluster(&sb, cluster)

		if !rendered {
			t.Error("renderRepresentativeCluster should return true")
		}

		got := sb.String()
		// Topic is uppercased in render

		if !strings.Contains(strings.ToUpper(got), strings.ToUpper(testTopicTechnology)) {
			t.Errorf("should contain topic (uppercased), got %q", got)
		}

		if !strings.Contains(got, testSummaryText) {
			t.Errorf("should contain summary, got %q", got)
		}
	})

	t.Run(testNameSkipsSeenSummaries, func(t *testing.T) {
		rc2 := &digestRenderContext{
			scheduler:     s,
			seenSummaries: map[string]bool{"Already seen": true},
		}

		cluster := db.ClusterWithItems{
			Topic: testTopicTech,
			Items: []db.Item{
				{Summary: "Already seen", SourceChannel: "ch1", SourceMsgID: 1},
			},
		}

		var sb strings.Builder

		rendered := rc2.renderRepresentativeCluster(&sb, cluster)

		if rendered {
			t.Error("renderRepresentativeCluster should return false for seen summary")
		}
	})

	t.Run("shows related count for multiple items", func(t *testing.T) {
		rc3 := &digestRenderContext{
			scheduler:     s,
			seenSummaries: make(map[string]bool),
		}

		cluster := db.ClusterWithItems{
			Topic: "Tech",
			Items: []db.Item{
				{Summary: "Main", SourceChannel: "ch1", SourceMsgID: 1},
				{Summary: "Related 1", SourceChannel: "ch2", SourceMsgID: 2},
				{Summary: "Related 2", SourceChannel: "ch3", SourceMsgID: 3},
			},
		}

		var sb strings.Builder
		rc3.renderRepresentativeCluster(&sb, cluster)

		got := sb.String()

		if !strings.Contains(got, "+2 related") {
			t.Errorf("should show '+2 related', got %q", got)
		}
	})
}

func TestRenderMultiItemCluster(t *testing.T) {
	s := &Scheduler{}

	t.Run("uses representative when consolidated disabled", func(t *testing.T) {
		rc := &digestRenderContext{
			scheduler: s,
			settings: digestSettings{
				consolidatedClustersEnabled: false,
			},
			seenSummaries: make(map[string]bool),
		}

		cluster := db.ClusterWithItems{
			Topic: "Tech",
			Items: []db.Item{
				{Summary: "Test", SourceChannel: "ch1", SourceMsgID: 1, ImportanceScore: 0.8},
			},
		}

		var sb strings.Builder

		rendered := rc.renderMultiItemCluster(context.Background(), &sb, cluster)

		if !rendered {
			t.Error("should render successfully")
		}
	})
}

func TestRatingStatsStruct(t *testing.T) {
	stats := ratingStats{
		count:              10,
		weightedTotal:      5.5,
		weightedGood:       3.0,
		weightedBad:        1.5,
		weightedIrrelevant: 1.0,
	}

	if stats.count != 10 {
		t.Errorf("count = %d, want 10", stats.count)
	}

	if stats.weightedTotal != 5.5 {
		t.Errorf("weightedTotal = %v, want 5.5", stats.weightedTotal)
	}
}

func TestTopicCandidateStruct(t *testing.T) {
	candidate := topicCandidate{
		index: 5,
		key:   "Technology",
	}

	if candidate.index != 5 {
		t.Errorf(testErrIndexWant5, candidate.index)
	}

	if candidate.key != testTopicTechnology {
		t.Errorf("key = %q, want %q", candidate.key, testTopicTechnology)
	}
}

func TestRenderContextFullFlow(t *testing.T) {
	base := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)

	s := &Scheduler{}

	t.Run("full context with all fields", func(t *testing.T) {
		rc := &digestRenderContext{
			scheduler:    s,
			settings:     digestSettings{digestLanguage: "en", topicsEnabled: true},
			displayStart: base,
			displayEnd:   base.Add(time.Hour),
			items: []db.Item{
				{ID: "1", Summary: "Breaking", ImportanceScore: 0.9, SourceChannel: "ch1", SourceMsgID: 1},
				{ID: "2", Summary: "Notable", ImportanceScore: 0.7, SourceChannel: "ch2", SourceMsgID: 2},
			},
			clusters:      []db.ClusterWithItems{},
			seenSummaries: make(map[string]bool),
		}

		// Test header
		var sb strings.Builder
		rc.buildHeaderSection(&sb)

		headerOutput := sb.String()

		if !strings.Contains(headerOutput, testDigestForHeader) {
			t.Errorf("header should contain %q, got %q", testDigestForHeader, headerOutput)
		}

		// Test metadata
		sb.Reset()
		rc.buildMetadataSection(&sb)

		metaOutput := sb.String()

		if !strings.Contains(metaOutput, "2 items") {
			t.Errorf("metadata should contain item count, got %q", metaOutput)
		}

		// Test categorization

		breaking, notable, also := rc.categorizeByImportance()

		if len(breaking.items) != 1 {
			t.Errorf(testErrExpected1BreakingItem, len(breaking.items))
		}

		if len(notable.items) != 1 {
			t.Errorf(testErrExpected1NotableItem, len(notable.items))
		}

		if len(also.items) != 0 {
			t.Errorf("expected 0 also items, got %d", len(also.items))
		}
	})
}

func TestAutoWeightConfigFields(t *testing.T) {
	cfg := DefaultAutoWeightConfig()

	// Verify all fields have sensible values

	if cfg.AutoMin < 0 || cfg.AutoMin > 1 {
		t.Errorf("AutoMin = %v, should be in [0,1]", cfg.AutoMin)
	}

	if cfg.AutoMax < cfg.AutoMin || cfg.AutoMax > 2 {
		t.Errorf("AutoMax = %v, should be >= AutoMin and <= 2", cfg.AutoMax)
	}

	if cfg.MinMessages < 0 {
		t.Errorf("MinMessages = %d, should be >= 0", cfg.MinMessages)
	}

	if cfg.ExpectedFrequency <= 0 {
		t.Errorf("ExpectedFrequency = %v, should be > 0", cfg.ExpectedFrequency)
	}

	if cfg.RollingDays <= 0 {
		t.Errorf("RollingDays = %d, should be > 0", cfg.RollingDays)
	}
}

func TestFormatSummaryLineWithAllFields(t *testing.T) {
	group := summaryGroup{
		summary: "A comprehensive test summary with enough content",
		items: []db.Item{
			{Topic: "Finance", SourceChannel: "ch1", SourceMsgID: 1},
			{Topic: "Finance", SourceChannel: "ch2", SourceMsgID: 2},
		},
		importanceScore: 0.85,
	}

	// Test with topic

	got := formatSummaryLine(group, true, EmojiBreaking, group.summary)

	if !strings.Contains(got, "Finance") {
		t.Errorf("should contain topic, got %q", got)
	}

	if !strings.Contains(got, EmojiBreaking) {
		t.Errorf("should contain emoji prefix, got %q", got)
	}

	// Test without topic

	got2 := formatSummaryLine(group, false, EmojiNotable, group.summary)

	if strings.Contains(got2, "<b>Finance</b>") {
		t.Errorf("should not contain topic when includeTopic=false, got %q", got2)
	}
}

func TestExtractTopicsFromDigestComprehensive(t *testing.T) {
	tests := []struct {
		name     string
		items    []db.Item
		clusters []db.ClusterWithItems
		wantLen  int
	}{
		{
			name:     "empty inputs",
			items:    nil,
			clusters: nil,
			wantLen:  0,
		},
		{
			name: "items only",
			items: []db.Item{
				{Topic: "Technology"},
				{Topic: "Finance"},
				{Topic: "Technology"}, // Duplicate
			},
			clusters: nil,
			wantLen:  2,
		},
		{
			name:  "clusters only",
			items: nil,
			clusters: []db.ClusterWithItems{
				{Topic: "Tech"},
				{Topic: "Science"},
			},
			wantLen: 2,
		},
		{
			name: "both items and clusters with overlap",
			items: []db.Item{
				{Topic: "Technology"},
				{Topic: "Finance"},
			},
			clusters: []db.ClusterWithItems{
				{Topic: "Science"},
				{Topic: "Technology"}, // Overlaps
			},
			wantLen: 3,
		},
		{
			name: "empty topic strings ignored",
			items: []db.Item{
				{Topic: ""},
				{Topic: "Finance"},
			},
			clusters: []db.ClusterWithItems{
				{Topic: ""},
				{Topic: "Science"},
			},
			wantLen: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			topics := extractTopicsFromDigest(tt.items, tt.clusters)

			if len(topics) != tt.wantLen {
				t.Errorf("extractTopicsFromDigest() got %d topics, want %d", len(topics), tt.wantLen)
			}
		})
	}
}

func TestTruncateForLogComprehensive(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "short string unchanged",
			input: "short",
			want:  "short",
		},
		{
			name:  "exactly at limit",
			input: strings.Repeat("a", LogTruncateLength),
			want:  strings.Repeat("a", LogTruncateLength),
		},
		{
			name:  "one over limit gets truncated",
			input: strings.Repeat("a", LogTruncateLength+1),
			want:  strings.Repeat("a", LogTruncateLength) + "...",
		},
		{
			name:  "much longer gets truncated",
			input: strings.Repeat("a", 200),
			want:  strings.Repeat("a", LogTruncateLength) + "...",
		},
		{
			name:  "empty string unchanged",
			input: "",
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateForLog(tt.input)

			if got != tt.want {
				t.Errorf(testErrTruncateForLog, got, tt.want)
			}
		})
	}
}

func TestCountItemsWithMediaComprehensive(t *testing.T) {
	tests := []struct {
		name  string
		items []db.ItemWithMedia
		want  int
	}{
		{
			name:  "empty slice",
			items: nil,
			want:  0,
		},
		{
			name: "all with media",
			items: []db.ItemWithMedia{
				{MediaData: []byte{1, 2}},
				{MediaData: []byte{3, 4, 5}},
			},
			want: 2,
		},
		{
			name: "none with media",
			items: []db.ItemWithMedia{
				{MediaData: nil},
				{MediaData: []byte{}},
			},
			want: 0,
		},
		{
			name: "mixed",
			items: []db.ItemWithMedia{
				{MediaData: []byte{1}},
				{MediaData: nil},
				{MediaData: []byte{2, 3}},
				{MediaData: []byte{}},
			},
			want: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := countItemsWithMedia(tt.items)

			if got != tt.want {
				t.Errorf(testErrCountItemsWithMedia, got, tt.want)
			}
		})
	}
}

func TestFormatItemLabelComprehensive(t *testing.T) {
	tests := []struct {
		name string
		item db.Item
		want string
	}{
		{
			name: "with source channel",
			item: db.Item{SourceChannel: "testchan"},
			want: "@testchan",
		},
		{
			name: "with channel title only",
			item: db.Item{SourceChannelTitle: "My Channel"},
			want: "My Channel",
		},
		{
			name: "no channel info",
			item: db.Item{},
			want: DefaultSourceLabel,
		},
		{
			name: "prefers channel over title",
			item: db.Item{SourceChannel: "preferred", SourceChannelTitle: "ignored"},
			want: "@preferred",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatItemLabel(tt.item)

			if got != tt.want {
				t.Errorf(testErrFormatItemLabel, got, tt.want)
			}
		})
	}
}

func TestGroupItemsBySummaryComprehensive(t *testing.T) {
	t.Run("groups items with same summary", func(t *testing.T) {
		items := []db.Item{
			{Summary: "News A", ImportanceScore: 0.5},
			{Summary: "News B", ImportanceScore: 0.7},
			{Summary: "News A", ImportanceScore: 0.8},
		}

		seen := make(map[string]bool)

		groups := groupItemsBySummary(items, seen)

		if len(groups) != 2 {
			t.Fatalf(testErrExpected2Groups, len(groups))
		}

		if len(groups[0].items) != 2 {
			t.Errorf("News A group should have 2 items, got %d", len(groups[0].items))
		}

		if groups[0].importanceScore != testSimilarityThresholdDefault {
			t.Errorf("News A group importance = %v, want 0.8", groups[0].importanceScore)
		}
	})

	t.Run("skips already seen summaries", func(t *testing.T) {
		items := []db.Item{
			{Summary: "Already Seen"},
			{Summary: "New Item"},
		}

		seen := map[string]bool{"Already Seen": true}

		groups := groupItemsBySummary(items, seen)

		if len(groups) != 1 {
			t.Fatalf(testErrExpected1Group, len(groups))
		}

		if groups[0].summary != "New Item" {
			t.Errorf("expected 'New Item', got %q", groups[0].summary)
		}
	})

	t.Run("empty items returns empty groups", func(t *testing.T) {
		groups := groupItemsBySummary(nil, make(map[string]bool))

		if len(groups) != 0 {
			t.Errorf(testErrExpected0Groups, len(groups))
		}
	})
}

func TestSchedulerFormatLinkComprehensive(t *testing.T) {
	s := &Scheduler{}

	t.Run("public channel link", func(t *testing.T) {
		item := db.Item{SourceChannel: "testchan", SourceMsgID: 123}

		got := s.formatLink(item, testLinkLabel)

		if !strings.Contains(got, "t.me/testchan/123") {
			t.Errorf("should contain public channel URL, got %q", got)
		}

		if !strings.Contains(got, "Label") {
			t.Errorf("should contain label, got %q", got)
		}
	})

	t.Run("private channel link", func(t *testing.T) {
		item := db.Item{SourceChannelID: 12345, SourceMsgID: 123}

		got := s.formatLink(item, "Private")

		if !strings.Contains(got, "t.me/c/12345/123") {
			t.Errorf("should contain private channel URL, got %q", got)
		}
	})

	t.Run(testNameEmptyLabelChannel, func(t *testing.T) {
		item := db.Item{SourceChannel: "testchan", SourceMsgID: 1}

		got := s.formatLink(item, "")

		if !strings.Contains(got, "testchan") {
			t.Errorf("should use channel name as label, got %q", got)
		}
	})

	t.Run("empty label uses channel title", func(t *testing.T) {
		item := db.Item{SourceChannelTitle: "My Title", SourceChannelID: 123, SourceMsgID: 1}

		got := s.formatLink(item, "")

		if !strings.Contains(got, testMyTitle) {
			t.Errorf("should use channel title as label, got %q", got)
		}
	})

	t.Run("empty label uses default", func(t *testing.T) {
		item := db.Item{SourceChannelID: 123, SourceMsgID: 1}

		got := s.formatLink(item, "")

		if !strings.Contains(got, DefaultSourceLabel) {
			t.Errorf("should use default label, got %q", got)
		}
	})
}

func TestExtractDigestHeaderComprehensive(t *testing.T) {
	t.Run("empty text", func(t *testing.T) {
		got := extractDigestHeader("")

		if got != "" {
			t.Errorf("expected empty result for empty input, got %q", got)
		}
	})

	t.Run("only header no items", func(t *testing.T) {
		text := testHeaderLines

		got := extractDigestHeader(text)

		if got != "Header Line 1\nHeader Line 2" {
			t.Errorf("unexpected result: %q", got)
		}
	})

	t.Run("stops at emoji bullet", func(t *testing.T) {
		text := "Header\n🔴 Breaking news item"

		got := extractDigestHeader(text)

		if strings.Contains(got, testBreakingWord) {
			t.Errorf("should not contain item, got %q", got)
		}
	})

	t.Run("stops at topic border", func(t *testing.T) {
		text := "Header\n┌─────────────────────\n" + testTopicBorder + " " + testTopicContent

		got := extractDigestHeader(text)

		if strings.Contains(got, testTopicContent) {
			t.Errorf(testErrNoTopicContent, got)
		}
	})
}

func TestCollectClusterSummariesComprehensive(t *testing.T) {
	clusters := []db.ClusterWithItems{
		{Topic: "A", Items: []db.Item{{Summary: "Sum1"}}},
		{Topic: "B", Items: []db.Item{{Summary: "Sum2"}}},
		{Topic: "C", Items: []db.Item{{Summary: "Sum3"}}},
		{Topic: "D", Items: []db.Item{{Summary: "Sum4"}}},
		{Topic: "E", Items: []db.Item{{Summary: "Sum5"}}},
	}

	t.Run(testNameRespectsMaxLimit, func(t *testing.T) {
		summaries := collectClusterSummaries(clusters, 3)

		if len(summaries) != 3 {
			t.Errorf(testErrExpected3Summaries, len(summaries))
		}
	})

	t.Run("returns all if under limit", func(t *testing.T) {
		summaries := collectClusterSummaries(clusters, 10)

		if len(summaries) != 5 {
			t.Errorf(testErrExpected5Summaries, len(summaries))
		}
	})

	t.Run("skips clusters without topic", func(t *testing.T) {
		clustersWithEmpty := []db.ClusterWithItems{
			{Topic: "", Items: []db.Item{{Summary: "Skip"}}},
			{Topic: "Valid", Items: []db.Item{{Summary: "Include"}}},
		}

		summaries := collectClusterSummaries(clustersWithEmpty, 10)

		if len(summaries) != 1 {
			t.Errorf(testErrExpected1Summary, len(summaries))
		}
	})

	t.Run("skips clusters without items", func(t *testing.T) {
		clustersNoItems := []db.ClusterWithItems{
			{Topic: "Empty", Items: nil},
			{Topic: "Valid", Items: []db.Item{{Summary: "Include"}}},
		}

		summaries := collectClusterSummaries(clustersNoItems, 10)

		if len(summaries) != 1 {
			t.Errorf(testErrExpected1Summary, len(summaries))
		}
	})
}

func TestAppendItemSummariesComprehensive(t *testing.T) {
	items := []db.Item{
		{Summary: "Item1"},
		{Summary: "Item2"},
		{Summary: "Item3"},
	}

	t.Run(testNameRespectsMaxLimit, func(t *testing.T) {
		existing := []string{"Existing"}

		result := appendItemSummaries(existing, items, 2)

		if len(result) != 2 {
			t.Errorf(testErrExpected2Summaries, len(result))
		}
	})

	t.Run("skips empty summaries", func(t *testing.T) {
		itemsWithEmpty := []db.Item{
			{Summary: ""},
			{Summary: "Valid"},
		}

		result := appendItemSummaries(nil, itemsWithEmpty, 10)

		if len(result) != 1 {
			t.Errorf(testErrExpected1Summary, len(result))
		}

		if result[0] != "Valid" {
			t.Errorf("expected 'Valid', got %q", result[0])
		}
	})

	t.Run("appends to existing", func(t *testing.T) {
		existing := []string{"Existing1", "Existing2"}

		result := appendItemSummaries(existing, items, 10)

		if len(result) != 5 {
			t.Errorf(testErrExpected5Summaries, len(result))
		}

		if result[0] != "Existing1" {
			t.Errorf("first should be 'Existing1', got %q", result[0])
		}
	})
}

func TestCategorizeClustersMultiple(t *testing.T) {
	clusters := []db.ClusterWithItems{
		{Topic: "Breaking", Items: []db.Item{{ImportanceScore: 0.9}, {ImportanceScore: 0.75}}},
		{Topic: "Notable", Items: []db.Item{{ImportanceScore: 0.65}}},
		{Topic: "Also1", Items: []db.Item{{ImportanceScore: 0.4}}},
		{Topic: "Also2", Items: []db.Item{{ImportanceScore: 0.3}}},
	}

	breaking, notable, also := categorizeClusters(clusters)

	if len(breaking.clusters) != 1 {
		t.Errorf(testErrExpected1Breaking, len(breaking.clusters))
	}

	if len(notable.clusters) != 1 {
		t.Errorf(testErrExpected1Notable, len(notable.clusters))
	}

	if len(also.clusters) != 2 {
		t.Errorf("expected 2 also clusters, got %d", len(also.clusters))
	}
}

func TestCategorizeItemsMultiple(t *testing.T) {
	// Breaking >= 0.8, Notable >= 0.6, Also < 0.6
	items := []db.Item{
		{ImportanceScore: 0.9},  // Breaking
		{ImportanceScore: 0.85}, // Breaking
		{ImportanceScore: 0.65}, // Notable
		{ImportanceScore: 0.55}, // Also
		{ImportanceScore: 0.4},  // Also
		{ImportanceScore: 0.3},  // Also
	}

	breaking, notable, also := categorizeItems(items)

	if len(breaking.items) != 2 {
		t.Errorf("expected 2 breaking items, got %d", len(breaking.items))
	}

	if len(notable.items) != 1 {
		t.Errorf(testErrExpected1NotableItem, len(notable.items))
	}

	if len(also.items) != 3 {
		t.Errorf(testErrExpected3AlsoItems, len(also.items))
	}
}

func TestClusterMaxImportanceVariants(t *testing.T) {
	tests := []struct {
		name    string
		cluster db.ClusterWithItems
		want    float32
	}{
		{
			name:    "empty cluster",
			cluster: db.ClusterWithItems{Items: nil},
			want:    0,
		},
		{
			name: "single item",
			cluster: db.ClusterWithItems{
				Items: []db.Item{{ImportanceScore: 0.75}},
			},
			want: 0.75,
		},
		{
			name: "multiple items",
			cluster: db.ClusterWithItems{
				Items: []db.Item{
					{ImportanceScore: 0.5},
					{ImportanceScore: 0.9},
					{ImportanceScore: 0.7},
				},
			},
			want: 0.9,
		},
		{
			name: "all same importance",
			cluster: db.ClusterWithItems{
				Items: []db.Item{
					{ImportanceScore: 0.6},
					{ImportanceScore: 0.6},
				},
			},
			want: 0.6,
		},
		{
			name: "first item is max",
			cluster: db.ClusterWithItems{
				Items: []db.Item{
					{ImportanceScore: 0.95},
					{ImportanceScore: 0.5},
				},
			},
			want: 0.95,
		},
		{
			name: "last item is max",
			cluster: db.ClusterWithItems{
				Items: []db.Item{
					{ImportanceScore: 0.5},
					{ImportanceScore: 0.95},
				},
			},
			want: 0.95,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := clusterMaxImportance(tt.cluster)

			if got != tt.want {
				t.Errorf(testErrClusterMaxImportance, got, tt.want)
			}
		})
	}
}

func TestCategorizeItemsEmpty(t *testing.T) {
	breaking, notable, also := categorizeItems(nil)

	if len(breaking.items) != 0 || len(notable.items) != 0 || len(also.items) != 0 {
		t.Error("empty items should produce empty categories")
	}
}

func TestCategorizeClustersEmpty(t *testing.T) {
	breaking, notable, also := categorizeClusters(nil)

	if len(breaking.clusters) != 0 || len(notable.clusters) != 0 || len(also.clusters) != 0 {
		t.Error("empty clusters should produce empty categories")
	}
}

func TestDigestRenderContextCategorizeByImportanceWithClusters(t *testing.T) {
	rc := &digestRenderContext{
		settings: digestSettings{topicsEnabled: true},
		clusters: []db.ClusterWithItems{
			{Topic: "Tech", Items: []db.Item{{ImportanceScore: 0.9}}},
			{Topic: "Finance", Items: []db.Item{{ImportanceScore: 0.65}}},
		},
	}

	breaking, notable, also := rc.categorizeByImportance()

	if len(breaking.clusters) != 1 {
		t.Errorf(testErrExpected1Breaking, len(breaking.clusters))
	}

	if len(notable.clusters) != 1 {
		t.Errorf(testErrExpected1Notable, len(notable.clusters))
	}

	if len(also.clusters) != 0 {
		t.Errorf("expected 0 also clusters, got %d", len(also.clusters))
	}
}

func TestDigestRenderContextCategorizeByImportanceWithItems(t *testing.T) {
	rc := &digestRenderContext{
		settings: digestSettings{topicsEnabled: false},
		items: []db.Item{
			{ImportanceScore: 0.9},
			{ImportanceScore: 0.65},
			{ImportanceScore: 0.4},
		},
	}

	breaking, notable, also := rc.categorizeByImportance()

	if len(breaking.items) != 1 {
		t.Errorf(testErrExpected1BreakingItem, len(breaking.items))
	}

	if len(notable.items) != 1 {
		t.Errorf(testErrExpected1NotableItem, len(notable.items))
	}

	if len(also.items) != 1 {
		t.Errorf(testErrExpected1AlsoItem, len(also.items))
	}
}

func TestDigestRenderContextBuildHeaderSectionContent(t *testing.T) {
	base := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)

	rc := &digestRenderContext{
		settings:     digestSettings{digestLanguage: "en"},
		displayStart: base,
		displayEnd:   base.Add(time.Hour),
	}

	var sb strings.Builder
	rc.buildHeaderSection(&sb)

	got := sb.String()

	if !strings.Contains(got, testDigestForHeader) {
		t.Errorf("should contain %q, got %q", testDigestForHeader, got)
	}

	if !strings.Contains(got, testTime1000) {
		t.Errorf("should contain start time '10:00', got %q", got)
	}

	if !strings.Contains(got, testTime1100) {
		t.Errorf("should contain end time '11:00', got %q", got)
	}

	if !strings.Contains(got, DigestSeparatorLine) {
		t.Errorf("should contain separator line, got %q", got)
	}
}

func TestDigestRenderContextBuildHeaderSectionLanguages(t *testing.T) {
	base := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)

	tests := []struct {
		lang string
		want string
	}{
		{"en", testDigestForHeader},
		{"ru", "Дайджест за"},
		{"de", "Digest für"},
		{"es", "Resumen para"},
		{"fr", "Résumé pour"},
		{"it", "Riassunto per"},
	}

	for _, tt := range tests {
		t.Run(tt.lang, func(t *testing.T) {
			rc := &digestRenderContext{
				settings:     digestSettings{digestLanguage: tt.lang},
				displayStart: base,
				displayEnd:   base.Add(time.Hour),
			}

			var sb strings.Builder
			rc.buildHeaderSection(&sb)

			got := sb.String()

			if !strings.Contains(got, tt.want) {
				t.Errorf("should contain %q for lang %q, got %q", tt.want, tt.lang, got)
			}
		})
	}
}

func TestClusterGroupEmpty(t *testing.T) {
	group := clusterGroup{}

	if len(group.clusters) != 0 {
		t.Errorf("expected 0 clusters, got %d", len(group.clusters))
	}

	if len(group.items) != 0 {
		t.Errorf(testErrExpected0Items, len(group.items))
	}
}

func TestDigestSettingsDefaults(t *testing.T) {
	ds := digestSettings{}

	if ds.topicsEnabled {
		t.Error("topicsEnabled should default to false")
	}

	if ds.editorEnabled {
		t.Error("editorEnabled should default to false")
	}

	if ds.digestLanguage != "" {
		t.Errorf("digestLanguage should default to empty, got %q", ds.digestLanguage)
	}
}

func TestCategorizeItemsBoundaries(t *testing.T) {
	t.Run("exactly at breaking threshold", func(t *testing.T) {
		items := []db.Item{{ImportanceScore: ImportanceScoreBreaking}}

		breaking, _, _ := categorizeItems(items)

		if len(breaking.items) != 1 {
			t.Error("item at breaking threshold should be breaking")
		}
	})

	t.Run("exactly at notable threshold", func(t *testing.T) {
		items := []db.Item{{ImportanceScore: ImportanceScoreNotable}}

		_, notable, _ := categorizeItems(items)

		if len(notable.items) != 1 {
			t.Error("item at notable threshold should be notable")
		}
	})

	t.Run("just below notable threshold", func(t *testing.T) {
		items := []db.Item{{ImportanceScore: ImportanceScoreNotable - 0.01}}

		_, _, also := categorizeItems(items)

		if len(also.items) != 1 {
			t.Error("item below notable threshold should be also")
		}
	})
}

func TestDigestRenderContextCollectSourceLinksEmpty(t *testing.T) {
	s := &Scheduler{}

	rc := &digestRenderContext{scheduler: s}

	links := rc.collectSourceLinks(nil)

	if len(links) != 0 {
		t.Errorf("expected 0 links for nil items, got %d", len(links))
	}

	links = rc.collectSourceLinks([]db.Item{})

	if len(links) != 0 {
		t.Errorf(testErrExpected0Links, len(links))
	}
}
