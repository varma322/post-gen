import { useCallback, useEffect, useState } from 'react';
import TrendChart, { Legend } from '../components/charts/TrendChart';
import Heatmap from '../components/charts/Heatmap';
import { chart } from '../components/charts/palette';
import {
  Button, Card, CardHeader, EmptyState, ErrorNotice, KpiCard, Skeleton,
} from '../components/ui';

const WINDOWS = [
  { days: 7, label: 'Last 7 days' },
  { days: 14, label: 'Last 14 days' },
  { days: 30, label: 'Last 30 days' },
  { days: 90, label: 'Last 90 days' },
];

/**
 * Publishing analytics: volume against failures over time, per-channel success
 * rankings, and a density heatmap.
 *
 * Both requests are scoped to the same window so the three panels always
 * describe the same period.
 */
export default function Analytics({ apiFetch }) {
  const [days, setDays] = useState(30);
  const [summary, setSummary] = useState(null);
  const [channels, setChannels] = useState([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');

  const load = useCallback(async () => {
    setLoading(true);
    setError('');
    try {
      const [summaryResp, channelsResp] = await Promise.all([
        apiFetch(`/analytics/summary?days=${days}`),
        apiFetch(`/analytics/channels?days=${days}`),
      ]);

      if (!summaryResp.ok || !channelsResp.ok) {
        const failed = summaryResp.ok ? channelsResp : summaryResp;
        const body = await failed.json().catch(() => ({}));
        throw new Error(body.error || `Request failed with status ${failed.status}`);
      }

      setSummary(await summaryResp.json());
      setChannels((await channelsResp.json()).channels || []);
    } catch (err) {
      setError(err.message);
    } finally {
      setLoading(false);
    }
  }, [apiFetch, days]);

  useEffect(() => { load(); }, [load]);

  if (loading && !summary) {
    return (
      <div className="space-y-4">
        <Skeleton height={72} />
        <Skeleton height={340} />
        <Skeleton height={280} />
      </div>
    );
  }

  if (error && !summary) return <ErrorNotice message={error} onRetry={load} />;
  if (!summary) return null;

  const totalPublished = summary.publishing.reduce((sum, d) => sum + d.count, 0);
  const totalFailed = summary.failures.reduce((sum, d) => sum + d.count, 0);
  const failureRate = totalPublished + totalFailed > 0
    ? ((totalFailed / (totalPublished + totalFailed)) * 100).toFixed(1)
    : '0.0';

  const series = [
    { name: 'Posts Published', color: chart.primary, area: true, points: summary.publishing },
    { name: 'Failures', color: chart.error, dashed: true, points: summary.failures },
  ];

  // Rank by success rate, then by volume, so a channel with one lucky post
  // doesn't outrank one with hundreds at the same rate.
  const ranked = [...channels]
    .filter((c) => c.posts_in_window > 0)
    .sort((a, b) => b.success_rate - a.success_rate || b.posts_in_window - a.posts_in_window);

  return (
    <div className="space-y-5">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex items-center gap-2">
          <select
            value={days}
            onChange={(e) => setDays(Number(e.target.value))}
            className="rounded border border-outline-variant bg-surface-container px-3 py-2 text-body-md text-on-surface focus:border-primary focus:outline-none"
          >
            {WINDOWS.map((w) => <option key={w.days} value={w.days}>{w.label}</option>)}
          </select>
          <Button onClick={load} icon="refresh">Refresh</Button>
        </div>

        <div className="grid grid-cols-2 gap-3">
          <KpiCard label="Total Published" value={totalPublished} delta={summary.posts_in_window} />
          <KpiCard
            label="Failure Rate"
            value={`${failureRate}%`}
            tone={Number(failureRate) > 5 ? 'error' : 'default'}
          />
        </div>
      </div>

      {error && <ErrorNotice message={error} onRetry={load} />}

      <div className="grid gap-5 lg:grid-cols-3">
        <Card className="lg:col-span-2">
          <CardHeader
            title="Global Publishing Trend"
            subtitle={`Volume against failures over the last ${days} days`}
          />
          <TrendChart series={series} height={300} />
          <div className="mt-3 border-t border-outline-variant pt-3">
            <Legend series={series} />
          </div>
        </Card>

        <Card>
          <CardHeader title="Success Rate Rankings" subtitle="Highest publish reliability first" />
          {ranked.length === 0 ? (
            <EmptyState icon="leaderboard" title="No publishing in this window" />
          ) : (
            <div className="space-y-2">
              <div className="flex items-center justify-between px-1 text-label-sm uppercase text-on-surface-variant">
                <span>Channel</span>
                <span className="flex gap-4"><span>Vol</span><span className="w-12 text-right">Rate</span></span>
              </div>
              {ranked.slice(0, 8).map((c) => (
                <div key={c.account_name} className="flex items-center gap-3 rounded border border-outline-variant/60 bg-surface-container-low px-3 py-2">
                  <span className="min-w-0 flex-1 truncate text-body-sm text-on-surface" title={c.account_name}>
                    {c.account_name}
                  </span>
                  <span className="tabular text-body-sm text-on-surface-variant">{c.posts_in_window}</span>
                  <span
                    className="w-12 text-right tabular text-body-sm"
                    style={{ color: c.success_rate >= 98 ? chart.success : c.success_rate >= 90 ? chart.warning : chart.error }}
                  >
                    {c.success_rate}%
                  </span>
                  <span
                    className="h-1.5 w-8 shrink-0 rounded-full"
                    style={{
                      background: c.success_rate >= 98 ? chart.success : c.success_rate >= 90 ? chart.warning : chart.error,
                    }}
                  />
                </div>
              ))}
            </div>
          )}
        </Card>
      </div>

      <Card>
        <CardHeader
          title="Channel Publishing Density"
          subtitle={`Post volume per channel across the last ${Math.min(days, 14)} days`}
        />
        <Heatmap channels={channels.map((c) => ({ ...c, daily: (c.daily || []).slice(-14) }))} />
      </Card>
    </div>
  );
}
