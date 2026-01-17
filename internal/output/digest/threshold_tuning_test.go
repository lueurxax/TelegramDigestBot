package digest

import (
	"testing"

	"github.com/lueurxax/telegram-digest-bot/internal/platform/config"
)

const (
	testVerySmallValue = 0.0001

	testErrCalculateThresholdDelta = "calculateThresholdDelta(%v, %v) = %v, want %v"
)

func TestClampThresholdThresholdTuning(t *testing.T) {
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
		{name: "narrow range", value: 0.5, minVal: 0.4, maxVal: 0.6, want: 0.5},
		{name: "below narrow", value: 0.3, minVal: 0.4, maxVal: 0.6, want: 0.4},
		{name: "above narrow", value: 0.7, minVal: 0.4, maxVal: 0.6, want: 0.6},
		{name: "equal min max", value: 0.5, minVal: 0.5, maxVal: 0.5, want: 0.5},
		{name: "equal min max below", value: 0.3, minVal: 0.5, maxVal: 0.5, want: 0.5},
		{name: "equal min max above", value: 0.7, minVal: 0.5, maxVal: 0.5, want: 0.5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := clampThreshold(tt.value, tt.minVal, tt.maxVal); got != tt.want {
				t.Errorf("clampThreshold(%v, %v, %v) = %v, want %v", tt.value, tt.minVal, tt.maxVal, got, tt.want)
			}
		})
	}
}

func TestGetThresholdTuningStep(t *testing.T) {
	tests := []struct {
		name     string
		cfgStep  float32
		wantStep float32
	}{
		{name: "positive step", cfgStep: 0.1, wantStep: 0.1},
		{name: "zero step uses default", cfgStep: 0.0, wantStep: 0.05},
		{name: "negative step uses default", cfgStep: -0.1, wantStep: 0.05},
		{name: "small positive step", cfgStep: 0.01, wantStep: 0.01},
		{name: "large positive step", cfgStep: 0.5, wantStep: 0.5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &Scheduler{cfg: &config.Config{ThresholdTuningStep: tt.cfgStep}}

			got := s.getThresholdTuningStep()

			if got != tt.wantStep {
				t.Errorf("getThresholdTuningStep() = %v, want %v", got, tt.wantStep)
			}
		})
	}
}

func TestGetThresholdTuningBounds(t *testing.T) {
	tests := []struct {
		name    string
		cfgMin  float32
		cfgMax  float32
		wantMin float32
		wantMax float32
	}{
		{name: "normal range", cfgMin: 0.2, cfgMax: 0.8, wantMin: 0.2, wantMax: 0.8},
		{name: "full range", cfgMin: 0.0, cfgMax: 1.0, wantMin: 0.0, wantMax: 1.0},
		{name: "negative min clamped", cfgMin: -0.5, cfgMax: 0.8, wantMin: 0.0, wantMax: 0.8},
		{name: "max above 1 clamped", cfgMin: 0.2, cfgMax: 1.5, wantMin: 0.2, wantMax: 1.0},
		{name: "both out of range", cfgMin: -0.5, cfgMax: 1.5, wantMin: 0.0, wantMax: 1.0},
		{name: "max less than min adjusted", cfgMin: 0.7, cfgMax: 0.3, wantMin: 0.7, wantMax: 0.7},
		{name: "equal min and max", cfgMin: 0.5, cfgMax: 0.5, wantMin: 0.5, wantMax: 0.5},
		{name: "zero bounds", cfgMin: 0.0, cfgMax: 0.0, wantMin: 0.0, wantMax: 0.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &Scheduler{cfg: &config.Config{
				ThresholdTuningMin: tt.cfgMin,
				ThresholdTuningMax: tt.cfgMax,
			}}

			gotMin, gotMax := s.getThresholdTuningBounds()

			if gotMin != tt.wantMin {
				t.Errorf("getThresholdTuningBounds() min = %v, want %v", gotMin, tt.wantMin)
			}

			if gotMax != tt.wantMax {
				t.Errorf("getThresholdTuningBounds() max = %v, want %v", gotMax, tt.wantMax)
			}
		})
	}
}

