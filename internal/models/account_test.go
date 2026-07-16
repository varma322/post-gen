package models

import (
	"testing"
	"time"
)

func TestIsEligibleToPostDefaults(t *testing.T) {
	acc := Account{Name: "acc"}
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	eligible, reason, _ := acc.IsEligibleToPost(now, 0, nil)
	if !eligible {
		t.Errorf("expected account with no rules configured to always be eligible, got reason: %q", reason)
	}
}

func TestIsEligibleToPostMaxPostsPerDay(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name       string
		limit      int
		todayCount int
		wantElig   bool
	}{
		{"under limit", 5, 4, true},
		{"at limit", 5, 5, false},
		{"over limit", 5, 6, false},
		{"zero means no limit", 0, 1000, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			acc := Account{Name: "acc", MaxPostsPerDay: tt.limit}
			eligible, reason, retryable := acc.IsEligibleToPost(now, tt.todayCount, nil)
			if eligible != tt.wantElig {
				t.Errorf("limit=%d count=%d: got eligible=%v reason=%q, want eligible=%v", tt.limit, tt.todayCount, eligible, reason, tt.wantElig)
			}
			if !eligible && reason == "" {
				t.Error("expected a non-empty reason when ineligible")
			}
			if !eligible && retryable {
				t.Error("expected daily limit block to be non-retryable (won't clear again today)")
			}
		})
	}
}

func TestIsEligibleToPostActiveHours(t *testing.T) {
	tests := []struct {
		name     string
		start    string
		end      string
		now      time.Time
		wantElig bool
	}{
		{"just before start", "09:00", "21:00", time.Date(2026, 1, 1, 8, 59, 0, 0, time.UTC), false},
		{"exactly at start", "09:00", "21:00", time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC), true},
		{"inside window", "09:00", "21:00", time.Date(2026, 1, 1, 15, 0, 0, 0, time.UTC), true},
		{"exactly at end", "09:00", "21:00", time.Date(2026, 1, 1, 21, 0, 0, 0, time.UTC), true},
		{"just after end", "09:00", "21:00", time.Date(2026, 1, 1, 21, 1, 0, 0, time.UTC), false},
		{"overnight window inside late", "22:00", "02:00", time.Date(2026, 1, 1, 23, 0, 0, 0, time.UTC), true},
		{"overnight window inside early", "22:00", "02:00", time.Date(2026, 1, 1, 1, 0, 0, 0, time.UTC), true},
		{"overnight window outside", "22:00", "02:00", time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC), false},
		{"unset window always eligible", "", "", time.Date(2026, 1, 1, 3, 0, 0, 0, time.UTC), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			acc := Account{Name: "acc", ActiveHoursStart: tt.start, ActiveHoursEnd: tt.end}
			eligible, reason, retryable := acc.IsEligibleToPost(tt.now, 0, nil)
			if eligible != tt.wantElig {
				t.Errorf("window=%s-%s now=%s: got eligible=%v reason=%q, want eligible=%v", tt.start, tt.end, tt.now, eligible, reason, tt.wantElig)
			}
			if !eligible && !retryable {
				t.Error("expected active-hours block to be retryable (window reopens later)")
			}
		})
	}
}

func TestIsEligibleToPostMinDelay(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name         string
		minDelay     int
		lastPostTime *time.Time
		wantElig     bool
	}{
		{"just under threshold", 60, timePtr(now.Add(-59 * time.Minute)), false},
		{"exactly at threshold", 60, timePtr(now.Add(-60 * time.Minute)), true},
		{"over threshold", 60, timePtr(now.Add(-61 * time.Minute)), true},
		{"never posted before", 60, nil, true},
		{"zero means no minimum", 0, timePtr(now.Add(-1 * time.Second)), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			acc := Account{Name: "acc", MinDelayMinutes: tt.minDelay}
			eligible, reason, retryable := acc.IsEligibleToPost(now, 0, tt.lastPostTime)
			if eligible != tt.wantElig {
				t.Errorf("minDelay=%d lastPost=%v: got eligible=%v reason=%q, want eligible=%v", tt.minDelay, tt.lastPostTime, eligible, reason, tt.wantElig)
			}
			if !eligible && !retryable {
				t.Error("expected min-delay block to be retryable (delay elapses later)")
			}
		})
	}
}

func timePtr(t time.Time) *time.Time {
	return &t
}
