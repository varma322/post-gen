package core

import (
	"context"
	"fmt"

	"post-gen/internal/db"
	"post-gen/internal/models"
)

// defaultWindowDays is the window the dashboard opens on.
const defaultWindowDays = 7

// maxWindowDays bounds a caller-supplied window so one request can't scan the
// whole publish history.
const maxWindowDays = 365

// Events returns stored pipeline events matching the filter.
func (e *Engine) QueryEvents(ctx context.Context, filter models.EventFilter) ([]models.Event, error) {
	if e.db == nil {
		return nil, fmt.Errorf("database required for the event log")
	}
	return e.db.QueryEvents(ctx, filter)
}

// EventsByTrace returns every event belonging to one pipeline run, in order.
func (e *Engine) EventsByTrace(ctx context.Context, traceID string) ([]models.Event, error) {
	if e.db == nil {
		return nil, fmt.Errorf("database required for the event log")
	}
	return e.db.EventsByTrace(ctx, traceID)
}

// AnalyticsSummary assembles everything the dashboard needs in one payload,
// so the screen makes a single request rather than eight.
func (e *Engine) AnalyticsSummary(ctx context.Context, days int) (*models.AnalyticsSummary, error) {
	if e.db == nil {
		return nil, fmt.Errorf("database required for analytics")
	}
	days = clampWindow(days)

	postsToday, postsPrevDay, postsWindow, postsPrevWindow,
		queueSize, failedPosts, activeItems, activeChannels, err := e.db.CountsForSummary(ctx, days)
	if err != nil {
		return nil, err
	}

	publishing, err := e.db.DailyPublishCounts(ctx, days)
	if err != nil {
		return nil, err
	}

	failures, err := e.db.DailyFailureCounts(ctx, days)
	if err != nil {
		return nil, err
	}

	queueHealth, err := e.db.QueueHealth(ctx)
	if err != nil {
		return nil, err
	}

	aiStats, err := e.db.AIStatsForWindow(ctx, days)
	if err != nil {
		return nil, err
	}

	scraperStats, err := e.db.ScraperStatsForWindow(ctx, days)
	if err != nil {
		return nil, err
	}

	channels, err := e.channelStats(ctx, days)
	if err != nil {
		return nil, err
	}

	return &models.AnalyticsSummary{
		Days:           days,
		PostsToday:     db.NewDelta(postsToday, postsPrevDay),
		PostsInWindow:  db.NewDelta(postsWindow, postsPrevWindow),
		ActiveChannels: activeChannels,
		QueueSize:      queueSize,
		FailedPosts:    failedPosts,
		ActiveJobItems: activeItems,
		Publishing:     publishing,
		Failures:       failures,
		QueueHealth:    queueHealth,
		AI:             aiStats,
		Scraper:        scraperStats,
		Channels:       channels,
	}, nil
}

// ChannelAnalytics returns per-account performance with its daily series,
// serving both the Channels screen and the Analytics heatmap.
func (e *Engine) ChannelAnalytics(ctx context.Context, days int) ([]models.ChannelStats, error) {
	if e.db == nil {
		return nil, fmt.Errorf("database required for analytics")
	}
	return e.channelStats(ctx, clampWindow(days))
}

// channelStats joins the per-account totals with their daily series.
func (e *Engine) channelStats(ctx context.Context, days int) ([]models.ChannelStats, error) {
	channels, err := e.db.ChannelStatsForWindow(ctx, days)
	if err != nil {
		return nil, err
	}

	series, err := e.db.DailyPublishCountsByAccount(ctx, days)
	if err != nil {
		return nil, err
	}

	for i := range channels {
		channels[i].Daily = series[channels[i].AccountName]
	}

	return channels, nil
}

// clampWindow keeps a caller-supplied day count sane.
func clampWindow(days int) int {
	switch {
	case days <= 0:
		return defaultWindowDays
	case days > maxWindowDays:
		return maxWindowDays
	default:
		return days
	}
}
