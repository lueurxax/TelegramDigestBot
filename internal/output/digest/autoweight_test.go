package digest

import (
	"math"
	"testing"

	"github.com/lueurxax/telegram-digest-bot/internal/storage"
)

const testErrWeightOutsideBounds = "weight %v outside bounds [%v, %v]"

func TestDefaultAutoWeightConfig(t *testing.T) {
	cfg := DefaultAutoWeightConfig()

	if cfg.MinMessages != AutoWeightDefaultMinMessages {
		t.Errorf("MinMessages = %d, want %d", cfg.MinMessages, AutoWeightDefaultMinMessages)
	}

	if cfg.ExpectedFrequency != AutoWeightDefaultExpectedFrequency {
		t.Errorf("ExpectedFrequency = %v, want %v", cfg.ExpectedFrequency, AutoWeightDefaultExpectedFrequency)
	}

	if cfg.AutoMin != AutoWeightDefaultMinWeight {
		t.Errorf("AutoMin = %v, want %v", cfg.AutoMin, AutoWeightDefaultMinWeight)
	}

	if cfg.AutoMax != AutoWeightDefaultMaxWeight {
		t.Errorf("AutoMax = %v, want %v", cfg.AutoMax, AutoWeightDefaultMaxWeight)
	}

	if cfg.RollingDays != AutoWeightDefaultRollingDays {
		t.Errorf("RollingDays = %d, want %d", cfg.RollingDays, AutoWeightDefaultRollingDays)
	}
}

func TestCalculateAutoWeight(t *testing.T) {
	cfg := DefaultAutoWeightConfig()

	tests := []struct {
		name        string
		stats       *db.RollingStats
		days        int
		expectedMin float32
		expectedMax float32
	}{
		{
			name: "insufficient data returns neutral",
			stats: &db.RollingStats{
				TotalMessages:      5, // Below MinMessages (10)
				TotalItemsCreated:  3,
				TotalItemsDigested: 2,
				AvgImportance:      0.7,
			},
			days:        30,
			expectedMin: 0.99,
			expectedMax: 1.01,
		},
		{
			name: "high quality channel gets boost",
			stats: &db.RollingStats{
				TotalMessages:      100,
				TotalItemsCreated:  80,  // 80% signal
				TotalItemsDigested: 60,  // 75% inclusion
				AvgImportance:      0.8, // High importance
			},
			days:        30,
			expectedMin: 1.2,
			expectedMax: 1.5,
		},
		{
			name: "low quality channel gets reduced",
			stats: &db.RollingStats{
				TotalMessages:      100,
				TotalItemsCreated:  20,  // 20% signal
				TotalItemsDigested: 5,   // 25% inclusion
				AvgImportance:      0.3, // Low importance
			},
			days:        30,
			expectedMin: 0.7,
			expectedMax: 0.9, // Still gets consistency boost from regular posting
		},
		{
			name: "average channel stays near neutral",
			stats: &db.RollingStats{
				TotalMessages:      100,
				TotalItemsCreated:  50,
				TotalItemsDigested: 25,
				AvgImportance:      0.5,
			},
			days:        30,
			expectedMin: 0.8,
			expectedMax: 1.1,
		},
		{
			name: "no digested items uses fallback importance",
			stats: &db.RollingStats{
				TotalMessages:      50,
				TotalItemsCreated:  20,
				TotalItemsDigested: 0, // None digested
				AvgImportance:      0,
			},
			days:        30,
			expectedMin: 0.5,
			expectedMax: 0.8,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			weight := CalculateAutoWeight(tt.stats, cfg, tt.days)
			if weight < tt.expectedMin || weight > tt.expectedMax {
				t.Errorf("CalculateAutoWeight() = %v, want between %v and %v", weight, tt.expectedMin, tt.expectedMax)
			}
		})
	}
}

