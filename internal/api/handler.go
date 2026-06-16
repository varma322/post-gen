package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"text/template"
	"time"

	"post-gen/internal/core"
	"post-gen/internal/generator"
	"post-gen/internal/models"
	"post-gen/internal/utils"
	postgenWeb "post-gen/web"
)

// NewServer creates an HTTP handler exposing the generation API and web UI.
// token is the required Bearer token for all protected endpoints.
// Pass an empty string to disable authentication (development only).
func NewServer(engine Generator, token string) http.Handler {
	return newServer(engine, "templates", token)
}

func newServer(engine Generator, templatesDir string, token string) http.Handler {
	srv := server{engine: engine, templatesDir: templatesDir}
	mux := http.NewServeMux()
	mux.HandleFunc("/health", srv.handleHealth)
	mux.HandleFunc("/accounts", srv.handleAccounts)
	mux.HandleFunc("/accounts/", srv.handleAccountByName)
	mux.HandleFunc("/generate", srv.handleGenerate)
	mux.HandleFunc("/generate/stream", srv.handleGenerateStream)
	mux.HandleFunc("/generate/link", srv.handleGenerateLink)
	mux.HandleFunc("/templates", srv.handleTemplates)
	mux.HandleFunc("/templates/", srv.handleTemplateByName)
	mux.Handle("/", http.FileServer(http.FS(postgenWeb.FS)))

	// Protect all routes except /health and static frontend files with Bearer token auth.
	// This allows the Web UI to load so the user can enter their token.
	skipPaths := []string{"/health", "/", "/index.html", "/app.js", "/styles.css"}
	handler := BearerTokenMiddleware(token, skipPaths, mux)
	return MaxBytesMiddleware(1024*1024, handler)
}

func (s server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s server) handleAccounts(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, map[string]any{"accounts": s.engine.Accounts()})
	case http.MethodPost:
		s.handleCreateAccount(w, r)
	default:
		methodNotAllowed(w, http.MethodGet+", "+http.MethodPost)
	}
}

func (s server) handleAccountByName(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/accounts/")
	if name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "account name is required"})
		return
	}

	switch r.Method {
	case http.MethodPut:
		s.handleUpdateAccount(w, r, name)
	case http.MethodDelete:
		s.handleDeleteAccount(w, name)
	default:
		methodNotAllowed(w, http.MethodPut+", "+http.MethodDelete)
	}
}

func (s server) handleCreateAccount(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()

	var req accountRequest
	if err := decoder.Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON payload"})
		return
	}

	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "account name is required"})
		return
	}

	accounts := s.engine.Accounts()
	for _, acc := range accounts {
		if acc.Name == req.Name {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "account already exists"})
			return
		}
	}

	newAcc := models.Account{
		Name:                req.Name,
		TemplatePath:        req.TemplatePath,
		AffiliateTag:        req.AffiliateTag,
		FacebookPageID:      req.FacebookPageID,
		FacebookAccessToken: req.FacebookAccessToken,
	}

	accounts = append(accounts, newAcc)
	if err := s.saveAndReloadAccounts(accounts); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusCreated, map[string]string{"status": "created", "name": req.Name})
}

func (s server) handleUpdateAccount(w http.ResponseWriter, r *http.Request, name string) {
	defer r.Body.Close()
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()

	var req accountRequest
	if err := decoder.Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON payload"})
		return
	}

	accounts := s.engine.Accounts()
	found := false
	for i, acc := range accounts {
		if acc.Name == name {
			accounts[i] = models.Account{
				Name:                name,
				TemplatePath:        req.TemplatePath,
				AffiliateTag:        req.AffiliateTag,
				FacebookPageID:      req.FacebookPageID,
				FacebookAccessToken: req.FacebookAccessToken,
			}
			found = true
			break
		}
	}

	if !found {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "account not found"})
		return
	}

	if err := s.saveAndReloadAccounts(accounts); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "updated", "name": name})
}

