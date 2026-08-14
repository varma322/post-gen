import { useCallback, useEffect, useRef, useState } from 'react';
import { Button, Card, EmptyState, ErrorNotice, Skeleton, StatusPill, relativeTime } from '../components/ui';

const LEVELS = [
  { id: 'ERR', label: 'Error', tone: 'text-error' },
  { id: 'WARN', label: 'Warning', tone: 'text-tertiary' },
  { id: 'SUCC', label: 'Success', tone: 'text-secondary' },
  { id: 'INFO', label: 'Info', tone: 'text-on-surface-variant' },
];

const SOURCES = ['amazon', 'ollama', 'gemini', 'facebook', 'queue', 'worker'];

const LIVE_POLL_MS = 5000;

/**
 * The system activity log: a filterable event table with a detail drawer that
 * can pivot to the full trace for one pipeline run.
 *
 * Live mode polls rather than streaming. The existing SSE handler is built
 * around a request-scoped generation run; a shared feed would need its own
 * broadcast layer, which buys nothing at a five-second refresh.
 */
export default function ActivityLog({ apiFetch, accounts = [] }) {
  const [events, setEvents] = useState([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [selected, setSelected] = useState(null);
  const [live, setLive] = useState(false);

  const [filters, setFilters] = useState({ level: '', source: '', account: '', q: '' });
  const [search, setSearch] = useState('');

  // Keep the latest filters in a ref so the polling effect doesn't tear down
  // and rebuild its interval on every keystroke.
  const filtersRef = useRef(filters);
  filtersRef.current = filters;

  const load = useCallback(async ({ quiet = false } = {}) => {
    if (!quiet) setLoading(true);
    try {
      const params = new URLSearchParams({ limit: '100' });
      const current = filtersRef.current;
      Object.entries(current).forEach(([key, value]) => {
        if (value) params.set(key, value);
      });

      const resp = await apiFetch(`/events?${params}`);
      if (!resp.ok) {
        const body = await resp.json().catch(() => ({}));
        throw new Error(body.error || `Request failed with status ${resp.status}`);
      }

      const data = await resp.json();
      setEvents(data.events || []);
      setError('');
    } catch (err) {
      setError(err.message);
    } finally {
      setLoading(false);
    }
  }, [apiFetch]);

  useEffect(() => { load(); }, [load, filters]);

  // Live polling pauses while the tab is hidden - a background tab hammering
  // the API for a feed nobody is reading is pure waste.
  useEffect(() => {
    if (!live) return undefined;

    const tick = () => {
      if (!document.hidden) load({ quiet: true });
    };
    const timer = setInterval(tick, LIVE_POLL_MS);
    return () => clearInterval(timer);
  }, [live, load]);

  const applySearch = (e) => {
    e.preventDefault();
    setFilters((f) => ({ ...f, q: search.trim() }));
  };

  const clearFilters = () => {
    setFilters({ level: '', source: '', account: '', q: '' });
    setSearch('');
  };

  const hasFilters = Object.values(filters).some(Boolean);

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <p className="text-body-md text-on-surface-variant">
          Every scrape, generation, publish, and job transition, newest first.
        </p>
        <div className="flex items-center gap-2">
          <button
            onClick={() => setLive((v) => !v)}
            className={`inline-flex items-center gap-2 rounded-full border px-3 py-1.5 text-label-md transition-colors ${
              live ? 'border-secondary/40 bg-secondary/10 text-secondary' : 'border-outline-variant text-on-surface-variant hover:text-on-surface'
            }`}
            aria-pressed={live}
          >
            <span className={`h-1.5 w-1.5 rounded-full bg-current ${live ? 'animate-pulse' : ''}`} />
            Live mode
          </button>
          <Button onClick={() => load()} icon="refresh">Refresh</Button>
        </div>
      </div>

      <Card padded={false}>
        <div className="flex flex-wrap items-center gap-3 border-b border-outline-variant p-4">
          <FilterGroup label="Level">
            {LEVELS.map((level) => (
              <FilterChip
                key={level.id}
                active={filters.level === level.id}
                onClick={() => setFilters((f) => ({ ...f, level: f.level === level.id ? '' : level.id }))}
              >
                <span className={`h-1.5 w-1.5 rounded-full bg-current ${level.tone}`} />
                {level.label}
              </FilterChip>
            ))}
          </FilterGroup>

          <FilterGroup label="Source">
            <select
              value={filters.source}
              onChange={(e) => setFilters((f) => ({ ...f, source: e.target.value }))}
              className="rounded border border-outline-variant bg-surface-container-lowest px-2.5 py-1.5 text-body-sm text-on-surface focus:border-primary focus:outline-none"
            >
              <option value="">All sources</option>
              {SOURCES.map((s) => <option key={s} value={s}>{s}</option>)}
            </select>
          </FilterGroup>

          <FilterGroup label="Channel">
            <select
              value={filters.account}
              onChange={(e) => setFilters((f) => ({ ...f, account: e.target.value }))}
              className="max-w-[200px] rounded border border-outline-variant bg-surface-container-lowest px-2.5 py-1.5 text-body-sm text-on-surface focus:border-primary focus:outline-none"
            >
              <option value="">All channels</option>
              {accounts.map((a) => <option key={a.name} value={a.name}>{a.name}</option>)}
            </select>
          </FilterGroup>

          <form onSubmit={applySearch} className="flex flex-1 items-center gap-2">
            <div className="relative min-w-[180px] flex-1">
              <span className="material-symbols-outlined pointer-events-none absolute left-2 top-1/2 -translate-y-1/2 text-on-surface-variant" style={{ fontSize: 18 }}>search</span>
              <input
                value={search}
                onChange={(e) => setSearch(e.target.value)}
                placeholder="Search messages and URLs"
                className="w-full rounded border border-outline-variant bg-surface-container-lowest py-1.5 pl-8 pr-2.5 text-body-sm text-on-surface placeholder:text-on-surface-variant focus:border-primary focus:outline-none"
              />
            </div>
          </form>

          {hasFilters && (
            <button onClick={clearFilters} className="text-label-md text-primary hover:underline">
              Clear filters
            </button>
          )}
        </div>

        {loading && events.length === 0 ? (
          <div className="space-y-2 p-4">
            {Array.from({ length: 8 }, (_, i) => <Skeleton key={i} height={40} />)}
          </div>
        ) : error ? (
          <div className="p-4"><ErrorNotice message={error} onRetry={load} /></div>
        ) : events.length === 0 ? (
          <div className="p-4">
            <EmptyState
              icon="history"
              title={hasFilters ? 'No events match these filters' : 'No events recorded yet'}
              hint={hasFilters ? 'Try clearing a filter.' : 'Generate a post or trigger a job to start the log.'}
            />
          </div>
        ) : (
          <EventTable events={events} onSelect={setSelected} selectedId={selected?.id} />
        )}
      </Card>

      {selected && (
        <EventDrawer event={selected} apiFetch={apiFetch} onClose={() => setSelected(null)} />
      )}
    </div>
  );
}

function FilterGroup({ label, children }) {
  return (
    <div className="flex items-center gap-2">
      <span className="text-label-sm uppercase text-on-surface-variant">{label}</span>
      <div className="flex items-center gap-1.5">{children}</div>
    </div>
  );
}

function FilterChip({ active, onClick, children }) {
  return (
    <button
      onClick={onClick}
      aria-pressed={active}
      className={`inline-flex items-center gap-1.5 rounded-full border px-2.5 py-1 text-label-md transition-colors ${
        active
          ? 'border-primary bg-primary/15 text-primary'
          : 'border-outline-variant text-on-surface-variant hover:text-on-surface'
      }`}
    >
      {children}
    </button>
  );
}

function EventTable({ events, onSelect, selectedId }) {
  return (
    <div className="overflow-x-auto">
      <table className="w-full min-w-[820px] text-body-sm">
        <thead>
          <tr className="border-b border-outline-variant text-label-sm uppercase text-on-surface-variant">
            <th className="px-4 py-3 text-left font-medium">Time</th>
            <th className="px-3 py-3 text-left font-medium">Level</th>
            <th className="px-3 py-3 text-left font-medium">Event</th>
            <th className="px-3 py-3 text-left font-medium">Source</th>
            <th className="px-3 py-3 text-left font-medium">Channel</th>
            <th className="px-3 py-3 text-left font-medium">Message</th>
            <th className="px-4 py-3 text-right font-medium">Duration</th>
          </tr>
        </thead>
        <tbody>
          {events.map((event) => (
            <tr
              key={event.id}
              onClick={() => onSelect(event)}
              className={`cursor-pointer border-b border-outline-variant/50 last:border-0 hover:bg-surface-container-high/60 ${
                selectedId === event.id ? 'bg-surface-container-high' : ''
              }`}
            >
              <td className="whitespace-nowrap px-4 py-2.5 font-label text-on-surface-variant">
                {new Date(event.created_at).toLocaleTimeString()}
              </td>
              <td className="px-3 py-2.5"><StatusPill status={event.level} /></td>
              <td className="whitespace-nowrap px-3 py-2.5 font-label text-on-surface">{event.event_type}</td>
              <td className="px-3 py-2.5 text-on-surface-variant">{event.source}</td>
              <td className="max-w-[140px] truncate px-3 py-2.5 text-on-surface-variant">{event.account_name || '—'}</td>
              <td className="max-w-[280px] truncate px-3 py-2.5 text-on-surface-variant" title={event.message}>
                {event.message || '—'}
              </td>
              <td className="px-4 py-2.5 text-right tabular text-on-surface-variant">
                {event.duration_ms != null ? `${event.duration_ms} ms` : '—'}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

/**
 * EventDrawer shows one event in full and can expand to the whole trace, which
 * is the point of correlating events by trace id in the first place.
 */
function EventDrawer({ event, apiFetch, onClose }) {
  const [trace, setTrace] = useState(null);
  const [traceLoading, setTraceLoading] = useState(false);
  const [copied, setCopied] = useState(false);

  const loadTrace = async () => {
    setTraceLoading(true);
    try {
      const resp = await apiFetch(`/events/${encodeURIComponent(event.trace_id)}`);
      if (!resp.ok) throw new Error('Could not load the trace');
      const data = await resp.json();
      setTrace(data.events || []);
    } catch {
      setTrace([]);
    } finally {
      setTraceLoading(false);
    }
  };

  const copyMetadata = async () => {
    await navigator.clipboard.writeText(JSON.stringify(event.metadata ?? {}, null, 2));
    setCopied(true);
    setTimeout(() => setCopied(false), 1500);
  };

  // Escape closes, matching every other drawer people have used.
  useEffect(() => {
    const onKey = (e) => { if (e.key === 'Escape') onClose(); };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [onClose]);

  return (
    <>
      <div className="fixed inset-0 z-40 bg-black/50" onClick={onClose} aria-hidden="true" />
      <aside
        className="fixed inset-y-0 right-0 z-50 flex w-full max-w-xl flex-col border-l border-outline-variant bg-surface-container shadow-2xl"
        role="dialog"
        aria-label="Event details"
      >
        <header className="flex items-start justify-between gap-4 border-b border-outline-variant p-5">
          <div className="min-w-0">
            <div className="flex items-center gap-2">
              <StatusPill status={event.level} />
              <h2 className="truncate font-label text-headline-sm text-on-surface">{event.event_type}</h2>
            </div>
            <p className="mt-1 text-body-sm text-on-surface-variant">
              {new Date(event.created_at).toLocaleString()} · {relativeTime(event.created_at)}
            </p>
          </div>
          <button onClick={onClose} className="text-on-surface-variant hover:text-on-surface" aria-label="Close">
            <span className="material-symbols-outlined">close</span>
          </button>
        </header>

        <div className="flex-1 space-y-4 overflow-y-auto p-5">
          <div className="grid grid-cols-2 gap-3">
            <Field label="Source" value={event.source} />
            <Field label="Channel" value={event.account_name || '—'} />
            <Field label="Trace ID" value={event.trace_id} mono />
            <Field label="Duration" value={event.duration_ms != null ? `${event.duration_ms} ms` : '—'} />
            {event.job_id != null && <Field label="Job" value={`#${event.job_id}`} />}
            {event.job_item_id != null && <Field label="Job item" value={`#${event.job_item_id}`} />}
          </div>

          {event.message && (
            <section>
              <SectionLabel icon="notes">Message</SectionLabel>
              <p className="rounded border border-outline-variant bg-surface-container-lowest p-3 text-body-sm text-on-surface">
                {event.message}
              </p>
            </section>
          )}

          {event.product_url && (
            <section>
              <SectionLabel icon="link">Product</SectionLabel>
              <a
                href={event.product_url}
                target="_blank"
                rel="noreferrer noopener"
                className="block break-all rounded border border-outline-variant bg-surface-container-lowest p-3 font-label text-body-sm text-primary hover:underline"
              >
                {event.product_url}
              </a>
            </section>
          )}

          {event.metadata && Object.keys(event.metadata).length > 0 && (
            <section>
              <SectionLabel icon="data_object" action={
                <button onClick={copyMetadata} className="text-label-md text-primary hover:underline">
                  {copied ? 'Copied' : 'Copy'}
                </button>
              }>
                Metadata
              </SectionLabel>
              <pre className="overflow-x-auto rounded border border-outline-variant bg-surface-container-lowest p-3 font-label text-body-sm text-on-surface">
                {JSON.stringify(event.metadata, null, 2)}
              </pre>
            </section>
          )}

          <section>
            <SectionLabel icon="account_tree" action={
              !trace && (
                <button onClick={loadTrace} className="text-label-md text-primary hover:underline" disabled={traceLoading}>
                  {traceLoading ? 'Loading…' : 'View in context'}
                </button>
              )
            }>
              Pipeline run
            </SectionLabel>

            {!trace ? (
              <p className="text-body-sm text-on-surface-variant">
                Load every event that shares this trace to see the full sequence for this post.
              </p>
            ) : trace.length === 0 ? (
              <p className="text-body-sm text-on-surface-variant">No other events in this trace.</p>
            ) : (
              <ol className="space-y-1.5">
                {trace.map((step) => (
                  <li
                    key={step.id}
                    className={`flex items-center gap-3 rounded border px-3 py-2 text-body-sm ${
                      step.id === event.id
                        ? 'border-primary/40 bg-primary/10'
                        : 'border-outline-variant/60 bg-surface-container-low'
                    }`}
                  >
                    <span className="w-16 shrink-0 font-label text-label-sm text-on-surface-variant">
                      {new Date(step.created_at).toLocaleTimeString()}
                    </span>
                    <span className="flex-1 truncate font-label text-on-surface">{step.event_type}</span>
                    {step.duration_ms != null && (
                      <span className="tabular text-label-sm text-on-surface-variant">{step.duration_ms} ms</span>
                    )}
                  </li>
                ))}
              </ol>
            )}
          </section>
        </div>
      </aside>
    </>
  );
}

function Field({ label, value, mono }) {
  return (
    <div className="rounded border border-outline-variant bg-surface-container-low px-3 py-2">
      <div className="text-label-sm uppercase text-on-surface-variant">{label}</div>
      <div className={`mt-0.5 break-all text-body-sm text-on-surface ${mono ? 'font-label' : ''}`}>{value}</div>
    </div>
  );
}

function SectionLabel({ icon, children, action }) {
  return (
    <div className="mb-2 flex items-center justify-between">
      <span className="flex items-center gap-1.5 text-label-md uppercase text-on-surface-variant">
        <span className="material-symbols-outlined" style={{ fontSize: 16 }}>{icon}</span>
        {children}
      </span>
      {action}
    </div>
  );
}
