import { useCallback, useEffect, useState } from 'react';
import BarChart from '../components/charts/BarChart';
import { Sparkline, SuccessBar } from '../components/charts/Sparkline';
import {
  Button, Card, CardHeader, EmptyState, ErrorNotice, KpiCard,
  MetricRow, Skeleton, StatusPill, clockTime, formatNumber, relativeTime,
} from '../components/ui';

/**
 * Operations overview: the KPI row, seven-day publishing activity, per-channel
 * performance, queue health, and live worker state.
 *
 * Everything except worker status arrives in one /analytics/summary call, so
 * the screen makes two requests on mount rather than eight.
 */
export default function Dashboard({ apiFetch, workerStatus, onRefreshWorker, onNavigate }) {
  const [summary, setSummary] = useState(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');

  const load = useCallback(async () => {
    setLoading(true);
    setError('');
    try {
      const resp = await apiFetch('/analytics/summary?days=7');
      if (!resp.ok) {
        const body = await resp.json().catch(() => ({}));
        throw new Error(body.error || `Request failed with status ${resp.status}`);
      }
      setSummary(await resp.json());
    } catch (err) {
      setError(err.message);
    } finally {
      setLoading(false);
    }
  }, [apiFetch]);

  useEffect(() => {
    load();
    onRefreshWorker?.();
  }, [load, onRefreshWorker]);

  if (loading && !summary) {
    return (
      <div className="space-y-4">
        <div className="grid grid-cols-2 gap-3 lg:grid-cols-6">
          {Array.from({ length: 6 }, (_, i) => <Skeleton key={i} height={92} />)}
        </div>
        <Skeleton height={320} />
      </div>
    );
  }

  if (error && !summary) {
    return <ErrorNotice message={error} onRetry={load} />;
  }
  if (!summary) return null;

  return (
    <div className="space-y-5">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <p className="text-body-md text-on-surface-variant">
          Real-time overview of your affiliate operations.
        </p>
        <Button onClick={() => { load(); onRefreshWorker?.(); }} icon="refresh">Refresh</Button>
      </div>

      {error && <ErrorNotice message={error} onRetry={load} />}

      <div className="grid grid-cols-2 gap-3 lg:grid-cols-6">
        <KpiCard label="Posts Today" value={summary.posts_today.current} icon="send" delta={summary.posts_today} />
        <KpiCard label="Posts This Week" value={summary.posts_in_window.current} icon="calendar_month" delta={summary.posts_in_window} />
        <KpiCard label="Active Channels" value={summary.active_channels} icon="share" />
        <KpiCard label="Queue Size" value={summary.queue_size} icon="inventory_2" />
        <KpiCard
          label="Failed Posts"
          value={summary.failed_posts}
          icon="warning"
          tone={summary.failed_posts > 0 ? 'warning' : 'default'}
          hint={summary.failed_posts > 0 ? 'Needs action' : undefined}
        />
        {/* The schema permits exactly one active job, so counting jobs would
            always read 0 or 1. Item-level is the number that actually moves. */}
        <KpiCard label="Active Job Items" value={summary.active_job_items} icon="sync" />
      </div>

      <div className="grid gap-5 lg:grid-cols-3">
        <div className="space-y-5 lg:col-span-2">
          <Card>
            <CardHeader
              title="Publishing Activity"
              subtitle="Last 7 days"
              action={
                <div className="flex items-center gap-4 text-label-md">
                  <LegendDot color="#8083ff" label="Published" />
                  <LegendDot color="#ffb4ab" label="Failed" />
                </div>
              }
            />
            <BarChart published={summary.publishing} failed={summary.failures} height={260} />
          </Card>

          <Card padded={false}>
            <div className="p-5 pb-0">
              <CardHeader
                title="Channel Performance"
                subtitle={`${summary.channels.length} configured pages`}
                action={<Button onClick={() => onNavigate?.('channels')}>View all</Button>}
              />
            </div>
            <ChannelTable channels={summary.channels.slice(0, 6)} />
          </Card>
        </div>

        <div className="space-y-5">
          <Card>
            <CardHeader title="Queue Health" icon="shield" />
            <div className="space-y-2">
              <MetricRow label="Pending" value={summary.queue_health.pending} dot />
              <MetricRow label="Publishing" value={summary.queue_health.publishing} tone="primary" dot />
              <MetricRow label="Published (24h)" value={summary.queue_health.published_24h} tone="success" dot />
              <MetricRow label="Failed" value={summary.queue_health.failed} tone="error" dot />
              <MetricRow label="Skipped" value={summary.queue_health.skipped} tone="warning" dot />
            </div>
          </Card>

          <WorkerPanel status={workerStatus} onRefresh={onRefreshWorker} />

          <Card>
            <CardHeader title="AI Enrichment" icon="auto_awesome" subtitle="Last 7 days" />
            <StatBlock
              success={summary.ai.success}
              failed={summary.ai.failed}
              rate={summary.ai.success_rate}
              avgMs={summary.ai.avg_ms}
            />
            {summary.ai.by_provider?.length > 0 && (
              <div className="mt-3 space-y-1.5 border-t border-outline-variant pt-3">
                {summary.ai.by_provider.map((p) => (
                  <div key={p.provider} className="flex items-center justify-between text-body-sm">
                    <span className="capitalize text-on-surface-variant">{p.provider}</span>
                    <span className="tabular">
                      <span className="text-secondary">{p.success}</span>
                      <span className="text-on-surface-variant"> / </span>
                      <span className={p.failed > 0 ? 'text-error' : 'text-on-surface-variant'}>{p.failed}</span>
                    </span>
                  </div>
                ))}
              </div>
            )}
          </Card>

          <Card>
            <CardHeader title="Scraper" icon="travel_explore" subtitle="Last 7 days" />
            <StatBlock
              success={summary.scraper.success}
              failed={summary.scraper.failed}
              rate={summary.scraper.success_rate}
              avgMs={summary.scraper.avg_ms}
            />
            <div className="mt-3 flex items-center justify-between border-t border-outline-variant pt-3 text-body-sm">
              <span className="text-on-surface-variant">HTML fallback used</span>
              <span className={`tabular ${summary.scraper.fallback_used > 0 ? 'text-tertiary' : 'text-on-surface-variant'}`}>
                {summary.scraper.fallback_used}
              </span>
            </div>
          </Card>
        </div>
      </div>
    </div>
  );
}

function LegendDot({ color, label }) {
  return (
    <span className="flex items-center gap-1.5 text-on-surface-variant">
      <span className="h-2.5 w-2.5 rounded-sm" style={{ background: color }} />
      {label}
    </span>
  );
}

function StatBlock({ success, failed, rate, avgMs }) {
  return (
    <div className="space-y-3">
      <div className="flex items-baseline justify-between">
        <div>
          <div className="text-headline-md font-semibold tabular text-on-surface">{rate}%</div>
          <div className="text-label-sm text-on-surface-variant">success rate</div>
        </div>
        <div className="text-right">
          <div className="text-body-md tabular">
            <span className="text-secondary">{formatNumber(success)}</span>
            <span className="text-on-surface-variant"> / </span>
            <span className={failed > 0 ? 'text-error' : 'text-on-surface-variant'}>{formatNumber(failed)}</span>
          </div>
          <div className="text-label-sm text-on-surface-variant">ok / failed</div>
        </div>
      </div>
      <SuccessBar rate={rate} showLabel={false} />
      {avgMs > 0 && (
        <div className="text-label-sm text-on-surface-variant">
          Average {(avgMs / 1000).toFixed(1)}s
        </div>
      )}
    </div>
  );
}

function ChannelTable({ channels }) {
  if (channels.length === 0) {
    return <div className="p-5"><EmptyState icon="hub" title="No channels configured" /></div>;
  }

  return (
    <div className="overflow-x-auto">
      <table className="w-full min-w-[640px] text-body-sm">
        <thead>
          <tr className="border-b border-outline-variant text-label-sm uppercase text-on-surface-variant">
            <th className="px-5 py-3 text-left font-medium">Channel</th>
            <th className="px-3 py-3 text-right font-medium">Today</th>
            <th className="px-3 py-3 text-right font-medium">7 days</th>
            <th className="px-3 py-3 text-left font-medium">Trend</th>
            <th className="px-3 py-3 text-right font-medium">Success</th>
            <th className="px-3 py-3 text-right font-medium">Pool</th>
            <th className="px-5 py-3 text-right font-medium">Last publish</th>
          </tr>
        </thead>
        <tbody>
          {channels.map((c) => (
            <tr key={c.account_name} className="border-b border-outline-variant/50 last:border-0 hover:bg-surface-container-high/50">
              <td className="px-5 py-3">
                <div className="flex items-center gap-2">
                  <span className={`h-1.5 w-1.5 shrink-0 rounded-full ${c.active ? 'bg-secondary' : 'bg-outline'}`} />
                  <span className="truncate text-on-surface">{c.account_name}</span>
                </div>
              </td>
              <td className="px-3 py-3 text-right tabular">{c.posts_today}</td>
              <td className="px-3 py-3 text-right tabular">{c.posts_in_window}</td>
              <td className="px-3 py-3"><Sparkline points={c.daily || []} /></td>
              <td className="px-3 py-3 text-right">
                <span className={`tabular ${c.success_rate >= 98 ? 'text-secondary' : c.success_rate >= 90 ? 'text-tertiary' : 'text-error'}`}>
                  {c.success_rate}%
                </span>
              </td>
              <td className="px-3 py-3 text-right tabular text-on-surface-variant">{c.queue_size}</td>
              <td className="px-5 py-3 text-right text-on-surface-variant">{relativeTime(c.last_publish_at)}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function WorkerPanel({ status, onRefresh }) {
  if (!status) return <Skeleton height={180} />;

  return (
    <Card>
      <CardHeader
        title="Worker Status"
        icon="memory"
        action={<StatusPill status={status.running ? (status.phase || 'idle') : 'stopped'} />}
      />

      <div className="space-y-2">
        <div className="rounded border border-outline-variant/60 bg-surface-container-low px-3 py-2.5">
          <div className="text-label-sm uppercase text-on-surface-variant">Current channel</div>
          <div className="mt-0.5 truncate text-body-md text-on-surface">
            {status.current_account || 'None'}
          </div>
        </div>

        <div className="grid grid-cols-2 gap-2">
          <div className="rounded border border-outline-variant/60 bg-surface-container-low px-3 py-2.5">
            <div className="text-label-sm uppercase text-on-surface-variant">Cooldown</div>
            <div className="mt-0.5 text-body-md tabular text-tertiary">
              <Countdown until={status.cooldown_until} fallback={`${status.cooldown_seconds}s`} />
            </div>
          </div>
          <div className="rounded border border-outline-variant/60 bg-surface-container-low px-3 py-2.5">
            <div className="text-label-sm uppercase text-on-surface-variant">Last publish</div>
            <div className="mt-0.5 text-body-md text-on-surface">{clockTime(status.last_publish_at)}</div>
          </div>
        </div>

        {status.last_error && (
          <div className="rounded border border-error/30 bg-error/10 px-3 py-2 text-body-sm text-on-surface-variant">
            <span className="text-error">Last error:</span> {status.last_error}
          </div>
        )}
      </div>

      <button
        onClick={onRefresh}
        className="mt-3 flex w-full items-center justify-center gap-1.5 text-label-md text-on-surface-variant hover:text-on-surface"
      >
        <span className="material-symbols-outlined" style={{ fontSize: 16 }}>refresh</span>
        Refresh status
      </button>
    </Card>
  );
}

/**
 * Countdown ticks locally from the server's cooldown_until timestamp, so the
 * panel counts down every second without polling the API every second.
 */
function Countdown({ until, fallback }) {
  const [remaining, setRemaining] = useState(() => secondsUntil(until));

  useEffect(() => {
    setRemaining(secondsUntil(until));
    if (!until) return undefined;

    const timer = setInterval(() => setRemaining(secondsUntil(until)), 1000);
    return () => clearInterval(timer);
  }, [until]);

  if (!until || remaining <= 0) return <span className="text-on-surface-variant">{fallback}</span>;

  const minutes = Math.floor(remaining / 60);
  const seconds = remaining % 60;
  return <>{minutes}m {String(seconds).padStart(2, '0')}s</>;
}

function secondsUntil(iso) {
  if (!iso) return 0;
  return Math.max(0, Math.floor((new Date(iso).getTime() - Date.now()) / 1000));
}