func (s server) handleDeleteAccount(w http.ResponseWriter, name string) {
	if err := s.engine.DeleteAccount(name); err != nil {
		if strings.Contains(err.Error(), "not found") {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "account not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if err := s.engine.ReloadAccounts(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (s server) saveAndReloadAccounts(accounts []models.Account) error {
	if err := s.engine.SaveAccounts(accounts); err != nil {
		return err
	}
	return s.engine.ReloadAccounts()
}

func (s server) handleGenerate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}

	defer r.Body.Close()

	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()

	var req generateRequest
	if err := decoder.Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON payload"})
		return
	}

	urls := normalizeValues(req.URLs)
	if len(urls) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "at least one URL is required"})
		return
	}

	accounts := normalizeValues(req.Accounts)
	results, err := s.engine.GeneratePostsWithPublish(r.Context(), urls, accounts, req.Publish, time.Duration(req.PublishDelayMinutes)*time.Minute, nil)
	if err != nil {
		var accountErr core.AccountNotFoundError
		if errors.As(err, &accountErr) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}

		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to generate posts"})
		return
	}

	writeJSON(w, http.StatusOK, generateResponse{Results: results})
}

func (s server) handleGenerateStream(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}

	defer r.Body.Close()

	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()

	var req generateRequest
	if err := decoder.Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON payload"})
		return
	}

	urls := normalizeValues(req.URLs)
	if len(urls) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "at least one URL is required"})
		return
	}

	accounts := normalizeValues(req.Accounts)
	if err := validateRequestedAccounts(s.engine.Accounts(), accounts); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "streaming unsupported"})
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	totalResults := 0
	successCount := 0
	failureCount := 0
	publishedCount := 0
	totalURLs := len(urls)

	for index, rawURL := range urls {
		if err := writeSSE(w, "progress", streamProgressPayload{Current: index + 1, Total: totalURLs, URL: rawURL}); err != nil {
			return
		}
		flusher.Flush()

		// Enforce inter-URL delay if we already published something in a previous iteration
		if req.Publish && publishedCount > 0 && req.PublishDelayMinutes > 0 {
			delay := time.Duration(req.PublishDelayMinutes) * time.Minute
			_ = writeSSE(w, "cooldown", map[string]int{"duration_seconds": int(delay.Seconds())})
			flusher.Flush()
			time.Sleep(delay)
		}

		results, err := s.engine.GeneratePostsWithPublish(r.Context(), []string{rawURL}, accounts, req.Publish, time.Duration(req.PublishDelayMinutes)*time.Minute, func(d time.Duration) {
			_ = writeSSE(w, "cooldown", map[string]int{"duration_seconds": int(d.Seconds())})
			flusher.Flush()
		})
		if err != nil {
			if writeErr := writeSSE(w, "error", map[string]string{"error": "failed to generate posts"}); writeErr != nil {
				return
			}
			flusher.Flush()
			continue
		}

		for _, result := range results {
			totalResults++
			if result.Error == "" {
				successCount++
			} else {
				failureCount++
			}

			if result.PublishID != "" {
				publishedCount++
			}

			if err := writeSSE(w, "result", streamResultPayload{Result: result}); err != nil {
				return
			}
			flusher.Flush()
		}
	}

	_ = writeSSE(w, "done", streamDonePayload{TotalResults: totalResults, Success: successCount, Failed: failureCount})
	flusher.Flush()
}

func normalizeValues(values []string) []string {
	normalized := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			normalized = append(normalized, trimmed)
		}
	}
	return normalized
}

func methodNotAllowed(w http.ResponseWriter, allowed string) {
	w.Header().Set("Allow", allowed)
	writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func validateRequestedAccounts(available []models.Account, requested []string) error {
	if len(requested) == 0 {
		return nil
	}

	lookup := make(map[string]struct{}, len(available))
	for _, account := range available {
		lookup[account.Name] = struct{}{}
	}

	for _, name := range requested {
		if _, exists := lookup[name]; !exists {
			return core.AccountNotFoundError{Name: name}
		}
	}

	return nil
}

func writeSSE(w http.ResponseWriter, event string, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	if _, err := fmt.Fprintf(w, "event: %s\n", event); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
		return err
	}

	return nil
}

