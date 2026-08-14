package models

import (
	"fmt"
	"strings"
	"time"
)

// Schedule kinds.
const (
	// ScheduleInterval fires every IntervalMinutes.
	ScheduleInterval = "interval"
	// ScheduleDaily fires once a day at DailyAt, in server-local time.
	ScheduleDaily = "daily"
)

// JobSchedule is a named recurring trigger for the auto-post pipeline.
type JobSchedule struct {
	ID              int        `json:"id"`
	Name            string     `json:"name"`
	Kind            string     `json:"kind"`
	IntervalMinutes int        `json:"interval_minutes,omitempty"`
	DailyAt         string     `json:"daily_at,omitempty"`
	RotateOldLinks  bool       `json:"rotate_old_links"`
	Enabled         bool       `json:"enabled"`
	NextRunAt       *time.Time `json:"next_run_at,omitempty"`
	LastRunAt       *time.Time `json:"last_run_at,omitempty"`
	LastJobID       *int       `json:"last_job_id,omitempty"`
	LastError       string     `json:"last_error,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
}

// Validate checks the schedule is coherent for its kind.
func (s JobSchedule) Validate() error {
	if strings.TrimSpace(s.Name) == "" {
		return fmt.Errorf("schedule name is required")
	}

	switch s.Kind {
	case ScheduleInterval:
		if s.IntervalMinutes < 1 {
			return fmt.Errorf("interval schedules need interval_minutes of at least 1")
		}

	case ScheduleDaily:
		if _, err := time.Parse("15:04", s.DailyAt); err != nil {
			return fmt.Errorf("invalid daily_at %q: expected HH:MM in 24-hour format", s.DailyAt)
		}

	default:
		return fmt.Errorf("unknown schedule kind %q: expected %q or %q", s.Kind, ScheduleInterval, ScheduleDaily)
	}

	return nil
}

// NextRun computes when this schedule should fire after `from`.
//
// An interval schedule counts from its last run, so a paused-then-resumed
// schedule doesn't immediately fire a backlog; if it has never run, it is due
// straight away. A daily schedule targets today's slot when that is still
// ahead, otherwise tomorrow's.
func (s JobSchedule) NextRun(from time.Time) time.Time {
	switch s.Kind {
	case ScheduleInterval:
		interval := time.Duration(s.IntervalMinutes) * time.Minute
		if s.LastRunAt == nil {
			return from
		}
		next := s.LastRunAt.Add(interval)
		if next.Before(from) {
			// Missed windows are not replayed - catching up would fire a burst
			// of jobs, which is the opposite of what pacing is for.
			return from
		}
		return next

	case ScheduleDaily:
		parsed, err := time.Parse("15:04", s.DailyAt)
		if err != nil {
			return from.Add(24 * time.Hour)
		}
		today := time.Date(from.Year(), from.Month(), from.Day(), parsed.Hour(), parsed.Minute(), 0, 0, from.Location())
		if today.After(from) {
			return today
		}
		return today.Add(24 * time.Hour)

	default:
		return from.Add(24 * time.Hour)
	}
}

// Describe renders the schedule in words, for the UI and log messages.
func (s JobSchedule) Describe() string {
	switch s.Kind {
	case ScheduleInterval:
		if s.IntervalMinutes%60 == 0 && s.IntervalMinutes >= 60 {
			hours := s.IntervalMinutes / 60
			if hours == 1 {
				return "Every hour"
			}
			return fmt.Sprintf("Every %d hours", hours)
		}
		return fmt.Sprintf("Every %d minutes", s.IntervalMinutes)

	case ScheduleDaily:
		return fmt.Sprintf("Daily at %s", s.DailyAt)

	default:
		return "Unknown schedule"
	}
}
