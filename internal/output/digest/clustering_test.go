package digest

import (
	"testing"
	"time"

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

func TestNormalizeClusterTopic(t *testing.T) {
	tests := []struct {
		name  string
		topic string
		want  string
	}{
		{name: "lowercase", topic: "technology", want: "Technology"},
		{name: "uppercase", topic: "FINANCE", want: "Finance"},
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
				t.Errorf("normalizeClusterTopic(%q) = %q, want %q", tt.topic, got, tt.want)
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
