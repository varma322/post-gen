package scraper

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
)

// CredentialRegistryFile is the manifest inside the credentials directory that
// maps each API-eligible Associates tag to the credential file Amazon issued
// for it.
const CredentialRegistryFile = "registry.json"

// NewCreatorAPIClient builds a Creators API client from the credential
// registry, falling back to single-account environment variables. fallback is
// the scraper it delegates to when the API cannot serve a product lookup; pass
// nil for catalog-only use such as discovery, which has no HTML equivalent yet.
//
// It returns nil when no credentials are configured at all, which is how a
// caller distinguishes "no API access" from "API access that failed".
func NewCreatorAPIClient(fallback Scraper) *AmazonCreatorAPIScraper {
	// Prefer the multi-account registry: API eligibility is granted per
	// Associates account, so the tag a page publishes under is usually not one
	// that may call the API. The registry keeps catalog calls on an eligible
	// account without touching the per-page tag in the published link.
	registry, err := LoadCredentialRegistry(CredentialsDir())
	if err == nil && registry.Len() > 0 {
		log.Printf("[INFO] Creators API: %d account(s) loaded: %s",
			registry.Len(), strings.Join(registry.Tags(), ", "))
		return NewAmazonCreatorAPIScraperWithRegistry(registry, fallback)
	}
	if err != nil && !os.IsNotExist(err) {
		// A malformed manifest is worth saying out loud - falling through to a
		// single env account silently would look like it worked.
		log.Printf("[WARN] Creators API credential registry not usable, falling back to environment credentials: %v", err)
	}

	clientID := firstNonEmpty(os.Getenv("Credential_ID"), os.Getenv("AMAZON_CREATOR_CLIENT_ID"))
	clientSecret := firstNonEmpty(os.Getenv("Secret"), os.Getenv("AMAZON_CREATOR_CLIENT_SECRET"))
	partnerTag := firstNonEmpty(os.Getenv("AMAZON_CREATOR_PARTNER_TAG"), os.Getenv("Application_ID"))
	tokenURL := os.Getenv("AMAZON_CREATOR_TOKEN_URL")

	if clientID == "" || clientSecret == "" {
		return nil
	}

	return NewAmazonCreatorAPIScraper(clientID, clientSecret, tokenURL, partnerTag, fallback)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

// CredentialsDir is where the credential manifest and CSVs live. It is
// overridable so a deployment can keep secrets outside the working directory.
func CredentialsDir() string {
	if dir := strings.TrimSpace(os.Getenv("AFF_CREDS_DIR")); dir != "" {
		return dir
	}
	return "AFF_creds"
}

// APICredential is one Associates account that holds Creators API access.
//
// Eligibility is granted per Associates account - Amazon requires qualifying
// sales in a rolling window - so most pages do not have it. Tag here is the one
// the API call is billed to. It is deliberately not the tag that ends up in the
// published link: that stays per-page and is applied by utils.AddAffiliateTag
// from the source URL, so which account serves a catalog lookup has no bearing
// on who earns the commission.
type APICredential struct {
	Tag          string
	ClientID     string
	ClientSecret string
	TokenURL     string
}

// credentialSet is an APICredential with its token manager attached, so a
// selected account carries the OAuth cache that belongs to it. Sharing one
// token across accounts is exactly the bug this type exists to prevent.
type credentialSet struct {
	tag          string
	tokenManager *TokenManager
}

// CredentialRegistry holds the API-eligible accounts a catalog lookup may run
// under, and picks between them.
type CredentialRegistry struct {
	sets   []*credentialSet
	byTag  map[string]*credentialSet
	cursor atomic.Uint64
}

// NewCredentialRegistry builds a registry from credentials, skipping any entry
// missing a tag, client ID or secret, and ignoring duplicate tags.
func NewCredentialRegistry(creds []APICredential) *CredentialRegistry {
	r := &CredentialRegistry{byTag: make(map[string]*credentialSet, len(creds))}

	for _, c := range creds {
		tag := strings.TrimSpace(c.Tag)
		if tag == "" || strings.TrimSpace(c.ClientID) == "" || strings.TrimSpace(c.ClientSecret) == "" {
			continue
		}
		if _, duplicate := r.byTag[tag]; duplicate {
			continue
		}

		set := &credentialSet{
			tag:          tag,
			tokenManager: NewTokenManager(c.ClientID, c.ClientSecret, c.TokenURL),
		}
		r.sets = append(r.sets, set)
		r.byTag[tag] = set
	}

	return r
}

// Len reports how many eligible accounts the registry holds.
func (r *CredentialRegistry) Len() int {
	if r == nil {
		return 0
	}
	return len(r.sets)
}

// Tags lists the eligible tags, in registry order.
func (r *CredentialRegistry) Tags() []string {
	if r == nil {
		return nil
	}
	tags := make([]string, 0, len(r.sets))
	for _, set := range r.sets {
		tags = append(tags, set.tag)
	}
	return tags
}

// resolve picks the credential a call should run under.
//
// The account's own tag wins when that account is itself eligible, so
// attribution matches the page being published for whenever it can. Otherwise -
// the common case, since most pages hold no API access - the call rotates onto
// a healthy eligible account. Accounts whose circuit is open for this
// marketplace are skipped, which is what turns two eligible accounts into two
// independent quotas: one throttled account moves its traffic to the other
// instead of dropping everything to HTML.
//
// It returns nil when every eligible account is throttled, leaving the caller
// to fall back.
func (r *CredentialRegistry) resolve(preferred, marketplace string) *credentialSet {
	if r.Len() == 0 {
		return nil
	}

	if set, ok := r.byTag[preferred]; ok && !creatorAPICircuitOpen(set.tag, marketplace) {
		return set
	}

	start := int(r.cursor.Add(1))
	for i := 0; i < len(r.sets); i++ {
		set := r.sets[(start+i)%len(r.sets)]
		if !creatorAPICircuitOpen(set.tag, marketplace) {
			return set
		}
	}

	return nil
}

// registryEntry maps an eligible tag to the credential file Amazon issued for
// that account. The CSV carries no tracking ID of its own, so the pairing is
// stated rather than guessed from the filename.
type registryEntry struct {
	Tag   string `json:"tag"`
	Creds string `json:"creds"`
}

// LoadCredentialRegistry reads dir/registry.json and the credential CSVs it
// names. A missing manifest is reported as an error so the caller can fall back
// to single-account environment variables.
//
// Onboarding an account that has just become eligible is therefore a matter of
// dropping its CSV into dir and adding one line to the manifest.
func LoadCredentialRegistry(dir string) (*CredentialRegistry, error) {
	manifestPath := filepath.Join(dir, CredentialRegistryFile)

	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, err
	}

	var entries []registryEntry
	if err := json.Unmarshal(raw, &entries); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", manifestPath, err)
	}

	// One token endpoint serves every account; only the client credentials
	// differ, and those come from the CSVs.
	tokenURL := os.Getenv("AMAZON_CREATOR_TOKEN_URL")

	creds := make([]APICredential, 0, len(entries))
	for _, entry := range entries {
		path := entry.Creds
		if !filepath.IsAbs(path) {
			path = filepath.Join(dir, path)
		}

		clientID, clientSecret, err := readCredentialCSV(path)
		if err != nil {
			return nil, fmt.Errorf("credential for %s: %w", entry.Tag, err)
		}

		creds = append(creds, APICredential{
			Tag:          entry.Tag,
			ClientID:     clientID,
			ClientSecret: clientSecret,
			TokenURL:     tokenURL,
		})
	}

	return NewCredentialRegistry(creds), nil
}

