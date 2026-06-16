package api

import (
	"context"
	"post-gen/internal/core"
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
	Name                string `json:"name"`
	TemplatePath        string `json:"template_path"`
	AffiliateTag        string `json:"affiliate_tag"`
	FacebookPageID      string `json:"facebook_page_id"`
	FacebookAccessToken string `json:"facebook_access_token"`
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

type affiliateLinkRequest struct {
	URL string `json:"url"`
	Tag string `json:"tag"`
}

type affiliateLinkResponse struct {
	AffiliateURL string `json:"affiliate_url"`
}

