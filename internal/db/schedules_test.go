//go:build integration
// +build integration

package db

import (
	"context"
	"testing"

	"post-gen/internal/models"
)

// Schedules created here are disabled and deleted afterwards, so nothing fires
// against a real database.
func createTestSchedule(t *testing.T, p *Pool, schedule models.JobSchedule) *models.JobSchedule {
	t.Helper()

	created, err := p.CreateSchedule(context.Background(), schedule)
	if err != nil {
		t.Fatalf("creating schedule: %v", err)
	}
	t.Cleanup(func() {
		_ = p.DeleteSchedule(context.Background(), created.ID)
	})
	return created
}

func TestScheduleTaskRoundTrips(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	created := createTestSchedule(t, db, models.JobSchedule{
		Name:            "test-discovery-roundtrip",
		Kind:            models.ScheduleInterval,
		Task:            models.TaskDealDiscovery,
		IntervalMinutes: 30,
		Enabled:         false,
	})

	if created.Task != models.TaskDealDiscovery {
		t.Errorf("Task = %q, want %q", created.Task, models.TaskDealDiscovery)
	}

	fetched, err := db.GetSchedule(ctx, created.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if fetched.Task != models.TaskDealDiscovery {
		t.Errorf("Task = %q after reload, want %q", fetched.Task, models.TaskDealDiscovery)
	}
}

func TestScheduleTaskDefaultsToAutoPost(t *testing.T) {
	// A payload with no task - which is every client that predates discovery -
	// must land on the auto-post pipeline, not an empty string.
	db := testDB(t)

	created := createTestSchedule(t, db, models.JobSchedule{
		Name:            "test-default-task",
		Kind:            models.ScheduleInterval,
		IntervalMinutes: 45,
		Enabled:         false,
	})

	if created.Task != models.TaskAutoPost {
		t.Errorf("Task = %q, want %q for a payload that omitted it", created.Task, models.TaskAutoPost)
	}
}

func TestUpdateSchedulePreservesTask(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	created := createTestSchedule(t, db, models.JobSchedule{
		Name:            "test-update-task",
		Kind:            models.ScheduleInterval,
		Task:            models.TaskDealDiscovery,
		IntervalMinutes: 30,
		Enabled:         false,
	})

	created.IntervalMinutes = 90
	updated, err := db.UpdateSchedule(ctx, *created)
	if err != nil {
		t.Fatalf("update: %v", err)
	}

	if updated.Task != models.TaskDealDiscovery {
		t.Errorf("Task = %q after an unrelated edit, want it preserved", updated.Task)
	}
	if updated.IntervalMinutes != 90 {
		t.Errorf("IntervalMinutes = %d, want the edit applied", updated.IntervalMinutes)
	}
}

func TestExistingSchedulesMigrateToAutoPost(t *testing.T) {
	// The migration adds task with a default, so rows that predate it must read
	// back as auto-post rather than blank.
	db := testDB(t)

	schedules, err := db.ListSchedules(context.Background())
	if err != nil {
		t.Fatalf("list: %v", err)
	}

	for _, schedule := range schedules {
		if schedule.Task == "" {
			t.Errorf("schedule %q has a blank task after migration", schedule.Name)
		}
	}
}
