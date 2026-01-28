package worker

import (
	"testing"
	"time"
)

func TestShouldRunWeekly(t *testing.T) {
	// Sunday at midnight (00:00)
	sundayMidnight := time.Date(2024, 1, 7, 0, 30, 0, 0, time.UTC) // Sunday

	tests := []struct {
		name        string
		now         time.Time
		day         time.Weekday
		hour        int
		lastRun     time.Time
		gracePeriod time.Duration
		want        bool
	}{
		{
			name:        "sunday midnight, never run",
			now:         sundayMidnight,
			day:         time.Sunday,
			hour:        0,
			lastRun:     time.Time{},
			gracePeriod: defaultWeeklyGracePeriod,
			want:        true,
		},
		{
			name:        "sunday midnight, run 7 days ago",
			now:         sundayMidnight,
			day:         time.Sunday,
			hour:        0,
			lastRun:     sundayMidnight.Add(-7 * 24 * time.Hour),
			gracePeriod: defaultWeeklyGracePeriod,
			want:        true,
		},
		{
			name:        "sunday midnight, run 3 days ago (within grace)",
			now:         sundayMidnight,
			day:         time.Sunday,
			hour:        0,
			lastRun:     sundayMidnight.Add(-3 * 24 * time.Hour),
			gracePeriod: defaultWeeklyGracePeriod,
			want:        false,
		},
		{
			name:        "wrong day (Monday)",
			now:         sundayMidnight.Add(24 * time.Hour), // Monday
			day:         time.Sunday,
			hour:        0,
			lastRun:     time.Time{},
			gracePeriod: defaultWeeklyGracePeriod,
			want:        false,
		},
		{
			name:        "wrong hour",
			now:         time.Date(2024, 1, 7, 15, 30, 0, 0, time.UTC), // Sunday 15:00
			day:         time.Sunday,
			hour:        0,
			lastRun:     time.Time{},
			gracePeriod: defaultWeeklyGracePeriod,
			want:        false,
		},
		{
			name:        "different day and hour config",
			now:         time.Date(2024, 1, 10, 3, 0, 0, 0, time.UTC), // Wednesday 03:00
			day:         time.Wednesday,
			hour:        3,
			lastRun:     time.Time{},
			gracePeriod: defaultWeeklyGracePeriod,
			want:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ShouldRunWeekly(tt.now, tt.day, tt.hour, tt.lastRun, tt.gracePeriod)
			if got != tt.want {
				t.Errorf("ShouldRunWeekly() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestShouldRunSundayMidnight(t *testing.T) {
	sundayMidnight := time.Date(2024, 1, 7, 0, 30, 0, 0, time.UTC)

	tests := []struct {
		name    string
		now     time.Time
		lastRun time.Time
		want    bool
	}{
		{
			name:    "sunday midnight, never run",
			now:     sundayMidnight,
			lastRun: time.Time{},
			want:    true,
		},
		{
			name:    "sunday midnight, run last week",
			now:     sundayMidnight,
			lastRun: sundayMidnight.Add(-7 * 24 * time.Hour),
			want:    true,
		},
		{
			name:    "not sunday",
			now:     sundayMidnight.Add(24 * time.Hour),
			lastRun: time.Time{},
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ShouldRunSundayMidnight(tt.now, tt.lastRun)
			if got != tt.want {
				t.Errorf("ShouldRunSundayMidnight() = %v, want %v", got, tt.want)
			}
		})
	}
}
