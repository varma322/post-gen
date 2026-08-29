# PostGen V3 Implementation Plan (v3)

Supersedes `V3_implementation_v2.md`. Two findings from reviewing v2 against the
codebase changed the design; everything else in v2 survives.

## What changed from v2

**1. Discovery is `searchItems`, not a new provider abstraction.**

v2 assumed the Creator API was already a discovery source. It is not — the
existing client at `internal/scraper/amazon_api.go:399` calls
`creatorsapi.amazon/catalog/v1/getItems`, an ASIN-lookup endpoint.

But the Creators API is the full successor to the now-deprecated PA-API 5, and
exposes four operations: **SearchItems**, **GetItems**, **GetVariations**,
**GetBrowseNodes**. Discovery is `POST creatorsapi.amazon/catalog/v1/searchItems`
— same host, same OAuth token, same partner tag, same circuit breaker. It is a
sibling method on `AmazonCreatorAPIScraper`, not a new subsystem.

**2. The scraper fallback lists Best Sellers, not Deals pages.**

`/deals` and `/gp/goldbox` render their grid client-side. `goquery` runs no
JavaScript, so it would extract nothing. `/gp/bestsellers/<category>` is
server-rendered and puts the category in the URL. The fallback only needs to
emit **ASINs** — discount comes from the per-product scrape that already works.

---

## Architecture

```text
              Discovery Schedule (task = deal_discovery)
                              │
                              ▼
                   ┌──────────────────────┐
                   │  Discovery Service   │
                   └──────────┬───────────┘
                              │
              ┌───────────────┴───────────────┐
              ▼                               ▼
     Creators API searchItems        [breaker open / zero results]
     (browse node × filter matrix)              │
              │                                 ▼
              │                    Best Sellers HTML lister
              │                    (ASIN candidates only)
              │                                 │
              │                                 ▼
              │                    Per-product HTML scrape
              │                    (existing AmazonScraper)
              └───────────────┬─────────────────┘
                              ▼
                         Deal Store  (deals, upsert on asin)
                              │
                              ▼
                        Deal Scoring
                              │
                              ▼
                    queued_products (existing)
                              │
                              ▼
                  Existing AI + Publisher pipeline
```

The API path yields numeric price, savings amount and discount percentage
directly — `apiPriceInfo.Amount float64` and `apiSavings.Percentage int` already
exist in `amazon_api.go:624-632`. Only the HTML fallback path needs string→numeric
parsing.

---

## Affiliate tag model

Two different tags flow through the pipeline. v2 and the current code treat them
as one value; V3 separates them.

| | What it decides | Constraint |
|---|---|---|
| API `partnerTag` | Which Associates account the API call is billed to | Must be API-eligible |
| Link `tag=` | Who earns commission on the post | Any valid tracking ID |

Today both are `acc.AffiliateTag`. `worker.go:340` passes it into
`scrapeWithEvents`, which becomes `WithPartnerTag(ctx, ...)` at `engine.go:473`,
and the same value is injected into the published URL by `AddAffiliateTag` at
`engine.go:285` and `worker.go:372`. With 14 active accounts and only 2 holding
API access, 12 of them call the API under an ineligible tag and silently fall
through to the HTML scraper.

**Decision: the link keeps each page's own tag; only the API call switches to an
eligible one.**

This is safe because the API response's own affiliate link never reaches the
post. `fetchFromAPI` builds its product with `Link: rawURL` (`amazon_api.go:483`)
— the original input URL, not anything the API returned. The published link is
always rebuilt by `AddAffiliateTag` from the source URL using the account's own
tag. The API `partnerTag` therefore affects quota attribution only, never post
content and never commission attribution.

### Credential registry

`GetScraper` builds one client from a single env credential set
(`scraper.go:66-79`). It becomes a registry of eligible accounts.

Amazon issues credentials as a per-account CSV — `AFF_creds/*.csv`, columns
`Application, Application Id, Credential Id, Secret, Version`, one row each.
That directory is the credential source, so onboarding a newly-eligible account
means dropping in its CSV and adding one config line. No code change.

