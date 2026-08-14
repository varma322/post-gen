import { useCallback, useEffect, useMemo, useState } from 'react';
import {
  Button, Card, CardHeader, EmptyState, ErrorNotice, Skeleton, StatusPill,
} from '../components/ui';

/**
 * Manual posting workflow: paste a product URL, pick channels, review the
 * generated copy per channel, then publish or queue it.
 *
 * The channel picker shows each channel's live daily-quota usage and unposted
 * pool size, so a channel that has already hit its cap is visibly unavailable
 * rather than silently skipped after you press publish.
 */
export default function Publisher({ apiFetch, accounts = [], onNavigate }) {
  const [url, setUrl] = useState('');
  const [channels, setChannels] = useState([]);
  const [selected, setSelected] = useState(() => new Set());
  const [results, setResults] = useState([]);
  const [generating, setGenerating] = useState(false);
  const [error, setError] = useState('');
  const [loadingChannels, setLoadingChannels] = useState(true);

  const loadChannels = useCallback(async () => {
    setLoadingChannels(true);
    try {
      const resp = await apiFetch('/analytics/channels?days=7');
      if (!resp.ok) throw new Error('Could not load channel status');
      setChannels((await resp.json()).channels || []);
    } catch (err) {
      setError(err.message);
    } finally {
      setLoadingChannels(false);
    }
  }, [apiFetch]);

  useEffect(() => { loadChannels(); }, [loadChannels]);

  // Default to every active channel that still has quota left, which is the
  // selection someone almost always wants.
  useEffect(() => {
    if (channels.length === 0 || selected.size > 0) return;
    const available = channels.filter((c) => c.active && quotaLeft(c) !== 0).map((c) => c.account_name);
    setSelected(new Set(available));
  }, [channels, selected.size]);

  const toggle = (name) => {
    setSelected((current) => {
      const next = new Set(current);
      if (next.has(name)) next.delete(name); else next.add(name);
      return next;
    });
  };

  const generate = async () => {
    const target = url.trim();
    if (!target || selected.size === 0) return;

    setGenerating(true);
    setError('');
    setResults([]);
    try {
      const resp = await apiFetch('/generate', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ urls: [target], accounts: [...selected] }),
      });
      if (!resp.ok) {
        const body = await resp.json().catch(() => ({}));
        throw new Error(body.error || `Generation failed with status ${resp.status}`);
      }
      setResults((await resp.json()).results || []);
    } catch (err) {
      setError(err.message);
    } finally {
      setGenerating(false);
    }
  };

  const queueForAll = async () => {
    const target = url.trim();
    if (!target) return;

    setError('');
    try {
      const resp = await apiFetch('/products', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ url: target }),
      });
      if (!resp.ok) {
        const body = await resp.json().catch(() => ({}));
        throw new Error(body.error || 'Could not queue that URL');
      }
      onNavigate?.('queue');
    } catch (err) {
      setError(err.message);
    }
  };

  const selectableCount = useMemo(
    () => channels.filter((c) => c.active && quotaLeft(c) !== 0).length,
    [channels]
  );

  return (
    <div className="space-y-5">
      <p className="text-body-md text-on-surface-variant">
        Manual posting workflow. Generate a preview per channel, then publish or send to the queue.
      </p>

      {error && <ErrorNotice message={error} />}

      <div className="grid gap-5 lg:grid-cols-5">
        <div className="space-y-5 lg:col-span-3">
          <Card>
            <CardHeader title="Source Material" icon="link" />
            <div className="flex flex-wrap gap-2">
              <input
                value={url}
                onChange={(e) => setUrl(e.target.value)}
                onKeyDown={(e) => { if (e.key === 'Enter') generate(); }}
                placeholder="https://www.amazon.in/dp/..."
                className="min-w-[240px] flex-1 rounded border border-outline-variant bg-surface-container-lowest px-3 py-2 text-body-md text-on-surface placeholder:text-on-surface-variant focus:border-primary focus:outline-none"
              />
              <Button
                onClick={generate}
                variant="primary"
                icon="auto_awesome"
                disabled={generating || !url.trim() || selected.size === 0}
              >
                {generating ? 'Generating…' : 'Generate preview'}
              </Button>
            </div>
            <div className="mt-2 flex flex-wrap items-center justify-between gap-2">
              <p className="text-label-sm text-on-surface-variant">
                Generating does not publish. Nothing goes live until you press publish on a card below.
              </p>
              <button onClick={queueForAll} className="text-label-md text-primary hover:underline" disabled={!url.trim()}>
                Add to shared queue instead
              </button>
            </div>
          </Card>

          <Card padded={false}>
            <div className="p-5 pb-3">
              <CardHeader
                title="Target Channels"
                icon="podcasts"
                subtitle={`${selectableCount} of ${channels.length} available right now`}
                action={
                  <div className="flex items-center gap-2">
                    <span className="rounded-full border border-outline-variant px-2 py-0.5 text-label-md text-on-surface-variant">
                      {selected.size} selected
                    </span>
                    <button
                      onClick={() => setSelected(new Set(channels.filter((c) => c.active && quotaLeft(c) !== 0).map((c) => c.account_name)))}
                      className="text-label-md text-primary hover:underline"
                    >
                      All
                    </button>
                    <button onClick={() => setSelected(new Set())} className="text-label-md text-primary hover:underline">
                      None
                    </button>
                  </div>
                }
              />
            </div>

            {loadingChannels ? (
              <div className="grid gap-3 p-5 pt-0 sm:grid-cols-2">
                {Array.from({ length: 4 }, (_, i) => <Skeleton key={i} height={104} />)}
              </div>
            ) : channels.length === 0 ? (
              <div className="p-5 pt-0">
                <EmptyState icon="hub" title="No channels configured" hint="Add one in Accounts first." />
              </div>
            ) : (
              <div className="grid gap-3 p-5 pt-0 sm:grid-cols-2">
                {channels.map((channel) => (
                  <ChannelTile
                    key={channel.account_name}
                    channel={channel}
                    selected={selected.has(channel.account_name)}
                    onToggle={() => toggle(channel.account_name)}
                  />
                ))}
              </div>
            )}
          </Card>
        </div>

        <div className="space-y-4 lg:col-span-2">
          <div className="flex items-center justify-between">
            <h3 className="text-headline-sm font-semibold text-on-surface">Composition</h3>
            {results.length > 0 && (
              <span className="text-label-md text-on-surface-variant">{results.length} drafts</span>
            )}
          </div>

          {generating ? (
            <div className="space-y-3">
              {Array.from({ length: 2 }, (_, i) => <Skeleton key={i} height={260} />)}
            </div>
          ) : results.length === 0 ? (
            <EmptyState
              icon="edit_note"
              title="No drafts yet"
              hint="Paste a product URL and generate a preview to see per-channel copy here."
            />
          ) : (
            <div className="space-y-4">
              {results.map((result, index) => (
                <DraftCard
                  key={`${result.account}-${index}`}
                  result={result}
                  apiFetch={apiFetch}
                  onPublished={loadChannels}
                />
              ))}
            </div>
          )}
        </div>
      </div>
    </div>
  );
}

