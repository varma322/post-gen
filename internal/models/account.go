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
	AIPrompt string `json:"ai_prompt,omitempty"`
}
