package schedule

import (
	"errors"
	"testing"
	"time"
)

const (
	testTimeFormat       = "15:04"
	testErrTimesBetween  = "TimesBetween returned error: %v"
	testErrExpectedTimes = "expected %d times, got %d"
	testErrUnexpected    = "unexpected error: %v"
	testErrExpectedErr   = "expected error, got nil"
	testErrIsEmpty       = "IsEmpty() = %v, want %v"
)

func TestScheduleIsEmpty(t *testing.T) {
	tests := []struct {
		name     string
		schedule Schedule
		want     bool
	}{
		{
			name:     "empty schedule",
			schedule: Schedule{},
			want:     true,
		},
		{
			name: "with weekday times",
			schedule: Schedule{
				Weekdays: DaySchedule{Times: []string{"09:00"}},
			},
			want: false,
		},
		{
			name: "with weekend times",
			schedule: Schedule{
				Weekends: DaySchedule{Times: []string{"10:00"}},
			},
			want: false,
		},
		{
			name: "with weekday hourly",
			schedule: Schedule{
				Weekdays: DaySchedule{Hourly: &HourlyRange{Start: "09:00", End: "17:00"}},
			},
			want: false,
		},
		{
			name: "timezone only is empty",
			schedule: Schedule{
				Timezone: "UTC",
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.schedule.IsEmpty(); got != tt.want {
				t.Errorf(testErrIsEmpty, got, tt.want)
			}
		})
	}
}

func TestDayScheduleIsEmpty(t *testing.T) {
	tests := []struct {
		name string
		day  DaySchedule
		want bool
	}{
		{
			name: "empty",
			day:  DaySchedule{},
			want: true,
		},
		{
			name: "with times",
			day:  DaySchedule{Times: []string{"09:00"}},
			want: false,
		},
		{
			name: "with hourly",
			day:  DaySchedule{Hourly: &HourlyRange{Start: "09:00", End: "17:00"}},
			want: false,
		},
		{
			name: "with both",
			day: DaySchedule{
				Times:  []string{"08:00"},
				Hourly: &HourlyRange{Start: "09:00", End: "17:00"},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.day.IsEmpty(); got != tt.want {
				t.Errorf(testErrIsEmpty, got, tt.want)
			}
		})
	}
}

func TestScheduleLocation(t *testing.T) {
	tests := []struct {
		name     string
		timezone string
		wantName string
		wantErr  bool
	}{
		{
			name:     "empty defaults to UTC",
			timezone: "",
			wantName: "UTC",
			wantErr:  false,
		},
		{
			name:     "whitespace defaults to UTC",
			timezone: "   ",
			wantName: "UTC",
			wantErr:  false,
		},
		{
			name:     "valid timezone",
			timezone: "America/New_York",
			wantName: "America/New_York",
			wantErr:  false,
		},
		{
			name:     "invalid timezone",
			timezone: "Invalid/Timezone",
			wantErr:  true,
		},
		{
			name:     "alias timezone",
			timezone: "Asia/Nicosia",
			wantName: "Europe/Nicosia",
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := Schedule{Timezone: tt.timezone}
			loc, err := s.Location()

			if tt.wantErr {
				if err == nil {
					t.Error(testErrExpectedErr)
				}

				return
			}

			if err != nil {
				t.Errorf(testErrUnexpected, err)
				return
			}

			if loc.String() != tt.wantName {
				t.Errorf("Location() = %v, want %v", loc.String(), tt.wantName)
			}
		})
	}
}