func TestCalculateThresholdDelta(t *testing.T) {
	tests := []struct {
		name        string
		net         float64
		step        float32
		netPositive float32
		netNegative float32
		wantDelta   float32
	}{
		{
			name:        "positive net above threshold - decrease",
			net:         0.5,
			step:        0.1,
			netPositive: 0.3,
			netNegative: -0.3,
			wantDelta:   -0.1,
		},
		{
			name:        "negative net below threshold - increase",
			net:         -0.5,
			step:        0.1,
			netPositive: 0.3,
			netNegative: -0.3,
			wantDelta:   0.1,
		},
		{
			name:        "net within neutral band - no change",
			net:         0.0,
			step:        0.1,
			netPositive: 0.3,
			netNegative: -0.3,
			wantDelta:   0.0,
		},
		{
			name:        "net at positive threshold - decrease",
			net:         0.31,
			step:        0.05,
			netPositive: 0.3,
			netNegative: -0.3,
			wantDelta:   -0.05,
		},
		{
			name:        "net at negative threshold - increase",
			net:         -0.31,
			step:        0.05,
			netPositive: 0.3,
			netNegative: -0.3,
			wantDelta:   0.05,
		},
		{
			name:        "net exactly at positive boundary - no change",
			net:         0.3,
			step:        0.1,
			netPositive: 0.3,
			netNegative: -0.3,
			wantDelta:   0.0,
		},
		{
			name:        "net exactly at negative boundary - no change",
			net:         -0.3,
			step:        0.1,
			netPositive: 0.3,
			netNegative: -0.3,
			wantDelta:   0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &Scheduler{cfg: &config.Config{
				ThresholdTuningNetPositive: tt.netPositive,
				ThresholdTuningNetNegative: tt.netNegative,
			}}

			got := s.calculateThresholdDelta(tt.net, tt.step)

			if got != tt.wantDelta {
				t.Errorf(testErrCalculateThresholdDelta, tt.net, tt.step, got, tt.wantDelta)
			}
		})
	}
}

func TestClampThresholdExtremeValues(t *testing.T) {
	t.Run("very small values", func(t *testing.T) {
		got := clampThreshold(testVerySmallValue, 0, 1)

		if got != testVerySmallValue {
			t.Errorf("clampThreshold(%v, 0, 1) = %v, want %v", testVerySmallValue, got, testVerySmallValue)
		}
	})

	t.Run("very large values", func(t *testing.T) {
		got := clampThreshold(100.0, 0, 1)

		if got != testScoreFull {
			t.Errorf("clampThreshold(100.0, 0, 1) = %v, want %v", got, testScoreFull)
		}
	})

	t.Run("very negative values", func(t *testing.T) {
		got := clampThreshold(-100.0, 0, 1)

		if got != testScoreZero {
			t.Errorf("clampThreshold(-100.0, 0, 1) = %v, want %v", got, testScoreZero)
		}
	})
}

func TestCalculateThresholdDeltaWithZeroThresholds(t *testing.T) {
	s := &Scheduler{cfg: &config.Config{
		ThresholdTuningNetPositive: testScoreZero,
		ThresholdTuningNetNegative: testScoreZero,
	}}

	t.Run("positive net with zero threshold", func(t *testing.T) {
		got := s.calculateThresholdDelta(0.1, DefaultThresholdTuningStep)

		if got != -DefaultThresholdTuningStep {
			t.Errorf("calculateThresholdDelta(0.1, %v) = %v, want %v", DefaultThresholdTuningStep, got, -DefaultThresholdTuningStep)
		}
	})

	t.Run("negative net with zero threshold", func(t *testing.T) {
		got := s.calculateThresholdDelta(-0.1, DefaultThresholdTuningStep)

		if got != DefaultThresholdTuningStep {
			t.Errorf("calculateThresholdDelta(-0.1, %v) = %v, want %v", DefaultThresholdTuningStep, got, DefaultThresholdTuningStep)
		}
	})

	t.Run("exactly zero net", func(t *testing.T) {
		got := s.calculateThresholdDelta(testScoreZero, DefaultThresholdTuningStep)

		if got != testScoreZero {
			t.Errorf("calculateThresholdDelta(%v, %v) = %v, want %v", testScoreZero, DefaultThresholdTuningStep, got, testScoreZero)
		}
	})
}
