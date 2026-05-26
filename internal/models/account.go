package models

// Account represents an affiliate account configuration.
type Account struct {
	Name                string `json:"name"`
	TemplatePath        string `json:"template_path"`
	AffiliateTag        string `json:"affiliate_tag,omitempty"`
	FacebookPageID      string `json:"facebook_page_id,omitempty"`
	FacebookAccessToken string `json:"facebook_access_token,omitempty"`
}