func TestScheduleValidate(t *testing.T) {
	tests := []struct {
		name     string
		schedule Schedule
		wantErr  bool
		errType  error
	}{
		{
			name:     "empty is valid",
			schedule: Schedule{},
			wantErr:  false,
		},
		{
			name: "valid with times",
			schedule: Schedule{
				Timezone: "UTC",
				Weekdays: DaySchedule{Times: []string{"09:00", "18:00"}},
			},
			wantErr: false,
		},
		{
			name: "invalid timezone",
			schedule: Schedule{
				Timezone: "Invalid/Zone",
			},
			wantErr: true,
		},
		{
			name: "invalid weekday time",
			schedule: Schedule{
				Weekdays: DaySchedule{Times: []string{"25:00"}},
			},
			wantErr: true,
			errType: ErrHourOutOfRange,
		},
		{
			name: "invalid weekend time",
			schedule: Schedule{
				Weekends: DaySchedule{Times: []string{"09:60"}},
			},
			wantErr: true,
			errType: ErrInvalidMinute,
		},
		{
			name: "hourly midnight crossing",
			schedule: Schedule{
				Weekdays: DaySchedule{
					Hourly: &HourlyRange{Start: "22:00", End: "06:00"},
				},
			},
			wantErr: true,
			errType: ErrMidnightCrossing,
		},
		{
			name: "valid hourly range",
			schedule: Schedule{
				Weekdays: DaySchedule{
					Hourly: &HourlyRange{Start: "09:00", End: "17:00"},
				},
			},
			wantErr: false,
		},
		{
			name: "single digit hour accepted",
			schedule: Schedule{
				Weekdays: DaySchedule{Times: []string{"9:00"}},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.schedule.Validate()

			if tt.wantErr {
				if err == nil {
					t.Error(testErrExpectedErr)
				}

				if tt.errType != nil && !errors.Is(err, tt.errType) {
					t.Errorf("expected error type %v, got %v", tt.errType, err)
				}

				return
			}

			if err != nil {
				t.Errorf(testErrUnexpected, err)
			}
		})
	}
}

