package scraper

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// The circuit breaker is package-global, so every test here scopes itself to
// its own tags rather than inheriting another test's open circuit.

func writeCSV(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("writing %s: %v", name, err)
	}
	return path
}

func TestReadCredentialCSVLocatesColumnsByName(t *testing.T) {
	dir := t.TempDir()
	path := writeCSV(t, dir, "creds.csv",
		"Application,Application Id,Credential Id,Secret,Version\n"+
			"offersbang,amzn1.app.1,amzn1.cred.ID,shhh-secret,1\n")

	id, secret, err := readCredentialCSV(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "amzn1.cred.ID" {
		t.Errorf("clientID = %q, want the Credential Id column", id)
	}
	if secret != "shhh-secret" {
		t.Errorf("clientSecret = %q, want the Secret column", secret)
	}
}

func TestReadCredentialCSVHandlesQuotedValuesAndNoTrailingNewline(t *testing.T) {
	// Amazon's download quotes some fields and does not always end with a
	// newline; both were true of the files this was written against.
	dir := t.TempDir()
	path := writeCSV(t, dir, "creds.csv",
		"Application,Application Id,Credential Id,Secret,Version\n"+
			`"uma","amzn1.app.2","amzn1.cred.TWO","secret,with,commas","1"`)

	id, secret, err := readCredentialCSV(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "amzn1.cred.TWO" {
		t.Errorf("clientID = %q, want the quoted Credential Id", id)
	}
	if secret != "secret,with,commas" {
		t.Errorf("clientSecret = %q, want the commas kept inside the quotes", secret)
	}
}

func TestReadCredentialCSVSurvivesAnExtraColumn(t *testing.T) {
	// Columns are located by name so a future download gaining a column does
	// not silently shift the secret out from under us.
	dir := t.TempDir()
	path := writeCSV(t, dir, "creds.csv",
		"Application,Marketplace,Application Id,Credential Id,Secret,Version\n"+
			"offersbang,www.amazon.in,amzn1.app.1,amzn1.cred.ID,shhh,1\n")

	id, secret, err := readCredentialCSV(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "amzn1.cred.ID" || secret != "shhh" {
		t.Errorf("got id=%q secret=%q, want the named columns despite the extra one", id, secret)
	}
}

func TestReadCredentialCSVRejectsAHeaderOnlyFile(t *testing.T) {
	dir := t.TempDir()
	path := writeCSV(t, dir, "creds.csv", "Application,Application Id,Credential Id,Secret,Version\n")

	if _, _, err := readCredentialCSV(path); err == nil {
		t.Error("expected an error for a file with no credential row")
	}
}

func TestReadCredentialCSVRejectsMissingColumns(t *testing.T) {
	dir := t.TempDir()
	path := writeCSV(t, dir, "creds.csv", "Application,Version\noffersbang,1\n")

	if _, _, err := readCredentialCSV(path); err == nil {
		t.Error("expected an error when Credential Id and Secret are absent")
	}
}

func TestNewCredentialRegistrySkipsIncompleteEntries(t *testing.T) {
	registry := NewCredentialRegistry([]APICredential{
		{Tag: "reg-complete-21", ClientID: "id", ClientSecret: "secret"},
		{Tag: "", ClientID: "id", ClientSecret: "secret"},          // no tag
		{Tag: "reg-notag-21", ClientID: "", ClientSecret: "secret"}, // no client id
		{Tag: "reg-nosecret-21", ClientID: "id", ClientSecret: ""},  // no secret
		{Tag: "reg-complete-21", ClientID: "other", ClientSecret: "other"}, // duplicate
	})

	if registry.Len() != 1 {
		t.Fatalf("Len = %d, want only the one complete, non-duplicate entry", registry.Len())
	}
	if got := registry.Tags(); len(got) != 1 || got[0] != "reg-complete-21" {
		t.Errorf("Tags = %v, want [reg-complete-21]", got)
	}
}

func TestResolvePrefersTheAccountsOwnTagWhenItIsEligible(t *testing.T) {
	registry := NewCredentialRegistry([]APICredential{
		{Tag: "resolve-a-21", ClientID: "id", ClientSecret: "secret"},
		{Tag: "resolve-b-21", ClientID: "id", ClientSecret: "secret"},
	})

	// Asking repeatedly must keep landing on the preferred account, not drift
	// onto the rotation - attribution should match the publishing page whenever
	// that page is itself eligible.
	for i := 0; i < 5; i++ {
		set := registry.resolve("resolve-b-21", "www.amazon.in")
		if set == nil || set.tag != "resolve-b-21" {
			t.Fatalf("resolve picked %v, want the preferred eligible tag", set)
		}
	}
}

func TestResolveRotatesWhenThePreferredTagIsNotEligible(t *testing.T) {
	registry := NewCredentialRegistry([]APICredential{
		{Tag: "rotate-a-21", ClientID: "id", ClientSecret: "secret"},
		{Tag: "rotate-b-21", ClientID: "id", ClientSecret: "secret"},
	})

	// A page with no API access of its own spreads its lookups across both
	// eligible accounts rather than exhausting one.
	seen := map[string]bool{}
	for i := 0; i < 6; i++ {
		set := registry.resolve("ineligible-page-21", "www.amazon.in")
		if set == nil {
			t.Fatal("resolve returned nil with two healthy accounts")
		}
		seen[set.tag] = true
	}

	if len(seen) != 2 {
		t.Errorf("used %d of 2 eligible accounts (%v), want both", len(seen), seen)
	}
}

func TestResolveSkipsAThrottledAccount(t *testing.T) {
	registry := NewCredentialRegistry([]APICredential{
		{Tag: "skip-open-21", ClientID: "id", ClientSecret: "secret"},
		{Tag: "skip-healthy-21", ClientID: "id", ClientSecret: "secret"},
	})

	tripCreatorAPICircuit("skip-open-21", "www.amazon.in", time.Hour)

	// One account being throttled moves its traffic to the other rather than
	// dropping the whole system to HTML.
	for i := 0; i < 4; i++ {
		set := registry.resolve("skip-open-21", "www.amazon.in")
		if set == nil {
			t.Fatal("resolve returned nil while one account was still healthy")
		}
		if set.tag != "skip-healthy-21" {
			t.Errorf("resolve picked %q, want the account whose circuit is closed", set.tag)
		}
	}
}

func TestResolveReturnsNilWhenEveryAccountIsThrottled(t *testing.T) {
	registry := NewCredentialRegistry([]APICredential{
		{Tag: "allopen-a-21", ClientID: "id", ClientSecret: "secret"},
		{Tag: "allopen-b-21", ClientID: "id", ClientSecret: "secret"},
	})

	tripCreatorAPICircuit("allopen-a-21", "www.amazon.in", time.Hour)
	tripCreatorAPICircuit("allopen-b-21", "www.amazon.in", time.Hour)

	if set := registry.resolve("allopen-a-21", "www.amazon.in"); set != nil {
		t.Errorf("resolve = %q, want nil so the caller falls back to HTML", set.tag)
	}
}

func TestResolveScopesCircuitsPerMarketplace(t *testing.T) {
	registry := NewCredentialRegistry([]APICredential{
		{Tag: "mkt-only-21", ClientID: "id", ClientSecret: "secret"},
	})

	tripCreatorAPICircuit("mkt-only-21", "www.amazon.in", time.Hour)

	if set := registry.resolve("mkt-only-21", "www.amazon.in"); set != nil {
		t.Error("expected the throttled marketplace to be skipped")
	}
	if set := registry.resolve("mkt-only-21", "www.amazon.com"); set == nil {
		t.Error("a circuit open on one marketplace should not close another")
	}
}

func TestResolveCredentialKeepsTheCallerTagForASingleAccount(t *testing.T) {
	// With nothing to choose between, the existing semantics stand: the tag the
	// caller asked for is the tag the call is made under.
	scraper := NewAmazonCreatorAPIScraper("id", "secret", "", "single-default-21", nil)

	ctx := WithPartnerTag(context.Background(), "single-caller-21")
	tokenManager, tag := scraper.resolveCredential(ctx, "www.amazon.in")

	if tag != "single-caller-21" {
		t.Errorf("tag = %q, want the caller's tag to stand with one credential set", tag)
	}
	if tokenManager == nil {
		t.Error("expected the single credential set's token manager")
	}
}

func TestResolveCredentialSwitchesAnIneligiblePageToAnEligibleAccount(t *testing.T) {
	registry := NewCredentialRegistry([]APICredential{
		{Tag: "switch-eligible-21", ClientID: "id", ClientSecret: "secret"},
		{Tag: "switch-other-21", ClientID: "id", ClientSecret: "secret"},
	})
	scraper := NewAmazonCreatorAPIScraperWithRegistry(registry, nil)

	// This is the case that matters in production: a page whose own tag has no
	// API access must still get its catalog lookup served.
	ctx := WithPartnerTag(context.Background(), "page-without-api-21")
	tokenManager, tag := scraper.resolveCredential(ctx, "www.amazon.in")

	if tag == "page-without-api-21" {
		t.Error("the API call must not run under a tag that has no API access")
	}
	if tag != "switch-eligible-21" && tag != "switch-other-21" {
		t.Errorf("tag = %q, want one of the eligible accounts", tag)
	}
	if tokenManager == nil {
		t.Error("expected the selected account's token manager")
	}
}

func TestResolveCredentialReportsNoTokenManagerWhenAllThrottled(t *testing.T) {
	registry := NewCredentialRegistry([]APICredential{
		{Tag: "exhausted-a-21", ClientID: "id", ClientSecret: "secret"},
		{Tag: "exhausted-b-21", ClientID: "id", ClientSecret: "secret"},
	})
	scraper := NewAmazonCreatorAPIScraperWithRegistry(registry, nil)

	tripCreatorAPICircuit("exhausted-a-21", "www.amazon.in", time.Hour)
	tripCreatorAPICircuit("exhausted-b-21", "www.amazon.in", time.Hour)

	ctx := WithPartnerTag(context.Background(), "page-tag-21")
	tokenManager, _ := scraper.resolveCredential(ctx, "www.amazon.in")

	if tokenManager != nil {
		t.Error("expected no token manager once every eligible account is throttled")
	}
}

func TestNewScraperWithSingleEntryRegistryCollapsesToTheSinglePath(t *testing.T) {
	registry := NewCredentialRegistry([]APICredential{
		{Tag: "collapse-21", ClientID: "id", ClientSecret: "secret"},
	})
	scraper := NewAmazonCreatorAPIScraperWithRegistry(registry, nil)

	if scraper.defaultPartnerTag != "collapse-21" {
		t.Errorf("defaultPartnerTag = %q, want the only eligible tag", scraper.defaultPartnerTag)
	}
	if scraper.tokenManager == nil {
		t.Error("a one-account registry should still supply a token manager")
	}
}

func TestLoadCredentialRegistryReadsManifestAndCSVs(t *testing.T) {
	dir := t.TempDir()
	writeCSV(t, dir, "one-credentials.csv",
		"Application,Application Id,Credential Id,Secret,Version\none,app1,cred-one,secret-one,1\n")
	writeCSV(t, dir, "two-credentials.csv",
		"Application,Application Id,Credential Id,Secret,Version\ntwo,app2,cred-two,secret-two,1\n")

	manifest := `[
	  {"tag": "load-one-21", "creds": "one-credentials.csv"},
	  {"tag": "load-two-21", "creds": "two-credentials.csv"}
	]`
	if err := os.WriteFile(filepath.Join(dir, CredentialRegistryFile), []byte(manifest), 0o600); err != nil {
		t.Fatalf("writing manifest: %v", err)
	}

	registry, err := LoadCredentialRegistry(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if registry.Len() != 2 {
		t.Fatalf("Len = %d, want both accounts loaded", registry.Len())
	}

	tags := registry.Tags()
	if tags[0] != "load-one-21" || tags[1] != "load-two-21" {
		t.Errorf("Tags = %v, want manifest order preserved", tags)
	}
}

func TestLoadCredentialRegistryReportsAMissingManifest(t *testing.T) {
	// A missing manifest is how a single-account install looks, and the caller
	// distinguishes it with os.IsNotExist to fall back to env credentials.
	_, err := LoadCredentialRegistry(t.TempDir())
	if !os.IsNotExist(err) {
		t.Errorf("err = %v, want a not-exist error the caller can recognise", err)
	}
}

func TestLoadCredentialRegistryReportsAMissingCSV(t *testing.T) {
	dir := t.TempDir()
	manifest := `[{"tag": "missing-21", "creds": "not-there.csv"}]`
	if err := os.WriteFile(filepath.Join(dir, CredentialRegistryFile), []byte(manifest), 0o600); err != nil {
		t.Fatalf("writing manifest: %v", err)
	}

	if _, err := LoadCredentialRegistry(dir); err == nil {
		t.Error("expected an error when the manifest names a file that is not there")
	}
}

func TestCredentialsDirIsOverridable(t *testing.T) {
	t.Setenv("AFF_CREDS_DIR", "/somewhere/else")
	if got := CredentialsDir(); got != "/somewhere/else" {
		t.Errorf("CredentialsDir = %q, want the override", got)
	}

	t.Setenv("AFF_CREDS_DIR", "")
	if got := CredentialsDir(); got != "AFF_creds" {
		t.Errorf("CredentialsDir = %q, want the default", got)
	}
}
