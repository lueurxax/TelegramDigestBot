package schedule

import (
	"testing"
	"time"
)

const (
	testTimeFormat       = "15:04"
	testErrTimesBetween  = "TimesBetween returned error: %v"
	testErrExpectedTimes = "expected %d times, got %d"
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
		t.Fatalf(testErrTimesBetween, err)
	}

	expectedCount := 2
	if len(times) != expectedCount {
		t.Fatalf(testErrExpectedTimes, expectedCount, len(times))
	}

	if times[0].Hour() != 7 || times[0].Minute() != 0 {
		t.Fatalf("expected first time 07:00, got %s", times[0].Format(testTimeFormat))
	}

	if times[1].Hour() != 8 || times[1].Minute() != 0 {
		t.Fatalf("expected second time 08:00, got %s", times[1].Format(testTimeFormat))
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
		t.Fatalf(testErrTimesBetween, err)
	}

	expectedCount := 4
	if len(times) != expectedCount {
		t.Fatalf(testErrExpectedTimes, expectedCount, len(times))
	}

	expected := []string{"18:00", "19:00", "19:30", "20:00"}
	for i, exp := range expected {
		if times[i].Format(testTimeFormat) != exp {
			t.Fatalf("expected %s at index %d, got %s", exp, i, times[i].Format(testTimeFormat))
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

func TestNormalizeTimezoneAlias(t *testing.T) {
	if NormalizeTimezone("Asia/Nicosia") != "Europe/Nicosia" {
		t.Fatal("expected Asia/Nicosia to normalize to Europe/Nicosia")
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
		t.Fatalf(testErrTimesBetween, err)
	}

	expectedCount := 2
	if len(times) != expectedCount {
		t.Fatalf(testErrExpectedTimes, expectedCount, len(times))
	}

	for _, tm := range times {
		if tm.Location().String() != loc.String() {
			t.Fatalf("expected location %s, got %s", loc, tm.Location())
		}
	}
}
