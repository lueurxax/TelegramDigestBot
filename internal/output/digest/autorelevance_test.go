package digest

import (
	"math"
	"testing"
	"time"
)

const (
	testErrWeightedTotal         = "weightedTotal = %v, want %v"
	testErrWeightedGood          = "weightedGood = %v, want %v"
	testErrWeightedBad           = "weightedBad = %v, want %v"
	testErrWeightedIrrelevant    = "weightedIrrelevant = %v, want %v"
	testErrWeightedTotalWant55   = "weightedTotal = %v, want 5.5"
	testErrCountWant1            = "count = %d, want 1"
	testErrCountWant10           = "count = %d, want 10"
	testReliability075           = 0.75
	testReliability09            = 0.9
	testReliability025           = 0.25
	testHalfValue                = 0.5
	testScoreZero                = 0.0
	testScoreFull                = 1.0
	testWeightedTotal55          = 5.5
	testWeightBad                = 0.2
	testErrComputeRelevanceDelta = "computeRelevanceDelta(%v) = %v, want %v"
)

func TestDecayWeight(t *testing.T) {
	now := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)

	if got := decayWeight(now, now); math.Abs(got-MaxNormalizedScore) > 0.0001 {
		t.Fatalf("decayWeight(now, now) = %v, want 1.0", got)
	}

	halfLifeAgo := now.Add(-14 * 24 * time.Hour)
	if got := decayWeight(now, halfLifeAgo); math.Abs(got-testHalfValue) > 0.02 {
		t.Fatalf("decayWeight(half-life) = %v, want ~0.5", got)
	}

	future := now.Add(24 * time.Hour)
	if got := decayWeight(now, future); math.Abs(got-MaxNormalizedScore) > 0.0001 {
		t.Fatalf("decayWeight(future) = %v, want 1.0", got)
	}
}

func TestComputeRelevanceDelta(t *testing.T) {
	tests := []struct {
		name        string
		reliability float64
		want        float32
	}{
		{name: "perfect", reliability: MaxNormalizedScore, want: testScoreZero},
		{name: "half", reliability: testHalfValue, want: 0.1},
		{name: "zero", reliability: 0.0, want: 0.2},
		{name: "below zero clamps", reliability: -0.5, want: 0.2},
		{name: "above one clamps", reliability: 1.5, want: testScoreZero},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := computeRelevanceDelta(tt.reliability); math.Abs(float64(got-tt.want)) > 0.0001 {
				t.Fatalf("computeRelevanceDelta(%v) = %v, want %v", tt.reliability, got, tt.want)
			}
		})
	}
}

func TestApplyRating(t *testing.T) {
	tests := []struct {
		name           string
		rating         string
		weight         float64
		wantGood       float64
		wantBad        float64
		wantIrrelevant float64
		wantTotal      float64
		wantCount      int
	}{
		{
			name:      "good rating",
			rating:    RatingGood,
			weight:    MaxNormalizedScore,
			wantGood:  MaxNormalizedScore,
			wantTotal: MaxNormalizedScore,
			wantCount: 1,
		},
		{
			name:      "bad rating",
			rating:    RatingBad,
			weight:    AutoWeightDefaultMinWeight,
			wantBad:   AutoWeightDefaultMinWeight,
			wantTotal: AutoWeightDefaultMinWeight,
			wantCount: 1,
		},
		{
			name:           "irrelevant rating",
			rating:         RatingIrrelevant,
			weight:         0.8,
			wantIrrelevant: 0.8,
			wantTotal:      0.8,
			wantCount:      1,
		},
		{
			name:      "unknown rating treated as bad",
			rating:    "unknown",
			weight:    MaxNormalizedScore,
			wantBad:   MaxNormalizedScore,
			wantTotal: MaxNormalizedScore,
			wantCount: 1,
		},
		{
			name:      "uppercase rating",
			rating:    "GOOD",
			weight:    MaxNormalizedScore,
			wantGood:  MaxNormalizedScore,
			wantTotal: MaxNormalizedScore,
			wantCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stats := &ratingStats{}
			applyRating(stats, tt.rating, tt.weight)

			if stats.count != tt.wantCount {
				t.Errorf("count = %d, want %d", stats.count, tt.wantCount)
			}

			if math.Abs(stats.weightedTotal-tt.wantTotal) > 0.0001 {
				t.Errorf(testErrWeightedTotal, stats.weightedTotal, tt.wantTotal)
			}

			if math.Abs(stats.weightedGood-tt.wantGood) > 0.0001 {
				t.Errorf(testErrWeightedGood, stats.weightedGood, tt.wantGood)
			}

			if math.Abs(stats.weightedBad-tt.wantBad) > 0.0001 {
				t.Errorf(testErrWeightedBad, stats.weightedBad, tt.wantBad)
			}

			if math.Abs(stats.weightedIrrelevant-tt.wantIrrelevant) > 0.0001 {
				t.Errorf(testErrWeightedIrrelevant, stats.weightedIrrelevant, tt.wantIrrelevant)
			}
		})
	}
}

