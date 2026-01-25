package digest

import (
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/lueurxax/telegram-digest-bot/internal/platform/config"
	"github.com/lueurxax/telegram-digest-bot/internal/storage"
)

// Test constants for clustering tests
const (
	testErrNormalizeClusterTopic   = "normalizeClusterTopic(%q) = %q, want %q"
	testNameSortsByImportance      = "sorts by importance descending"
	testTopicTechnology            = "Technology"
	testTopicFinance               = "Finance"
	testTopicTech                  = "Tech"
	testErrExpected1ItemFinance    = "expected 1 item in Finance group, got %d"
	testErrExpected0Items          = "expected 0 items, got %d"
	testErrExpected3Links          = "expected 3 links, got %d"
	testLabelPrivateChannel        = "Private Channel"
	testSimilarityThresholdHigh    = 0.9
	testSimilarityThresholdDefault = 0.8
	testItemID1                    = "testItemID1"
	testItemID2                    = "testItemID2"
	testMinAgreementDefault        = 0.5
)

func TestCalculateCoherence(t *testing.T) {
	s := &Scheduler{}

	tests := []struct {
		name       string
		items      []db.Item
		embeddings map[string][]float32
		want       float32
	}{
		{
			name: "Perfect coherence",
			items: []db.Item{
				{ID: "1"},
				{ID: "2"},
			},
			embeddings: map[string][]float32{
				"1": {1, 0},
				"2": {1, 0},
			},
			want: 1.0,
		},
		{
			name: "Zero coherence",
			items: []db.Item{
				{ID: "1"},
				{ID: "2"},
			},
			embeddings: map[string][]float32{
				"1": {1, 0},
				"2": {0, 1},
			},
			want: 0.0,
		},
		{
			name: "Negative coherence",
			items: []db.Item{
				{ID: "1"},
				{ID: "2"},
			},
			embeddings: map[string][]float32{
				"1": {1, 0},
				"2": {-1, 0},
			},
			want: -1.0,
		},
		{
			name: "Three items mixed",
			items: []db.Item{
				{ID: "1"},
				{ID: "2"},
				{ID: "3"},
			},
			embeddings: map[string][]float32{
				"1": {1, 0},
				"2": {0, 1},
				"3": {-1, 0},
			},
			want: -0.33333334, // (0 + -1 + 0) / 3
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := s.calculateCoherence(tt.items, tt.embeddings)
			if got != tt.want {
				t.Errorf("calculateCoherence() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetImportancePrefix(t *testing.T) {
	tests := []struct {
		score float32
		want  string
	}{
		{ImportanceScoreBreaking, EmojiBreaking},
		{ImportanceScoreNotable, EmojiNotable},
		{ImportanceScoreStandard, EmojiStandard},
		{ImportanceScoreStandard - 0.1, EmojiBullet},
	}
	for _, tt := range tests {
		if got := getImportancePrefix(tt.score); got != tt.want {
			t.Errorf("getImportancePrefix(%v) = %v, want %v", tt.score, got, tt.want)
		}
	}
}

func TestNormalizeClusterTopic(t *testing.T) {
	tests := []struct {
		name  string
		topic string
		want  string
	}{
		{name: "lowercase", topic: "technology", want: testTopicTechnology},
		{name: "uppercase", topic: "FINANCE", want: testTopicFinance},
		{name: "mixed case", topic: "wOrLd NeWs", want: "World News"},
		{name: "with spaces", topic: "  politics  ", want: "Politics"},
		{name: "empty returns default", topic: "", want: DefaultTopic},
		{name: "whitespace only", topic: "   ", want: DefaultTopic},
		{name: "already normalized", topic: "Science", want: "Science"},
		{name: "multi word", topic: "local news", want: "Local News"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeClusterTopic(tt.topic); got != tt.want {
				t.Errorf(testErrNormalizeClusterTopic, tt.topic, got, tt.want)
			}
		})
	}
}

func TestWithinClusterWindow(t *testing.T) {
	now := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
	hour := time.Hour

	tests := []struct {
		name   string
		a      time.Time
		b      time.Time
		window time.Duration
		want   bool
	}{
		{
			name:   "same time",
			a:      now,
			b:      now,
			window: hour,
			want:   true,
		},
		{
			name:   "within window",
			a:      now,
			b:      now.Add(30 * time.Minute),
			window: hour,
			want:   true,
		},
		{
			name:   "exactly at window",
			a:      now,
			b:      now.Add(hour),
			window: hour,
			want:   true,
		},
		{
			name:   "outside window",
			a:      now,
			b:      now.Add(2 * hour),
			window: hour,
			want:   false,
		},
		{
			name:   "reverse order still works",
			a:      now.Add(30 * time.Minute),
			b:      now,
			window: hour,
			want:   true,
		},
		{
			name:   "zero time a",
			a:      time.Time{},
			b:      now,
			window: hour,
			want:   true,
		},
		{
			name:   "zero time b",
			a:      now,
			b:      time.Time{},
			window: hour,
			want:   true,
		},
		{
			name:   "both zero",
			a:      time.Time{},
			b:      time.Time{},
			window: hour,
			want:   true,
		},
		{
			name:   "large window",
			a:      now,
			b:      now.Add(24 * time.Hour),
			window: 48 * time.Hour,
			want:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := withinClusterWindow(tt.a, tt.b, tt.window); got != tt.want {
				t.Errorf("withinClusterWindow() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCalculateCoherenceEdgeCases(t *testing.T) {
	s := &Scheduler{}

	t.Run("single item returns perfect score", func(t *testing.T) {
		items := []db.Item{{ID: "1"}}
		embeddings := map[string][]float32{"1": {1, 0, 0}}

		got := s.calculateCoherence(items, embeddings)
		if got != PerfectSimilarityScore {
			t.Errorf("calculateCoherence() for single item = %v, want %v", got, PerfectSimilarityScore)
		}
	})

	t.Run("empty items returns perfect score", func(t *testing.T) {
		items := []db.Item{}
		embeddings := map[string][]float32{}

		got := s.calculateCoherence(items, embeddings)
		if got != PerfectSimilarityScore {
			t.Errorf("calculateCoherence() for empty items = %v, want %v", got, PerfectSimilarityScore)
		}
	})

	t.Run("missing embeddings returns zero", func(t *testing.T) {
		items := []db.Item{{ID: "1"}, {ID: "2"}}
		embeddings := map[string][]float32{} // No embeddings

		got := s.calculateCoherence(items, embeddings)
		if got != 0 {
			t.Errorf("calculateCoherence() with no embeddings = %v, want 0", got)
		}
	})

	t.Run("partial embeddings", func(t *testing.T) {
		items := []db.Item{{ID: "1"}, {ID: "2"}, {ID: "3"}}
		embeddings := map[string][]float32{
			"1": {1, 0},
			"3": {1, 0},
		} // Missing embedding for "2"
		got := s.calculateCoherence(items, embeddings)
		// Only pair (1,3) should contribute
		if got != PerfectSimilarityScore {
			t.Errorf("calculateCoherence() with partial embeddings = %v, want %v", got, PerfectSimilarityScore)
		}
	})
}

func TestSortClusterItems(t *testing.T) {
	s := &Scheduler{}

	t.Run(testNameSortsByImportance, func(t *testing.T) {
		items := []db.Item{
			{ID: "1", ImportanceScore: 0.3, Summary: "short"},
			{ID: "2", ImportanceScore: testSimilarityThresholdHigh, Summary: "medium text"},
			{ID: "3", ImportanceScore: 0.5, Summary: "a"},
		}
		s.sortClusterItems(items)

		if items[0].ID != "2" || items[1].ID != "3" || items[2].ID != "1" {
			t.Errorf("items not sorted correctly by importance: %v", items)
		}
	})

	t.Run("uses summary length as tiebreaker", func(t *testing.T) {
		items := []db.Item{
			{ID: "1", ImportanceScore: 0.5, Summary: "short"},
			{ID: "2", ImportanceScore: 0.5, Summary: "longer summary here"},
			{ID: "3", ImportanceScore: 0.5, Summary: "a"},
		}
		s.sortClusterItems(items)

		if items[0].ID != "2" || items[1].ID != "1" || items[2].ID != "3" {
			t.Errorf("items not sorted correctly by summary length: %v", items)
		}
	})

	t.Run("empty slice does not panic", func(*testing.T) {
		items := []db.Item{}
		s.sortClusterItems(items) // Should not panic
	})

	t.Run("single item unchanged", func(t *testing.T) {
		items := []db.Item{{ID: "1", ImportanceScore: 0.5}}
		s.sortClusterItems(items)

		if items[0].ID != "1" {
			t.Error("single item should remain unchanged")
		}
	})
}

func TestGetTopicIndex(t *testing.T) {
	s := &Scheduler{}

	t.Run("creates topic index", func(t *testing.T) {
		items := []db.Item{
			{ID: "1", Topic: "technology"},
			{ID: "2", Topic: "FINANCE"},
			{ID: "3", Topic: ""},
		}
		idx := s.getTopicIndex(items)

		if idx["1"] != testTopicTechnology {
			t.Errorf("topic for item 1 = %q, want %q", idx["1"], testTopicTechnology)
		}

		if idx["2"] != testTopicFinance {
			t.Errorf("topic for item 2 = %q, want %q", idx["2"], testTopicFinance)
		}

		if idx["3"] != DefaultTopic {
			t.Errorf("topic for item 3 = %q, want %q", idx["3"], DefaultTopic)
		}
	})
}

func TestGetTopicGroups(t *testing.T) {
	s := &Scheduler{}

	t.Run("groups items by normalized topic", func(t *testing.T) {
		items := []db.Item{
			{ID: "1", Topic: "technology"},
			{ID: "2", Topic: testTopicTechnology},
			{ID: "3", Topic: "FINANCE"},
		}
		groups := s.getTopicGroups(items)

		if len(groups[testTopicTechnology]) != 2 {
			t.Errorf("expected 2 items in %s group, got %d", testTopicTechnology, len(groups[testTopicTechnology]))
		}

		if len(groups[testTopicFinance]) != 1 {
			t.Errorf(testErrExpected1ItemFinance, len(groups[testTopicFinance]))
		}
	})

	t.Run("empty items returns empty map", func(t *testing.T) {
		groups := s.getTopicGroups([]db.Item{})
		if len(groups) != 0 {
			t.Errorf("expected empty map for empty items, got %d groups", len(groups))
		}
	})
}

func TestLimitClusterItems(t *testing.T) {
	s := &Scheduler{}
	logger := zerolog.Nop()

	t.Run("items under limit unchanged", func(t *testing.T) {
		items := make([]db.Item, 100)
		for i := range items {
			items[i] = db.Item{ID: string(rune(i))}
		}

		result := s.limitClusterItems(items, &logger)
		if len(result) != 100 {
			t.Errorf("expected 100 items, got %d", len(result))
		}
	})

	t.Run("items at limit unchanged", func(t *testing.T) {
		items := make([]db.Item, ClusterMaxItemsLimit)
		for i := range items {
			items[i] = db.Item{ID: string(rune(i))}
		}

		result := s.limitClusterItems(items, &logger)
		if len(result) != ClusterMaxItemsLimit {
			t.Errorf(testErrExpectedItemsCountGot, ClusterMaxItemsLimit, len(result))
		}
	})

	t.Run("items over limit truncated", func(t *testing.T) {
		items := make([]db.Item, ClusterMaxItemsLimit+100)
		for i := range items {
			items[i] = db.Item{ID: string(rune(i))}
		}

		result := s.limitClusterItems(items, &logger)
		if len(result) != ClusterMaxItemsLimit {
			t.Errorf(testErrExpectedItemsCountGot, ClusterMaxItemsLimit, len(result))
		}
	})

	t.Run("empty items unchanged", func(t *testing.T) {
		result := s.limitClusterItems([]db.Item{}, &logger)
		if len(result) != 0 {
			t.Errorf(testErrExpected0Items, len(result))
		}
	})
}

func TestFindClusterItems(t *testing.T) {
	s := &Scheduler{}

	t.Run("single item without embedding", func(t *testing.T) {
		itemA := db.Item{ID: "1"}
		groupItems := []db.Item{itemA}
		assigned := make(map[string]bool)
		embeddings := map[string][]float32{} // No embeddings
		topicIndex := map[string]string{"1": testTopicTech}
		cfg := clusteringConfig{
			similarityThreshold: testSimilarityThresholdDefault,
		}

		result := s.findClusterItems(itemA, groupItems, groupItems, assigned, embeddings, topicIndex, nil, cfg)
		if len(result) != 1 {
			t.Errorf("expected 1 item (only anchor), got %d", len(result))
		}
	})

	t.Run("clusters similar items", func(t *testing.T) {
		itemA := db.Item{ID: "1", TGDate: time.Now()}
		itemB := db.Item{ID: "2", TGDate: time.Now()}
		groupItems := []db.Item{itemA, itemB}
		assigned := make(map[string]bool)
		// Identical embeddings = similarity 1.0
		embeddings := map[string][]float32{
			"1": {1, 0, 0},
			"2": {1, 0, 0},
		}
		topicIndex := map[string]string{"1": testTopicTech, "2": testTopicTech}
		cfg := clusteringConfig{
			similarityThreshold: testSimilarityThresholdDefault,
			clusterWindow:       24 * time.Hour,
		}

		result := s.findClusterItems(itemA, groupItems, groupItems, assigned, embeddings, topicIndex, nil, cfg)
		if len(result) != 2 {
			t.Errorf("expected 2 items in cluster, got %d", len(result))
		}
	})

	t.Run("does not cluster dissimilar items", func(t *testing.T) {
		itemA := db.Item{ID: "1", TGDate: time.Now()}
		itemB := db.Item{ID: "2", TGDate: time.Now()}
		groupItems := []db.Item{itemA, itemB}
		assigned := make(map[string]bool)
		// Orthogonal embeddings = similarity 0
		embeddings := map[string][]float32{
			"1": {1, 0, 0},
			"2": {0, 1, 0},
		}
		topicIndex := map[string]string{"1": testTopicTech, "2": testTopicTech}
		cfg := clusteringConfig{
			similarityThreshold: testSimilarityThresholdDefault,
			clusterWindow:       24 * time.Hour,
		}

		result := s.findClusterItems(itemA, groupItems, groupItems, assigned, embeddings, topicIndex, nil, cfg)
		if len(result) != 1 {
			t.Errorf("expected 1 item (dissimilar should not cluster), got %d", len(result))
		}
	})
}

func TestShouldAddToCluster(t *testing.T) {
	s := &Scheduler{}

	t.Run("skips already assigned", func(t *testing.T) {
		itemA := db.Item{ID: "1"}
		itemB := db.Item{ID: "2"}
		assigned := map[string]bool{"2": true}
		embeddings := map[string][]float32{}
		topicIndex := map[string]string{"1": testTopicTech, "2": testTopicTech}
		cfg := clusteringConfig{}
		embA := []float32{1, 0}

		ok := s.shouldAddToCluster(itemA, itemB, testTopicTech, assigned, embeddings, topicIndex, nil, cfg, embA)
		if ok {
			t.Error("shouldAddToCluster should return false for already assigned items")
		}
	})

	t.Run("skips same item", func(t *testing.T) {
		itemA := db.Item{ID: "1"}
		itemB := db.Item{ID: "1"}
		assigned := make(map[string]bool)
		embeddings := map[string][]float32{}
		topicIndex := map[string]string{"1": testTopicTech}
		cfg := clusteringConfig{}
		embA := []float32{1, 0}

		ok := s.shouldAddToCluster(itemA, itemB, testTopicTech, assigned, embeddings, topicIndex, nil, cfg, embA)
		if ok {
			t.Error("shouldAddToCluster should return false for same item")
		}
	})

	t.Run("skips without embeddings", func(t *testing.T) {
		itemA := db.Item{ID: "1"}
		itemB := db.Item{ID: "2"}
		assigned := make(map[string]bool)
		embeddings := map[string][]float32{
			"1": {1, 0},
			// No embedding for "2"
		}
		topicIndex := map[string]string{"1": testTopicTech, "2": testTopicTech}
		cfg := clusteringConfig{similarityThreshold: testSimilarityThresholdDefault}
		embA := []float32{1, 0}

		ok := s.shouldAddToCluster(itemA, itemB, testTopicTech, assigned, embeddings, topicIndex, nil, cfg, embA)
		if ok {
			t.Error("shouldAddToCluster should return false without embedding")
		}
	})
}

func TestApplyClusteringDefaults(t *testing.T) {
	s := &Scheduler{
		cfg: &config.Config{
			SimilarityThreshold: 0.85,
		},
	}

	t.Run("applies defaults when values are zero", func(t *testing.T) {
		cfg := &clusteringConfig{
			similarityThreshold: 0,
			crossTopicThreshold: 0,
			coherenceThreshold:  0,
		}

		s.applyClusteringDefaults(cfg)

		// Use approximate comparison for float values
		const epsilon = 0.001
		if cfg.similarityThreshold < 0.85-epsilon || cfg.similarityThreshold > 0.85+epsilon {
			t.Errorf("similarityThreshold = %v, want ~0.85", cfg.similarityThreshold)
		}

		if cfg.crossTopicThreshold < 0.85-epsilon || cfg.crossTopicThreshold > 0.85+epsilon {
			t.Errorf("crossTopicThreshold = %v, want ~0.85 (same as similarity)", cfg.crossTopicThreshold)
		}

		if cfg.coherenceThreshold != float32(ClusterDefaultCoherenceThreshold) {
			t.Errorf("coherenceThreshold = %v, want %v", cfg.coherenceThreshold, ClusterDefaultCoherenceThreshold)
		}
	})

	t.Run("preserves non-zero values", func(t *testing.T) {
		cfg := &clusteringConfig{
			similarityThreshold: testSimilarityThresholdHigh,
			crossTopicThreshold: 0.95,
			coherenceThreshold:  ClusterDefaultCoherenceThreshold,
		}

		s.applyClusteringDefaults(cfg)

		if cfg.similarityThreshold != testSimilarityThresholdHigh {
			t.Errorf("similarityThreshold changed unexpectedly to %v", cfg.similarityThreshold)
		}

		if cfg.crossTopicThreshold != 0.95 {
			t.Errorf("crossTopicThreshold changed unexpectedly to %v", cfg.crossTopicThreshold)
		}

		if cfg.coherenceThreshold != ClusterDefaultCoherenceThreshold {
			t.Errorf("coherenceThreshold changed unexpectedly to %v", cfg.coherenceThreshold)
		}
	})
}

func TestValidateClusterCoherence(t *testing.T) {
	s := &Scheduler{}
	logger := zerolog.Nop()

	t.Run("accepts cluster with high coherence", func(t *testing.T) {
		items := []db.Item{
			{ID: "1"},
			{ID: "2"},
			{ID: "3"},
		}
		bc := &clusterBuildContext{
			embeddings: map[string][]float32{
				"1": {1, 0},
				"2": {1, 0}, // identical
				"3": {1, 0},
			},
			cfg: clusteringConfig{coherenceThreshold: 0.5},
		}

		result := s.validateClusterCoherence(items, bc, &logger)
		if len(result) != 3 {
			t.Errorf("expected 3 items (high coherence), got %d", len(result))
		}
	})

	t.Run("rejects cluster with low coherence", func(t *testing.T) {
		items := []db.Item{
			{ID: "1"},
			{ID: "2"},
			{ID: "3"},
		}
		bc := &clusterBuildContext{
			embeddings: map[string][]float32{
				"1": {1, 0, 0},
				"2": {0, 1, 0}, // orthogonal
				"3": {0, 0, 1}, // orthogonal
			},
			assigned: make(map[string]bool),
			cfg:      clusteringConfig{coherenceThreshold: testSimilarityThresholdHigh},
		}

		result := s.validateClusterCoherence(items, bc, &logger)
		if len(result) != 1 {
			t.Errorf("expected 1 item (low coherence rejected), got %d", len(result))
		}
	})

	t.Run("skips coherence check for small clusters", func(t *testing.T) {
		items := []db.Item{
			{ID: "1"},
			{ID: "2"},
		}
		bc := &clusterBuildContext{
			embeddings: map[string][]float32{
				"1": {1, 0},
				"2": {0, 1}, // orthogonal but only 2 items
			},
			cfg: clusteringConfig{coherenceThreshold: testSimilarityThresholdHigh},
		}

		result := s.validateClusterCoherence(items, bc, &logger)
		// Coherence check is only for len > 2
		if len(result) != 2 {
			t.Errorf("expected 2 items (small cluster), got %d", len(result))
		}
	})
}

func TestCollectSourceLinks(t *testing.T) {
	s := &Scheduler{}
	rc := &digestRenderContext{scheduler: s}

	t.Run("collects links for multiple items", func(t *testing.T) {
		items := []db.Item{
			{SourceChannel: "ch1", SourceMsgID: 1},
			{SourceChannel: "ch2", SourceMsgID: 2},
		}
		links := rc.collectSourceLinks(items)

		if len(links) != 2 {
			t.Errorf("expected 2 links, got %d", len(links))
		}
	})

	t.Run("uses channel title when no username", func(t *testing.T) {
		items := []db.Item{
			{SourceChannel: "", SourceChannelTitle: "My Channel", SourceMsgID: 1, SourceChannelID: 123},
		}
		links := rc.collectSourceLinks(items)

		if !strings.Contains(links[0], "My Channel") {
			t.Errorf("link should use channel title, got %q", links[0])
		}
	})

	t.Run("uses default label when no channel info", func(t *testing.T) {
		items := []db.Item{
			{SourceMsgID: 1, SourceChannelID: 123},
		}
		links := rc.collectSourceLinks(items)

		if !strings.Contains(links[0], DefaultSourceLabel) {
			t.Errorf("link should use default label, got %q", links[0])
		}
	})
}

func TestClusteringConfigStruct(t *testing.T) {
	cfg := clusteringConfig{
		similarityThreshold: testSimilarityThresholdDefault,
		crossTopicEnabled:   true,
		crossTopicThreshold: testSimilarityThresholdHigh,
		coherenceThreshold:  ClusterDefaultCoherenceThreshold,
		clusterWindow:       2 * time.Hour,
		digestLanguage:      "en",
	}

	if cfg.similarityThreshold != testSimilarityThresholdDefault {
		t.Errorf("similarityThreshold = %v, want %v", cfg.similarityThreshold, testSimilarityThresholdDefault)
	}

	if !cfg.crossTopicEnabled {
		t.Error("crossTopicEnabled should be true")
	}

	if cfg.clusterWindow != 2*time.Hour {
		t.Errorf("clusterWindow = %v, want 2h", cfg.clusterWindow)
	}
}

func TestClusterBuildContextStruct(t *testing.T) {
	bc := &clusterBuildContext{
		topicIndex:  map[string]string{"1": testTopicTech},
		topicGroups: map[string][]db.Item{testTopicTech: {{ID: "1"}}},
		embeddings:  map[string][]float32{"1": {1, 0}},
		assigned:    map[string]bool{"1": false},
		allItems:    []db.Item{{ID: "1"}},
		cfg:         clusteringConfig{similarityThreshold: testSimilarityThresholdDefault},
	}

	if len(bc.topicIndex) != 1 {
		t.Errorf("topicIndex length = %d, want 1", len(bc.topicIndex))
	}

	if bc.topicIndex["1"] != testTopicTech {
		t.Errorf("topicIndex[1] = %q, want %q", bc.topicIndex["1"], testTopicTech)
	}
}

func TestNormalizeClusterTopicSpecialCases(t *testing.T) {
	tests := []struct {
		name  string
		topic string
		want  string
	}{
		{name: "tabs and spaces", topic: "\t  tech  \t", want: testTopicTech},
		{name: "newlines", topic: "tech\n", want: testTopicTech},
		{name: "multiple words", topic: "world news today", want: "World News Today"},
		{name: "numbers in topic", topic: "tech2025", want: "Tech2025"},
		{name: "single letter", topic: "a", want: "A"},
		{name: "special characters", topic: "tech & finance", want: "Tech & Finance"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeClusterTopic(tt.topic)
			if got != tt.want {
				t.Errorf(testErrNormalizeClusterTopic, tt.topic, got, tt.want)
			}
		})
	}
}

func TestWithinClusterWindowEdgeCases(t *testing.T) {
	now := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
	hour := time.Hour

	t.Run("microsecond difference within window", func(t *testing.T) {
		a := now

		b := now.Add(time.Microsecond)
		if !withinClusterWindow(a, b, hour) {
			t.Error("microsecond difference should be within window")
		}
	})

	t.Run("exactly one nanosecond over", func(t *testing.T) {
		a := now

		b := now.Add(hour + time.Nanosecond)
		if withinClusterWindow(a, b, hour) {
			t.Error("one nanosecond over should be outside window")
		}
	})

	t.Run("zero window duration", func(t *testing.T) {
		a := now

		b := now
		if !withinClusterWindow(a, b, 0) {
			t.Error("same time with zero window should be within")
		}
	})
}

func TestCalculateCoherenceWithVariousPatterns(t *testing.T) {
	s := &Scheduler{}

	t.Run("ascending similarity pattern", func(t *testing.T) {
		items := []db.Item{{ID: "1"}, {ID: "2"}, {ID: "3"}}
		embeddings := map[string][]float32{
			"1": {1, 0, 0, 0},
			"2": {0.9, 0.1, 0, 0},
			"3": {0.8, 0.2, 0, 0},
		}

		coherence := s.calculateCoherence(items, embeddings)
		// All items are fairly similar, coherence should be high
		if coherence < ClusterDefaultCoherenceThreshold {
			t.Errorf("coherence = %v, expected > %v for similar items", coherence, ClusterDefaultCoherenceThreshold)
		}
	})

	t.Run("one outlier reduces coherence", func(t *testing.T) {
		items := []db.Item{{ID: "1"}, {ID: "2"}, {ID: "3"}}
		embeddings := map[string][]float32{
			"1": {1, 0, 0, 0},
			"2": {1, 0, 0, 0},
			"3": {0, 1, 0, 0}, // Orthogonal outlier
		}

		coherence := s.calculateCoherence(items, embeddings)
		// One orthogonal item should reduce coherence
		if coherence > ClusterDefaultCoherenceThreshold {
			t.Errorf("coherence = %v, expected < %v with outlier", coherence, ClusterDefaultCoherenceThreshold)
		}
	})
}

func TestSortClusterItemsComplexCases(t *testing.T) {
	s := &Scheduler{}

	t.Run("many items with same importance", func(t *testing.T) {
		items := []db.Item{
			{ID: "1", ImportanceScore: 0.5, Summary: "a"},
			{ID: "2", ImportanceScore: 0.5, Summary: "abc"},
			{ID: "3", ImportanceScore: 0.5, Summary: "abcdef"},
			{ID: "4", ImportanceScore: 0.5, Summary: "ab"},
		}
		s.sortClusterItems(items)

		// Should be sorted by summary length (descending)
		if items[0].ID != "3" {
			t.Errorf("longest summary should be first, got %s", items[0].ID)
		}

		if items[3].ID != "1" {
			t.Errorf("shortest summary should be last, got %s", items[3].ID)
		}
	})

	t.Run("mixed importance and summary length", func(t *testing.T) {
		items := []db.Item{
			{ID: "1", ImportanceScore: 0.5, Summary: "very long summary text"},
			{ID: "2", ImportanceScore: testSimilarityThresholdHigh, Summary: "short"},
		}
		s.sortClusterItems(items)

		// Higher importance should come first regardless of summary length
		if items[0].ID != "2" {
			t.Errorf("higher importance should be first, got %s", items[0].ID)
		}
	})
}

func TestGetTopicIndexWithVariedTopics(t *testing.T) {
	s := &Scheduler{}

	items := []db.Item{
		{ID: "1", Topic: "technology"},
		{ID: "2", Topic: "TECHNOLOGY"},
		{ID: "3", Topic: testTopicTechnology},
		{ID: "4", Topic: "finance"},
		{ID: "5", Topic: ""},
	}

	idx := s.getTopicIndex(items)

	// All "technology" variants should normalize to same value
	if idx["1"] != idx["2"] || idx["2"] != idx["3"] {
		t.Error("technology variants should normalize to same value")
	}

	// Empty topic should get default
	if idx["5"] != DefaultTopic {
		t.Errorf("empty topic should be %q, got %q", DefaultTopic, idx["5"])
	}
}

func TestGetTopicGroupsWithMixedTopics(t *testing.T) {
	s := &Scheduler{}

	items := []db.Item{
		{ID: "1", Topic: "tech"},
		{ID: "2", Topic: testTopicTech},
		{ID: "3", Topic: "TECH"},
		{ID: "4", Topic: "finance"},
		{ID: "5", Topic: ""},
	}

	groups := s.getTopicGroups(items)

	// All tech variants should be in one group
	if len(groups[testTopicTech]) != 3 {
		t.Errorf("expected 3 items in Tech group, got %d", len(groups[testTopicTech]))
	}

	// Finance should have 1
	if len(groups[testTopicFinance]) != 1 {
		t.Errorf(testErrExpected1ItemFinance, len(groups[testTopicFinance]))
	}

	// Empty topic should get default
	if len(groups[DefaultTopic]) != 1 {
		t.Errorf("expected 1 item in %s group, got %d", DefaultTopic, len(groups[DefaultTopic]))
	}
}

func TestFindClusterItemsCrossTopicEnabled(t *testing.T) {
	s := &Scheduler{}

	itemA := db.Item{ID: "1", TGDate: time.Now()}
	itemB := db.Item{ID: "2", TGDate: time.Now()}
	itemC := db.Item{ID: "3", TGDate: time.Now()}
	groupItems := []db.Item{itemA}
	allItems := []db.Item{itemA, itemB, itemC}
	assigned := make(map[string]bool)

	// Similar embeddings
	embeddings := map[string][]float32{
		"1": {1, 0, 0},
		"2": {0.95, 0.05, 0}, // Similar to 1
		"3": {0, 1, 0},       // Orthogonal
	}
	topicIndex := map[string]string{
		"1": testTopicTech,
		"2": testTopicFinance, // Different topic
		"3": "Politics",
	}

	cfg := clusteringConfig{
		similarityThreshold: testSimilarityThresholdDefault,
		crossTopicEnabled:   true,
		crossTopicThreshold: testSimilarityThresholdHigh,
		clusterWindow:       24 * time.Hour,
	}

	result := s.findClusterItems(itemA, groupItems, allItems, assigned, embeddings, topicIndex, nil, cfg)

	// Should include item 2 despite different topic because similarity > crossTopicThreshold
	if len(result) != 2 {
		t.Errorf("expected 2 items (cross-topic enabled), got %d", len(result))
	}
}

func TestFindClusterItemsCrossTopicDisabled(t *testing.T) {
	s := &Scheduler{}

	itemA := db.Item{ID: "1", TGDate: time.Now()}
	itemB := db.Item{ID: "2", TGDate: time.Now()}
	groupItems := []db.Item{itemA, itemB}
	assigned := make(map[string]bool)

	embeddings := map[string][]float32{
		"1": {1, 0, 0},
		"2": {1, 0, 0}, // Identical
	}
	topicIndex := map[string]string{
		"1": testTopicTech,
		"2": testTopicFinance, // Different topic
	}

	cfg := clusteringConfig{
		similarityThreshold: testSimilarityThresholdDefault,
		crossTopicEnabled:   false,
		clusterWindow:       24 * time.Hour,
	}

	result := s.findClusterItems(itemA, groupItems, groupItems, assigned, embeddings, topicIndex, nil, cfg)

	// Should only include item 1 because cross-topic is disabled
	if len(result) != 1 {
		t.Errorf("expected 1 item (cross-topic disabled), got %d", len(result))
	}
}

func TestShouldAddToClusterTimeWindow(t *testing.T) {
	s := &Scheduler{}

	now := time.Now()
	itemA := db.Item{ID: "1", TGDate: now}
	itemB := db.Item{ID: "2", TGDate: now.Add(3 * time.Hour)}
	assigned := make(map[string]bool)
	embeddings := map[string][]float32{
		"1": {1, 0},
		"2": {1, 0},
	}
	topicIndex := map[string]string{"1": testTopicTech, "2": testTopicTech}
	embA := []float32{1, 0}

	t.Run("within time window", func(t *testing.T) {
		cfg := clusteringConfig{
			similarityThreshold: testSimilarityThresholdDefault,
			clusterWindow:       5 * time.Hour,
		}

		ok := s.shouldAddToCluster(itemA, itemB, testTopicTech, assigned, embeddings, topicIndex, nil, cfg, embA)
		if !ok {
			t.Error("should add item within time window")
		}
	})

	t.Run("outside time window", func(t *testing.T) {
		cfg := clusteringConfig{
			similarityThreshold: testSimilarityThresholdDefault,
			clusterWindow:       1 * time.Hour,
		}

		ok := s.shouldAddToCluster(itemA, itemB, testTopicTech, assigned, embeddings, topicIndex, nil, cfg, embA)
		if ok {
			t.Error("should not add item outside time window")
		}
	})

	t.Run("zero time window ignores time check", func(t *testing.T) {
		cfg := clusteringConfig{
			similarityThreshold: testSimilarityThresholdDefault,
			clusterWindow:       0,
		}

		ok := s.shouldAddToCluster(itemA, itemB, testTopicTech, assigned, embeddings, topicIndex, nil, cfg, embA)
		if !ok {
			t.Error("should add item when time window is 0")
		}
	})
}

func TestApplyClusteringDefaultsPartial(t *testing.T) {
	s := &Scheduler{
		cfg: &config.Config{
			SimilarityThreshold: 0.85,
		},
	}

	t.Run("only similarity set", func(t *testing.T) {
		cfg := &clusteringConfig{
			similarityThreshold: testSimilarityThresholdHigh, // Set
			crossTopicThreshold: 0,                           // Not set
			coherenceThreshold:  0,                           // Not set
		}

		s.applyClusteringDefaults(cfg)

		if cfg.similarityThreshold != testSimilarityThresholdHigh {
			t.Error("should preserve set similarity threshold")
		}

		if cfg.crossTopicThreshold != testSimilarityThresholdHigh {
			t.Error("crossTopicThreshold should equal similarity when not set")
		}

		if cfg.coherenceThreshold != float32(ClusterDefaultCoherenceThreshold) {
			t.Error("should use default coherence threshold")
		}
	})
}

func TestValidateClusterCoherenceSingleItem(t *testing.T) {
	s := &Scheduler{}
	logger := zerolog.Nop()

	items := []db.Item{{ID: "1"}}
	bc := &clusterBuildContext{
		embeddings: map[string][]float32{"1": {1, 0}},
		cfg:        clusteringConfig{coherenceThreshold: testSimilarityThresholdHigh},
	}

	result := s.validateClusterCoherence(items, bc, &logger)

	if len(result) != 1 {
		t.Errorf("single item should return as-is, got %d items", len(result))
	}
}

func TestValidateClusterCoherenceTwoItems(t *testing.T) {
	s := &Scheduler{}
	logger := zerolog.Nop()

	items := []db.Item{{ID: "1"}, {ID: "2"}}
	bc := &clusterBuildContext{
		embeddings: map[string][]float32{
			"1": {1, 0},
			"2": {0, 1}, // Orthogonal = low coherence
		},
		assigned: make(map[string]bool),
		cfg:      clusteringConfig{coherenceThreshold: testSimilarityThresholdHigh},
	}

	result := s.validateClusterCoherence(items, bc, &logger)

	// Coherence check only applies when len > 2
	if len(result) != 2 {
		t.Errorf("two items should not trigger coherence check, got %d items", len(result))
	}
}

func TestCollectSourceLinksVariedChannels(t *testing.T) {
	s := &Scheduler{}
	rc := &digestRenderContext{scheduler: s}

	items := []db.Item{
		{SourceChannel: "channel1", SourceMsgID: 1},
		{SourceChannel: "", SourceChannelTitle: testLabelPrivateChannel, SourceMsgID: 2, SourceChannelID: 12345},
		{SourceChannel: "", SourceChannelTitle: "", SourceMsgID: 3, SourceChannelID: 67890},
	}

	links := rc.collectSourceLinks(items)

	if len(links) != 3 {
		t.Fatalf(testErrExpected3Links, len(links))
	}

	// First should use @channel
	if !strings.Contains(links[0], "@channel1") {
		t.Errorf("first link should contain @channel1, got %q", links[0])
	}

	// Second should use title
	if !strings.Contains(links[1], testLabelPrivateChannel) {
		t.Errorf("second link should contain %q, got %q", testLabelPrivateChannel, links[1])
	}

	// Third should use default
	if !strings.Contains(links[2], DefaultSourceLabel) {
		t.Errorf("third link should contain default label, got %q", links[2])
	}
}

func TestCalculateEvidenceBoost(t *testing.T) {
	cfg := clusteringConfig{
		evidenceEnabled:      true,
		evidenceBoost:        0.15,
		evidenceMinAgreement: testMinAgreementDefault,
	}

	t.Run("no evidence for either item", func(t *testing.T) {
		evidenceMap := map[string][]db.ItemEvidenceWithSource{}

		boost := calculateEvidenceBoost(testItemID1, testItemID2, evidenceMap, cfg)
		if boost != 0 {
			t.Errorf("expected 0 boost for empty evidence, got %f", boost)
		}
	})

	t.Run("no shared evidence", func(t *testing.T) {
		evidenceMap := map[string][]db.ItemEvidenceWithSource{
			testItemID1: {
				{ItemEvidence: db.ItemEvidence{AgreementScore: 0.7}, Source: db.EvidenceSource{URL: "http://a.com"}},
			},
			testItemID2: {
				{ItemEvidence: db.ItemEvidence{AgreementScore: 0.8}, Source: db.EvidenceSource{URL: "http://b.com"}},
			},
		}

		boost := calculateEvidenceBoost(testItemID1, testItemID2, evidenceMap, cfg)
		if boost != 0 {
			t.Errorf("expected 0 boost for no shared evidence, got %f", boost)
		}
	})

	t.Run("shared evidence with high agreement", func(t *testing.T) {
		evidenceMap := map[string][]db.ItemEvidenceWithSource{
			testItemID1: {
				{ItemEvidence: db.ItemEvidence{AgreementScore: 0.8}, Source: db.EvidenceSource{URL: "http://shared.com"}},
			},
			testItemID2: {
				{ItemEvidence: db.ItemEvidence{AgreementScore: 0.9}, Source: db.EvidenceSource{URL: "http://shared.com"}},
			},
		}

		boost := calculateEvidenceBoost(testItemID1, testItemID2, evidenceMap, cfg)

		// Min agreement is 0.8, boost is 0.8 * 0.15 = 0.12
		expectedBoost := float32(0.8 * 0.15)
		tolerance := float32(0.001)

		if boost < expectedBoost-tolerance || boost > expectedBoost+tolerance {
			t.Errorf("expected boost ~%f, got %f", expectedBoost, boost)
		}
	})

	t.Run("evidence below min agreement threshold", func(t *testing.T) {
		evidenceMap := map[string][]db.ItemEvidenceWithSource{
			testItemID1: {
				{ItemEvidence: db.ItemEvidence{AgreementScore: 0.3}, Source: db.EvidenceSource{URL: "http://shared.com"}},
			},
			testItemID2: {
				{ItemEvidence: db.ItemEvidence{AgreementScore: 0.4}, Source: db.EvidenceSource{URL: "http://shared.com"}},
			},
		}

		boost := calculateEvidenceBoost(testItemID1, testItemID2, evidenceMap, cfg)
		if boost != 0 {
			t.Errorf("expected 0 boost for low agreement evidence, got %f", boost)
		}
	})
}

func TestBuildEvidenceURLMap(t *testing.T) {
	minAgreement := float32(testMinAgreementDefault)

	t.Run("filters by min agreement", func(t *testing.T) {
		evidence := []db.ItemEvidenceWithSource{
			{ItemEvidence: db.ItemEvidence{AgreementScore: 0.3}, Source: db.EvidenceSource{URL: "http://low.com"}},
			{ItemEvidence: db.ItemEvidence{AgreementScore: 0.7}, Source: db.EvidenceSource{URL: "http://high.com"}},
		}

		urlMap := buildEvidenceURLMap(evidence, minAgreement)

		if len(urlMap) != 1 {
			t.Errorf("expected 1 URL, got %d", len(urlMap))
		}

		if _, ok := urlMap["http://high.com"]; !ok {
			t.Error("expected http://high.com in URL map")
		}
	})
}

func TestCalculateBoostedSimilarity(t *testing.T) {
	cfg := clusteringConfig{
		evidenceEnabled:      true,
		evidenceBoost:        0.15,
		evidenceMinAgreement: testMinAgreementDefault,
	}

	t.Run("no evidence returns base similarity", func(t *testing.T) {
		embA := []float32{1, 0, 0}
		embB := []float32{1, 0, 0}

		similarity := calculateBoostedSimilarity(testItemID1, testItemID2, embA, embB, nil, cfg)

		// Identical embeddings should have similarity 1.0
		if similarity != 1.0 {
			t.Errorf("expected similarity 1.0 for identical embeddings, got %f", similarity)
		}
	})

	t.Run("with shared evidence adds boost", func(t *testing.T) {
		embA := []float32{1, 0, 0}
		embB := []float32{0.8, 0.6, 0} // ~0.8 cosine similarity
		evidenceMap := map[string][]db.ItemEvidenceWithSource{
			testItemID1: {
				{ItemEvidence: db.ItemEvidence{AgreementScore: 0.8}, Source: db.EvidenceSource{URL: "http://shared.com"}},
			},
			testItemID2: {
				{ItemEvidence: db.ItemEvidence{AgreementScore: 0.8}, Source: db.EvidenceSource{URL: "http://shared.com"}},
			},
		}

		similarity := calculateBoostedSimilarity(testItemID1, testItemID2, embA, embB, evidenceMap, cfg)

		// Base similarity is ~0.8, boost is 0.8 * 0.15 = 0.12
		if similarity <= 0.8 {
			t.Errorf("expected boosted similarity > 0.8, got %f", similarity)
		}
	})
}