The CSV carries no tracking ID, so the tag is mapped explicitly rather than
guessed from the filename:

```json
[
  { "tag": "offersbang08-21", "creds": "AFF_creds/offersbang-credentials.csv" },
  { "tag": "umabharti00-21",  "creds": "AFF_creds/uma-credentials.csv" }
]
```

`AFF_creds/` must stay gitignored — it holds live client secrets.

### Selection and failover

`circuitKey(partnerTag, marketplace)` (`amazon_api.go:86`) already keys the
breaker per tag, so each eligible account gets an independent breaker and quota
for free. Selection walks the registry, skipping any account whose breaker is
open, starting from a rotating index so load spreads across all of them:

- **Both healthy** — calls alternate, roughly doubling effective quota.
- **One throttled or refused** — its breaker opens and traffic moves to the
  other. `SearchItems` retries across accounts within a single call, so one bad
  account costs a retry, not a failed query.
- **All unavailable** — discovery yields nothing and the HTML path takes over,
  which is the existing behaviour.

> **Credentials are not eligibility.** Amazon issues client credentials per
> application but grants API access per Associates account. A registry entry can
> authenticate successfully and still be refused with
> `403 AssociateNotEligible`. That is why failover is per-call rather than
> per-run.

### Only one account is currently eligible

Verified live on 2026-08-29:

| Account | Credentials | API access |
|---|---|---|
| `offersbang08-21` | Valid | **Works** |
| `umabharti00-21` | Valid | **Refused** — `403 AssociateNotEligible` |

`umabharti00-21` authenticates fine and its CSV parses, but the account does not
meet the qualifying-sales bar. It is deliberately left in the registry: the
breaker sidelines it for an hour at a time, and it starts working by itself the
moment the account qualifies. The cost is one wasted call per hour.

Practically, **all 14 pages currently depend on `offersbang08-21` alone.**

`AMAZON_CATALOG_PARTNER_TAG` in `.env` is read by no code today. It is superseded
by the registry and should be removed.

---

## Package structure

```text
internal/models/deal.go   Deal, DealCandidate, status + provider constants
internal/db/deals.go      Persistence

internal/deals/
├── discovery.go     Service: orchestrates API → fallback, dedupe, upsert
├── query.go         Browse-node × filter query matrix
├── scoring.go       Score(Deal) int
├── service.go       Engine-facing methods (list, queue, ignore)
└── providers/
    ├── creator_api.go   searchItems client
    └── bestsellers.go   HTML lister
```

**Correction to the original layout.** v2 and the first draft of this plan put
`Deal` in `internal/deals/models.go`. That creates an import cycle: `internal/db`
needs the type to persist it, while `internal/deals` needs `db.Pool` to store
through. The repo already resolves this everywhere else — `internal/db` imports
`internal/models`, and `internal/core` imports both — so `Deal` and
`DealCandidate` live in `internal/models/deal.go` beside `QueuedProduct` and
`JobSchedule`. `internal/deals` holds discovery and scoring logic only.

Persistence matches the existing `internal/db/schedules.go` layout: a column
constant, a `scanDeal(rowScanner)` helper, and one method per operation.

### Types

```go
// DealCandidate is what a discovery provider emits: enough to identify and
// dedupe a product, not necessarily enough to score it.
type DealCandidate struct {
    ASIN        string
    Title       string
    URL         string
    Category    string   // known by construction: browse node or bestseller slug
    ImageURL    string
    Price       float64  // 0 when the provider could not determine it
    OldPrice    float64
    DiscountPct int
    Provider    string   // "creator_api" | "scraper"
}

// DiscoveryProvider returns candidates for one query.
type DiscoveryProvider interface {
    Discover(ctx context.Context, q Query) ([]DealCandidate, error)
    Name() string
}
```