// readCredentialCSV pulls the client ID and secret out of the CSV Amazon issues
// per account: columns Application, Application Id, Credential Id, Secret,
// Version, with a single data row. Columns are located by name rather than
// position so an extra column in a future download doesn't silently shift them.
func readCredentialCSV(path string) (clientID, clientSecret string, err error) {
	file, err := os.Open(path)
	if err != nil {
		return "", "", err
	}
	defer file.Close()

	reader := csv.NewReader(file)
	reader.FieldsPerRecord = -1
	reader.TrimLeadingSpace = true

	records, err := reader.ReadAll()
	if err != nil {
		return "", "", fmt.Errorf("reading %s: %w", path, err)
	}
	if len(records) < 2 {
		return "", "", fmt.Errorf("%s has a header but no credential row", path)
	}

	header := records[0]
	columnIndex := func(name string) int {
		for i, h := range header {
			if strings.EqualFold(strings.TrimSpace(h), name) {
				return i
			}
		}
		return -1
	}

	idIdx, secretIdx := columnIndex("Credential Id"), columnIndex("Secret")
	if idIdx < 0 || secretIdx < 0 {
		return "", "", fmt.Errorf("%s: want 'Credential Id' and 'Secret' columns, got %v", path, header)
	}

	row := records[1]
	if idIdx >= len(row) || secretIdx >= len(row) {
		return "", "", fmt.Errorf("%s: credential row has fewer columns than the header", path)
	}

	clientID = strings.TrimSpace(row[idIdx])
	clientSecret = strings.TrimSpace(row[secretIdx])
	if clientID == "" || clientSecret == "" {
		return "", "", fmt.Errorf("%s: credential row is missing an ID or secret", path)
	}

	return clientID, clientSecret, nil
}
