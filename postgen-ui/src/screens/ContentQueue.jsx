import { useCallback, useEffect, useMemo, useState } from 'react';
import {
  Button, Card, EmptyState, ErrorNotice, KpiCard, Skeleton, StatusPill, relativeTime,
} from '../components/ui';

const PAGE_SIZE = 25;

/**
 * The shared product queue and each channel's dedicated link pool.
 *
 * These are two different pools with different semantics, and conflating them
 * is how links get double-posted: the shared queue is scraped on submission
 * and available to every channel, while a pool belongs to one channel and is
 * scraped only at publish time.
 */
export default function ContentQueue({ apiFetch, accounts = [] }) {
  const [tab, setTab] = useState('shared');

  return (
    <div className="space-y-5">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <p className="text-body-md text-on-surface-variant">
          Monitor and manage outbound publishing material.
        </p>
        <div className="flex rounded border border-outline-variant p-0.5">
          <button
            onClick={() => setTab('shared')}
            className={`rounded px-3 py-1.5 text-label-md transition-colors ${
              tab === 'shared' ? 'bg-primary text-on-primary' : 'text-on-surface-variant hover:text-on-surface'
            }`}
          >
            Shared queue
          </button>
          <button
            onClick={() => setTab('pools')}
            className={`rounded px-3 py-1.5 text-label-md transition-colors ${
              tab === 'pools' ? 'bg-primary text-on-primary' : 'text-on-surface-variant hover:text-on-surface'
            }`}
          >
            Channel pools
          </button>
        </div>
      </div>

      {tab === 'shared'
        ? <SharedQueue apiFetch={apiFetch} />
        : <ChannelPools apiFetch={apiFetch} accounts={accounts} />}
    </div>
  );
}

