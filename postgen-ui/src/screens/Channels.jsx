import { useCallback, useEffect, useMemo, useState } from 'react';
import { Sparkline, SuccessBar } from '../components/charts/Sparkline';
import {
  Button, Card, DeltaPill, EmptyState, ErrorNotice, Skeleton,
  StatusPill, formatNumber, relativeTime,
} from '../components/ui';

/**
 * All managed Facebook pages as performance cards.
 *
 * The platform is read from the account rather than assumed, so a second
 * publisher can slot in later without reworking this screen - though today
 * every channel is a Facebook page.
 */
export default function Channels({ apiFetch, onNavigate }) {
  const [channels, setChannels] = useState([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [filter, setFilter] = useState('all');
  const [search, setSearch] = useState('');

  const load = useCallback(async () => {
    setLoading(true);
    setError('');
    try {
      const resp = await apiFetch('/analytics/channels?days=7');
      if (!resp.ok) {
        const body = await resp.json().catch(() => ({}));
        throw new Error(body.error || `Request failed with status ${resp.status}`);
      }
      setChannels((await resp.json()).channels || []);
    } catch (err) {
      setError(err.message);
    } finally {
      setLoading(false);
    }
  }, [apiFetch]);

  useEffect(() => { load(); }, [load]);

  const visible = useMemo(() => {
    const term = search.trim().toLowerCase();
    return channels.filter((c) => {
      if (filter === 'active' && !c.active) return false;
      if (filter === 'paused' && c.active) return false;
      if (term && !c.account_name.toLowerCase().includes(term)) return false;
      return true;
    });
  }, [channels, filter, search]);

  const totals = useMemo(() => ({
    all: channels.length,
    active: channels.filter((c) => c.active).length,
    paused: channels.filter((c) => !c.active).length,
  }), [channels]);

  if (loading && channels.length === 0) {
    return (
      <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
        {Array.from({ length: 6 }, (_, i) => <Skeleton key={i} height={220} />)}
      </div>
    );
  }

  if (error && channels.length === 0) return <ErrorNotice message={error} onRetry={load} />;

  return (
    <div className="space-y-5">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <p className="text-body-md text-on-surface-variant">
          {totals.all} Facebook pages managed by PostGen, {totals.active} active.
        </p>
        <div className="flex flex-wrap items-center gap-2">
          <div className="relative">
            <span className="material-symbols-outlined pointer-events-none absolute left-2 top-1/2 -translate-y-1/2 text-on-surface-variant" style={{ fontSize: 18 }}>search</span>
            <input
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              placeholder="Search channels"
              className="rounded border border-outline-variant bg-surface-container py-2 pl-8 pr-3 text-body-sm text-on-surface placeholder:text-on-surface-variant focus:border-primary focus:outline-none"
            />
          </div>
          <div className="flex rounded border border-outline-variant p-0.5">
            {[['all', 'All'], ['active', 'Active'], ['paused', 'Paused']].map(([id, label]) => (
              <button
                key={id}
                onClick={() => setFilter(id)}
                className={`rounded px-3 py-1.5 text-label-md transition-colors ${
                  filter === id ? 'bg-primary text-on-primary' : 'text-on-surface-variant hover:text-on-surface'
                }`}
              >
                {label} <span className="tabular opacity-70">{totals[id]}</span>
              </button>
            ))}
          </div>
          <Button onClick={() => onNavigate?.('accounts')} icon="add" variant="primary">Add channel</Button>
        </div>
      </div>

      {error && <ErrorNotice message={error} onRetry={load} />}

      {visible.length === 0 ? (
        <EmptyState
          icon="hub"
          title="No channels match"
          hint={search ? 'Try a different search term.' : 'Add a channel in Accounts to get started.'}
        />
      ) : (
        <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
          {visible.map((c) => <ChannelCard key={c.account_name} channel={c} />)}
        </div>
      )}
    </div>
  );
}

function ChannelCard({ channel }) {
  const quotaUsed = channel.max_posts_per_day > 0
    ? Math.min(100, (channel.posts_today / channel.max_posts_per_day) * 100)
    : null;

  return (
    <Card>
      <div className="mb-4 flex items-start justify-between gap-3">
        <div className="flex min-w-0 items-start gap-3">
          <div className="grid h-10 w-10 shrink-0 place-items-center rounded bg-primary/15 text-primary">
            <span className="material-symbols-outlined" style={{ fontSize: 20 }}>thumb_up</span>
          </div>
          <div className="min-w-0">
            <h3 className="truncate text-body-lg font-semibold text-on-surface" title={channel.account_name}>
              {channel.account_name}
            </h3>
            {channel.facebook_page_id && (
              <a
                href={`https://www.facebook.com/${channel.facebook_page_id}`}
                target="_blank"
                rel="noreferrer noopener"
                className="font-label text-label-sm text-on-surface-variant hover:text-primary hover:underline"
              >
                ID: {channel.facebook_page_id}
              </a>
            )}
          </div>
        </div>
        <StatusPill status={channel.active ? 'active' : 'paused'} />
      </div>

      <dl className="space-y-2 border-b border-outline-variant pb-3">
        <Row label="Total posts" value={formatNumber(channel.total_posts)} />
        <Row
          label="Posts (7d)"
          value={
            <span className="flex items-center gap-2">
              <span className="tabular">{channel.posts_in_window}</span>
              <DeltaPill delta={{
                current: channel.posts_in_window,
                previous: channel.previous_window,
                pct_change: channel.previous_window > 0
                  ? Math.round(((channel.posts_in_window - channel.previous_window) / channel.previous_window) * 1000) / 10
                  : null,
              }} />
            </span>
          }
        />
        <Row label="Unposted pool" value={formatNumber(channel.queue_size)} />
        <Row
          label="Today"
          value={
            quotaUsed === null
              ? <span className="tabular">{channel.posts_today} <span className="text-on-surface-variant">/ ∞</span></span>
              : <span className="tabular">{channel.posts_today} <span className="text-on-surface-variant">/ {channel.max_posts_per_day}</span></span>
          }
        />
      </dl>

      <div className="pt-3">
        <SuccessBar rate={channel.success_rate} />
      </div>

      <div className="mt-3 flex items-end justify-between gap-3">
        <div>
          <div className="text-label-sm uppercase text-on-surface-variant">Last publish</div>
          <div className="text-body-sm text-on-surface">{relativeTime(channel.last_publish_at)}</div>
        </div>
        <Sparkline points={channel.daily || []} width={110} height={30} />
      </div>
    </Card>
  );
}

function Row({ label, value }) {
  return (
    <div className="flex items-center justify-between">
      <dt className="text-body-sm text-on-surface-variant">{label}</dt>
      <dd className="text-body-sm font-medium text-on-surface">{value}</dd>
    </div>
  );
}
