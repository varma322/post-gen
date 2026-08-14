import { useCallback, useEffect, useState } from 'react';
import { Button, Card, CardHeader, ErrorNotice, Skeleton, StatusPill } from '../components/ui';

/**
 * Runtime configuration.
 *
 * Values resolve database override -> environment variable -> built-in
 * default, and each field shows which of those it currently comes from, so
 * it's clear whether editing here will actually change anything.
 *
 * Secrets are write-only server-side: this screen can see whether one is
 * configured but never its value.
 */
export default function Settings({ apiFetch }) {
  const [settings, setSettings] = useState(null);
  const [draft, setDraft] = useState({});
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');
  const [saved, setSaved] = useState(false);

  const load = useCallback(async () => {
    setLoading(true);
    setError('');
    try {
      const resp = await apiFetch('/settings');
      if (!resp.ok) {
        const body = await resp.json().catch(() => ({}));
        throw new Error(body.error || `Request failed with status ${resp.status}`);
      }
      const data = await resp.json();
      setSettings(data);
      setDraft({});
    } catch (err) {
      setError(err.message);
    } finally {
      setLoading(false);
    }
  }, [apiFetch]);

  useEffect(() => { load(); }, [load]);

  const edit = (key, value) => {
    setDraft((d) => ({ ...d, [key]: value }));
    setSaved(false);
  };

  const save = async () => {
    if (Object.keys(draft).length === 0) return;

    setSaving(true);
    setError('');
    try {
      const resp = await apiFetch('/settings', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(draft),
      });
      if (!resp.ok) {
        const body = await resp.json().catch(() => ({}));
        throw new Error(body.error || 'Could not save settings');
      }
      setSettings(await resp.json());
      setDraft({});
      setSaved(true);
      setTimeout(() => setSaved(false), 2500);
    } catch (err) {
      setError(err.message);
    } finally {
      setSaving(false);
    }
  };

  if (loading && !settings) {
    return <div className="space-y-4"><Skeleton height={240} /><Skeleton height={200} /></div>;
  }
  if (error && !settings) return <ErrorNotice message={error} onRetry={load} />;
  if (!settings) return null;

  const value = (key, current) => (draft[key] !== undefined ? draft[key] : current);
  const dirty = Object.keys(draft).length > 0;

  return (
    <div className="space-y-5">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <p className="text-body-md text-on-surface-variant">
          Manage AI providers, platform integrations, and worker behaviour.
        </p>
        <div className="flex items-center gap-3">
          {saved && <span className="text-body-sm text-secondary">Saved</span>}
          {dirty && (
            <Button onClick={() => setDraft({})} disabled={saving}>Discard</Button>
          )}
          <Button onClick={save} variant="primary" icon="save" disabled={!dirty || saving}>
            {saving ? 'Saving…' : 'Save changes'}
          </Button>
        </div>
      </div>

      {error && <ErrorNotice message={error} onRetry={load} />}

      <div className="grid gap-5 lg:grid-cols-3">
        <div className="space-y-5 lg:col-span-2">
          <Card>
            <CardHeader
              title="AI Engine"
              icon="auto_awesome"
              subtitle="Providers are tried in order; the first success wins"
              action={
                <div className="flex items-center gap-1.5">
                  {settings.ai.provider_order.map((p, i) => (
                    <span key={p} className="flex items-center gap-1.5">
                      {i > 0 && <span className="text-on-surface-variant">→</span>}
                      <StatusPill status={p} />
                    </span>
                  ))}
                </div>
              }
            />

            <div className="grid gap-4 sm:grid-cols-2">
              <Field
                label="Ollama host"
                source={settings.sources.ollama_host}
                value={value('ollama_host', settings.ai.ollama_host)}
                onChange={(v) => edit('ollama_host', v)}
                placeholder="http://127.0.0.1:11434"
              />
              <Field
                label="Ollama model"
                source={settings.sources.ollama_model}
                value={value('ollama_model', settings.ai.ollama_model)}
                onChange={(v) => edit('ollama_model', v)}
                placeholder="qwen2.5:7b-instruct"
              />
              <Field
                label="Ollama timeout"
                source={settings.sources.ollama_timeout}
                value={value('ollama_timeout', settings.ai.ollama_timeout)}
                onChange={(v) => edit('ollama_timeout', v)}
                hint="Must cover a cold model load, not just generation"
              />
              <Field
                label="Gemini model"
                source={settings.sources.gemini_model}
                value={value('gemini_model', settings.ai.gemini_model)}
                onChange={(v) => edit('gemini_model', v)}
              />
            </div>

            <div className="mt-4 border-t border-outline-variant pt-4">
              <SecretRow
                label="Gemini API key"
                configured={settings.ai.gemini_key_set}
                hint="Set with the GEMINI_API environment variable. Never returned by this API."
              />
            </div>
          </Card>

          <Card>
            <CardHeader title="Meta Integration" icon="share" subtitle="Facebook Graph API" />
            <Field
              label="Graph API version"
              source={settings.sources.graph_api_version}
              value={value('graph_api_version', settings.meta.graph_api_version)}
              onChange={(v) => edit('graph_api_version', v)}
              hint="Graph versions sunset about two years after release. A sunset version is served silently as a newer one."
            />
          </Card>

          <Card>
            <CardHeader title="Amazon Creators API" icon="storefront" />
            <Field
              label="Partner tag"
              source={settings.sources.partner_tag}
              value={value('partner_tag', settings.amazon.partner_tag)}
              onChange={(v) => edit('partner_tag', v)}
              hint="Used for every catalog lookup regardless of which channel is publishing."
              warning={!value('partner_tag', settings.amazon.partner_tag)}
              warningText="Unset — the scraper falls back to a hardcoded tag."
            />
            <div className="mt-4 grid gap-3 border-t border-outline-variant pt-4 sm:grid-cols-2">
              <SecretRow label="Client ID" configured={settings.amazon.client_id_set} />
              <SecretRow label="Client secret" configured={settings.amazon.client_secret_set} />
            </div>
          </Card>
        </div>

        <div className="space-y-5">
          <Card>
            <CardHeader title="System Control" icon="tune" />

            <div className="space-y-4">
              <div>
                <label className="mb-1 block text-label-md text-on-surface" htmlFor="cooldown">
                  Worker cooldown
                </label>
                <div className="flex items-center gap-2">
                  <input
                    id="cooldown"
                    type="number"
                    min="0"
                    value={value('worker_cooldown_seconds', settings.system.worker_cooldown_seconds)}
                    onChange={(e) => edit('worker_cooldown_seconds', Number(e.target.value))}
                    className="w-28 rounded border border-outline-variant bg-surface-container-lowest px-3 py-2 text-body-md tabular text-on-surface focus:border-primary focus:outline-none"
                  />
                  <span className="text-body-sm text-on-surface-variant">seconds</span>
                </div>
                <p className="mt-1.5 text-label-sm text-on-surface-variant">
                  Pacing is global, not per channel: the whole estate publishes at most one post per cooldown.
                  At {value('worker_cooldown_seconds', settings.system.worker_cooldown_seconds)}s that is{' '}
                  <span className="tabular text-on-surface">
                    {Math.floor(86400 / Math.max(1, value('worker_cooldown_seconds', settings.system.worker_cooldown_seconds)))}
                  </span>{' '}
                  posts per day across every channel.
                </p>
              </div>

              <Toggle
                label="Debug logging"
                hint="Verbose output to the server console"
                checked={value('debug_logging', settings.system.debug_logging)}
                onChange={(v) => edit('debug_logging', v)}
              />
            </div>
          </Card>

          <Card>
            <CardHeader title="Where values come from" icon="help" />
            <ul className="space-y-2 text-body-sm text-on-surface-variant">
              <li className="flex items-start gap-2">
                <SourceBadge source="database" />
                <span>Saved here. Overrides everything else.</span>
              </li>
              <li className="flex items-start gap-2">
                <SourceBadge source="environment" />
                <span>From <code className="font-label">.env</code>. Saving here overrides it.</span>
              </li>
              <li className="flex items-start gap-2">
                <SourceBadge source="default" />
                <span>Built in. Nothing configured.</span>
              </li>
            </ul>
            <p className="mt-3 border-t border-outline-variant pt-3 text-label-sm text-on-surface-variant">
              Changes apply to work started after saving. A job already mid-publish finishes with the values it began with.
            </p>
          </Card>
        </div>
      </div>
    </div>
  );
}

