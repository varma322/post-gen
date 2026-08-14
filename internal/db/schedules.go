package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"post-gen/internal/models"
)

const scheduleColumns = `id, name, kind, interval_minutes, daily_at, rotate_old_links,
	enabled, next_run_at, last_run_at, last_job_id, last_error, created_at`

// ListSchedules returns every configured schedule, newest first.
func (p *Pool) ListSchedules(ctx context.Context) ([]models.JobSchedule, error) {
	rows, err := p.pool.Query(ctx, `SELECT `+scheduleColumns+` FROM job_schedules ORDER BY id DESC`)
	if err != nil {
		return nil, fmt.Errorf("querying schedules: %w", err)
	}
	defer rows.Close()

	schedules := make([]models.JobSchedule, 0, 8)
	for rows.Next() {
		schedule, err := scanSchedule(rows)
		if err != nil {
			return nil, err
		}
		schedules = append(schedules, schedule)
	}
	return schedules, rows.Err()
}

// GetSchedule returns one schedule by id.
func (p *Pool) GetSchedule(ctx context.Context, id int) (*models.JobSchedule, error) {
	row := p.pool.QueryRow(ctx, `SELECT `+scheduleColumns+` FROM job_schedules WHERE id = $1`, id)

	schedule, err := scanSchedule(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &schedule, nil
}

// CreateSchedule inserts a schedule and returns it with its computed next run.
func (p *Pool) CreateSchedule(ctx context.Context, schedule models.JobSchedule) (*models.JobSchedule, error) {
	nextRun := schedule.NextRun(time.Now())

	var id int
	err := p.pool.QueryRow(ctx, `
		INSERT INTO job_schedules (name, kind, interval_minutes, daily_at, rotate_old_links, enabled, next_run_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id
	`, schedule.Name, schedule.Kind, schedule.IntervalMinutes, schedule.DailyAt,
		schedule.RotateOldLinks, schedule.Enabled, nextRun).Scan(&id)
	if err != nil {
		return nil, fmt.Errorf("creating schedule: %w", err)
	}

	return p.GetSchedule(ctx, id)
}

// UpdateSchedule replaces a schedule's configuration and recomputes when it
// next fires, so an edited cadence takes effect immediately rather than after
// one more run on the old timing.
func (p *Pool) UpdateSchedule(ctx context.Context, schedule models.JobSchedule) (*models.JobSchedule, error) {
	existing, err := p.GetSchedule(ctx, schedule.ID)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return nil, nil
	}

	schedule.LastRunAt = existing.LastRunAt
	nextRun := schedule.NextRun(time.Now())

	_, err = p.pool.Exec(ctx, `
		UPDATE job_schedules
		SET name = $1, kind = $2, interval_minutes = $3, daily_at = $4,
		    rotate_old_links = $5, enabled = $6, next_run_at = $7
		WHERE id = $8
	`, schedule.Name, schedule.Kind, schedule.IntervalMinutes, schedule.DailyAt,
		schedule.RotateOldLinks, schedule.Enabled, nextRun, schedule.ID)
	if err != nil {
		return nil, fmt.Errorf("updating schedule: %w", err)
	}

	return p.GetSchedule(ctx, schedule.ID)
}

// DeleteSchedule removes a schedule. Jobs it already created are untouched.
func (p *Pool) DeleteSchedule(ctx context.Context, id int) error {
	tag, err := p.pool.Exec(ctx, `DELETE FROM job_schedules WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("deleting schedule: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("schedule %d not found", id)
	}
	return nil
}

// DueSchedules returns enabled schedules whose next run has arrived.
func (p *Pool) DueSchedules(ctx context.Context, now time.Time) ([]models.JobSchedule, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT `+scheduleColumns+`
		FROM job_schedules
		WHERE enabled AND next_run_at IS NOT NULL AND next_run_at <= $1
		ORDER BY next_run_at ASC
	`, now)
	if err != nil {
		return nil, fmt.Errorf("querying due schedules: %w", err)
	}
	defer rows.Close()

	schedules := make([]models.JobSchedule, 0, 4)
	for rows.Next() {
		schedule, err := scanSchedule(rows)
		if err != nil {
			return nil, err
		}
		schedules = append(schedules, schedule)
	}
	return schedules, rows.Err()
}

// RecordScheduleRun stamps the outcome of a firing and arms the next one.
//
// next is written even on failure, so a schedule whose job could not start -
// because one was already active, or no links were available - waits for its
// next window instead of retrying every tick.
func (p *Pool) RecordScheduleRun(ctx context.Context, id int, ranAt time.Time, next time.Time, jobID *int, runErr string) error {
	_, err := p.pool.Exec(ctx, `
		UPDATE job_schedules
		SET last_run_at = $1, next_run_at = $2, last_job_id = $3, last_error = $4
		WHERE id = $5
	`, ranAt, next, jobID, nullIfEmpty(runErr), id)
	if err != nil {
		return fmt.Errorf("recording schedule run: %w", err)
	}
	return nil
}

// rowScanner covers both pgx.Row and pgx.Rows so one scan helper serves both.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanSchedule(row rowScanner) (models.JobSchedule, error) {
	var (
		schedule  models.JobSchedule
		lastError *string
	)

	err := row.Scan(
		&schedule.ID, &schedule.Name, &schedule.Kind, &schedule.IntervalMinutes,
		&schedule.DailyAt, &schedule.RotateOldLinks, &schedule.Enabled,
		&schedule.NextRunAt, &schedule.LastRunAt, &schedule.LastJobID,
		&lastError, &schedule.CreatedAt,
	)
	if err != nil {
		return schedule, err
	}

	if lastError != nil {
		schedule.LastError = *lastError
	}
	return schedule, nil
}

// NameJob records which schedule produced a job and what to call it, so a run
// is identifiable in the scheduler rather than being an anonymous id.
func (p *Pool) NameJob(ctx context.Context, jobID int, name string, scheduleID *int) error {
	_, err := p.pool.Exec(ctx, `
		UPDATE publication_jobs SET name = $1, schedule_id = $2 WHERE id = $3
	`, name, scheduleID, jobID)
	return err
}