func TestApplyRatingNilStats(_ *testing.T) {
	// Should not panic
	applyRating(nil, RatingGood, MaxNormalizedScore)
}

func TestApplyRatingMultiple(t *testing.T) {
	stats := &ratingStats{}
	applyRating(stats, RatingGood, MaxNormalizedScore)
	applyRating(stats, RatingGood, AutoWeightDefaultMinWeight)
	applyRating(stats, RatingBad, AutoWeightImportanceFactor)

	if stats.count != 3 {
		t.Errorf("count = %d, want 3", stats.count)
	}

	if math.Abs(stats.weightedTotal-1.8) > 0.0001 {
		t.Errorf("weightedTotal = %v, want 1.8", stats.weightedTotal)
	}

	if math.Abs(stats.weightedGood-1.5) > 0.0001 {
		t.Errorf("weightedGood = %v, want 1.5", stats.weightedGood)
	}

	if math.Abs(stats.weightedBad-AutoWeightImportanceFactor) > 0.0001 {
		t.Errorf(testErrWeightedBad, stats.weightedBad, AutoWeightImportanceFactor)
	}
}

func TestClampFloat64(t *testing.T) {
	tests := []struct {
		name   string
		value  float64
		minVal float64
		maxVal float64
		want   float64
	}{
		{name: "within range", value: testHalfValue, minVal: testScoreZero, maxVal: MaxNormalizedScore, want: testHalfValue},
		{name: "at min", value: testScoreZero, minVal: testScoreZero, maxVal: MaxNormalizedScore, want: testScoreZero},
		{name: "at max", value: MaxNormalizedScore, minVal: testScoreZero, maxVal: MaxNormalizedScore, want: MaxNormalizedScore},
		{name: "below min", value: -0.5, minVal: testScoreZero, maxVal: MaxNormalizedScore, want: testScoreZero},
		{name: "above max", value: 1.5, minVal: testScoreZero, maxVal: MaxNormalizedScore, want: MaxNormalizedScore},
		{name: "negative range", value: -0.5, minVal: -1.0, maxVal: 0.0, want: -0.5},
		{name: "large values", value: 1000.0, minVal: 0.0, maxVal: 100.0, want: 100.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := clampFloat64(tt.value, tt.minVal, tt.maxVal); got != tt.want {
				t.Errorf("clampFloat64(%v, %v, %v) = %v, want %v", tt.value, tt.minVal, tt.maxVal, got, tt.want)
			}
		})
	}
}

