package schedule

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Setting keys for schedule configuration.
const (
	SettingDigestSchedule       = "digest_schedule"
	SettingDigestScheduleAnchor = "digest_schedule_anchor"
)

// Schedule defines digest send times for weekdays/weekends in a timezone.
type Schedule struct {
	Timezone string      `json:"timezone"`
	Weekdays DaySchedule `json:"weekdays"`
	Weekends DaySchedule `json:"weekends"`
}

// DaySchedule defines explicit times and optional hourly range.
type DaySchedule struct {
	Times  []string     `json:"times,omitempty"`
	Hourly *HourlyRange `json:"hourly,omitempty"`
}

// HourlyRange defines an inclusive on-the-hour range.
type HourlyRange struct {
	Start string `json:"start"`
	End   string `json:"end"`
}

// IsEmpty reports whether the schedule has no data.
func (s Schedule) IsEmpty() bool {
	return s.Weekdays.IsEmpty() && s.Weekends.IsEmpty()
}

// IsEmpty reports whether the day schedule has any entries.
func (d DaySchedule) IsEmpty() bool {
	return len(d.Times) == 0 && d.Hourly == nil
}

// Location resolves the schedule timezone or defaults to UTC.
func (s Schedule) Location() (*time.Location, error) {
	if strings.TrimSpace(s.Timezone) == "" {
		return time.UTC, nil
	}

	loc, err := time.LoadLocation(s.Timezone)
	if err != nil {
		return nil, fmt.Errorf("invalid timezone: %w", err)
	}

	return loc, nil
}

// Validate checks schedule fields for correctness.
func (s Schedule) Validate() error {
	if strings.TrimSpace(s.Timezone) != "" {
		if _, err := time.LoadLocation(s.Timezone); err != nil {
			return fmt.Errorf("invalid timezone: %w", err)
		}
	}

	if err := s.Weekdays.validate("weekdays"); err != nil {
		return err
	}

	if err := s.Weekends.validate("weekends"); err != nil {
		return err
	}

	return nil
}

// TimesBetween returns scheduled times within [start, end] in the schedule timezone.
func (s Schedule) TimesBetween(start, end time.Time) ([]time.Time, error) {
	if end.Before(start) {
		return nil, nil
	}

	loc, err := s.Location()
	if err != nil {
		return nil, err
	}

	startLocal := start.In(loc)
	endLocal := end.In(loc)

	startDate := dateOnly(startLocal)
	endDate := dateOnly(endLocal)

	var results []time.Time

	for d := startDate; !d.After(endDate); d = d.AddDate(0, 0, 1) {
		daySchedule := s.daySchedule(d.Weekday())
		minutes, err := expandDayTimes(daySchedule)
		if err != nil {
			return nil, err
		}

		for _, min := range minutes {
			t := time.Date(d.Year(), d.Month(), d.Day(), min/60, min%60, 0, 0, loc)
			if t.Before(startLocal) || t.After(endLocal) {
				continue
			}

			results = append(results, t)
		}
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Before(results[j])
	})

	return results, nil
}

// PreviousTimeBefore returns the latest scheduled time before the given moment.
func (s Schedule) PreviousTimeBefore(before time.Time) (time.Time, bool, error) {
	loc, err := s.Location()
	if err != nil {
		return time.Time{}, false, err
	}

	beforeLocal := before.In(loc)
	startDate := dateOnly(beforeLocal)

	for offset := 0; offset < 8; offset++ {
		d := startDate.AddDate(0, 0, -offset)
		daySchedule := s.daySchedule(d.Weekday())
		minutes, err := expandDayTimes(daySchedule)
		if err != nil {
			return time.Time{}, false, err
		}

		for i := len(minutes) - 1; i >= 0; i-- {
			t := time.Date(d.Year(), d.Month(), d.Day(), minutes[i]/60, minutes[i]%60, 0, 0, loc)
			if t.Before(beforeLocal) {
				return t, true, nil
			}
		}
	}

	return time.Time{}, false, nil
}

func (s Schedule) daySchedule(day time.Weekday) DaySchedule {
	if day == time.Saturday || day == time.Sunday {
		return s.Weekends
	}

	return s.Weekdays
}

func (d DaySchedule) validate(label string) error {
	for _, t := range d.Times {
		if _, err := parseTimeHM(t); err != nil {
			return fmt.Errorf("invalid %s time %q: %w", label, t, err)
		}
	}

	if d.Hourly != nil {
		start, err := parseTimeHM(d.Hourly.Start)
		if err != nil {
			return fmt.Errorf("invalid %s hourly start %q: %w", label, d.Hourly.Start, err)
		}

		end, err := parseTimeHM(d.Hourly.End)
		if err != nil {
			return fmt.Errorf("invalid %s hourly end %q: %w", label, d.Hourly.End, err)
		}

		if start > end {
			return fmt.Errorf("%s hourly range crosses midnight", label)
		}
	}

	return nil
}

func expandDayTimes(d DaySchedule) ([]int, error) {
	if d.IsEmpty() {
		return nil, nil
	}

	set := make(map[int]struct{})

	for _, t := range d.Times {
		minutes, err := parseTimeHM(t)
		if err != nil {
			return nil, err
		}

		set[minutes] = struct{}{}
	}

	if d.Hourly != nil {
		startMin, err := parseTimeHM(d.Hourly.Start)
		if err != nil {
			return nil, err
		}

		endMin, err := parseTimeHM(d.Hourly.End)
		if err != nil {
			return nil, err
		}

		if startMin > endMin {
			return nil, fmt.Errorf("hourly range crosses midnight")
		}

		firstHour := startMin / 60
		if startMin%60 != 0 {
			firstHour++
		}

		for hour := firstHour; hour*60 <= endMin; hour++ {
			set[hour*60] = struct{}{}
		}
	}

	minutes := make([]int, 0, len(set))
	for min := range set {
		minutes = append(minutes, min)
	}

	sort.Ints(minutes)

	return minutes, nil
}

var timePattern = regexp.MustCompile(`^[0-2][0-9]:[0-5][0-9]$`)

func parseTimeHM(value string) (int, error) {
	if !timePattern.MatchString(value) {
		return 0, fmt.Errorf("time must be HH:MM")
	}

	hour, err := strconv.Atoi(value[:2])
	if err != nil {
		return 0, fmt.Errorf("invalid hour")
	}

	minute, err := strconv.Atoi(value[3:])
	if err != nil {
		return 0, fmt.Errorf("invalid minute")
	}

	if hour > 23 {
		return 0, fmt.Errorf("hour out of range")
	}

	return hour*60 + minute, nil
}

func dateOnly(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}