function Field({ label, value, onChange, source, hint, placeholder, warning, warningText }) {
  return (
    <div>
      <div className="mb-1 flex items-center justify-between gap-2">
        <label className="text-label-md text-on-surface">{label}</label>
        {source && <SourceBadge source={source} />}
      </div>
      <input
        value={value ?? ''}
        onChange={(e) => onChange(e.target.value)}
        placeholder={placeholder}
        className="w-full rounded border border-outline-variant bg-surface-container-lowest px-3 py-2 text-body-md text-on-surface placeholder:text-on-surface-variant focus:border-primary focus:outline-none"
      />
      {warning && warningText && (
        <p className="mt-1.5 flex items-center gap-1 text-label-sm text-tertiary">
          <span className="material-symbols-outlined" style={{ fontSize: 14 }}>warning</span>
          {warningText}
        </p>
      )}
      {hint && <p className="mt-1.5 text-label-sm text-on-surface-variant">{hint}</p>}
    </div>
  );
}

function SecretRow({ label, configured, hint }) {
  return (
    <div>
      <div className="flex items-center justify-between gap-2">
        <span className="text-label-md text-on-surface">{label}</span>
        <span className={`inline-flex items-center gap-1 text-label-sm ${configured ? 'text-secondary' : 'text-tertiary'}`}>
          <span className="material-symbols-outlined" style={{ fontSize: 14 }}>
            {configured ? 'check_circle' : 'error'}
          </span>
          {configured ? 'Configured' : 'Not set'}
        </span>
      </div>
      {hint && <p className="mt-1 text-label-sm text-on-surface-variant">{hint}</p>}
    </div>
  );
}

function SourceBadge({ source }) {
  const styles = {
    database: 'border-primary/30 bg-primary/10 text-primary',
    environment: 'border-secondary/30 bg-secondary/10 text-secondary',
    default: 'border-outline-variant text-on-surface-variant',
  };

  return (
    <span className={`shrink-0 rounded-full border px-1.5 py-0.5 text-label-sm ${styles[source] || styles.default}`}>
      {source}
    </span>
  );
}

function Toggle({ label, hint, checked, onChange }) {
  return (
    <div className="flex items-start justify-between gap-3">
      <div className="min-w-0">
        <div className="text-label-md text-on-surface">{label}</div>
        {hint && <p className="mt-0.5 text-label-sm text-on-surface-variant">{hint}</p>}
      </div>
      <button
        role="switch"
        aria-checked={checked}
        aria-label={label}
        onClick={() => onChange(!checked)}
        className={`relative h-6 w-11 shrink-0 rounded-full transition-colors ${checked ? 'bg-primary' : 'bg-surface-container-highest'}`}
      >
        <span
          className={`absolute top-0.5 h-5 w-5 rounded-full transition-transform ${
            checked ? 'left-0.5 translate-x-5 bg-on-primary' : 'left-0.5 bg-outline'
          }`}
        />
      </button>
    </div>
  );
}