function SharedQueue({ apiFetch }) {
  const [products, setProducts] = useState([]);
  const [summary, setSummary] = useState(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [search, setSearch] = useState('');
  const [page, setPage] = useState(0);
  const [newUrl, setNewUrl] = useState('');
  const [adding, setAdding] = useState(false);

  const load = useCallback(async () => {
    setLoading(true);
    setError('');
    try {
      const [productsResp, summaryResp] = await Promise.all([
        apiFetch('/products'),
        apiFetch('/analytics/summary?days=7'),
      ]);
      if (!productsResp.ok) throw new Error('Failed to load the queue');
      setProducts((await productsResp.json()).products || []);
      if (summaryResp.ok) setSummary(await summaryResp.json());
    } catch (err) {
      setError(err.message);
    } finally {
      setLoading(false);
    }
  }, [apiFetch]);

  useEffect(() => { load(); }, [load]);

  const addProduct = async (e) => {
    e.preventDefault();
    const url = newUrl.trim();
    if (!url) return;

    setAdding(true);
    setError('');
    try {
      const resp = await apiFetch('/products', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ url }),
      });
      if (!resp.ok) {
        const body = await resp.json().catch(() => ({}));
        throw new Error(body.error || 'Could not queue that URL');
      }
      setNewUrl('');
      await load();
    } catch (err) {
      setError(err.message);
    } finally {
      setAdding(false);
    }
  };

  const removeProduct = async (id) => {
    try {
      const resp = await apiFetch(`/products/${id}`, { method: 'DELETE' });
      if (!resp.ok) throw new Error('Could not remove that item');
      setProducts((current) => current.filter((p) => p.id !== id));
    } catch (err) {
      setError(err.message);
    }
  };

  const filtered = useMemo(() => {
    const term = search.trim().toLowerCase();
    if (!term) return products;
    return products.filter((p) =>
      (p.title || '').toLowerCase().includes(term) || (p.url || '').toLowerCase().includes(term)
    );
  }, [products, search]);

  const pageCount = Math.max(1, Math.ceil(filtered.length / PAGE_SIZE));
  const safePage = Math.min(page, pageCount - 1);
  const visible = filtered.slice(safePage * PAGE_SIZE, (safePage + 1) * PAGE_SIZE);

  if (loading && products.length === 0) {
    return <div className="space-y-3"><Skeleton height={92} /><Skeleton height={360} /></div>;
  }

  return (
    <div className="space-y-4">
      <div className="grid grid-cols-2 gap-3 lg:grid-cols-5">
        <KpiCard label="Total Items" value={products.length} icon="view_list" />
        <KpiCard label="Pending" value={summary?.queue_health.pending ?? 0} icon="schedule" />
        <KpiCard label="Publishing" value={summary?.queue_health.publishing ?? 0} icon="sync" />
        <KpiCard label="Published (24h)" value={summary?.queue_health.published_24h ?? 0} icon="check_circle" />
        <KpiCard
          label="Failed"
          value={summary?.queue_health.failed ?? 0}
          icon="error"
          tone={(summary?.queue_health.failed ?? 0) > 0 ? 'error' : 'default'}
        />
      </div>

      {error && <ErrorNotice message={error} onRetry={load} />}

      <Card>
        <form onSubmit={addProduct} className="flex flex-wrap items-center gap-2">
          <input
            value={newUrl}
            onChange={(e) => setNewUrl(e.target.value)}
            placeholder="https://www.amazon.in/dp/..."
            className="min-w-[240px] flex-1 rounded border border-outline-variant bg-surface-container-lowest px-3 py-2 text-body-md text-on-surface placeholder:text-on-surface-variant focus:border-primary focus:outline-none"
          />
          <Button type="submit" variant="primary" icon="add" disabled={adding || !newUrl.trim()}>
            {adding ? 'Scraping…' : 'Add to queue'}
          </Button>
        </form>
        <p className="mt-2 text-label-sm text-on-surface-variant">
          Adding scrapes the product immediately, so its title and price are available here.
        </p>
      </Card>

      <Card padded={false}>
        <div className="flex flex-wrap items-center justify-between gap-3 border-b border-outline-variant p-4">
          <div className="relative min-w-[220px] flex-1">
            <span className="material-symbols-outlined pointer-events-none absolute left-2 top-1/2 -translate-y-1/2 text-on-surface-variant" style={{ fontSize: 18 }}>search</span>
            <input
              value={search}
              onChange={(e) => { setSearch(e.target.value); setPage(0); }}
              placeholder="Filter by title or URL"
              className="w-full rounded border border-outline-variant bg-surface-container-lowest py-1.5 pl-8 pr-2.5 text-body-sm text-on-surface placeholder:text-on-surface-variant focus:border-primary focus:outline-none"
            />
          </div>
          <div className="flex items-center gap-3 text-body-sm text-on-surface-variant">
            <span className="tabular">
              {filtered.length === 0 ? '0' : `${safePage * PAGE_SIZE + 1}–${Math.min((safePage + 1) * PAGE_SIZE, filtered.length)}`} of {filtered.length}
            </span>
            <div className="flex gap-1">
              <PagerButton onClick={() => setPage((p) => Math.max(0, p - 1))} disabled={safePage === 0} icon="chevron_left" label="Previous page" />
              <PagerButton onClick={() => setPage((p) => Math.min(pageCount - 1, p + 1))} disabled={safePage >= pageCount - 1} icon="chevron_right" label="Next page" />
            </div>
          </div>
        </div>

        {visible.length === 0 ? (
          <div className="p-4">
            <EmptyState
              icon="inventory_2"
              title={search ? 'No items match that filter' : 'The shared queue is empty'}
              hint={search ? undefined : 'Add an Amazon product URL above to queue it for every channel.'}
            />
          </div>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full min-w-[720px] text-body-sm">
              <thead>
                <tr className="border-b border-outline-variant text-label-sm uppercase text-on-surface-variant">
                  <th className="px-4 py-3 text-left font-medium">Product</th>
                  <th className="px-3 py-3 text-left font-medium">Price</th>
                  <th className="px-3 py-3 text-left font-medium">Status</th>
                  <th className="px-3 py-3 text-left font-medium">Queued</th>
                  <th className="px-4 py-3 text-right font-medium">Actions</th>
                </tr>
              </thead>
              <tbody>
                {visible.map((product) => (
                  <tr key={product.id} className="border-b border-outline-variant/50 last:border-0 hover:bg-surface-container-high/50">
                    <td className="px-4 py-3">
                      <div className="flex items-center gap-3">
                        {product.image_url && (
                          <img
                            src={product.image_url}
                            alt=""
                            className="h-10 w-10 shrink-0 rounded object-cover"
                            loading="lazy"
                          />
                        )}
                        <div className="min-w-0">
                          <div className="truncate text-on-surface" title={product.title}>{product.title || 'Untitled'}</div>
                          <a
                            href={product.url}
                            target="_blank"
                            rel="noreferrer noopener"
                            className="truncate font-label text-label-sm text-on-surface-variant hover:text-primary hover:underline"
                          >
                            ID: {product.id}
                          </a>
                        </div>
                      </div>
                    </td>
                    <td className="whitespace-nowrap px-3 py-3 tabular text-on-surface">₹{product.price || '—'}</td>
                    <td className="px-3 py-3"><StatusPill status={product.status || 'queued'} /></td>
                    <td className="whitespace-nowrap px-3 py-3 text-on-surface-variant">{relativeTime(product.created_at)}</td>
                    <td className="px-4 py-3 text-right">
                      <button
                        onClick={() => removeProduct(product.id)}
                        className="text-on-surface-variant hover:text-error"
                        aria-label={`Remove ${product.title || product.id} from the queue`}
                      >
                        <span className="material-symbols-outlined" style={{ fontSize: 18 }}>delete</span>
                      </button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </Card>
    </div>
  );
}

function ChannelPools({ apiFetch, accounts }) {
  const [selected, setSelected] = useState(accounts[0]?.name || '');
  const [links, setLinks] = useState([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  const [bulk, setBulk] = useState('');
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    if (!selected && accounts.length > 0) setSelected(accounts[0].name);
  }, [accounts, selected]);

  const load = useCallback(async () => {
    if (!selected) return;
    setLoading(true);
    setError('');
    try {
      const resp = await apiFetch(`/accounts/${encodeURIComponent(selected)}/links`);
      if (!resp.ok) throw new Error('Failed to load this channel pool');
      setLinks((await resp.json()).links || []);
    } catch (err) {
      setError(err.message);
    } finally {
      setLoading(false);
    }
  }, [apiFetch, selected]);

  useEffect(() => { load(); }, [load]);

  const addLinks = async (e) => {
    e.preventDefault();
    const urls = bulk.split('\n').map((l) => l.trim()).filter(Boolean);
    if (urls.length === 0) return;

    setSaving(true);
    setError('');
    // Posted one at a time: the endpoint takes a single URL, and a partial
    // failure should still leave the successful ones in the pool.
    const failures = [];
    for (const url of urls) {
      try {
        const resp = await apiFetch(`/accounts/${encodeURIComponent(selected)}/links`, {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ url }),
        });
        if (!resp.ok) {
          const body = await resp.json().catch(() => ({}));
          failures.push(`${url}: ${body.error || resp.status}`);
        }
      } catch (err) {
        failures.push(`${url}: ${err.message}`);
      }
    }

    setSaving(false);
    setBulk('');
    if (failures.length > 0) {
      setError(`${failures.length} of ${urls.length} links failed. ${failures[0]}`);
    }
    await load();
  };

  const removeLink = async (id) => {
    try {
      const resp = await apiFetch(`/accounts/${encodeURIComponent(selected)}/links/${id}`, { method: 'DELETE' });
      if (!resp.ok) throw new Error('Could not remove that link');
      setLinks((current) => current.filter((l) => l.id !== id));
    } catch (err) {
      setError(err.message);
    }
  };

  const unposted = links.filter((l) => !l.posted).length;

  if (accounts.length === 0) {
    return <EmptyState icon="hub" title="No channels configured" hint="Add a channel in Accounts first." />;
  }

  return (
    <div className="space-y-4">
      <Card>
        <div className="flex flex-wrap items-end gap-3">
          <div className="min-w-[200px]">
            <label className="mb-1 block text-label-sm uppercase text-on-surface-variant" htmlFor="pool-channel">
              Channel
            </label>
            <select
              id="pool-channel"
              value={selected}
              onChange={(e) => setSelected(e.target.value)}
              className="w-full rounded border border-outline-variant bg-surface-container-lowest px-3 py-2 text-body-md text-on-surface focus:border-primary focus:outline-none"
            >
              {accounts.map((a) => <option key={a.name} value={a.name}>{a.name}</option>)}
            </select>
          </div>
          <div className="flex gap-3">
            <Stat label="In pool" value={links.length} />
            <Stat label="Unposted" value={unposted} tone={unposted === 0 ? 'warning' : 'success'} />
          </div>
        </div>
      </Card>

      {error && <ErrorNotice message={error} onRetry={load} />}

      <Card>
        <form onSubmit={addLinks} className="space-y-2">
          <label className="block text-label-sm uppercase text-on-surface-variant" htmlFor="bulk-links">
            Add links, one per line
          </label>
          <textarea
            id="bulk-links"
            value={bulk}
            onChange={(e) => setBulk(e.target.value)}
            rows={4}
            placeholder={'https://www.amazon.in/dp/...\nhttps://www.amazon.in/dp/...'}
            className="w-full rounded border border-outline-variant bg-surface-container-lowest px-3 py-2 font-label text-body-sm text-on-surface placeholder:text-on-surface-variant focus:border-primary focus:outline-none"
          />
          <div className="flex items-center justify-between">
            <p className="text-label-sm text-on-surface-variant">
              Pool links are scraped at publish time, not now, so pasting a large batch is fast.
            </p>
            <Button type="submit" variant="primary" icon="add" disabled={saving || !bulk.trim()}>
              {saving ? 'Adding…' : 'Add to pool'}
            </Button>
          </div>
        </form>
      </Card>

      <Card padded={false}>
        {loading ? (
          <div className="space-y-2 p-4">{Array.from({ length: 5 }, (_, i) => <Skeleton key={i} height={36} />)}</div>
        ) : links.length === 0 ? (
          <div className="p-4">
            <EmptyState icon="link" title="This channel has no dedicated links" hint="Paste some above to give it its own rotation." />
          </div>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full min-w-[560px] text-body-sm">
              <thead>
                <tr className="border-b border-outline-variant text-label-sm uppercase text-on-surface-variant">
                  <th className="px-4 py-3 text-left font-medium">URL</th>
                  <th className="px-3 py-3 text-left font-medium">Status</th>
                  <th className="px-3 py-3 text-left font-medium">Added</th>
                  <th className="px-4 py-3 text-right font-medium">Actions</th>
                </tr>
              </thead>
              <tbody>
                {links.map((link) => (
                  <tr key={link.id} className="border-b border-outline-variant/50 last:border-0 hover:bg-surface-container-high/50">
                    <td className="max-w-[380px] px-4 py-2.5">
                      <a
                        href={link.url}
                        target="_blank"
                        rel="noreferrer noopener"
                        className="block truncate font-label text-on-surface hover:text-primary hover:underline"
                        title={link.url}
                      >
                        {link.url}
                      </a>
                    </td>
                    <td className="px-3 py-2.5">
                      <StatusPill status={link.posted ? 'published' : 'pending'} />
                    </td>
                    <td className="whitespace-nowrap px-3 py-2.5 text-on-surface-variant">{relativeTime(link.created_at)}</td>
                    <td className="px-4 py-2.5 text-right">
                      <button
                        onClick={() => removeLink(link.id)}
                        className="text-on-surface-variant hover:text-error"
                        aria-label="Remove link from pool"
                      >
                        <span className="material-symbols-outlined" style={{ fontSize: 18 }}>delete</span>
                      </button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </Card>
    </div>
  );
}

function Stat({ label, value, tone }) {
  const tones = { success: 'text-secondary', warning: 'text-tertiary' };
  return (
    <div className="rounded border border-outline-variant bg-surface-container-low px-4 py-2">
      <div className="text-label-sm uppercase text-on-surface-variant">{label}</div>
      <div className={`text-headline-sm tabular ${tones[tone] || 'text-on-surface'}`}>{value}</div>
    </div>
  );
}

function PagerButton({ onClick, disabled, icon, label }) {
  return (
    <button
      onClick={onClick}
      disabled={disabled}
      aria-label={label}
      className="rounded border border-outline-variant p-1 text-on-surface-variant transition-colors hover:text-on-surface disabled:opacity-40"
    >
      <span className="material-symbols-outlined" style={{ fontSize: 18 }}>{icon}</span>
    </button>
  );
}
