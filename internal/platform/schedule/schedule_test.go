package schedule

import (
	"testing"
	"time"
)

func TestTimesBetweenHourlySemantics(t *testing.T) {
	s := Schedule{
		Timezone: "UTC",
		Weekdays: DaySchedule{
			Hourly: &HourlyRange{Start: "06:30", End: "08:00"},
		},
	}

	start := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 2, 9, 0, 0, 0, time.UTC)

	times, err := s.TimesBetween(start, end)
	if err != nil {
		t.Fatalf("TimesBetween returned error: %v", err)
	}

	if len(times) != 2 {
		t.Fatalf("expected 2 times, got %d", len(times))
	}

	if times[0].Hour() != 7 || times[0].Minute() != 0 {
		t.Fatalf("expected first time 07:00, got %s", times[0].Format("15:04"))
	}

	if times[1].Hour() != 8 || times[1].Minute() != 0 {
		t.Fatalf("expected second time 08:00, got %s", times[1].Format("15:04"))
	}
}

func TestTimesBetweenMergeAndDedup(t *testing.T) {
	s := Schedule{
		Timezone: "UTC",
		Weekdays: DaySchedule{
			Times:  []string{"18:00", "19:30"},
			Hourly: &HourlyRange{Start: "18:00", End: "20:00"},
		},
	}

	start := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 2, 23, 59, 0, 0, time.UTC)

	times, err := s.TimesBetween(start, end)
	if err != nil {
		t.Fatalf("TimesBetween returned error: %v", err)
	}

	if len(times) != 4 {
		t.Fatalf("expected 4 times, got %d", len(times))
	}

	expected := []string{"18:00", "19:00", "19:30", "20:00"}
	for i, exp := range expected {
		if times[i].Format("15:04") != exp {
			t.Fatalf("expected %s at index %d, got %s", exp, i, times[i].Format("15:04"))
		}
	}
}

func TestValidateRejectsBadTime(t *testing.T) {
	s := Schedule{
		Timezone: "UTC",
		Weekdays: DaySchedule{
			Times: []string{"9:00"},
		},
	}

	if err := s.Validate(); err == nil {
		t.Fatal("expected validation error for bad time format")
	}
}

func TestTimesBetweenDSTBoundary(t *testing.T) {
	loc, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}

	s := Schedule{
		Timezone: "Europe/Berlin",
		Weekdays: DaySchedule{
			Times: []string{"02:00", "03:00"},
		},
	}

	start := time.Date(2026, 3, 28, 0, 0, 0, 0, loc)
	end := time.Date(2026, 3, 30, 4, 0, 0, 0, loc)

	times, err := s.TimesBetween(start, end)
	if err != nil {
		t.Fatalf("TimesBetween returned error: %v", err)
	}

	if len(times) != 2 {
		t.Fatalf("expected 2 times, got %d", len(times))
	}

	for _, tm := range times {
		if tm.Location().String() != loc.String() {
			t.Fatalf("expected location %s, got %s", loc, tm.Location())
		}
	}
}