func TestNormalizeTimeHM(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr error
	}{
		{
			name:  "two digit hour",
			input: "09:00",
			want:  "09:00",
		},
		{
			name:  "single digit hour",
			input: "9:00",
			want:  "09:00",
		},
		{
			name:  "with whitespace",
			input: "  09:30  ",
			want:  "09:30",
		},
		{
			name:    "empty string",
			input:   "",
			wantErr: ErrTimeFormat,
		},
		{
			name:    "no colon",
			input:   "0900",
			wantErr: ErrTimeFormat,
		},
		{
			name:    "single digit minute",
			input:   "09:0",
			wantErr: ErrTimeFormat,
		},
		{
			name:    "hour out of range",
			input:   "25:00",
			wantErr: ErrHourOutOfRange,
		},
		{
			name:    "negative hour",
			input:   "-1:00",
			wantErr: ErrHourOutOfRange,
		},
		{
			name:    "minute out of range",
			input:   "09:60",
			wantErr: ErrInvalidMinute,
		},
		{
			name:  "midnight",
			input: "00:00",
			want:  "00:00",
		},
		{
			name:  "max valid time",
			input: "23:59",
			want:  "23:59",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NormalizeTimeHM(tt.input)

			if tt.wantErr != nil {
				if err == nil {
					t.Error(testErrExpectedErr)
				}

				if !errors.Is(err, tt.wantErr) {
					t.Errorf("expected error %v, got %v", tt.wantErr, err)
				}

				return
			}

			if err != nil {
				t.Errorf(testErrUnexpected, err)

				return
			}

			if got != tt.want {
				t.Errorf("NormalizeTimeHM(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestNormalizeTimezone(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "empty",
			input: "",
			want:  "",
		},
		{
			name:  "whitespace",
			input: "   ",
			want:  "",
		},
		{
			name:  "known alias",
			input: "Asia/Nicosia",
			want:  "Europe/Nicosia",
		},
		{
			name:  "unknown passes through",
			input: "America/New_York",
			want:  "America/New_York",
		},
		{
			name:  "trims whitespace",
			input: "  UTC  ",
			want:  "UTC",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NormalizeTimezone(tt.input); got != tt.want {
				t.Errorf("NormalizeTimezone(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestPreviousTimeBefore(t *testing.T) {
	s := Schedule{
		Timezone: "UTC",
		Weekdays: DaySchedule{Times: []string{"09:00", "18:00"}},
		Weekends: DaySchedule{Times: []string{"12:00"}},
	}

	tests := []struct {
		name      string
		before    time.Time
		wantTime  string
		wantFound bool
	}{
		{
			name:      "find previous on same day",
			before:    time.Date(2026, 1, 5, 19, 0, 0, 0, time.UTC), // Monday
			wantTime:  "18:00",
			wantFound: true,
		},
		{
			name:      "find previous from morning",
			before:    time.Date(2026, 1, 5, 10, 0, 0, 0, time.UTC), // Monday
			wantTime:  "09:00",
			wantFound: true,
		},
		{
			name:      "find previous from previous day",
			before:    time.Date(2026, 1, 5, 8, 0, 0, 0, time.UTC), // Monday morning
			wantTime:  "12:00",                                     // Sunday
			wantFound: true,
		},
		{
			name:      "weekend schedule",
			before:    time.Date(2026, 1, 4, 15, 0, 0, 0, time.UTC), // Sunday
			wantTime:  "12:00",
			wantFound: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, found, err := s.PreviousTimeBefore(tt.before)
			if err != nil {
				t.Fatalf(testErrUnexpected, err)
			}

			if found != tt.wantFound {
				t.Errorf("found = %v, want %v", found, tt.wantFound)
				return
			}

			if !found {
				return
			}

			if got.Format(testTimeFormat) != tt.wantTime {
				t.Errorf("time = %s, want %s", got.Format(testTimeFormat), tt.wantTime)
			}
		})
	}
}

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

func TestTimesBetweenEndBeforeStart(t *testing.T) {
	s := Schedule{
		Timezone: "UTC",
		Weekdays: DaySchedule{Times: []string{"09:00"}},
	}

	start := time.Date(2026, 1, 5, 12, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 5, 8, 0, 0, 0, time.UTC)

	times, err := s.TimesBetween(start, end)
	if err != nil {
		t.Fatalf(testErrUnexpected, err)
	}

	if times != nil {
		t.Errorf("expected nil, got %v", times)
	}
}

func TestTimesBetweenMultipleDays(t *testing.T) {
	s := Schedule{
		Timezone: "UTC",
		Weekdays: DaySchedule{Times: []string{"12:00"}},
		Weekends: DaySchedule{Times: []string{"14:00"}},
	}

	// Friday to Monday
	start := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC) // Friday
	end := time.Date(2026, 1, 5, 23, 59, 0, 0, time.UTC) // Monday

	times, err := s.TimesBetween(start, end)
	if err != nil {
		t.Fatalf(testErrTimesBetween, err)
	}

	// Friday 12:00, Saturday 14:00, Sunday 14:00, Monday 12:00
	expectedCount := 4
	if len(times) != expectedCount {
		t.Fatalf(testErrExpectedTimes, expectedCount, len(times))
	}
}

func TestValidateAcceptsSingleDigitHour(t *testing.T) {
	s := Schedule{
		Timezone: "UTC",
		Weekdays: DaySchedule{
			Times: []string{"9:00"},
		},
	}

	if err := s.Validate(); err != nil {
		t.Fatalf("expected validation to pass, got %v", err)
	}
}

func TestValidateRejectsBadMinuteFormat(t *testing.T) {
	s := Schedule{
		Timezone: "UTC",
		Weekdays: DaySchedule{
			Times: []string{"09:0"},
		},
	}

	if err := s.Validate(); err == nil {
		t.Fatal("expected validation error for bad minute format")
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

func TestTimesBetweenEmptySchedule(t *testing.T) {
	s := Schedule{Timezone: "UTC"}

	start := time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 5, 23, 59, 0, 0, time.UTC)

	times, err := s.TimesBetween(start, end)
	if err != nil {
		t.Fatalf(testErrUnexpected, err)
	}

	if len(times) != 0 {
		t.Errorf("expected 0 times, got %d", len(times))
	}
}

func TestPreviousTimeBeforeEmptySchedule(t *testing.T) {
	s := Schedule{Timezone: "UTC"}

	before := time.Date(2026, 1, 5, 12, 0, 0, 0, time.UTC)

	_, found, err := s.PreviousTimeBefore(before)
	if err != nil {
		t.Fatalf(testErrUnexpected, err)
	}

	if found {
		t.Error("expected not found for empty schedule")
	}
}

func TestPreviousTimeBeforeInvalidTimezone(t *testing.T) {
	s := Schedule{
		Timezone: "Invalid/Zone",
		Weekdays: DaySchedule{Times: []string{"09:00"}},
	}

	before := time.Date(2026, 1, 5, 12, 0, 0, 0, time.UTC)

	_, _, err := s.PreviousTimeBefore(before)
	if err == nil {
		t.Error("expected error for invalid timezone")
	}
}
