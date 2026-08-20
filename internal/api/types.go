package api

import (
	"context"
	"post-gen/internal/core"
	"post-gen/internal/events"
	"post-gen/internal/models"
	"time"
)

// Generator describes the core capabilities required by the HTTP layer.
type Generator interface {
	GeneratePosts(ctx context.Context, urls []string, accountNames []string) ([]core.Result, error)
	GeneratePostsWithPublish(ctx context.Context, urls []string, accountNames []string, publish bool, delayBetweenPosts time.Duration, onCooldown func(time.Duration)) ([]core.Result, error)
	PublishPost(accountName, postText string) (string, error)
	Accounts() []models.Account
	ReloadAccounts() error
	SaveAccounts(accounts []models.Account) error
	DeleteAccount(name string) error
	Paths() core.Paths
	GetStats(ctx context.Context, limit int) (*models.Stats, error)
	AddQueuedProduct(ctx context.Context, url string) error
	GetQueuedProducts(ctx context.Context) ([]models.QueuedProduct, error)
	DeleteQueuedProduct(ctx context.Context, id int) error
	AddAccountLink(ctx context.Context, accountName, url string) error
	GetAccountLinks(ctx context.Context, accountName string) ([]models.AccountLink, error)
	DeleteAccountLink(ctx context.Context, id int) error
	TriggerAutoPostJob(ctx context.Context, rotateOldLinks bool) (int, error)
	GetActiveJob(ctx context.Context) (*models.PublicationJob, error)
	CancelActiveJobs(ctx context.Context) error
	Events() *events.Logger
	QueryEvents(ctx context.Context, filter models.EventFilter) ([]models.Event, error)
	EventsByTrace(ctx context.Context, traceID string) ([]models.Event, error)
	AnalyticsSummary(ctx context.Context, days int) (*models.AnalyticsSummary, error)
	ChannelAnalytics(ctx context.Context, days int) ([]models.ChannelStats, error)
	WorkerStatus() models.WorkerStatus
	Settings(ctx context.Context) (*models.SettingsView, error)
	SaveSettings(ctx context.Context, update models.SettingsUpdate) error
	Schedules(ctx context.Context) ([]models.JobSchedule, error)
	CreateSchedule(ctx context.Context, schedule models.JobSchedule) (*models.JobSchedule, error)
	UpdateSchedule(ctx context.Context, schedule models.JobSchedule) (*models.JobSchedule, error)
	DeleteSchedule(ctx context.Context, id int) error
	RunSchedule(ctx context.Context, id int) (int, error)
}

type generateRequest struct {
	URLs                []string `json:"urls"`
	Accounts            []string `json:"accounts"`
	Publish             bool     `json:"publish"`
	PublishDelayMinutes int      `json:"publish_delay_minutes"`
}

type generateResponse struct {
	Results []core.Result `json:"results"`
}

type accountRequest struct {
	Name                string            `json:"name"`
	TemplatePath        string            `json:"template_path"`
	AffiliateTag        string            `json:"affiliate_tag"`
	FacebookPageID      string            `json:"facebook_page_id"`
	FacebookAccessToken string            `json:"facebook_access_token"`
	Active              *bool             `json:"active,omitempty"`
	// UseAI is a pointer so an absent key is distinguishable from an explicit
	// false: create defaults it to true, and update leaves the stored value
	// alone rather than silently switching enrichment off.
	UseAI               *bool             `json:"use_ai,omitempty"`
	ExtraParams         map[string]string `json:"extra_params,omitempty"`
	MaxPostsPerDay      int               `json:"max_posts_per_day"`
	ActiveHoursStart    string            `json:"active_hours_start"`
	ActiveHoursEnd      string            `json:"active_hours_end"`
	MinDelayMinutes     int               `json:"min_delay_minutes"`
}

type streamProgressPayload struct {
	Current int    `json:"current"`
	Total   int    `json:"total"`
	URL     string `json:"url"`
}

type streamResultPayload struct {
	Result core.Result `json:"result"`
}

type streamDonePayload struct {
	TotalResults int `json:"totalResults"`
	Success      int `json:"success"`
	Failed       int `json:"failed"`
}

type templateInfo struct {
	Name     string   `json:"name"`
	Path     string   `json:"path"`
	Accounts []string `json:"accounts"`
}

type templatesResponse struct {
	Templates []templateInfo `json:"templates"`
}

type templateResponse struct {
	Name    string `json:"name"`
	Content string `json:"content"`
}

type updateTemplateRequest struct {
	Content string `json:"content"`
}

type server struct {
	engine       Generator
	templatesDir string
}

type accountLinkRequest struct {
	URL string `json:"url"`
}

type triggerJobRequest struct {
	RotateOldLinks bool `json:"rotate_old_links"`
}

type affiliateLinkRequest struct {
	URL string `json:"url"`
	Tag string `json:"tag"`
}

type affiliateLinkResponse struct {
	AffiliateURL string `json:"affiliate_url"`
}

type circuitBreakerStatus struct {
	PartnerTag  string     `json:"partner_tag"`
	Marketplace string     `json:"marketplace"`
	Open        bool       `json:"open"`
	Until       *time.Time `json:"until,omitempty"`
}

type healthResponse struct {
	Status      string                 `json:"status"`
	DBConnected bool                   `json:"db_connected"`
	ActiveJob   *models.PublicationJob `json:"active_job,omitempty"`
	// EventsDropped counts telemetry discarded because the event buffer was
	// full. A non-zero value means the dashboard is showing an incomplete
	// picture, so it needs to be visible rather than silently absorbed.
	EventsDropped   uint64                 `json:"events_dropped"`
	CircuitBreakers []circuitBreakerStatus `json:"circuit_breakers"`
}