func TestCalculateAutoWeight_Bounds(t *testing.T) {
	cfg := DefaultAutoWeightConfig()

	// Perfect channel should not exceed max
	perfectStats := &db.RollingStats{
		TotalMessages:      150, // 5 per day
		TotalItemsCreated:  150, // 100% signal
		TotalItemsDigested: 150, // 100% inclusion
		AvgImportance:      1.0,
	}

	weight := CalculateAutoWeight(perfectStats, cfg, 30)
	if weight > cfg.AutoMax {
		t.Errorf("Perfect channel weight %v exceeds AutoMax %v", weight, cfg.AutoMax)
	}

	// Terrible channel should not go below min
	terribleStats := &db.RollingStats{
		TotalMessages:      100,
		TotalItemsCreated:  1,
		TotalItemsDigested: 0,
		AvgImportance:      0,
	}

	weight = CalculateAutoWeight(terribleStats, cfg, 30)
	if weight < cfg.AutoMin {
		t.Errorf("Terrible channel weight %v below AutoMin %v", weight, cfg.AutoMin)
	}
}

func TestCalculateAutoWeightEdgeCases(t *testing.T) {
	cfg := DefaultAutoWeightConfig()

	t.Run("zero days clamps to 1", func(t *testing.T) {
		stats := &db.RollingStats{
			TotalMessages:      100,
			TotalItemsCreated:  50,
			TotalItemsDigested: 25,
			AvgImportance:      0.5,
		}

		weight := CalculateAutoWeight(stats, cfg, 0)
		if weight < cfg.AutoMin || weight > cfg.AutoMax {
			t.Errorf(testErrWeightOutsideBounds, weight, cfg.AutoMin, cfg.AutoMax)
		}
	})

	t.Run("negative days clamps to 1", func(t *testing.T) {
		stats := &db.RollingStats{
			TotalMessages:      100,
			TotalItemsCreated:  50,
			TotalItemsDigested: 25,
			AvgImportance:      0.5,
		}

		weight := CalculateAutoWeight(stats, cfg, -5)
		if weight < cfg.AutoMin || weight > cfg.AutoMax {
			t.Errorf(testErrWeightOutsideBounds, weight, cfg.AutoMin, cfg.AutoMax)
		}
	})

	t.Run("zero total messages returns neutral", func(t *testing.T) {
		stats := &db.RollingStats{
			TotalMessages:      0,
			TotalItemsCreated:  0,
			TotalItemsDigested: 0,
			AvgImportance:      0,
		}

		weight := CalculateAutoWeight(stats, cfg, 30)
		if math.Abs(float64(weight-db.DefaultImportanceWeight)) > 0.01 {
			t.Errorf("weight = %v, want neutral %v", weight, db.DefaultImportanceWeight)
		}
	})

	t.Run("custom config bounds respected", func(t *testing.T) {
		customCfg := AutoWeightConfig{
			MinMessages:       5,
			ExpectedFrequency: 3.0,
			AutoMin:           0.8,
			AutoMax:           1.2,
			RollingDays:       7,
		}

		stats := &db.RollingStats{
			TotalMessages:      1000,
			TotalItemsCreated:  1000,
			TotalItemsDigested: 1000,
			AvgImportance:      1.0,
		}

		weight := CalculateAutoWeight(stats, customCfg, 7)
		if weight > customCfg.AutoMax {
			t.Errorf("weight %v exceeds custom max %v", weight, customCfg.AutoMax)
		}
	})
}

func TestCalculateAutoWeightComponents(t *testing.T) {
	cfg := DefaultAutoWeightConfig()

	t.Run("high inclusion rate boosts weight", func(t *testing.T) {
		highInclusion := &db.RollingStats{
			TotalMessages:      100,
			TotalItemsCreated:  50,
			TotalItemsDigested: 50, // 100% inclusion
			AvgImportance:      0.5,
		}

		lowInclusion := &db.RollingStats{
			TotalMessages:      100,
			TotalItemsCreated:  50,
			TotalItemsDigested: 10, // 20% inclusion
			AvgImportance:      0.5,
		}

		highWeight := CalculateAutoWeight(highInclusion, cfg, 30)
		lowWeight := CalculateAutoWeight(lowInclusion, cfg, 30)

		if highWeight <= lowWeight {
			t.Errorf("high inclusion weight %v should exceed low inclusion weight %v", highWeight, lowWeight)
		}
	})

	t.Run("high importance boosts weight", func(t *testing.T) {
		highImportance := &db.RollingStats{
			TotalMessages:      100,
			TotalItemsCreated:  50,
			TotalItemsDigested: 25,
			AvgImportance:      0.9,
		}

		lowImportance := &db.RollingStats{
			TotalMessages:      100,
			TotalItemsCreated:  50,
			TotalItemsDigested: 25,
			AvgImportance:      0.2,
		}

		highWeight := CalculateAutoWeight(highImportance, cfg, 30)
		lowWeight := CalculateAutoWeight(lowImportance, cfg, 30)

		if highWeight <= lowWeight {
			t.Errorf("high importance weight %v should exceed low importance weight %v", highWeight, lowWeight)
		}
	})

	t.Run("high signal ratio boosts weight", func(t *testing.T) {
		highSignal := &db.RollingStats{
			TotalMessages:      100,
			TotalItemsCreated:  80, // 80% signal
			TotalItemsDigested: 40,
			AvgImportance:      0.5,
		}

		lowSignal := &db.RollingStats{
			TotalMessages:      100,
			TotalItemsCreated:  20, // 20% signal
			TotalItemsDigested: 10,
			AvgImportance:      0.5,
		}

		highWeight := CalculateAutoWeight(highSignal, cfg, 30)
		lowWeight := CalculateAutoWeight(lowSignal, cfg, 30)

		if highWeight <= lowWeight {
			t.Errorf("high signal weight %v should exceed low signal weight %v", highWeight, lowWeight)
		}
	})
}

