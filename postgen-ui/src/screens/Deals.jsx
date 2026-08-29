import { useCallback, useEffect, useMemo, useState } from 'react';
import {
  Button, Card, CardHeader, EmptyState, ErrorNotice, KpiCard, Skeleton, StatusPill,
  formatNumber, relativeTime,
} from '../components/ui';

const PAGE_SIZE = 25;

const STATUSES = ['new', 'approved', 'queued', 'posted', 'expired', 'ignored'];
const PROVIDERS = [
  { id: 'creator_api', label: 'Creators API' },
  { id: 'scraper', label: 'Scraper' },
];

/** Score bands mirror the scoring thresholds in internal/deals/scoring.go. */
const AUTO_QUEUE_SCORE = 70;
const REVIEW_SCORE = 50;

function scoreTone(score) {
  if (score >= AUTO_QUEUE_SCORE) return 'text-secondary';
  if (score >= REVIEW_SCORE) return 'text-tertiary';
  return 'text-on-surface-variant';
}

function rupees(value) {
  if (!value) return '—';
  return `₹${Number(value).toLocaleString('en-IN', { maximumFractionDigits: 0 })}`;
}

/**
 * Deals discovered from Amazon, before they reach the publishing queue.
 *
 * Discovery scores every deal it finds and marks anything above the auto-queue
 * threshold as approved. This screen is where an operator sees what was found,
 * queues what scoring left undecided, and rejects what it should not have
 * surfaced.
 */