/** quotaLeft returns remaining posts for today, or null when uncapped. */
function quotaLeft(channel) {
  if (!channel.max_posts_per_day || channel.max_posts_per_day <= 0) return null;
  return Math.max(0, channel.max_posts_per_day - channel.posts_today);
}

function ChannelTile({ channel, selected, onToggle }) {
  const remaining = quotaLeft(channel);
  const exhausted = remaining === 0;
  const disabled = !channel.active || exhausted;

  return (
    <button
      onClick={onToggle}
      disabled={disabled}
      aria-pressed={selected}
      className={`rounded-md border p-3 text-left transition-colors disabled:cursor-not-allowed disabled:opacity-50 ${
        selected
          ? 'border-primary bg-primary/10'
          : 'border-outline-variant bg-surface-container-low hover:border-outline'
      }`}
    >
      <div className="flex items-start justify-between gap-2">
        <div className="min-w-0">
          <div className="truncate text-body-md font-medium text-on-surface">{channel.account_name}</div>
          <div className="text-label-sm text-on-surface-variant">Facebook Page</div>
        </div>
        <span className={`mt-1 h-2 w-2 shrink-0 rounded-full ${channel.active ? 'bg-secondary' : 'bg-outline'}`} />
      </div>

      <div className="mt-3 grid grid-cols-2 gap-2">
        <div>
          <div className="text-label-sm uppercase text-on-surface-variant">Daily limit</div>
          <div className="text-body-sm tabular text-on-surface">
            {channel.max_posts_per_day > 0 ? (
              <>
                {channel.posts_today}/{channel.max_posts_per_day}{' '}
                <span className={exhausted ? 'text-error' : 'text-secondary'}>
                  {exhausted ? 'full' : 'used'}
                </span>
              </>
            ) : (
              <>{channel.posts_today} <span className="text-on-surface-variant">/ ∞</span></>
            )}
          </div>
        </div>
        <div>
          <div className="text-label-sm uppercase text-on-surface-variant">Pool</div>
          <div className="text-body-sm tabular text-on-surface">
            {channel.queue_size} <span className="text-on-surface-variant">unposted</span>
          </div>
        </div>
      </div>
    </button>
  );
}

/**
 * DraftCard shows one channel's generated copy, editable before publishing.
 *
 * The provider badge is resolved from the pipeline events for this result's
 * trace, so it reports which model actually produced the copy rather than
 * assuming one.
 */
