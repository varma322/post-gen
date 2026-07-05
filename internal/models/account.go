package models

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
}

// IsActive reports whether the account should be used for auto-post candidate
// selection. A nil Active field (never explicitly set) defaults to active.
func (a Account) IsActive() bool {
	return a.Active == nil || *a.Active
}
