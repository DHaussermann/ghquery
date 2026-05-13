// Package schedule provides a cross-platform interface for installing
// daily/weekly scheduled runs of the ghquery binary on the user's machine.
//
// The platform-specific implementation is selected at compile time via build tags:
//   - schedule_darwin.go  (launchd)
//   - schedule_windows.go (Task Scheduler)
//   - schedule_linux.go   (crontab)
package schedule

import (
	"fmt"
	"strings"
	"time"
)

type Frequency string

const (
	FreqDaily    Frequency = "daily"
	FreqWeekdays Frequency = "weekdays"
	FreqWeekly   Frequency = "weekly"
)

// Schedule describes a desired scheduled run.
type Schedule struct {
	Time      string    // "HH:MM" 24h, local time
	Frequency Frequency // daily | weekdays | weekly
	Weekday   string    // "monday" .. "sunday", only when Frequency == weekly
	Binary    string    // absolute path to ghquery binary
	WorkDir   string    // absolute path to project dir (so config.yaml is found)
	LogDir    string    // ~/.ghquery/logs/
}

// Validate returns an error if the Schedule fields aren't usable.
func (s Schedule) Validate() error {
	if !strings.Contains(s.Time, ":") {
		return fmt.Errorf("time must be HH:MM, got %q", s.Time)
	}
	if _, err := time.Parse("15:04", s.Time); err != nil {
		return fmt.Errorf("invalid time %q: %w", s.Time, err)
	}
	switch s.Frequency {
	case FreqDaily, FreqWeekdays:
		// no extra validation
	case FreqWeekly:
		if WeekdayNum(s.Weekday) < 0 {
			return fmt.Errorf("invalid weekday %q for weekly schedule", s.Weekday)
		}
	default:
		return fmt.Errorf("invalid frequency %q", s.Frequency)
	}
	if s.Binary == "" {
		return fmt.Errorf("Binary path is required")
	}
	if s.WorkDir == "" {
		return fmt.Errorf("WorkDir is required")
	}
	return nil
}

// HourMinute parses the Time field into hour and minute integers.
// Returns an error if Time isn't a valid HH:MM string.
func (s Schedule) HourMinute() (int, int, error) {
	t, err := time.Parse("15:04", s.Time)
	if err != nil {
		return 0, 0, err
	}
	return t.Hour(), t.Minute(), nil
}

// Status reports whether a schedule is currently installed for this user
// on this machine, plus context for display.
type Status struct {
	Installed bool
	NextRun   time.Time // zero if not installed or unknown
	Detail    string    // platform-specific detail, e.g. "macOS launchd, label com.ghquery.daily"
}

// Scheduler installs / removes / inspects the OS-level scheduled job.
type Scheduler interface {
	Install(s Schedule) error
	Remove() error
	Status() (Status, error)
}

// WeekdayNum maps a weekday name (case-insensitive) to its 0-6 number where
// 0 = Sunday (matching launchd's Weekday convention).
// Returns -1 for invalid input.
func WeekdayNum(name string) int {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "sunday", "sun":
		return 0
	case "monday", "mon":
		return 1
	case "tuesday", "tue":
		return 2
	case "wednesday", "wed":
		return 3
	case "thursday", "thu":
		return 4
	case "friday", "fri":
		return 5
	case "saturday", "sat":
		return 6
	}
	return -1
}

// WeekdayShortUpper returns "MON", "TUE", ... for the given weekday name.
// Useful for Windows schtasks /D flag. Returns "" for invalid input.
func WeekdayShortUpper(name string) string {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "sunday", "sun":
		return "SUN"
	case "monday", "mon":
		return "MON"
	case "tuesday", "tue":
		return "TUE"
	case "wednesday", "wed":
		return "WED"
	case "thursday", "thu":
		return "THU"
	case "friday", "fri":
		return "FRI"
	case "saturday", "sat":
		return "SAT"
	}
	return ""
}
