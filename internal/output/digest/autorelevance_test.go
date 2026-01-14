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
