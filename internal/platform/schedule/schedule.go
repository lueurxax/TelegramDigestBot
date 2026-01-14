package schedule

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
	// Embed tzdata for environments without zoneinfo.
	_ "time/tzdata"
)

// Setting keys for schedule configuration.
const (
	SettingDigestSchedule       = "digest_schedule"
	SettingDigestScheduleAnchor = "digest_schedule_anchor"
)

// Time conversion constants.
const (
	minutesPerHour = 60
	maxHour        = 23
)

// Error messages.
const (
	errFmtInvalidTimezone = "invalid timezone: %w"
)

// Static errors for schedule validation.
var (
	ErrMidnightCrossing = errors.New("hourly range crosses midnight")
	ErrTimeFormat       = errors.New("time must be HH:MM")
	ErrInvalidHour      = errors.New("invalid hour")
	ErrInvalidMinute    = errors.New("invalid minute")
	ErrHourOutOfRange   = errors.New("hour out of range")
)

var timezoneAliases = map[string]string{
	"Asia/Nicosia": "Europe/Nicosia",
}

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

	loc, err := time.LoadLocation(NormalizeTimezone(s.Timezone))
	if err != nil {
		return nil, fmt.Errorf(errFmtInvalidTimezone, err)
	}

	return loc, nil
}

// Validate checks schedule fields for correctness.
func (s Schedule) Validate() error {
	if strings.TrimSpace(s.Timezone) != "" {
		if _, err := time.LoadLocation(NormalizeTimezone(s.Timezone)); err != nil {
			return fmt.Errorf(errFmtInvalidTimezone, err)
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

// NormalizeTimezone maps known aliases to canonical IANA names.
func NormalizeTimezone(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}

	if canonical, ok := timezoneAliases[value]; ok {
		return canonical
	}

	return value
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
			t := time.Date(d.Year(), d.Month(), d.Day(), min/minutesPerHour, min%minutesPerHour, 0, 0, loc)
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
			t := time.Date(d.Year(), d.Month(), d.Day(), minutes[i]/minutesPerHour, minutes[i]%minutesPerHour, 0, 0, loc)
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
			return fmt.Errorf("%s: %w", label, ErrMidnightCrossing)
		}
	}

	return nil
}

func expandDayTimes(d DaySchedule) ([]int, error) {
	if d.IsEmpty() {
		return nil, nil
	}

	set := make(map[int]struct{})

	if err := addExplicitTimes(d.Times, set); err != nil {
		return nil, err
	}

	if err := addHourlyTimes(d.Hourly, set); err != nil {
		return nil, err
	}

	return sortedMinutes(set), nil
}

func addExplicitTimes(times []string, set map[int]struct{}) error {
	for _, t := range times {
		minutes, err := parseTimeHM(t)
		if err != nil {
			return err
		}

		set[minutes] = struct{}{}
	}

	return nil
}

func addHourlyTimes(hourly *HourlyRange, set map[int]struct{}) error {
	if hourly == nil {
		return nil
	}

	startMin, err := parseTimeHM(hourly.Start)
	if err != nil {
		return err
	}

	endMin, err := parseTimeHM(hourly.End)
	if err != nil {
		return err
	}

	if startMin > endMin {
		return ErrMidnightCrossing
	}

	firstHour := startMin / minutesPerHour
	if startMin%minutesPerHour != 0 {
		firstHour++
	}

	for hour := firstHour; hour*minutesPerHour <= endMin; hour++ {
		set[hour*minutesPerHour] = struct{}{}
	}

	return nil
}

func sortedMinutes(set map[int]struct{}) []int {
	minutes := make([]int, 0, len(set))
	for min := range set {
		minutes = append(minutes, min)
	}

	sort.Ints(minutes)

	return minutes
}

func parseTimeHM(value string) (int, error) {
	normalized, err := NormalizeTimeHM(value)
	if err != nil {
		return 0, err
	}

	hour, err := strconv.Atoi(normalized[:2])
	if err != nil {
		return 0, ErrInvalidHour
	}

	minute, err := strconv.Atoi(normalized[3:])
	if err != nil {
		return 0, ErrInvalidMinute
	}

	return hour*minutesPerHour + minute, nil
}

// NormalizeTimeHM accepts H:MM or HH:MM and returns HH:MM.
func NormalizeTimeHM(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", ErrTimeFormat
	}

	parts := strings.Split(value, ":")
	if len(parts) != 2 {
		return "", ErrTimeFormat
	}

	if len(parts[1]) != 2 {
		return "", ErrTimeFormat
	}

	hour, err := strconv.Atoi(parts[0])
	if err != nil {
		return "", ErrInvalidHour
	}

	minute, err := strconv.Atoi(parts[1])
	if err != nil {
		return "", ErrInvalidMinute
	}

	if hour > maxHour || hour < 0 {
		return "", ErrHourOutOfRange
	}

	if minute < 0 || minute >= minutesPerHour {
		return "", ErrInvalidMinute
	}

	return fmt.Sprintf("%02d:%02d", hour, minute), nil
}

func dateOnly(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}
