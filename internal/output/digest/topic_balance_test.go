package digest

import (
	"math"
	"testing"
	"time"

	"github.com/lueurxax/telegram-digest-bot/internal/storage"
)

const (
	testErrExpectedItemsCountGot = "expected %d items, got %d"

	testErrExpected2Selected = "expected 2 selected, got %d"
)

func TestApplyFreshnessDecayFloor(t *testing.T) {
	now := time.Now()

	score := applyFreshnessDecay(testScoreFull, now.Add(-36*time.Hour), 36, AutoWeightInclusionFactor)

	if math.Abs(float64(score-AutoWeightInclusionFactor)) > 0.01 {
		t.Fatalf("applyFreshnessDecay floor = %v, want ~%v", score, AutoWeightInclusionFactor)
	}
}

func TestApplyTopicBalanceCapAndMin(t *testing.T) {
	items := []db.Item{
		{Topic: "A"},
		{Topic: "A"},
		{Topic: "A"},
		{Topic: "B"},
		{Topic: "B"},
		{Topic: "C"},
		{Topic: "C"},
	}

	result := applyTopicBalance(items, 5, AutoWeightInclusionFactor, 3)

	if len(result.Items) != 5 {
		t.Fatalf(testErrExpectedItemsCountGot, 5, len(result.Items))
	}

	counts := make(map[string]int)

	for _, item := range result.Items {
		key, _ := topicKey(item.Topic)
		counts[key]++
	}

	if counts["a"] > 2 || counts["b"] > 2 || counts["c"] > 2 {
		t.Fatalf("topic cap violated: %v", counts)
	}

	if result.TopicsSelected != 3 {
		t.Fatalf("expected 3 topics selected, got %d", result.TopicsSelected)
	}
}

func TestApplyTopicBalanceRelaxesCap(t *testing.T) {
	items := []db.Item{
		{Topic: "A"},
		{Topic: "A"},
		{Topic: "A"},
		{Topic: "B"},
	}

	result := applyTopicBalance(items, 4, 0.25, 0)

	if len(result.Items) != 4 {
		t.Fatalf(testErrExpectedItemsCountGot, 4, len(result.Items))
	}

	if !result.Relaxed {
		t.Fatalf("expected relaxed cap when topics are insufficient")
	}
}

func TestApplyFreshnessDecay(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name       string
		score      float32
		tgDate     time.Time
		decayHours int
		floor      float32
		wantMin    float32
		wantMax    float32
	}{
		{
			name:       "zero decay hours returns original",
			score:      0.8,
			tgDate:     now.Add(-10 * time.Hour),
			decayHours: 0,
			floor:      0.0,
			wantMin:    0.79,
			wantMax:    0.81,
		},
		{
			name:       "negative decay hours returns original",
			score:      0.8,
			tgDate:     now.Add(-10 * time.Hour),
			decayHours: -1,
			floor:      0.0,
			wantMin:    0.79,
			wantMax:    0.81,
		},
		{
			name:       "recent time has high score",
			score:      testScoreFull,
			tgDate:     now,
			decayHours: 24,
			floor:      0.0,
			wantMin:    0.95,
			wantMax:    testScoreFull,
		},
		{
			name:       "old time decays toward floor",
			score:      testScoreFull,
			tgDate:     now.Add(-48 * time.Hour),
			decayHours: 24,
			floor:      0.3,
			wantMin:    0.29,
			wantMax:    0.31,
		},
		{
			name:       "floor clamped to 0",
			score:      testScoreFull,
			tgDate:     now.Add(-100 * time.Hour),
			decayHours: 10,
			floor:      -0.5,
			wantMin:    0.0,
			wantMax:    0.01,
		},
		{
			name:       "floor clamped to 1",
			score:      testScoreFull,
			tgDate:     now.Add(-100 * time.Hour),
			decayHours: 10,
			floor:      1.5,
			wantMin:    0.99,
			wantMax:    1.01,
		},
		{
			name:       "future date treated as now",
			score:      testScoreFull,
			tgDate:     now.Add(10 * time.Hour),
			decayHours: 24,
			floor:      0.0,
			wantMin:    0.95,
			wantMax:    testScoreFull,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := applyFreshnessDecay(tt.score, tt.tgDate, tt.decayHours, tt.floor)

			if got < tt.wantMin || got > tt.wantMax {
				t.Errorf("applyFreshnessDecay() = %v, want between %v and %v", got, tt.wantMin, tt.wantMax)
			}
		})
	}
}

