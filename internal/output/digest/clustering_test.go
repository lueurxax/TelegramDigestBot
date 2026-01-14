package digest

import (
	"testing"

	"github.com/lueurxax/telegram-digest-bot/internal/storage"
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
		{0.9, "🔴"},
		{0.7, "📌"},
		{0.5, "📝"},
		{0.2, "•"},
	}
	for _, tt := range tests {
		if got := getImportancePrefix(tt.score); got != tt.want {
			t.Errorf("getImportancePrefix(%v) = %v, want %v", tt.score, got, tt.want)
		}
	}
}
