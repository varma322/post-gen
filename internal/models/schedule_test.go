package models

import (
	"strings"
	"testing"
	"time"
)

func TestScheduleValidate(t *testing.T) {
	cases := []struct {
		name     string
		schedule JobSchedule
		wantErr  string
	}{
		{
			name:     "valid interval",
			schedule: JobSchedule{Name: "Hourly", Kind: ScheduleInterval, IntervalMinutes: 60},
		},
		{
			name:     "valid daily",
			schedule: JobSchedule{Name: "Morning", Kind: ScheduleDaily, DailyAt: "09:30"},
		},
		{
			name:     "missing name",
			schedule: JobSchedule{Kind: ScheduleInterval, IntervalMinutes: 60},
			wantErr:  "name is required",
		},
		{
			name:     "interval below one minute",
			schedule: JobSchedule{Name: "Too fast", Kind: ScheduleInterval, IntervalMinutes: 0},
			wantErr:  "interval_minutes",
		},
		{
			name:     "daily with malformed time",
			schedule: JobSchedule{Name: "Bad", Kind: ScheduleDaily, DailyAt: "9am"},
			wantErr:  "HH:MM",
		},
		{
			name:     "unknown kind",
			schedule: JobSchedule{Name: "Weird", Kind: "cron"},
			wantErr:  "unknown schedule kind",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.schedule.Validate()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("expected valid, got %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected an error containing %q, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestIntervalNextRunFromLastRun(t *testing.T) {
	now := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	lastRun := now.Add(-20 * time.Minute)

	schedule := JobSchedule{Kind: ScheduleInterval, IntervalMinutes: 60, LastRunAt: &lastRun}

	// 20 minutes into a 60-minute interval leaves 40 to go.
	want := lastRun.Add(60 * time.Minute)
	if got := schedule.NextRun(now); !got.Equal(want) {
		t.Errorf("NextRun = %v, want %v", got, want)
	}
}

func TestIntervalNeverRunIsDueImmediately(t *testing.T) {
	now := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	schedule := JobSchedule{Kind: ScheduleInterval, IntervalMinutes: 60}

	if got := schedule.NextRun(now); !got.Equal(now) {
		t.Errorf("a never-run schedule should be due now, got %v", got)
	}
}

func TestIntervalDoesNotReplayMissedWindows(t *testing.T) {
	// A schedule that was disabled for a day must not fire a burst of catch-up
	// jobs when re-enabled - that is the opposite of what pacing is for.
	now := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	lastRun := now.Add(-24 * time.Hour)

	schedule := JobSchedule{Kind: ScheduleInterval, IntervalMinutes: 60, LastRunAt: &lastRun}

	if got := schedule.NextRun(now); !got.Equal(now) {
		t.Errorf("expected a single run at %v, got %v", now, got)
	}
}

func TestDailyTargetsTodayWhenStillAhead(t *testing.T) {
	now := time.Date(2026, 8, 14, 8, 0, 0, 0, time.UTC)
	schedule := JobSchedule{Kind: ScheduleDaily, DailyAt: "09:30"}

	want := time.Date(2026, 8, 14, 9, 30, 0, 0, time.UTC)
	if got := schedule.NextRun(now); !got.Equal(want) {
		t.Errorf("NextRun = %v, want today at 09:30", got)
	}
}

func TestDailyRollsToTomorrowOncePassed(t *testing.T) {
	now := time.Date(2026, 8, 14, 10, 0, 0, 0, time.UTC)
	schedule := JobSchedule{Kind: ScheduleDaily, DailyAt: "09:30"}

	want := time.Date(2026, 8, 15, 9, 30, 0, 0, time.UTC)
	if got := schedule.NextRun(now); !got.Equal(want) {
		t.Errorf("NextRun = %v, want tomorrow at 09:30", got)
	}
}

func TestDailyAtExactSlotRollsForward(t *testing.T) {
	// Firing exactly on the slot must arm tomorrow, or the schedule would
	// re-fire on every tick for the rest of that minute.
	now := time.Date(2026, 8, 14, 9, 30, 0, 0, time.UTC)
	schedule := JobSchedule{Kind: ScheduleDaily, DailyAt: "09:30"}

	want := time.Date(2026, 8, 15, 9, 30, 0, 0, time.UTC)
	if got := schedule.NextRun(now); !got.Equal(want) {
		t.Errorf("NextRun = %v, want tomorrow at 09:30", got)
	}
}

func TestDescribe(t *testing.T) {
	cases := map[string]JobSchedule{
		"Every 30 minutes": {Kind: ScheduleInterval, IntervalMinutes: 30},
		"Every hour":       {Kind: ScheduleInterval, IntervalMinutes: 60},
		"Every 4 hours":    {Kind: ScheduleInterval, IntervalMinutes: 240},
		"Every 90 minutes": {Kind: ScheduleInterval, IntervalMinutes: 90},
		"Daily at 09:30":   {Kind: ScheduleDaily, DailyAt: "09:30"},
	}

	for want, schedule := range cases {
		if got := schedule.Describe(); got != want {
			t.Errorf("Describe() = %q, want %q", got, want)
		}
	}
}
