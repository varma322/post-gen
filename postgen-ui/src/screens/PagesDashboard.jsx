import { useState, useEffect } from 'react';
import PageHeader from '../components/PageHeader';

export default function PagesDashboard({ apiFetch }) {
  const [stats, setStats] = useState(null);
  const [statsLoading, setStatsLoading] = useState(false);
  const [statsError, setStatsError] = useState('');

  const loadStats = async () => {
    setStatsLoading(true);
    setStatsError('');
    try {
      const resp = await apiFetch("/stats");
      if (!resp.ok) throw new Error("Failed to fetch stats");
      const data = await resp.json();
      setStats(data);
    } catch (err) {
      console.error(err);
      setStatsError(err.message);
    } finally {
      setStatsLoading(false);
    }
  };

  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect -- fetch-on-mount is intentional
    loadStats();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  return (
    <div className="space-y-8">
      <PageHeader
        title="Pages Dashboard"
        subtitle="Overview of total posts and page activity."
        action={
          <button
            onClick={loadStats}
            className="bg-primary text-on-secondary hover:bg-primary-fixed-dim px-5 py-2.5 rounded-lg font-bold text-sm transition-all shadow-md flex items-center gap-2 w-fit"
          >
            <span className="material-symbols-outlined">refresh</span>
            Refresh Stats
          </button>
        }
      />

      {statsLoading && <div className="text-on-surface-variant">Loading stats...</div>}
      {statsError && <div className="text-error bg-error-container/10 p-4 rounded-xl border border-error/20">{statsError}</div>}

      {!statsLoading && stats && (
        <div className="space-y-6">
          {/* Top Level Cards */}
          <div className="grid grid-cols-1 md:grid-cols-3 gap-6">
            <div className="bg-surface-container rounded-xl p-6 border border-outline-variant shadow-sm flex items-center gap-4">
              <div className="bg-primary/10 p-4 rounded-full text-primary">
                <span className="material-symbols-outlined text-3xl">post_add</span>
              </div>
              <div>
                <p className="text-sm text-on-surface-variant font-semibold">Total Posts</p>
                <h3 className="text-3xl font-bold text-on-surface">{stats.total_posts || 0}</h3>
              </div>
            </div>
            <div className="bg-surface-container rounded-xl p-6 border border-outline-variant shadow-sm flex items-center gap-4">
              <div className="bg-secondary-container p-4 rounded-full text-on-secondary-container">
                <span className="material-symbols-outlined text-3xl">today</span>
              </div>
              <div>
                <p className="text-sm text-on-surface-variant font-semibold">Posts Today</p>
                <h3 className="text-3xl font-bold text-on-surface">{stats.posts_today || 0}</h3>
              </div>
            </div>
            <div className="bg-surface-container rounded-xl p-6 border border-outline-variant shadow-sm flex items-center gap-4">
              <div className="bg-tertiary/10 p-4 rounded-full text-tertiary">
                <span className="material-symbols-outlined text-3xl">groups</span>
              </div>
              <div>
                <p className="text-sm text-on-surface-variant font-semibold">Active Pages</p>
                <h3 className="text-3xl font-bold text-on-surface">{stats.account_stats ? stats.account_stats.length : 0}</h3>
              </div>
            </div>
          </div>

          <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
            {/* Breakdown per Page */}
            <div className="bg-surface-container rounded-xl border border-outline-variant shadow-sm overflow-hidden flex flex-col">
              <div className="p-5 border-b border-outline-variant">
                <h2 className="text-xl font-headline font-semibold text-on-surface">Posts by Page</h2>
              </div>
              <div className="overflow-x-auto flex-1">
                <table className="w-full text-left border-collapse">
                  <thead>
                    <tr className="border-b border-outline-variant bg-surface-variant/40 text-on-surface-variant text-xs font-bold uppercase tracking-wider">
                      <th className="px-6 py-4">Page Account</th>
                      <th className="px-6 py-4 text-right">Total Posts</th>
                      <th className="px-6 py-4 text-right">Today</th>
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-outline-variant/60">
                    {(!stats.account_stats || stats.account_stats.length === 0) ? (
                      <tr>
                        <td colSpan="3" className="px-6 py-8 text-center text-on-surface-variant text-sm">
                          No data available.
                        </td>
                      </tr>
                    ) : (
                      stats.account_stats.map((acc, i) => (
                        <tr key={i} className="hover:bg-surface-variant/20 transition-colors text-sm text-on-surface">
                          <td className="px-6 py-4 font-bold text-primary">{acc.account_name}</td>
                          <td className="px-6 py-4 text-right font-mono">{acc.total_posts}</td>
                          <td className="px-6 py-4 text-right font-mono">
                            {acc.posts_today > 0 ? (
                              <span className="bg-primary/10 text-primary px-2 py-1 rounded-md text-xs font-bold">+{acc.posts_today}</span>
                            ) : '0'}
                          </td>
                        </tr>
                      ))
                    )}
                  </tbody>
                </table>
              </div>
            </div>

            {/* Recent Activity */}
            <div className="bg-surface-container rounded-xl border border-outline-variant shadow-sm flex flex-col">
              <div className="p-5 border-b border-outline-variant">
                <h2 className="text-xl font-headline font-semibold text-on-surface">Recent Activity</h2>
              </div>
              <div className="p-5 overflow-y-auto max-h-[400px] space-y-4">
                {(!stats.recent_posts || stats.recent_posts.length === 0) ? (
                  <p className="text-center text-on-surface-variant text-sm">No recent activity.</p>
                ) : (
                  stats.recent_posts.map((post, i) => (
                    <div key={i} className="bg-surface-variant/50 p-4 rounded-lg border border-outline-variant">
                      <div className="flex justify-between items-start mb-2">
                        <span className="font-bold text-primary text-sm">{post.account_name}</span>
                        <span className="text-xs text-on-surface-variant">{new Date(post.created_at).toLocaleString()}</span>
                      </div>
                      <p className="text-sm text-on-surface font-semibold line-clamp-1">{post.product_title || 'Unknown Product'}</p>
                      {post.product_url && (
                        <a href={post.product_url} target="_blank" rel="noopener noreferrer" className="text-xs text-primary hover:underline mt-1 inline-flex items-center gap-1">
                          {post.product_url}
                          <span className="material-symbols-outlined text-[10px]">open_in_new</span>
                        </a>
                      )}
                    </div>
                  ))
                )}
              </div>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