`models.Product` is left untouched. It has no ASIN, no category and string
prices, and it is used across the whole generation path — widening it would ripple.
`Deal` is a separate type with numeric fields, converted to a `Product` only when
handed to the AI layer.

---

## Discovery: Creators API

`POST https://creatorsapi.amazon/catalog/v1/searchItems`

Request follows the same shape as the existing `getItems` payload
(`amazon_api.go:382-396`) — lowerCamelCase, `partnerTag`, `marketplace`,
`resources`, `Authorization: Bearer`, `x-marketplace`.

```go
payload := map[string]any{
    "keywords":         q.Keywords,     // optional
    "browseNodeId":     q.BrowseNodeID,
    "minSavingPercent": q.MinSavingPct,
    "itemPage":         page,
    "marketplace":      marketplace,
    "partnerTag":       partnerTag,
    "resources": []string{
        "itemInfo.title",
        "images.primary.large",
        "offersV2.listings.price",
    },
}
```

### Verified against the live API

Probed on 2026-08-29 under `offersbang08-21`. All names below are confirmed, not
inferred.

| Field | Status |
|---|---|
| `keywords`, `marketplace`, `partnerTag`, `resources` | Work |
| `browseNodeId` | Works. **Numeric only** — a non-numeric value is rejected |
| `minSavingPercent` | Genuinely filters, not just reorders |
| `itemPage` | Changes the result set |
| `sortBy` | Changes the result set |
| `browseNodeIds` (plural) | **Not recognised** — silently ignored |

Response shape is `searchResult.items[]` plus `searchResult.totalResultCount`.
Page size is **up to 10**, sometimes fewer (a 9-item page was observed), so the
matrix must not assume exactly 10.

Confirmed `resources` values: `itemInfo.title`, `images.primary.large`,
`offersV2.listings.price`, `browseNodeInfo.browseNodes`.

Auth is unchanged from `getItems` — the same OAuth token and `x-marketplace`
header work as-is.

### The stale-node trap

`browseNodeId` validates its *format*, not its *existence*:

```text
"not-a-real-node"  → HTTP 400  ValidationException
                                 Member must satisfy [1-9][0-9]*
"999999999031"     → HTTP 200  results identical to an unfiltered search
```

A numeric node ID that no longer exists therefore returns a silently unfiltered
keyword search rather than an error. Discovery would keep running and quietly
widen, scoring products from the wrong categories.

**The query matrix must validate its node IDs at startup** via `getBrowseNodes`,
and refuse to run a category query whose node did not resolve.

### Query matrix

SearchItems returns ~10 items per page with a page cap, so a single call cannot
fill a discovery run. Discovery fans out across a matrix:

```go
type Query struct {
    Category     string
    BrowseNodeID string
    MinSavingPct int
    Keywords     string
}
```

Seeded from the scoring categories (Electronics, Home, Kitchen, Fashion, Books)
crossed with saving tiers (30, 50, 70). Browse node IDs are stored in settings
rather than hardcoded, and validated at startup — see the stale-node trap above.

**Electronics on `www.amazon.in` is `976419031`**, confirmed by walking ancestors
from a live result. The remaining categories are found the same way:

```json
{
  "browseNodeIds": ["<any node from a search result>"],
  "marketplace": "www.amazon.in",
  "partnerTag":  "<eligible tag>",
  "resources":   ["browseNodes.ancestor", "browseNodes.children"]
}
```

Note that `getBrowseNodes` *does* take the plural `browseNodeIds`, unlike
`searchItems`.

One caveat when harvesting nodes: the browse nodes attached to an item are mostly
merchandising groupings — observed examples include "March Headset Mania |
Clearance store", "Top150ASINhero" and "low asp inclusion" — not clean
categories. Taking the first node on an item yields junk. The category root only
appears by following `ancestor` to the top of the chain.

Pacing: one page per query per run, sequential, reusing the existing retry and
throttle handling. The circuit breaker in `amazon_api.go:90` already covers this
host — a 429 during discovery trips the same breaker that protects enrichment,
which is correct: it is one quota.

