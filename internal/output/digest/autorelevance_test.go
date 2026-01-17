package digest

import (
	"math"
	"testing"
	"time"
)

func TestDecayWeight(t *testing.T) {
	now := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)

	if got := decayWeight(now, now); math.Abs(got-1.0) > 0.0001 {
		t.Fatalf("decayWeight(now, now) = %v, want 1.0", got)
	}

	halfLifeAgo := now.Add(-14 * 24 * time.Hour)
	if got := decayWeight(now, halfLifeAgo); math.Abs(got-0.5) > 0.02 {
		t.Fatalf("decayWeight(half-life) = %v, want ~0.5", got)
	}

	future := now.Add(24 * time.Hour)
	if got := decayWeight(now, future); math.Abs(got-1.0) > 0.0001 {
		t.Fatalf("decayWeight(future) = %v, want 1.0", got)
	}
}

func TestComputeRelevanceDelta(t *testing.T) {
	tests := []struct {
		name        string
		reliability float64
		want        float32
	}{
		{name: "perfect", reliability: 1.0, want: 0.0},
		{name: "half", reliability: 0.5, want: 0.1},
		{name: "zero", reliability: 0.0, want: 0.2},
		{name: "below zero clamps", reliability: -0.5, want: 0.2},
		{name: "above one clamps", reliability: 1.5, want: 0.0},
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
				t.Errorf("weightedTotal = %v, want %v", stats.weightedTotal, tt.wantTotal)
			}

			if math.Abs(stats.weightedGood-tt.wantGood) > 0.0001 {
				t.Errorf("weightedGood = %v, want %v", stats.weightedGood, tt.wantGood)
			}

			if math.Abs(stats.weightedBad-tt.wantBad) > 0.0001 {
				t.Errorf("weightedBad = %v, want %v", stats.weightedBad, tt.wantBad)
			}

			if math.Abs(stats.weightedIrrelevant-tt.wantIrrelevant) > 0.0001 {
				t.Errorf("weightedIrrelevant = %v, want %v", stats.weightedIrrelevant, tt.wantIrrelevant)
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
	applyRating(stats, RatingBad, 0.3)

	if stats.count != 3 {
		t.Errorf("count = %d, want 3", stats.count)
	}

	if math.Abs(stats.weightedTotal-1.8) > 0.0001 {
		t.Errorf("weightedTotal = %v, want 1.8", stats.weightedTotal)
	}

	if math.Abs(stats.weightedGood-1.5) > 0.0001 {
		t.Errorf("weightedGood = %v, want 1.5", stats.weightedGood)
	}

	if math.Abs(stats.weightedBad-0.3) > 0.0001 {
		t.Errorf("weightedBad = %v, want 0.3", stats.weightedBad)
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
		{name: "within range", value: 0.5, minVal: 0.0, maxVal: 1.0, want: 0.5},
		{name: "at min", value: 0.0, minVal: 0.0, maxVal: 1.0, want: 0.0},
		{name: "at max", value: 1.0, minVal: 0.0, maxVal: 1.0, want: 1.0},
		{name: "below min", value: -0.5, minVal: 0.0, maxVal: 1.0, want: 0.0},
		{name: "above max", value: 1.5, minVal: 0.0, maxVal: 1.0, want: 1.0},
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