function DraftCard({ result, apiFetch, onPublished }) {
  const [content, setContent] = useState(result.output || '');
  const [publishing, setPublishing] = useState(false);
  const [published, setPublished] = useState(null);
  const [error, setError] = useState('');
  const [provider, setProvider] = useState(null);
  const [copied, setCopied] = useState(false);

  // Resolve which provider generated this draft from its trace.
  useEffect(() => {
    if (!result.trace_id || result.error) return;

    let cancelled = false;
    (async () => {
      try {
        const resp = await apiFetch(`/events/${encodeURIComponent(result.trace_id)}`);
        if (!resp.ok) return;
        const data = await resp.json();
        const success = (data.events || [])
          .filter((e) => e.event_type === 'AI_GENERATION_SUCCESS' && e.account_name === result.account)
          .pop();
        if (!cancelled && success) {
          setProvider({ name: success.source, model: success.metadata?.model, ms: success.duration_ms });
        }
      } catch {
        // The badge is decorative; a failed lookup just leaves it off.
      }
    })();

    return () => { cancelled = true; };
  }, [apiFetch, result.trace_id, result.account, result.error]);

  const publish = async () => {
    setPublishing(true);
    setError('');
    try {
      const resp = await apiFetch('/publish', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ account: result.account, content }),
      });
      if (!resp.ok) {
        const body = await resp.json().catch(() => ({}));
        throw new Error(body.error || 'Publish failed');
      }
      const data = await resp.json();
      setPublished(data.publish_id);
      onPublished?.();
    } catch (err) {
      setError(err.message);
    } finally {
      setPublishing(false);
    }
  };

  const copy = async () => {
    await navigator.clipboard.writeText(content);
    setCopied(true);
    setTimeout(() => setCopied(false), 1500);
  };

  if (result.error) {
    return (
      <Card>
        <div className="flex items-center justify-between gap-2">
          <h4 className="text-body-lg font-semibold text-on-surface">{result.account}</h4>
          <StatusPill status="failed" />
        </div>
        <p className="mt-2 break-words text-body-sm text-on-surface-variant">{result.error}</p>
      </Card>
    );
  }

  const affiliateLink = extractLink(content);

  return (
    <Card>
      <div className="mb-3 flex flex-wrap items-center justify-between gap-2">
        <h4 className="truncate text-body-lg font-semibold text-on-surface">{result.account}</h4>
        {provider ? (
          <span
            className="rounded border border-outline-variant px-2 py-0.5 font-label text-label-sm uppercase text-on-surface-variant"
            title={`${provider.model || provider.name} took ${provider.ms} ms`}
          >
            {provider.name} draft
          </span>
        ) : (
          <span className="rounded border border-outline-variant px-2 py-0.5 font-label text-label-sm uppercase text-on-surface-variant">
            draft
          </span>
        )}
      </div>

      <label className="mb-1 block text-label-sm uppercase text-on-surface-variant" htmlFor={`body-${result.account}`}>
        Post body
      </label>
      <textarea
        id={`body-${result.account}`}
        value={content}
        onChange={(e) => setContent(e.target.value)}
        rows={10}
        disabled={Boolean(published)}
        className="w-full rounded border border-outline-variant bg-surface-container-lowest px-3 py-2 text-body-sm text-on-surface focus:border-primary focus:outline-none disabled:opacity-60"
      />
      <div className="mt-1 flex items-center justify-between">
        <span className="text-label-sm text-on-surface-variant tabular">{content.length} chars</span>
        <button onClick={copy} className="text-label-md text-primary hover:underline">
          {copied ? 'Copied' : 'Copy'}
        </button>
      </div>

      {affiliateLink && (
        <div className="mt-3">
          <div className="mb-1 text-label-sm uppercase text-on-surface-variant">Injected affiliate link</div>
          <div className="truncate rounded border border-outline-variant bg-surface-container-lowest px-3 py-2 font-label text-label-sm text-on-surface-variant" title={affiliateLink}>
            {affiliateLink}
          </div>
        </div>
      )}

      {error && (
        <p className="mt-3 rounded border border-error/30 bg-error/10 px-3 py-2 text-body-sm text-on-surface-variant">
          {error}
        </p>
      )}

      <div className="mt-4">
        {published ? (
          <div className="flex items-center gap-2 rounded border border-secondary/30 bg-secondary/10 px-3 py-2 text-body-sm">
            <span className="material-symbols-outlined text-secondary" style={{ fontSize: 18 }}>check_circle</span>
            <span className="text-on-surface">Published</span>
            <span className="truncate font-label text-label-sm text-on-surface-variant">{published}</span>
          </div>
        ) : (
          <Button onClick={publish} variant="primary" icon="send" disabled={publishing || !content.trim()} className="w-full">
            {publishing ? 'Publishing…' : 'Publish now'}
          </Button>
        )}
      </div>
    </Card>
  );
}

/** extractLink pulls the first URL out of the rendered post. */
function extractLink(text) {
  const match = String(text).match(/https?:\/\/\S+/);
  return match ? match[0] : '';
}
