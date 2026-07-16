package models

import (
	"fmt"
	"time"
)

// Account represents an affiliate account configuration.
type Account struct {
	Name                string `json:"name"`
	TemplatePath        string `json:"template_path"`
	AffiliateTag        string `json:"affiliate_tag,omitempty"`
	FacebookPageID      string `json:"facebook_page_id,omitempty"`
	FacebookAccessToken string `json:"facebook_access_token,omitempty"`
	// UseAI controls whether Gemini AI enriches product content before template rendering.
	// Defaults to true; set to false to use raw scraped data only.
	UseAI    bool   `json:"use_ai,omitempty"`
	// AIPrompt is an optional persona/tone instruction appended to the AI prompt for this account.
	// e.g. "Write in a casual, emoji-heavy style for a young audience."
	AIPrompt    string            `json:"ai_prompt,omitempty"`
	ExtraParams map[string]string `json:"extra_params,omitempty"`
	// Active controls whether this account participates in auto-post candidate
	// selection and job creation. A nil value (legacy data predating this field,
	// or a JSON/DB row that never set it) is treated as active for backward
	// compatibility - use IsActive() rather than reading this field directly.
	Active *bool `json:"active,omitempty"`
	// MaxPostsPerDay caps how many posts this account may publish per calendar
	// day via the auto-post pipeline. 0 means no limit.
	MaxPostsPerDay int `json:"max_posts_per_day,omitempty"`
	// ActiveHoursStart/ActiveHoursEnd restrict auto-posting to a window of the
	// day, in "HH:MM" 24-hour server-local time (e.g. "09:00"/"21:00"). If
	// either is empty, there is no time-of-day restriction. A start after end
	// is treated as an overnight window (e.g. "22:00"-"02:00").
	ActiveHoursStart string `json:"active_hours_start,omitempty"`
	ActiveHoursEnd   string `json:"active_hours_end,omitempty"`
	// MinDelayMinutes is the minimum rest time required between consecutive
	// auto-posts for this account. 0 means no minimum delay.
	MinDelayMinutes int `json:"min_delay_minutes,omitempty"`
}

// IsActive reports whether the account should be used for auto-post candidate
// selection. A nil Active field (never explicitly set) defaults to active.
func (a Account) IsActive() bool {
	return a.Active == nil || *a.Active
}

// IsEligibleToPost evaluates the account's rate-limit and scheduling rules
// against the current state of the auto-post pipeline. todayCount is how many
// posts this account has already published today; lastPostTime is when it
// last published (nil if never). It returns false with a human-readable
// reason for the first rule that is violated, or true if the account may post.
//
// The third return value, retryable, distinguishes a block that will clear on
// its own later today (outside the active-hours window, or the minimum delay
// between posts hasn't elapsed yet) from one that won't (the daily post limit
// has already been reached). Callers pacing a queue of multiple pending posts
// for this account use it to decide whether to leave an item pending for a
// later retry or give up on it for the rest of the day.
func (a Account) IsEligibleToPost(now time.Time, todayCount int, lastPostTime *time.Time) (eligible bool, reason string, retryable bool) {
	if a.MaxPostsPerDay > 0 && todayCount >= a.MaxPostsPerDay {
		return false, fmt.Sprintf("Daily limit of %d posts reached", a.MaxPostsPerDay), false
	}

	if a.ActiveHoursStart != "" && a.ActiveHoursEnd != "" {
		start, errStart := parseMinutesOfDay(a.ActiveHoursStart)
		end, errEnd := parseMinutesOfDay(a.ActiveHoursEnd)
		if errStart == nil && errEnd == nil {
			cur := now.Hour()*60 + now.Minute()
			inWindow := false
			if start <= end {
				inWindow = cur >= start && cur <= end
			} else {
				// Overnight window, e.g. 22:00-02:00.
				inWindow = cur >= start || cur <= end
			}
			if !inWindow {
				return false, fmt.Sprintf("Outside active posting window %s-%s", a.ActiveHoursStart, a.ActiveHoursEnd), true
			}
		}
	}

	if a.MinDelayMinutes > 0 && lastPostTime != nil {
		minDelay := time.Duration(a.MinDelayMinutes) * time.Minute
		if elapsed := now.Sub(*lastPostTime); elapsed < minDelay {
			return false, fmt.Sprintf("Minimum delay of %d minutes between posts not met", a.MinDelayMinutes), true
		}
	}

	return true, "", true
}

// parseMinutesOfDay parses a "HH:MM" 24-hour time string into minutes since midnight.
func parseMinutesOfDay(s string) (int, error) {
	t, err := time.Parse("15:04", s)
	if err != nil {
		return 0, err
	}
	return t.Hour()*60 + t.Minute(), nil
}