export default function Deals({ apiFetch }) {
  const [deals, setDeals] = useState([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [discovering, setDiscovering] = useState(false);
  const [lastRun, setLastRun] = useState(null);
  const [busyAsin, setBusyAsin] = useState('');
  const [analytics, setAnalytics] = useState(null);
  const [page, setPage] = useState(0);

  const [status, setStatus] = useState('');
  const [provider, setProvider] = useState('');
  const [minScore, setMinScore] = useState('');
  const [search, setSearch] = useState('');

  const load = useCallback(async () => {
    setLoading(true);
    setError('');
    try {
      const params = new URLSearchParams();
      if (status) params.set('status', status);
      if (provider) params.set('provider', provider);
      if (minScore) params.set('min_score', minScore);
      params.set('limit', '500');

      const [dealsResp, analyticsResp] = await Promise.all([
        apiFetch(`/deals?${params}`),
        apiFetch('/analytics/deals'),
      ]);
      if (!dealsResp.ok) {
        const body = await dealsResp.json().catch(() => ({}));
        throw new Error(body.error || 'Failed to load deals');
      }
      setDeals((await dealsResp.json()).deals || []);
      // Catalog-wide counts, so the tiles stay true as the table outgrows one
      // page of results.
      if (analyticsResp.ok) setAnalytics(await analyticsResp.json());
    } catch (err) {
      setError(err.message);
    } finally {
      setLoading(false);
    }
  }, [apiFetch, status, provider, minScore]);

  useEffect(() => { load(); }, [load]);
  useEffect(() => { setPage(0); }, [status, provider, minScore, search]);

  const runDiscovery = async () => {
    setDiscovering(true);
    setError('');
    try {
      const resp = await apiFetch('/deals/discover', { method: 'POST' });
      const body = await resp.json().catch(() => ({}));
      if (!resp.ok) throw new Error(body.error || 'Discovery failed');
      setLastRun(body);
      await load();
    } catch (err) {
      setError(err.message);
    } finally {
      setDiscovering(false);
    }
  };

  const act = async (asin, action) => {
    setBusyAsin(asin);
    setError('');
    try {
      const resp = await apiFetch(`/deals/${asin}/${action}`, { method: 'POST' });
      if (!resp.ok) {
        const body = await resp.json().catch(() => ({}));
        throw new Error(body.error || `Could not ${action} ${asin}`);
      }
      await load();
    } catch (err) {
      setError(err.message);
    } finally {
      setBusyAsin('');
    }
  };

  const filtered = useMemo(() => {
    const needle = search.trim().toLowerCase();
    if (!needle) return deals;
    return deals.filter((deal) =>
      deal.title?.toLowerCase().includes(needle) || deal.asin?.toLowerCase().includes(needle));
  }, [deals, search]);

  const totals = useMemo(() => ({
    total: analytics?.total ?? deals.length,
    approved: analytics?.by_status?.approved ?? 0,
    queued: analytics?.by_status?.queued ?? 0,
    creator_api: analytics?.by_provider?.creator_api ?? 0,
    scraper: analytics?.by_provider?.scraper ?? 0,
    apiShare: analytics?.provider_share?.creator_api,
  }), [analytics, deals.length]);

  const pageCount = Math.max(1, Math.ceil(filtered.length / PAGE_SIZE));
  const visible = filtered.slice(page * PAGE_SIZE, (page + 1) * PAGE_SIZE);

  return (
    <div className="space-y-5">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <p className="text-body-md text-on-surface-variant">
          Discounted products found on Amazon, scored before they reach the queue.
        </p>
        <Button variant="primary" icon="travel_explore" onClick={runDiscovery} disabled={discovering}>
          {discovering ? 'Discovering…' : 'Run discovery'}
        </Button>
      </div>

      {error && <ErrorNotice message={error} onRetry={load} />}

      {lastRun && (
        <Card padded={false}>
          <div className="flex flex-wrap items-center gap-x-6 gap-y-2 px-4 py-3 text-body-sm">
            <span className="text-on-surface-variant">Last run</span>
            <span><strong className="tabular">{formatNumber(lastRun.new)}</strong> new</span>
            <span><strong className="tabular">{formatNumber(lastRun.updated)}</strong> updated</span>
            <span><strong className="tabular">{formatNumber(lastRun.candidates)}</strong> candidates</span>
            <span>
              <strong className="tabular">{formatNumber(lastRun.queries)}</strong> queries in{' '}
              {((lastRun.elapsed_ms || 0) / 1000).toFixed(1)}s
            </span>
            {lastRun.failed > 0 && (
              <span className="text-tertiary">{lastRun.failed} failed</span>
            )}
          </div>
          {lastRun.errors?.length > 0 && (
            <ul className="border-t border-outline-variant px-4 py-2 text-body-sm text-tertiary">
              {lastRun.errors.map((message) => <li key={message}>{message}</li>)}
            </ul>
          )}
        </Card>
      )}

      <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
        <KpiCard label="Deals stored" value={totals.total} icon="local_offer" />
        <KpiCard label="Approved" value={totals.approved} icon="task_alt"
                 hint={`Score ${AUTO_QUEUE_SCORE}+ queues automatically`} />
        <KpiCard label="Queued" value={totals.queued} icon="playlist_add_check" />
        <KpiCard label="From Creators API" value={totals.creator_api} icon="api"
                 hint={totals.apiShare != null
                   ? `${totals.apiShare}% of the catalog · ${totals.scraper} from the scraper`
                   : 'No scraper fallback used'} />
      </div>

      {analytics?.top_categories?.length > 1 && (
        <Card padded={false}>
          <CardHeader title="By category" icon="category" />
          <div className="overflow-x-auto">
            <table className="w-full min-w-[28rem] text-body-sm">
              <thead className="border-b border-outline-variant text-label-md text-on-surface-variant">
                <tr>
                  <th className="px-4 py-2 text-left font-medium">Category</th>
                  <th className="px-3 py-2 text-right font-medium">Deals</th>
                  <th className="px-3 py-2 text-right font-medium">Avg score</th>
                  <th className="px-4 py-2 text-right font-medium">Reached the queue</th>
                </tr>
              </thead>
              <tbody>
                {analytics.top_categories.map((row) => (
                  <tr key={row.category} className="border-b border-outline-variant/60 last:border-0">
                    <td className="px-4 py-2">{row.category}</td>
                    <td className="px-3 py-2 text-right tabular">{formatNumber(row.deals)}</td>
                    <td className={`px-3 py-2 text-right tabular ${scoreTone(row.average_score)}`}>
                      {row.average_score.toFixed(1)}
                    </td>
                    <td className="px-4 py-2 text-right tabular">{formatNumber(row.queued)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </Card>
      )}

      <Card padded={false}>
        <CardHeader
          title="Discovered deals"
          subtitle={`${filtered.length} of ${deals.length} shown`}
          icon="local_offer"
        />

        <div className="flex flex-wrap items-center gap-2 border-b border-outline-variant px-4 py-3">
          <input
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            placeholder="Search title or ASIN"
            className="min-w-48 flex-1 rounded border border-outline-variant bg-surface px-3 py-1.5 text-body-sm"
          />
          <select
            value={status}
            onChange={(e) => setStatus(e.target.value)}
            className="rounded border border-outline-variant bg-surface px-3 py-1.5 text-body-sm"
          >
            <option value="">All statuses</option>
            {STATUSES.map((value) => <option key={value} value={value}>{value}</option>)}
          </select>
          <select
            value={provider}
            onChange={(e) => setProvider(e.target.value)}
            className="rounded border border-outline-variant bg-surface px-3 py-1.5 text-body-sm"
          >
            <option value="">All sources</option>
            {PROVIDERS.map((item) => <option key={item.id} value={item.id}>{item.label}</option>)}
          </select>
          <select
            value={minScore}
            onChange={(e) => setMinScore(e.target.value)}
            className="rounded border border-outline-variant bg-surface px-3 py-1.5 text-body-sm"
          >
            <option value="">Any score</option>
            <option value={AUTO_QUEUE_SCORE}>{AUTO_QUEUE_SCORE}+ (auto-queue)</option>
            <option value={REVIEW_SCORE}>{REVIEW_SCORE}+ (review)</option>
          </select>
        </div>

        {loading ? (
          <div className="space-y-2 p-4">
            {[0, 1, 2, 3].map((i) => <Skeleton key={i} height={44} />)}
          </div>
        ) : filtered.length === 0 ? (
          <EmptyState
            icon="local_offer"
            title="No deals yet"
            hint="Run discovery to search Amazon for discounted products."
          />
        ) : (
          <>
            <div className="overflow-x-auto">
              <table className="w-full min-w-[56rem] text-body-sm">
                <thead className="border-b border-outline-variant text-label-md text-on-surface-variant">
                  <tr>
                    <th className="px-4 py-2 text-left font-medium">Product</th>
                    <th className="px-3 py-2 text-left font-medium">Category</th>
                    <th className="px-3 py-2 text-right font-medium">Price</th>
                    <th className="px-3 py-2 text-right font-medium">Off</th>
                    <th className="px-3 py-2 text-right font-medium">Score</th>
                    <th className="px-3 py-2 text-left font-medium">Source</th>
                    <th className="px-3 py-2 text-left font-medium">Status</th>
                    <th className="px-4 py-2 text-right font-medium">Actions</th>
                  </tr>
                </thead>
                <tbody>
                  {visible.map((deal) => {
                    const busy = busyAsin === deal.asin;
                    const settled = deal.status === 'queued' || deal.status === 'posted';

                    return (
                      <tr key={deal.asin} className="border-b border-outline-variant/60 last:border-0">
                        <td className="max-w-md px-4 py-2.5">
                          <a
                            href={deal.url}
                            target="_blank"
                            rel="noopener noreferrer"
                            className="line-clamp-2 text-on-surface hover:text-primary"
                            title={deal.title}
                          >
                            {deal.title}
                          </a>
                          <span className="text-label-sm text-on-surface-variant tabular">
                            {deal.asin} · seen {relativeTime(deal.last_seen)}
                          </span>
                        </td>
                        <td className="px-3 py-2.5 text-on-surface-variant">{deal.category || '—'}</td>
                        <td className="px-3 py-2.5 text-right tabular">
                          {rupees(deal.price)}
                          {deal.old_price > deal.price && (
                            <span className="block text-label-sm text-on-surface-variant line-through">
                              {rupees(deal.old_price)}
                            </span>
                          )}
                        </td>
                        <td className="px-3 py-2.5 text-right tabular">
                          {deal.discount_percent ? `${deal.discount_percent}%` : '—'}
                        </td>
                        <td className={`px-3 py-2.5 text-right tabular font-medium ${scoreTone(deal.score)}`}>
                          {deal.score}
                        </td>
                        <td className="px-3 py-2.5 text-on-surface-variant">
                          {deal.provider === 'creator_api' ? 'API' : 'Scraper'}
                        </td>
                        <td className="px-3 py-2.5"><StatusPill status={deal.status} /></td>
                        <td className="px-4 py-2.5">
                          <div className="flex justify-end gap-1.5">
                            <Button
                              icon="playlist_add"
                              onClick={() => act(deal.asin, 'queue')}
                              disabled={busy || settled}
                            >
                              Queue
                            </Button>
                            <Button
                              icon="block"
                              onClick={() => act(deal.asin, 'ignore')}
                              disabled={busy || deal.status === 'ignored'}
                            >
                              Ignore
                            </Button>
                          </div>
                        </td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            </div>

            {pageCount > 1 && (
              <div className="flex items-center justify-between border-t border-outline-variant px-4 py-2.5 text-body-sm">
                <span className="text-on-surface-variant">Page {page + 1} of {pageCount}</span>
                <div className="flex gap-1.5">
                  <Button icon="chevron_left" onClick={() => setPage((p) => Math.max(0, p - 1))} disabled={page === 0}>
                    Previous
                  </Button>
                  <Button
                    icon="chevron_right"
                    onClick={() => setPage((p) => Math.min(pageCount - 1, p + 1))}
                    disabled={page >= pageCount - 1}
                  >
                    Next
                  </Button>
                </div>
              </div>
            )}
          </>
        )}
      </Card>
    </div>
  );
}