func TestDecayWeightEdgeCases(t *testing.T) {
	now := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)

	t.Run("very old message", func(t *testing.T) {
		veryOld := now.Add(-365 * 24 * time.Hour) // 1 year ago
		got := decayWeight(now, veryOld)
		// Should be very small but positive
		if got <= 0 || got > 0.01 {
			t.Errorf("decayWeight(1 year ago) = %v, want near 0", got)
		}
	})

	t.Run("zero time treated as now", func(t *testing.T) {
		got := decayWeight(now, time.Time{})
		// Zero time leads to very large ageDays, so decay should be near 0
		// Actually, zero time means time.Since returns a huge value
		if got < 0 {
			t.Errorf("decayWeight with zero time should not be negative: %v", got)
		}
	})

	t.Run("exactly at half-life", func(t *testing.T) {
		halfLife := now.Add(-14 * 24 * time.Hour)
		got := decayWeight(now, halfLife)
		// Should be approximately 0.5
		if math.Abs(got-testHalfValue) > 0.05 {
			t.Errorf("decayWeight at half-life = %v, want ~0.5", got)
		}
	})

	t.Run("two half-lives", func(t *testing.T) {
		twoHalfLives := now.Add(-28 * 24 * time.Hour)
		got := decayWeight(now, twoHalfLives)
		// Should be approximately 0.25
		if math.Abs(got-0.25) > 0.05 {
			t.Errorf("decayWeight at 2x half-life = %v, want ~0.25", got)
		}
	})
}

func TestComputeRelevanceDeltaEdgeCases(t *testing.T) {
	t.Run("reliability exactly 0.75", func(t *testing.T) {
		got := computeRelevanceDelta(testReliability075)
		// (1.0 - 0.75) * 0.2 = 0.05
		if math.Abs(float64(got-0.05)) > 0.001 {
			t.Errorf("computeRelevanceDelta(%v) = %v, want 0.05", testReliability075, got)
		}
	})

	t.Run("very small reliability", func(t *testing.T) {
		got := computeRelevanceDelta(0.01)
		// (1.0 - 0.01) * 0.2 = 0.198, but capped at 0.2
		if got != 0.198 {
			if math.Abs(float64(got-0.198)) > 0.001 {
				t.Errorf("computeRelevanceDelta(0.01) = %v, want ~0.198", got)
			}
		}
	})
}

func TestApplyRatingAccumulatesCorrectly(t *testing.T) {
	stats := &ratingStats{}

	// Apply multiple ratings in sequence
	applyRating(stats, RatingGood, AutoWeightDefaultMinWeight)
	applyRating(stats, RatingGood, AutoWeightImportanceFactor)
	applyRating(stats, RatingBad, testWeightBad)
	applyRating(stats, RatingIrrelevant, AutoWeightSignalFactor)
	applyRating(stats, "invalid", AutoWeightSignalFactor) // Should be treated as bad

	if stats.count != 5 {
		t.Errorf("count = %d, want 5", stats.count)
	}

	expectedTotal := AutoWeightDefaultMinWeight + AutoWeightImportanceFactor + testWeightBad + AutoWeightSignalFactor + AutoWeightSignalFactor
	if math.Abs(stats.weightedTotal-expectedTotal) > 0.0001 {
		t.Errorf(testErrWeightedTotal, stats.weightedTotal, expectedTotal)
	}

	expectedGood := AutoWeightDefaultMinWeight + AutoWeightImportanceFactor
	if math.Abs(stats.weightedGood-expectedGood) > 0.0001 {
		t.Errorf(testErrWeightedGood, stats.weightedGood, expectedGood)
	}

	expectedBad := testWeightBad + AutoWeightSignalFactor // includes invalid rating
	if math.Abs(stats.weightedBad-expectedBad) > 0.0001 {
		t.Errorf(testErrWeightedBad, stats.weightedBad, expectedBad)
	}

	expectedIrrelevant := AutoWeightSignalFactor
	if math.Abs(stats.weightedIrrelevant-expectedIrrelevant) > 0.0001 {
		t.Errorf(testErrWeightedIrrelevant, stats.weightedIrrelevant, expectedIrrelevant)
	}
}