func (s server) handleTemplates(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}

	entries, err := os.ReadDir(s.templatesDir)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to read templates"})
		return
	}

	accountsByTemplate := make(map[string][]string)
	for _, account := range s.engine.Accounts() {
		templateName := filepath.Base(account.TemplatePath)
		accountsByTemplate[templateName] = append(accountsByTemplate[templateName], account.Name)
	}

	infos := make([]templateInfo, 0)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		if filepath.Ext(name) != ".tmpl" {
			continue
		}

		infos = append(infos, templateInfo{
			Name:     name,
			Path:     filepath.ToSlash(filepath.Join(s.templatesDir, name)),
			Accounts: accountsByTemplate[name],
		})
	}

	writeJSON(w, http.StatusOK, templatesResponse{Templates: infos})
}

func (s server) handleTemplateByName(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/templates/")
	if name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "template name is required"})
		return
	}

	if err := validateTemplateName(name); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	switch r.Method {
	case http.MethodGet:
		s.handleGetTemplate(w, name)
	case http.MethodPut:
		s.handleUpdateTemplate(w, r, name)
	default:
		methodNotAllowed(w, http.MethodGet+", "+http.MethodPut)
	}
}

func (s server) handleGetTemplate(w http.ResponseWriter, name string) {
	contentPath := filepath.Join(s.templatesDir, name)
	data, err := os.ReadFile(contentPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "template not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to read template"})
		return
	}

	writeJSON(w, http.StatusOK, templateResponse{Name: name, Content: string(data)})
}

func (s server) handleUpdateTemplate(w http.ResponseWriter, r *http.Request, name string) {
	defer r.Body.Close()

	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()

	var req updateTemplateRequest
	if err := decoder.Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON payload"})
		return
	}

	if strings.TrimSpace(req.Content) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "template content cannot be empty"})
		return
	}

	if _, err := template.New(name).Parse(req.Content); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "template parse error: " + err.Error()})
		return
	}

	templatePath := filepath.Join(s.templatesDir, name)
	if err := backupTemplateIfExists(templatePath); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to backup existing template"})
		return
	}

	if err := os.WriteFile(templatePath, []byte(req.Content), 0644); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to save template"})
		return
	}

	// Invalidate the in-memory template cache so the next render picks up the new content.
	generator.InvalidateCache(templatePath)

	writeJSON(w, http.StatusOK, map[string]string{"status": "saved", "name": name})
}

func validateTemplateName(name string) error {
	if filepath.Base(name) != name {
		return errors.New("invalid template name")
	}
	if filepath.Ext(name) != ".tmpl" {
		return errors.New("template must have .tmpl extension")
	}
	if strings.Contains(name, "..") {
		return errors.New("invalid template name")
	}
	return nil
}

func backupTemplateIfExists(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}

	backupPath := fmt.Sprintf("%s.bak-%s", path, time.Now().Format("20060102150405"))
	return os.WriteFile(backupPath, data, 0644)
}

func (s server) handleGenerateLink(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}

	defer r.Body.Close()
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()

	var req affiliateLinkRequest
	if err := decoder.Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON payload"})
		return
	}

	req.URL = strings.TrimSpace(req.URL)
	if req.URL == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "url is required"})
		return
	}

	// 1. Resolve short URL if it's amzn.to/amzn.in
	resolved := utils.ResolveAmazonShortURL(req.URL)

	// 2. Extract tag from the URL query if none explicitly specified in payload
	req.Tag = strings.TrimSpace(req.Tag)
	if req.Tag == "" {
		if u, err := url.Parse(resolved); err == nil {
			req.Tag = u.Query().Get("tag")
		}
	}
	if req.Tag == "" {
		req.Tag = os.Getenv("DEFAULT_AFFILIATE_TAG")
	}
	if req.Tag == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "affiliate tag is required (specify 'tag' in request or set DEFAULT_AFFILIATE_TAG environment variable)"})
		return
	}

	// 3. Normalize the URL (canonicalize dp link)
	normalized := utils.NormalizeAmazonURL(resolved)

	// 4. Append affiliate tag
	tagged := utils.AddAffiliateTag(normalized, req.Tag)

	writeJSON(w, http.StatusOK, affiliateLinkResponse{AffiliateURL: tagged})
}