---

## Discovery: Best Sellers fallback

Runs only when the breaker is open or the API returned nothing.

Paths: `/gp/bestsellers/<slug>`, plus `/gp/movers-and-shakers/<slug>` (identical
HTML structure, ranked by rank-gain, which tracks price drops better than raw
bestseller rank).

Two mitigations are part of the lister, not later optimisations — without them a
run means ~50 product fetches on the exact path taken *because Amazon already
failed*, and 50 chances at a hard 403:

1. **Pre-filter from the tile.** Parse price and strikethrough MRP from the
   listing tile and compute the discount there. Only candidates already clearing
   the threshold get a full product fetch. Turns ~50 fetches into ~7.
2. **Dedupe against `deals.asin` before fetching.** Bestseller lists barely move
   day to day, so after the first run most ASINs are known and need no fetch —
   which is what `last_seen` is for.

### Selector config

Listing selectors go in `selectors.json` as a parallel section, not overloaded
onto the product selectors:

```json
{
  "amazon": { "title": "#productTitle", "...": "..." },
  "amazon_listings": {
    "item": "[id='gridItemRoot'], .zg-grid-general-faceout",
    "asin": "[data-asin]",
    "title": ".p13n-sc-truncate-desktop-type2",
    "price": ".p13n-sc-price",
    "mrp": ".a-text-price .a-offscreen",
    "link": "a.a-link-normal"
  }
}
```

`config.Selectors` is `map[string]PlatformSelectors` (`selectors.go:17`), so this
needs a second struct and a second loader field rather than a new map entry.
Selectors stay out of Go code, per existing convention.

---

## Database

Follows existing migration style in `internal/db/db.go:95` — `SERIAL`,
`IF NOT EXISTS`, `updated_at` trigger.

```sql
CREATE TABLE IF NOT EXISTS deals (
    id               SERIAL PRIMARY KEY,
    asin             VARCHAR(16) UNIQUE NOT NULL,
    title            TEXT NOT NULL,
    url              TEXT NOT NULL,
    category         VARCHAR(64),
    image_url        TEXT,
    price            NUMERIC(12,2),
    old_price        NUMERIC(12,2),
    discount_percent INT,
    score            INT NOT NULL DEFAULT 0,
    provider         VARCHAR(32) NOT NULL,
    status           VARCHAR(32) NOT NULL DEFAULT 'new',
    first_seen       TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_seen        TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_at       TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at       TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_deals_status_score ON deals(status, score DESC);
CREATE INDEX IF NOT EXISTS idx_deals_last_seen ON deals(last_seen);

DROP TRIGGER IF EXISTS update_deals_updated_at ON deals;
CREATE TRIGGER update_deals_updated_at
    BEFORE UPDATE ON deals
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();
```

`asin UNIQUE` is required — v2 omitted it, leaving the `last_seen` upsert nothing
to conflict on.

Status: `new`, `approved`, `queued`, `posted`, `expired`, `ignored`.
Provider: `creator_api`, `scraper`.

### Queue bridge

`queued_products.url` is `UNIQUE` (`db.go:152`). Queuing a deal inserts through
the existing `AddQueuedProduct` path using the deal's canonical URL, so the whole
downstream pipeline is untouched. The deal row moves to `status = 'queued'`.

---

## Scoring

Unchanged from v2 — discount + savings + category tiers, no ratings or review
counts. v2's category tier is now trivially satisfiable: the API path searches
*within* a browse node and the fallback reads the category from the bestseller
slug, so category is known by construction. No classifier needed.

```text
score = discountScore + savingsScore + categoryScore

discount:  10%+ →10   20%+ →20   30%+ →30   50%+ →50   70%+ →70
savings:   ₹100+ →5   ₹500+ →10  ₹1000+ →20 ₹2000+ →30
category:  Electronics 25  Home 20  Kitchen 20  Fashion 15  Books 5

70+ auto-queue    50+ manual review    <50 ignore
```

