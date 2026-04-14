package api

import (
	"post-gen/internal/core"
	"post-gen/internal/models"
)

// Generator describes the core capabilities required by the HTTP layer.
type Generator interface {
	GeneratePosts(urls []string, accountNames []string) ([]core.Result, error)
	Accounts() []models.Account
}

type generateRequest struct {
	URLs     []string `json:"urls"`
	Accounts []string `json:"accounts"`
}

type generateResponse struct {
	Results []core.Result `json:"results"`
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

type server struct {
	engine Generator
}
