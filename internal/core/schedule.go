package core

import (
	"context"
	"fmt"
	"log"
	"time"

	"post-gen/internal/events"
	"post-gen/internal/models"
)

// Schedules returns every configured recurring trigger.
func (e *Engine) Schedules(ctx context.Context) ([]models.JobSchedule, error) {
	if e.db == nil {
		return nil, fmt.Errorf("database required for schedules")
	}
	return e.db.ListSchedules(ctx)
}

// CreateSchedule validates and stores a new recurring trigger.
func (e *Engine) CreateSchedule(ctx context.Context, schedule models.JobSchedule) (*models.JobSchedule, error) {
	if e.db == nil {
		return nil, fmt.Errorf("database required for schedules")
	}
	if err := schedule.Validate(); err != nil {
		return nil, err
	}
	return e.db.CreateSchedule(ctx, schedule)
}

// UpdateSchedule replaces a schedule's configuration.
func (e *Engine) UpdateSchedule(ctx context.Context, schedule models.JobSchedule) (*models.JobSchedule, error) {
	if e.db == nil {
		return nil, fmt.Errorf("database required for schedules")
	}
	if err := schedule.Validate(); err != nil {
		return nil, err
	}
	return e.db.UpdateSchedule(ctx, schedule)
}

// DeleteSchedule removes a schedule.
func (e *Engine) DeleteSchedule(ctx context.Context, id int) error {
	if e.db == nil {
		return fmt.Errorf("database required for schedules")
	}
	return e.db.DeleteSchedule(ctx, id)
}

// RunSchedule fires a schedule immediately, without waiting for its window.
func (e *Engine) RunSchedule(ctx context.Context, id int) (int, error) {
	if e.db == nil {
		return 0, fmt.Errorf("database required for schedules")
	}

	schedule, err := e.db.GetSchedule(ctx, id)
	if err != nil {
		return 0, err
	}
	if schedule == nil {
		return 0, fmt.Errorf("schedule %d not found", id)
	}

	return e.fireSchedule(ctx, *schedule)
}

// runDueSchedules fires every schedule whose window has arrived. Called from
// the worker tick, so scheduling needs no goroutine of its own.
func (e *Engine) runDueSchedules(ctx context.Context, now time.Time) {
	if e.db == nil {
		return
	}

	due, err := e.db.DueSchedules(ctx, now)
	if err != nil {
		log.Printf("[ERR] Could not query due schedules: %v", err)
		return
	}

	for _, schedule := range due {
		if _, err := e.fireSchedule(ctx, schedule); err != nil {
			// Already recorded against the schedule by fireSchedule; this is
			// just the operator-facing line.
			log.Printf("[INFO] Schedule %q did not start a job: %v", schedule.Name, err)
		}
	}
}

// fireSchedule triggers one job for a schedule and records the outcome.
//
// A failure to start is not exceptional: the most common reason is that a job
// is already running, or that no account has unposted links. Both are recorded
// against the schedule and the next window is armed regardless, so a schedule
// never retries in a tight loop.
func (e *Engine) fireSchedule(ctx context.Context, schedule models.JobSchedule) (int, error) {
	ranAt := time.Now()

	// Compute the next window from this firing, not from the stored
	// last_run_at, so an interval schedule paces from when it actually ran.
	armed := schedule
	armed.LastRunAt = &ranAt
	next := armed.NextRun(ranAt)

	jobID, triggerErr := e.TriggerAutoPostJob(ctx, schedule.RotateOldLinks)

	var (
		recordedJobID *int
		errText       string
	)
	if triggerErr != nil {
		errText = triggerErr.Error()
	} else {
		recordedJobID = &jobID
		if err := e.db.NameJob(ctx, jobID, schedule.Name, &schedule.ID); err != nil {
			log.Printf("[WARN] Could not name job %d for schedule %q: %v", jobID, schedule.Name, err)
		}
	}

	if err := e.db.RecordScheduleRun(ctx, schedule.ID, ranAt, next, recordedJobID, errText); err != nil {
		log.Printf("[ERR] Could not record run for schedule %q: %v", schedule.Name, err)
	}

	scheduleID := schedule.ID
	if triggerErr != nil {
		e.events.Emit(events.Event{
			Type:    events.JobSkipped,
			Source:  events.SourceWorker,
			TraceID: events.NewTraceID(),
			Message: fmt.Sprintf("Schedule %q did not start a job: %v", schedule.Name, triggerErr),
			Metadata: map[string]any{
				"schedule_id":   scheduleID,
				"schedule_name": schedule.Name,
				"reason":        "trigger_failed",
			},
		})
		return 0, triggerErr
	}

	log.Printf("[INFO] Schedule %q started job %d; next run %s", schedule.Name, jobID, next.Format(time.RFC3339))
	return jobID, nil
}