v2's `performanceScore` term is dropped from the formula until CTR data exists to
feed it (Sprint 4). It was listed in the formula but never defined.

---

## Scheduler

`job_schedules.kind` is `interval|daily` and answers *when* (`models/schedule.go:12`).
Discovery needs a second axis answering *what*. v2's `"type": "deal_discovery"`
conflated the two.

```sql
ALTER TABLE job_schedules ADD COLUMN IF NOT EXISTS task VARCHAR(32) NOT NULL DEFAULT 'auto_post';
```

`fireSchedule` (`core/schedule.go:96`) branches on `task`: `auto_post` keeps
calling `TriggerAutoPostJob`, `deal_discovery` calls the discovery service. Run
recording, next-window arming and error capture are shared and unchanged. Kind
still controls timing for both, so a discovery schedule can be interval or daily
with no extra work.

---

## Events

Existing constants are `SCREAMING_SNAKE` (`events.go:27`) and `source` is a
filtered analytics column (`db/analytics.go:39`). v2's lowercase dotted names
would break both.

```go
const (
    DealDiscovered Type = "DEAL_DISCOVERED"
    DealUpdated    Type = "DEAL_UPDATED"
    DealScored     Type = "DEAL_SCORED"
    DealQueued     Type = "DEAL_QUEUED"
    DealPosted     Type = "DEAL_POSTED"
    DealExpired    Type = "DEAL_EXPIRED"
)

const SourceDiscovery = "discovery"
```

`deal.generated` from v2 is dropped — the existing `AI_GENERATION_*` events
already cover that step once a deal is in the queue.

Provider attribution rides in `Event.Metadata` (`provider: creator_api|scraper`),
which analytics already carries as JSONB.

---

## API

Flat mux, matching `handler.go:34-56` (prefix handler for the by-ASIN routes):

```text
GET  /deals               list; status/category/provider/min_score/limit
GET  /deals/{asin}        single deal (ASIN is upper-cased)
POST /deals/discover      trigger a discovery run now
POST /deals/{asin}/ignore mark ignored
POST /deals/{asin}/queue  push into queued_products     (Sprint 2)
GET  /analytics/deals     counts + provider split        (Sprint 3)
```

Status codes follow the existing handlers: 400 for a bad filter, 404 for an
unknown ASIN, 405 for the wrong method, 401 without a bearer token. A missing
database or missing Creators API credentials reports **503**, not 500 — that is
a configuration state rather than a server fault.

`POST /deals/discover` runs synchronously. The matrix is paced but small, and an
operator pressing the button wants the counts back rather than a job id to poll.
A failed run still returns its partial counts alongside the error, so the
operator can see how far it got.

`POST /deals/{asin}/publish` from v2 is dropped for now — publishing already goes
through the job pipeline, and a second immediate-publish path would bypass quota
and cooldown checks in `checkAccountEligibility` (`engine.go:1042`). Queue then
publish.

---

## UI

Nav entry between Analytics and Content Queue in `AppShell.jsx:15-16`:

```js
{ id: 'deals', label: 'Deals', icon: 'local_offer' },
```

New `screens/Deals.jsx`, wired in `App.jsx` alongside the other screens.

Columns: ASIN · Product · Category · Discount · Price · Score · Provider · Status
Actions: Queue · Generate Post · Ignore
Header: last run time, provider split, breaker state (already available from
`GetCircuitBreakerStatus`, `amazon_api.go:105`).

---

## Sprints

### Sprint 1 — Discovery spine
1. ~~Live `searchItems` call to confirm filter parameter names.~~ **Done** — see
   "Verified against the live API" above.
2. ~~Credential registry — load `AFF_creds/*.csv`, decouple API `partnerTag` from
   the link tag, rotate across eligible accounts and skip open breakers.~~
   **Done** — `internal/scraper/credentials.go`.