func TestTopicKey(t *testing.T) {
	tests := []struct {
		name         string
		topic        string
		wantKey      string
		wantEligible bool
	}{
		{name: "normal topic", topic: "Technology", wantKey: "technology", wantEligible: true},
		{name: "uppercase", topic: "FINANCE", wantKey: "finance", wantEligible: true},
		{name: "with spaces", topic: "  World News  ", wantKey: "world news", wantEligible: true},
		{name: "empty string", topic: "", wantKey: unknownTopicKey, wantEligible: false},
		{name: "whitespace only", topic: "   ", wantKey: unknownTopicKey, wantEligible: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotKey, gotEligible := topicKey(tt.topic)

			if gotKey != tt.wantKey {
				t.Errorf("topicKey() key = %q, want %q", gotKey, tt.wantKey)
			}

			if gotEligible != tt.wantEligible {
				t.Errorf("topicKey() eligible = %v, want %v", gotEligible, tt.wantEligible)
			}
		})
	}
}

func TestCalculateMaxPerTopic(t *testing.T) {
	tests := []struct {
		name        string
		targetN     int
		capFraction float32
		want        int
	}{
		{name: "normal case", targetN: 10, capFraction: AutoWeightInclusionFactor, want: 4},
		{name: "rounds down", targetN: 10, capFraction: 0.35, want: 3},
		{name: "minimum is 1", targetN: 10, capFraction: 0.05, want: 1},
		{name: "zero cap", targetN: 10, capFraction: 0.0, want: 1},
		{name: "small targetN", targetN: 3, capFraction: 0.3, want: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := calculateMaxPerTopic(tt.targetN, tt.capFraction); got != tt.want {
				t.Errorf("calculateMaxPerTopic() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestClampMinTopics(t *testing.T) {
	tests := []struct {
		name      string
		minTopics int
		targetN   int
		want      int
	}{
		{name: "normal case", minTopics: 3, targetN: 10, want: 3},
		{name: "negative clamped", minTopics: -1, targetN: 10, want: 0},
		{name: "exceeds target", minTopics: 15, targetN: 10, want: 10},
		{name: "equals target", minTopics: 10, targetN: 10, want: 10},
		{name: "zero", minTopics: 0, targetN: 10, want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := clampMinTopics(tt.minTopics, tt.targetN); got != tt.want {
				t.Errorf("clampMinTopics() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMin(t *testing.T) {
	tests := []struct {
		a, b, want int
	}{
		{1, 2, 1},
		{2, 1, 1},
		{5, 5, 5},
		{0, 10, 0},
		{-1, 1, -1},
	}

	for _, tt := range tests {
		if got := min(tt.a, tt.b); got != tt.want {
			t.Errorf("min(%d, %d) = %d, want %d", tt.a, tt.b, got, tt.want)
		}
	}
}

func TestApplyTopicBalanceEdgeCases(t *testing.T) {
	t.Run("empty items", func(t *testing.T) {
		result := applyTopicBalance(nil, 5, AutoWeightInclusionFactor, 2)

		if result.Items != nil {
			t.Errorf("expected nil items for empty input")
		}
	})

	t.Run("zero topN", func(t *testing.T) {
		items := []db.Item{{Topic: "A"}}

		result := applyTopicBalance(items, 0, AutoWeightInclusionFactor, 0)

		if result.Items != nil {
			t.Errorf("expected nil items for zero topN")
		}
	})

	t.Run("invalid cap fraction uses simple selection", func(t *testing.T) {
		items := []db.Item{{Topic: "A"}, {Topic: "B"}, {Topic: "C"}}

		result := applyTopicBalance(items, 2, 0.0, 0)

		if len(result.Items) != 2 {
			t.Errorf(testErrExpectedItemsCountGot, 2, len(result.Items))
		}
	})

	t.Run("cap fraction >= 1 uses simple selection", func(t *testing.T) {
		items := []db.Item{{Topic: "A"}, {Topic: "B"}, {Topic: "C"}}

		result := applyTopicBalance(items, 2, testScoreFull, 0)

		if len(result.Items) != 2 {
			t.Errorf(testErrExpectedItemsCountGot, 2, len(result.Items))
		}
	})
}

func TestSelectInitialTopN(t *testing.T) {
	items := []db.Item{
		{Topic: "A"},
		{Topic: "B"},
		{Topic: "C"},
		{Topic: "D"},
	}

	t.Run("less than available", func(t *testing.T) {
		result := selectInitialTopN(items, 2)

		if len(result.Items) != 2 {
			t.Errorf(testErrExpectedItemsCountGot, 2, len(result.Items))
		}
	})

	t.Run("equals available", func(t *testing.T) {
		result := selectInitialTopN(items, 4)

		if len(result.Items) != 4 {
			t.Errorf(testErrExpectedItemsCountGot, 4, len(result.Items))
		}
	})

	t.Run("more than available", func(t *testing.T) {
		result := selectInitialTopN(items, 10)

		if len(result.Items) != 4 {
			t.Errorf(testErrExpectedItemsCountGot, 4, len(result.Items))
		}
	})
}

func TestGetTopicCandidates(t *testing.T) {
	items := []db.Item{
		{Topic: "A"},
		{Topic: "B"},
		{Topic: "A"}, // Duplicate, should be ignored
		{Topic: "C"},
		{Topic: ""}, // Empty, not eligible
	}

	candidates := getTopicCandidates(items)

	if len(candidates) != 3 {
		t.Errorf("expected 3 candidates, got %d", len(candidates))
	}

	// Should be sorted by index

	if candidates[0].index != 0 || candidates[1].index != 1 || candidates[2].index != 3 {
		t.Errorf("candidates not sorted by index: %v", candidates)
	}
}

func TestFillRemainingSlots(t *testing.T) {
	t.Run("fills slots respecting cap", func(t *testing.T) {
		items := []db.Item{
			{Topic: "A"},
			{Topic: "A"},
			{Topic: "A"},
			{Topic: "B"},
			{Topic: "B"},
		}

		selectedIndices := make(map[int]struct{})

		topicCounts := make(map[string]int)

		targetN := 4

		maxPerTopic := 2

		fillRemainingSlots(items, selectedIndices, topicCounts, targetN, maxPerTopic)

		if len(selectedIndices) != 4 {
			t.Errorf("expected 4 selected, got %d", len(selectedIndices))
		}

		if topicCounts["a"] > 2 {
			t.Errorf("topic A exceeded cap: %d", topicCounts["a"])
		}

		if topicCounts["b"] > 2 {
			t.Errorf("topic B exceeded cap: %d", topicCounts["b"])
		}
	})

	t.Run("no cap when maxPerTopic is -1", func(t *testing.T) {
		items := []db.Item{
			{Topic: "A"},
			{Topic: "A"},
			{Topic: "A"},
		}

		selectedIndices := make(map[int]struct{})

		topicCounts := make(map[string]int)

		targetN := 3

		maxPerTopic := -1 // No cap

		fillRemainingSlots(items, selectedIndices, topicCounts, targetN, maxPerTopic)

		if len(selectedIndices) != 3 {
			t.Errorf("expected 3 selected, got %d", len(selectedIndices))
		}

		if topicCounts["a"] != 3 {
			t.Errorf("expected all 3 items selected, got %d", topicCounts["a"])
		}
	})

	t.Run("skips already selected indices", func(t *testing.T) {
		items := []db.Item{
			{Topic: "A"},
			{Topic: "B"},
			{Topic: "C"},
		}

		selectedIndices := map[int]struct{}{0: {}}

		topicCounts := map[string]int{"a": 1}

		targetN := 2

		maxPerTopic := 2

		fillRemainingSlots(items, selectedIndices, topicCounts, targetN, maxPerTopic)

		if len(selectedIndices) != 2 {
			t.Errorf(testErrExpected2Selected, len(selectedIndices))
		}
	})

	t.Run("stops when target reached", func(t *testing.T) {
		items := []db.Item{
			{Topic: "A"},
			{Topic: "B"},
			{Topic: "C"},
			{Topic: "D"},
		}

		selectedIndices := make(map[int]struct{})

		topicCounts := make(map[string]int)

		targetN := 2

		maxPerTopic := 10

		fillRemainingSlots(items, selectedIndices, topicCounts, targetN, maxPerTopic)

		if len(selectedIndices) != 2 {
			t.Errorf(testErrExpected2Selected, len(selectedIndices))
		}
	})
}

func TestBuildTopicBalanceResult(t *testing.T) {
	items := []db.Item{
		{Topic: "A"},
		{Topic: "B"},
		{Topic: "C"},
		{Topic: "A"},
	}

	t.Run("builds result with selected indices", func(t *testing.T) {
		selectedIndices := map[int]struct{}{0: {}, 2: {}, 3: {}}

		relaxed := false

		topicsAvailable := 3

		maxPerTopic := 2

		result := buildTopicBalanceResult(items, selectedIndices, relaxed, topicsAvailable, maxPerTopic)

		if len(result.Items) != 3 {
			t.Errorf(testErrExpectedItemsCountGot, 3, len(result.Items))
		}

		if result.TopicsAvailable != 3 {
			t.Errorf("expected 3 topics available, got %d", result.TopicsAvailable)
		}

		if result.MaxPerTopic != 2 {
			t.Errorf("expected max per topic 2, got %d", result.MaxPerTopic)
		}

		if result.Relaxed {
			t.Error("expected not relaxed")
		}
	})

	t.Run("preserves item order", func(t *testing.T) {
		selectedIndices := map[int]struct{}{0: {}, 1: {}}

		result := buildTopicBalanceResult(items, selectedIndices, false, 2, 2)

		if result.Items[0].Topic != "A" || result.Items[1].Topic != "B" {
			t.Errorf("items not in order: %v", result.Items)
		}
	})
}

func TestApplyTopicBalanceFullFlow(t *testing.T) {
	t.Run("handles topics exhausted before target", func(t *testing.T) {
		// Only 2 unique topics but requesting 5 items
		items := []db.Item{
			{Topic: "A"},
			{Topic: "A"},
			{Topic: "B"},
		}

		result := applyTopicBalance(items, 5, testHalfValue, 0)

		// Should return all 3 items

		if len(result.Items) != 3 {
			t.Errorf(testErrExpectedItemsCountGot, 3, len(result.Items))
		}
	})

	t.Run("large topN limited to available items", func(t *testing.T) {
		items := []db.Item{
			{Topic: "A"},
			{Topic: "B"},
		}

		result := applyTopicBalance(items, 100, testHalfValue, 0)

		if len(result.Items) != 2 {
			t.Errorf("expected 2 items (all available), got %d", len(result.Items))
		}
	})

	t.Run("minTopics respects topic order", func(t *testing.T) {
		items := []db.Item{
			{Topic: "A", ImportanceScore: 0.9},
			{Topic: "B", ImportanceScore: 0.8},
			{Topic: "C", ImportanceScore: 0.7},
			{Topic: "D", ImportanceScore: 0.6},
		}

		result := applyTopicBalance(items, 3, testHalfValue, 2)

		// Should have at least 2 distinct topics

		if result.TopicsSelected < 2 {
			t.Errorf("expected at least 2 topics selected, got %d", result.TopicsSelected)
		}
	})

	t.Run("negative topN returns nil items", func(t *testing.T) {
		items := []db.Item{{Topic: "A"}}

		result := applyTopicBalance(items, -1, testHalfValue, 0)

		if result.Items != nil {
			t.Error("expected nil items for negative topN")
		}
	})
}

func TestCountDistinctTopicsComplex(t *testing.T) {
	t.Run("handles tab characters in topics", func(t *testing.T) {
		items := []db.Item{
			{Topic: "\tTech\t"},
		}

		got := countDistinctTopics(items)

		if got != 1 {
			t.Errorf("expected 1 topic, got %d", got)
		}
	})

	t.Run("handles unicode topics", func(t *testing.T) {
		items := []db.Item{
			{Topic: "Tecnologia"},
			{Topic: "Finanzas"},
		}

		got := countDistinctTopics(items)

		if got != 2 {
			t.Errorf("expected 2 topics, got %d", got)
		}
	})
}

func TestApplyFreshnessDecayAllCases(t *testing.T) {
	now := time.Now()

	t.Run("zero score stays zero", func(t *testing.T) {
		got := applyFreshnessDecay(0, now, 24, AutoWeightImportanceFactor)

		if got != 0 {
			t.Errorf("zero score should stay zero, got %v", got)
		}
	})

	t.Run("score at floor when very old", func(t *testing.T) {
		// Very old message
		veryOld := now.Add(-365 * 24 * time.Hour)

		got := applyFreshnessDecay(testScoreFull, veryOld, 24, AutoWeightImportanceFactor)

		if got != 0.3 {
			t.Errorf("very old message should be at floor 0.3, got %v", got)
		}
	})

	t.Run("negative floor clamped to 0", func(t *testing.T) {
		veryOld := now.Add(-100 * time.Hour)

		got := applyFreshnessDecay(testScoreFull, veryOld, 24, -0.5)

		if got < 0 {
			t.Errorf("floor should clamp negative to 0, got %v", got)
		}
	})

	t.Run("floor above 1 clamped", func(t *testing.T) {
		veryOld := now.Add(-100 * time.Hour)

		got := applyFreshnessDecay(testScoreFull, veryOld, 24, 1.5)

		if got > testScoreFull {
			t.Errorf("floor should clamp above 1, got %v", got)
		}
	})
}

func TestTopicCandidateStructFields(t *testing.T) {
	tc := topicCandidate{
		key:   "technology",
		index: 5,
	}

	if tc.key != "technology" {
		t.Errorf("key = %q, want 'technology'", tc.key)
	}

	if tc.index != 5 {
		t.Errorf("index = %d, want 5", tc.index)
	}
}

func TestGetTopicCandidatesAllEmpty(t *testing.T) {
	items := []db.Item{
		{Topic: ""},
		{Topic: "   "},
		{Topic: "\t"},
	}

	candidates := getTopicCandidates(items)

	if len(candidates) != 0 {
		t.Errorf("expected 0 candidates for all empty topics, got %d", len(candidates))
	}
}

func TestFillRemainingSlotsAllSelectedAlready(t *testing.T) {
	items := []db.Item{
		{Topic: "A"},
		{Topic: "B"},
	}

	selectedIndices := map[int]struct{}{0: {}, 1: {}}

	topicCounts := map[string]int{"a": 1, "b": 1}

	fillRemainingSlots(items, selectedIndices, topicCounts, 5, 10)

	// Should not add more since all are already selected

	if len(selectedIndices) != 2 {
		t.Errorf("expected 2 selected (no new), got %d", len(selectedIndices))
	}
}

func TestBuildTopicBalanceResultEmpty(t *testing.T) {
	items := []db.Item{}

	selectedIndices := make(map[int]struct{})

	result := buildTopicBalanceResult(items, selectedIndices, false, 0, 0)

	if len(result.Items) != 0 {
		t.Errorf("expected 0 items, got %d", len(result.Items))
	}

	if result.TopicsSelected != 0 {
		t.Errorf("expected 0 topics selected, got %d", result.TopicsSelected)
	}
}

func TestCountDistinctTopicsEmpty(t *testing.T) {
	got := countDistinctTopics(nil)

	if got != 0 {
		t.Errorf("expected 0 for nil items, got %d", got)
	}

	got = countDistinctTopics([]db.Item{})

	if got != 0 {
		t.Errorf("expected 0 for empty slice, got %d", got)
	}
}

func TestCountDistinctTopicsAllEmpty(t *testing.T) {
	items := []db.Item{
		{Topic: ""},
		{Topic: ""},
	}

	got := countDistinctTopics(items)

	if got != 0 {
		t.Errorf("expected 0 for all empty topics, got %d", got)
	}
}

func TestApplyTopicBalanceSingleItem(t *testing.T) {
	items := []db.Item{{Topic: "A"}}

	result := applyTopicBalance(items, 1, testHalfValue, 1)

	if len(result.Items) != 1 {
		t.Errorf("expected 1 item, got %d", len(result.Items))
	}

	if result.TopicsSelected != 1 {
		t.Errorf("expected 1 topic selected, got %d", result.TopicsSelected)
	}
}

func TestApplyTopicBalanceAllSameTopic(t *testing.T) {
	items := []db.Item{
		{Topic: "A"},
		{Topic: "A"},
		{Topic: "A"},
		{Topic: "A"},
	}

	result := applyTopicBalance(items, 3, testHalfValue, 0)

	// With cap of 0.5 and 3 items, max per topic = 1
	// But there's only 1 topic, so relaxation should occur

	if !result.Relaxed {
		t.Error("expected relaxation with single topic")
	}

	if len(result.Items) != 3 {
		t.Errorf(testErrExpectedItemsCountGot, 3, len(result.Items))
	}
}

func TestTopicKeyMixedCase(t *testing.T) {
	tests := []struct {
		input    string
		wantKey  string
		wantElig bool
	}{
		{"MiXeD CaSe", "mixed case", true},
		{"ALLCAPS", "allcaps", true},
		{"lowercase", "lowercase", true},
		{"With Numbers 123", "with numbers 123", true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			key, elig := topicKey(tt.input)

			if key != tt.wantKey {
				t.Errorf("topicKey(%q) key = %q, want %q", tt.input, key, tt.wantKey)
			}

			if elig != tt.wantElig {
				t.Errorf("topicKey(%q) eligible = %v, want %v", tt.input, elig, tt.wantElig)
			}
		})
	}
}

func TestSelectInitialTopNPreservesOrder(t *testing.T) {
	items := []db.Item{
		{Topic: "First", ImportanceScore: 0.9},
		{Topic: "Second", ImportanceScore: 0.8},
		{Topic: "Third", ImportanceScore: 0.7},
	}

	result := selectInitialTopN(items, 2)

	if result.Items[0].Topic != "First" {
		t.Errorf("first item should be 'First', got %q", result.Items[0].Topic)
	}

	if result.Items[1].Topic != "Second" {
		t.Errorf("second item should be 'Second', got %q", result.Items[1].Topic)
	}
}
