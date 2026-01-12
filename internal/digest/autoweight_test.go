package digest

import (
	"testing"

	"github.com/lueurxax/telegram-digest-bot/internal/db"
)

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