3. ~~`deals` table + `internal/db/deals.go`~~ **Done** — schema in `db.go`
   `migrate()`, model in `internal/models/deal.go`, persistence in
   `internal/db/deals.go`, verified against the live database.
4. ~~`providers/creator_api.go` — `searchItems` on the existing client~~
   **Done** — `SearchItems` in `internal/scraper/amazon_search.go` with
   cross-account failover, shared request handling extracted to
   `amazon_catalog.go`, provider in `internal/deals/providers/creator_api.go`.
   Verified live: 10 candidates for Electronics at 50%+ off.
5. ~~`internal/deals/discovery.go` — query matrix, upsert, dedupe~~ **Done** —
   matrix in `query.go` with node validation, service in `discovery.go`,
   `getBrowseNodes` in `internal/scraper/amazon_browsenodes.go`. Verified live:
   2 queries, 20 candidates, 19 stored after dedupe.
6. ~~`GET /deals`, `POST /deals/discover`~~ **Done** — engine methods in
   `internal/core/deals.go`, handlers in `internal/api/deals.go`. Verified
   against a running server.
7. ~~Events + provider attribution~~ **Done** — `DISCOVERY_*` and `DEAL_*`
   types in `events.go`, provider carried in event metadata.

### Sprint 2 — Scoring, queue, UI
8. `scoring.go` + auto-queue threshold
9. Queue bridge into `queued_products`
10. `Deals.jsx` + nav
11. `POST /deals/{asin}/queue`, `/ignore`

### Sprint 3 — Automation
12. ~~`job_schedules.task` column + `fireSchedule` branch~~ **Done** — task is a
    second axis beside kind, so discovery runs on either cadence.
13. ~~Best Sellers HTML lister + `amazon_listings` selectors~~ **Done** —
    verified live: 8 tiles parsed from the real page. See the pre-filter note.
14. ~~`GET /analytics/deals` + provider split panel~~ **Done**

### Sprint 4 — Later
15. Price history table (defends against inflated MRP, which discount% trusts)
16. CTR-based `performanceScore`
17. Additional API-eligible accounts as they qualify — drop in a CSV, add one
    registry line
18. Additional affiliate networks

The fallback lister moves from Sprint 1 to Sprint 3 deliberately. Discovery and
enrichment now share one credential and one breaker, so a discovery fallback only
fires when Associates access is gone entirely. Building it first would mean
building the fragile part before the reliable part works.

---

## Risks

**Associates eligibility — currently a single point of failure.** Creators API
access requires enrollment plus **at least 10 qualifying sales in the past 30
days**, per account. Live testing found only `offersbang08-21` eligible;
`umabharti00-21` authenticates but is refused. So all 14 active pages depend on
**one** account for both discovery and enrichment, and if it lapses the whole
system drops to HTML scraping.

The failover machinery is built and tested, but it has nothing to fail over to
until a second account qualifies. Getting `umabharti00-21` over the bar is the
highest-value operational task in this plan — it needs no code.

**Stale browse node IDs degrade silently.** A numeric node that no longer exists
returns an unfiltered keyword search with HTTP 200, so discovery would keep
running while quietly widening. Node IDs must be validated at startup rather
than trusted.

**Inflated MRP.** `discountPercent` is computed against `savingBasis`, which is
Amazon's list price and frequently inflated. Scoring will over-rank fake
discounts until price history exists (Sprint 4).

**Bestseller selectors.** Amazon rotates class names on listing pages more often
than on product pages. Keeping them in `selectors.json` means a break is a config
edit, not a redeploy.

---

## Success criteria

- Creators API `searchItems` is the primary discovery source
- Discovery fans out across a browse-node × saving-tier matrix
- Provider recorded per deal and visible in analytics
- Existing circuit breaker, token manager and partner tag reused unchanged
- Deals reach Facebook through the existing queue → AI → publisher pipeline
- Best Sellers fallback yields ASINs when the API is unavailable
- API calls run under an eligible tag; published links keep each page's own tag
- A newly eligible account is onboarded by adding a CSV and one registry line