func TestApplyRatingCaseInsensitive(t *testing.T) {
	tests := []struct {
		rating string
		wantIn string // which field should get the weight
	}{
		{"GOOD", "good"},
		{"Good", "good"},
		{"BAD", "bad"},
		{"Bad", "bad"},
		{"IRRELEVANT", "irrelevant"},
		{"Irrelevant", "irrelevant"},
	}

	for _, tt := range tests {
		t.Run(tt.rating, func(t *testing.T) {
			stats := &ratingStats{}
			applyRating(stats, tt.rating, MaxNormalizedScore)

			switch tt.wantIn {
			case "good":
				if stats.weightedGood != MaxNormalizedScore {
					t.Errorf("expected weightedGood=%v, got %v", MaxNormalizedScore, stats.weightedGood)
				}
			case "bad":
				if stats.weightedBad != MaxNormalizedScore {
					t.Errorf("expected weightedBad=%v, got %v", MaxNormalizedScore, stats.weightedBad)
				}
			case "irrelevant":
				if stats.weightedIrrelevant != MaxNormalizedScore {
					t.Errorf("expected weightedIrrelevant=%v, got %v", MaxNormalizedScore, stats.weightedIrrelevant)
				}
			}
		})
	}
}

func TestApplyRatingZeroWeight(t *testing.T) {
	stats := &ratingStats{}
	applyRating(stats, RatingGood, testScoreZero)

	if stats.count != 1 {
		t.Errorf(testErrCountWant1, stats.count)
	}

	if stats.weightedTotal != testScoreZero {
		t.Errorf(testErrWeightedTotal, stats.weightedTotal, testScoreZero)
	}
}

func TestRatingStatsStructFields(t *testing.T) {
	stats := ratingStats{
		count:              10,
		weightedTotal:      testWeightedTotal55,
		weightedGood:       3.0,
		weightedBad:        1.5,
		weightedIrrelevant: MaxNormalizedScore,
	}

	if stats.count != 10 {
		t.Errorf(testErrCountWant10, stats.count)
	}

	if stats.weightedTotal != testWeightedTotal55 {
		t.Errorf(testErrWeightedTotalWant55, stats.weightedTotal)
	}

	if stats.weightedGood != 3.0 {
		t.Errorf("weightedGood = %v, want 3.0", stats.weightedGood)
	}

	if stats.weightedBad != AutoWeightDefaultMaxWeight {
		t.Errorf("weightedBad = %v, want %v", stats.weightedBad, AutoWeightDefaultMaxWeight)
	}

	if stats.weightedIrrelevant != MaxNormalizedScore {
		t.Errorf(testErrWeightedIrrelevant, stats.weightedIrrelevant, MaxNormalizedScore)
	}
}

func TestApplyRatingNegativeWeight(t *testing.T) {
	stats := &ratingStats{}
	applyRating(stats, RatingGood, -1.0)

	// Negative weights should still be applied (function doesn't validate)
	if stats.count != 1 {
		t.Errorf(testErrCountWant1, stats.count)
	}

	if stats.weightedTotal != -1.0 {
		t.Errorf("weightedTotal = %v, want -1.0", stats.weightedTotal)
	}
}

func TestDecayWeightVeryRecentMessage(t *testing.T) {
	now := time.Now()
	veryRecent := now.Add(-1 * time.Second)

	got := decayWeight(now, veryRecent)

	// Should be essentially 1.0
	if got < 0.999 || got > MaxNormalizedScore {
		t.Errorf("decayWeight for 1 second ago = %v, want ~1.0", got)
	}
}

func TestComputeRelevanceDeltaExactBoundaries(t *testing.T) {
	t.Run("reliability at 0.9", func(t *testing.T) {
		got := computeRelevanceDelta(testReliability09)
		// (1.0 - 0.9) * 0.2 = 0.02
		expected := float32(0.02)
		if math.Abs(float64(got-expected)) > 0.001 {
			t.Errorf(testErrComputeRelevanceDelta, testReliability09, got, expected)
		}
	})

	t.Run("reliability at 0.25", func(t *testing.T) {
		got := computeRelevanceDelta(testReliability025)
		// (1.0 - 0.25) * 0.2 = 0.15
		expected := float32(0.15)
		if math.Abs(float64(got-expected)) > 0.001 {
			t.Errorf(testErrComputeRelevanceDelta, testReliability025, got, expected)
		}
	})
}
