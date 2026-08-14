import { useState, useEffect } from 'react';
import PageHeader from '../components/PageHeader';
import SectionLabel from '../components/SectionLabel';
import StatusChip from '../components/StatusChip';

export default function AutoPublisher({ apiFetch, accounts, active }) {
  const [queuedProducts, setQueuedProducts] = useState([]);
  const [queueLoading, setQueueLoading] = useState(false);
  const [queueError, setQueueError] = useState('');
  const [newUrls, setNewUrls] = useState('');
  const [activeJob, setActiveJob] = useState(null);
  const [actionStatus, setActionStatus] = useState('');
  const [rotateOldLinks, setRotateOldLinks] = useState(false);

  const loadQueuedProducts = async () => {
    setQueueLoading(true);
    setQueueError('');
    try {
      const resp = await apiFetch("/products");
      if (!resp.ok) {
        const data = await resp.json().catch(() => ({}));
        throw new Error(data.error || `Failed to load product pool (HTTP ${resp.status})`);
      }
      const data = await resp.json();
      setQueuedProducts(data.products || []);
    } catch (err) {
      setQueueError(err.message);
    } finally {
      setQueueLoading(false);
    }
  };

  const checkActiveJob = async () => {
    try {
      const resp = await apiFetch("/jobs/active");
      if (resp.ok) {
        const data = await resp.json();
        setActiveJob(data.active ? data.job : null);
      }
    } catch (err) {
      console.error("Error polling active job:", err);
    }
  };

  // Only poll while this tab is the active one, mirroring the original
  // App-level effect that started/stopped the interval on tab change.
  useEffect(() => {
    if (!active) return;
    // eslint-disable-next-line react-hooks/set-state-in-effect -- poll-while-tab-active is intentional
    checkActiveJob();
    loadQueuedProducts();
    const intervalId = setInterval(checkActiveJob, 8000);
    return () => clearInterval(intervalId);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [active]);

  const handleQueueUrls = async () => {
    const parsed = newUrls.split(/\r?\n/).map(u => u.trim()).filter(Boolean);
    if (parsed.length === 0) return;

    setActionStatus(`Adding ${parsed.length} items to pool...`);

    const failedUrls = [];
    let successCount = 0;

    for (const url of parsed) {
      try {
        const resp = await apiFetch("/products", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ url })
        });
        if (resp.ok) {
          successCount++;
        } else {
          const data = await resp.json().catch(() => ({}));
          console.error(data.error);
          failedUrls.push(url);
        }
      } catch (err) {
        console.error(err);
        failedUrls.push(url);
      }
    }

    // Only clear the input of URLs that succeeded, so failures can be retried.
    setNewUrls(failedUrls.join('\n'));
    setActionStatus(
      failedUrls.length === 0
        ? `Queued ${successCount}/${parsed.length} product(s).`
        : `Queued ${successCount}/${parsed.length} product(s), ${failedUrls.length} failed (left in the box for retry).`
    );
    loadQueuedProducts();
    setTimeout(() => setActionStatus(""), 4000);
  };

  const handleDeleteQueuedProduct = async (id) => {
    try {
      const resp = await apiFetch(`/products/${id}`, { method: "DELETE" });
      if (resp.ok) {
        loadQueuedProducts();
      }
    } catch (err) {
      console.error(err);
    }
  };

  const handleTriggerAutoPost = async () => {
    try {
      const resp = await apiFetch("/jobs", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ rotate_old_links: rotateOldLinks })
      });
      if (resp.ok) {
        setActionStatus(`Auto-post job triggered!`);
        setTimeout(() => setActionStatus(""), 3000);
        await checkActiveJob();
      } else {
        const data = await resp.json();
        alert(data.error || "Failed to trigger auto post");
      }
    } catch (err) {
      alert("Error: " + err.message);
    }
  };

  const handleCancelAutoPost = async () => {
    if (!confirm("Are you sure you want to cancel the active posting job?")) return;
    try {
      const resp = await apiFetch("/jobs/cancel", { method: "POST" });
      if (resp.ok) {
        setActiveJob(null);
        setActionStatus("Job cancelled.");
        setTimeout(() => setActionStatus(""), 2000);
      }
    } catch (err) {
      alert("Error: " + err.message);
    }
  };

  return (
    <div className="space-y-8">
      <PageHeader
        title="Auto-Publisher"
        subtitle="Queue product listings in a central pool and auto-distribute them to all active Facebook pages."
      />

      {actionStatus && (
        <div className="bg-surface-container border border-outline-variant px-4 py-2.5 rounded-xl text-sm text-primary font-medium fadein">
          {actionStatus}
        </div>
      )}

      {/* Top Stats & Actions */}
      <div className="grid grid-cols-1 lg:grid-cols-3 gap-6">
        <div className="lg:col-span-2 bg-surface-container rounded-xl border border-outline-variant p-6 shadow-sm flex flex-col justify-between">
          <div>
            <SectionLabel icon="add_to_photos">Queue Products to Pool</SectionLabel>
            <textarea
              value={newUrls}
              onChange={(e) => setNewUrls(e.target.value)}
              className="w-full h-32 bg-surface-variant border border-outline-variant rounded-lg p-4 text-on-surface placeholder:text-on-surface-variant/40 focus:ring-2 focus:ring-primary focus:border-transparent transition-all resize-none font-mono text-sm leading-relaxed"
              placeholder="Paste Amazon product URLs here... One per line."
            />
          </div>
          <div className="mt-4 flex gap-3">
            <button
              onClick={handleQueueUrls}
              className="bg-primary text-on-secondary hover:bg-primary-fixed-dim px-6 py-3 rounded-lg font-label font-bold transition-colors shadow-lg flex items-center gap-2"
            >
              <span className="material-symbols-outlined text-lg">queue</span>
              Queue URLs
            </button>
          </div>
        </div>

        {/* Auto post control panel */}
        <div className="bg-surface-container rounded-xl border border-outline-variant p-6 shadow-sm flex flex-col justify-between">
          <div>
            <SectionLabel icon="smart_toy">Scheduler Control</SectionLabel>
            <p className="text-xs text-on-surface-variant mb-4 leading-relaxed">
              Triggering a job fills each active Facebook page's remaining daily quota with links from its own dedicated pool first, falling back to this shared pool for any shortfall, then posts them out over time respecting each account's active-hours and minimum-delay rules.
            </p>
            <div className="space-y-2 text-xs font-semibold bg-surface-variant/50 p-3 rounded-lg border border-outline-variant">
              <div className="flex justify-between">
                <span>Queued Items:</span>
                <span className="text-primary">{queuedProducts.length}</span>
              </div>
              <div className="flex justify-between">
                <span>Active Pages:</span>
                <span className="text-primary">{accounts.length}</span>
              </div>
            </div>
          </div>
          <div className="mt-6 flex flex-col gap-2">
            {!activeJob && (
              <label className="flex items-start gap-2 text-xs text-on-surface-variant mb-1 cursor-pointer select-none">
                <input
                  type="checkbox"
                  checked={rotateOldLinks}
                  onChange={(e) => setRotateOldLinks(e.target.checked)}
                  className="mt-0.5 rounded border-outline-variant"
                />
                <span>
                  <span className="font-semibold text-on-surface">Rotate old links</span> — if an account has no
                  unposted links left in its pool or the shared queue, repost its least-recently-used link instead
                  of skipping it.
                </span>
              </label>
            )}
            {activeJob ? (
              <button
                onClick={handleCancelAutoPost}
                className="w-full bg-red-950/20 hover:bg-red-950/40 border border-red-900/40 text-red-200 px-5 py-3 rounded-lg font-bold text-sm transition-all flex items-center justify-center gap-2"
              >
                <span className="material-symbols-outlined text-base">cancel</span>
                Cancel Active Job
              </button>
            ) : (
              <button
                onClick={handleTriggerAutoPost}
                disabled={queuedProducts.length === 0 && !rotateOldLinks}
                className="w-full bg-primary text-on-secondary hover:bg-primary-fixed-dim disabled:bg-surface-container-high disabled:text-on-surface-variant/50 px-5 py-3 rounded-lg font-bold text-sm transition-all flex items-center justify-center gap-2 shadow-lg"
              >
                <span className="material-symbols-outlined text-base">play_arrow</span>
                Start Auto-Post Job
              </button>
            )}
          </div>
        </div>
      </div>

      {/* Active Job Progress */}
      {activeJob && (
        <div className="bg-surface-container rounded-xl border border-primary/30 p-6 shadow-md space-y-4 fadein">
          <div className="flex justify-between items-center border-b border-outline-variant pb-3">
            <div className="flex items-center gap-2">
              <span className="material-symbols-outlined text-primary animate-spin">sync</span>
              <h3 className="font-bold text-on-surface text-lg">Active Auto-Post Job (ID: {activeJob.id})</h3>
            </div>
            <span className="bg-primary/10 text-primary px-3 py-1 rounded-full text-xs font-bold uppercase">
              Status: {activeJob.status}
            </span>
          </div>

          <div className="space-y-3">
            <span className="text-xs text-on-surface-variant font-semibold uppercase block">Job Progression Table</span>
            <div className="overflow-x-auto border border-outline-variant rounded-lg">
              <table className="w-full text-left border-collapse text-xs">
                <thead>
                  <tr className="bg-surface-variant text-on-surface-variant font-bold border-b border-outline-variant">
                    <th className="px-4 py-3">Page Account</th>
                    <th className="px-4 py-3">Assigned Product Link</th>
                    <th className="px-4 py-3">Status</th>
                    <th className="px-4 py-3 text-right">Details</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-outline-variant/60">
                  {activeJob.items && activeJob.items.map((item, idx) => (
                    <tr key={idx} className="hover:bg-surface-variant/20 transition-colors">
                      <td className="px-4 py-3 font-semibold text-primary">{item.account_name}</td>
                      <td className="px-4 py-3 font-mono truncate max-w-[200px]">{item.product_url}</td>
                      <td className="px-4 py-3">
                        <StatusChip status={item.status} label={item.status} />
                      </td>
                      <td className="px-4 py-3 text-right text-on-surface-variant font-medium">
                        {item.error_message || (item.published_at ? new Date(item.published_at).toLocaleTimeString() : '-')}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </div>
        </div>
      )}

      {/* Queued Products Table/Grid */}
      <div className="bg-surface-container rounded-xl border border-outline-variant shadow-sm p-6 space-y-4">
        <div className="flex justify-between items-center">
          <h2 className="text-xl font-headline font-semibold text-on-surface flex items-center gap-2">
            <span className="material-symbols-outlined text-primary">view_list</span>
            Current Product Pool ({queuedProducts.length} Items)
          </h2>
          <button
            onClick={loadQueuedProducts}
            className="text-xs text-primary hover:text-primary-fixed-dim font-bold flex items-center gap-1.5"
          >
            <span className="material-symbols-outlined text-sm">refresh</span>
            Refresh Pool
          </button>
        </div>

        {queueError && (
          <div className="text-error bg-error-container/10 p-4 rounded-xl border border-error/20 text-sm">{queueError}</div>
        )}

        {queueLoading ? (
          <p className="text-center py-6 text-on-surface-variant text-sm">Loading pool...</p>
        ) : queuedProducts.length === 0 ? (
          <div className="text-center py-12 border border-dashed border-outline-variant bg-surface-variant/20 rounded-lg">
            <p className="text-on-surface-variant text-sm">No products currently queued in pool.</p>
          </div>
        ) : (
          <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
            {queuedProducts.map((p) => (
              <div key={p.id} className="bg-surface-variant/40 border border-outline-variant rounded-xl p-4 flex gap-4 hover:border-outline transition-colors relative group">
                <div className="flex-1 min-w-0">
                  <h4 className="font-semibold text-sm text-on-surface line-clamp-1 mb-1">{p.title}</h4>
                  <p className="text-xs text-primary font-bold mb-2">Deal Price: {p.price || 'N/A'}</p>
                  <a
                    href={p.url}
                    target="_blank"
                    rel="noopener noreferrer"
                    className="text-[11px] text-on-surface-variant hover:underline font-mono truncate block max-w-[280px]"
                  >
                    {p.url}
                  </a>
                </div>
                <button
                  onClick={() => handleDeleteQueuedProduct(p.id)}
                  className="text-error bg-red-950/15 hover:bg-red-950/30 p-2 rounded-lg border border-red-900/20 opacity-0 group-hover:opacity-100 transition-opacity duration-200 w-fit h-fit self-center"
                  title="Remove from pool"
                >
                  <span className="material-symbols-outlined text-sm">delete</span>
                </button>
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  );
}