func TestCalculateAutoWeightConsistencyScore(t *testing.T) {
	cfg := DefaultAutoWeightConfig()

	t.Run("higher posting frequency boosts weight", func(t *testing.T) {
		// 150 messages over 30 days = 5/day (matches expected freq)
		consistentChannel := &db.RollingStats{
			TotalMessages:      150,
			TotalItemsCreated:  75,
			TotalItemsDigested: 50,
			AvgImportance:      0.5,
		}

		// 30 messages over 30 days = 1/day (below expected freq)
		infrequentChannel := &db.RollingStats{
			TotalMessages:      30,
			TotalItemsCreated:  15,
			TotalItemsDigested: 10,
			AvgImportance:      0.5,
		}

		consistentWeight := CalculateAutoWeight(consistentChannel, cfg, 30)
		infrequentWeight := CalculateAutoWeight(infrequentChannel, cfg, 30)

		if consistentWeight <= infrequentWeight {
			t.Errorf("consistent channel weight %v should exceed infrequent weight %v", consistentWeight, infrequentWeight)
		}
	})

	t.Run("very high frequency caps at max consistency", func(t *testing.T) {
		// 300 messages over 30 days = 10/day (2x expected freq)
		// Consistency should cap at 1.0
		highFreq := &db.RollingStats{
			TotalMessages:      300,
			TotalItemsCreated:  150,
			TotalItemsDigested: 100,
			AvgImportance:      0.5,
		}

		weight := CalculateAutoWeight(highFreq, cfg, 30)
		if weight > cfg.AutoMax {
			t.Errorf("weight %v should not exceed AutoMax %v", weight, cfg.AutoMax)
		}
	})
}

func TestCalculateAutoWeightZeroItemsCreated(t *testing.T) {
	cfg := DefaultAutoWeightConfig()

	stats := &db.RollingStats{
		TotalMessages:      100,
		TotalItemsCreated:  0,
		TotalItemsDigested: 0,
		AvgImportance:      0.5,
	}

	weight := CalculateAutoWeight(stats, cfg, 30)

	// inclusionScore should be 0, signalScore should be 0
	// Should still get some weight from importance and consistency
	if weight < cfg.AutoMin || weight > cfg.AutoMax {
		t.Errorf(testErrWeightOutsideBounds, weight, cfg.AutoMin, cfg.AutoMax)
	}
}

func TestCalculateAutoWeightExactlyAtMinMessages(t *testing.T) {
	cfg := DefaultAutoWeightConfig()

	// Exactly at the minimum threshold
	stats := &db.RollingStats{
		TotalMessages:      cfg.MinMessages,
		TotalItemsCreated:  5,
		TotalItemsDigested: 3,
		AvgImportance:      0.5,
	}

	weight := CalculateAutoWeight(stats, cfg, 30)

	// Should be processed, not return neutral
	// Since min is exactly met, we expect a calculated weight
	if weight < cfg.AutoMin || weight > cfg.AutoMax {
		t.Errorf(testErrWeightOutsideBounds, weight, cfg.AutoMin, cfg.AutoMax)
	}
}
