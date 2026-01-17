package digest

import (
	"math"
	"testing"
	"time"

	"github.com/lueurxax/telegram-digest-bot/internal/storage"
)

const (
	testErrExpectedItemsCountGot         = "expected %d items, got %d"
	testScoreFull                float32 = 1.0
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
			score:      1.0,
			tgDate:     now,
			decayHours: 24,
			floor:      0.0,
			wantMin:    0.95,
			wantMax:    1.0,
		},
		{
			name:       "old time decays toward floor",
			score:      1.0,
			tgDate:     now.Add(-48 * time.Hour),
			decayHours: 24,
			floor:      0.3,
			wantMin:    0.29,
			wantMax:    0.31,
		},
		{
			name:       "floor clamped to 0",
			score:      1.0,
			tgDate:     now.Add(-100 * time.Hour),
			decayHours: 10,
			floor:      -0.5,
			wantMin:    0.0,
			wantMax:    0.01,
		},
		{
			name:       "floor clamped to 1",
			score:      1.0,
			tgDate:     now.Add(-100 * time.Hour),
			decayHours: 10,
			floor:      1.5,
			wantMin:    0.99,
			wantMax:    1.01,
		},
		{
			name:       "future date treated as now",
			score:      1.0,
			tgDate:     now.Add(10 * time.Hour),
			decayHours: 24,
			floor:      0.0,
			wantMin:    0.95,
			wantMax:    1.0,
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

		result := applyTopicBalance(items, 2, 1.0, 0)

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
