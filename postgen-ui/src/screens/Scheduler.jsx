import { useCallback, useEffect, useState } from 'react';
import {
  Button, Card, CardHeader, EmptyState, ErrorNotice, Skeleton,
  StatusPill, clockTime, relativeTime,
} from '../components/ui';

const POLL_MS = 10000;

/**
 * Recurring triggers for the auto-post pipeline, plus the run in flight.
 *
 * Only one job can be active at a time - the database enforces it - so a
 * schedule that comes due while another job is running records why it did not
 * start and waits for its next window rather than queueing up behind it.
 */
export default function Scheduler({ apiFetch, active }) {
  const [schedules, setSchedules] = useState([]);
  const [job, setJob] = useState(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [editing, setEditing] = useState(null);
  const [busy, setBusy] = useState(false);

  const load = useCallback(async ({ quiet = false } = {}) => {
    if (!quiet) setLoading(true);
    try {
      const [schedulesResp, jobResp] = await Promise.all([
        apiFetch('/schedules'),
        apiFetch('/jobs/active'),
      ]);
      if (!schedulesResp.ok) throw new Error('Could not load schedules');

      setSchedules((await schedulesResp.json()).schedules || []);
      if (jobResp.ok) {
        const data = await jobResp.json();
        setJob(data.active ? data.job : null);
      }
      setError('');
    } catch (err) {
      setError(err.message);
    } finally {
      setLoading(false);
    }
  }, [apiFetch]);

  useEffect(() => { load(); }, [load]);

  // Poll only while this screen is on top, since every screen stays mounted.
  useEffect(() => {
    if (!active) return undefined;
    const timer = setInterval(() => {
      if (!document.hidden) load({ quiet: true });
    }, POLL_MS);
    return () => clearInterval(timer);
  }, [active, load]);

  const act = async (path, options, onDone) => {
    setBusy(true);
    setError('');
    try {
      const resp = await apiFetch(path, options);
      if (!resp.ok) {
        const body = await resp.json().catch(() => ({}));
        throw new Error(body.error || `Request failed with status ${resp.status}`);
      }
      onDone?.();
      await load({ quiet: true });
    } catch (err) {
      setError(err.message);
    } finally {
      setBusy(false);
    }
  };

  const save = async (schedule) => {
    const isNew = !schedule.id;
    await act(
      isNew ? '/schedules' : `/schedules/${schedule.id}`,
      {
        method: isNew ? 'POST' : 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          name: schedule.name,
          kind: schedule.kind,
          interval_minutes: Number(schedule.interval_minutes) || 0,
          daily_at: schedule.daily_at || '',
          rotate_old_links: Boolean(schedule.rotate_old_links),
          enabled: Boolean(schedule.enabled),
        }),
      },
      () => setEditing(null)
    );
  };

  return (
    <div className="space-y-5">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <p className="text-body-md text-on-surface-variant">
          Recurring triggers for the auto-post pipeline.
        </p>
        <div className="flex items-center gap-2">
          <Button onClick={() => load()} icon="refresh">Refresh</Button>
          <Button
            onClick={() => setEditing({ kind: 'interval', interval_minutes: 60, enabled: true, rotate_old_links: false, name: '' })}
            variant="primary"
            icon="add"
          >
            New schedule
          </Button>
        </div>
      </div>

      {error && <ErrorNotice message={error} onRetry={load} />}

      <ActiveRun job={job} apiFetch={apiFetch} onChanged={() => load({ quiet: true })} />

      <Card padded={false}>
        <div className="p-5 pb-3">
          <CardHeader title="Scheduled Jobs" icon="calendar_month" subtitle={`${schedules.length} configured`} />
        </div>

        {loading && schedules.length === 0 ? (
          <div className="space-y-2 p-5 pt-0">
            {Array.from({ length: 3 }, (_, i) => <Skeleton key={i} height={56} />)}
          </div>
        ) : schedules.length === 0 ? (
          <div className="p-5 pt-0">
            <EmptyState
              icon="calendar_month"
              title="No schedules yet"
              hint="Create one to trigger the auto-post pipeline on a cadence instead of by hand."
            />
          </div>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full min-w-[760px] text-body-sm">
              <thead>
                <tr className="border-b border-outline-variant text-label-sm uppercase text-on-surface-variant">
                  <th className="px-5 py-3 text-left font-medium">Name</th>
                  <th className="px-3 py-3 text-left font-medium">Cadence</th>
                  <th className="px-3 py-3 text-left font-medium">Status</th>
                  <th className="px-3 py-3 text-left font-medium">Last run</th>
                  <th className="px-3 py-3 text-left font-medium">Next run</th>
                  <th className="px-5 py-3 text-right font-medium">Actions</th>
                </tr>
              </thead>
              <tbody>
                {schedules.map((schedule) => (
                  <ScheduleRow
                    key={schedule.id}
                    schedule={schedule}
                    busy={busy}
                    onEdit={() => setEditing(schedule)}
                    onToggle={() => save({ ...schedule, enabled: !schedule.enabled })}
                    onRun={() => act(`/schedules/${schedule.id}/run`, { method: 'POST' })}
                    onDelete={() => act(`/schedules/${schedule.id}`, { method: 'DELETE' })}
                  />
                ))}
              </tbody>
            </table>
          </div>
        )}
      </Card>

      {editing && (
        <ScheduleDialog
          schedule={editing}
          busy={busy}
          onCancel={() => setEditing(null)}
          onSave={save}
        />
      )}
    </div>
  );
}

function describe(schedule) {
  if (schedule.kind === 'daily') return `Daily at ${schedule.daily_at}`;
  const minutes = schedule.interval_minutes;
  if (minutes % 60 === 0 && minutes >= 60) {
    const hours = minutes / 60;
    return hours === 1 ? 'Every hour' : `Every ${hours} hours`;
  }
  return `Every ${minutes} minutes`;
}

function ScheduleRow({ schedule, busy, onEdit, onToggle, onRun, onDelete }) {
  return (
    <tr className="border-b border-outline-variant/50 last:border-0 hover:bg-surface-container-high/50">
      <td className="px-5 py-3">
        <div className="font-medium text-on-surface">{schedule.name}</div>
        {schedule.rotate_old_links && (
          <div className="text-label-sm text-on-surface-variant">Reposts old links when pools run dry</div>
        )}
        {schedule.last_error && (
          <div className="mt-0.5 max-w-[280px] truncate text-label-sm text-tertiary" title={schedule.last_error}>
            Last attempt: {schedule.last_error}
          </div>
        )}
      </td>
      <td className="whitespace-nowrap px-3 py-3 text-on-surface-variant">{describe(schedule)}</td>
      <td className="px-3 py-3"><StatusPill status={schedule.enabled ? 'active' : 'paused'} /></td>
      <td className="whitespace-nowrap px-3 py-3 text-on-surface-variant">{relativeTime(schedule.last_run_at)}</td>
      <td className="whitespace-nowrap px-3 py-3 text-on-surface-variant">
        {schedule.enabled
          ? schedule.next_run_at ? `${clockTime(schedule.next_run_at)} · ${relativeDue(schedule.next_run_at)}` : '—'
          : <span className="text-on-surface-variant">paused</span>}
      </td>
      <td className="px-5 py-3">
        <div className="flex items-center justify-end gap-1">
          <IconButton icon="play_arrow" label="Run now" onClick={onRun} disabled={busy} />
          <IconButton icon={schedule.enabled ? 'pause' : 'resume'} label={schedule.enabled ? 'Pause' : 'Resume'} onClick={onToggle} disabled={busy} />
          <IconButton icon="edit" label="Edit" onClick={onEdit} disabled={busy} />
          <IconButton icon="delete" label="Delete" onClick={onDelete} disabled={busy} danger />
        </div>
      </td>
    </tr>
  );
}

/** relativeDue renders how long until a future timestamp. */
function relativeDue(iso) {
  const seconds = Math.floor((new Date(iso).getTime() - Date.now()) / 1000);
  if (seconds <= 0) return 'due now';
  if (seconds < 60) return `in ${seconds}s`;
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `in ${minutes}m`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `in ${hours}h`;
  return `in ${Math.floor(hours / 24)}d`;
}

function IconButton({ icon, label, onClick, disabled, danger }) {
  return (
    <button
      onClick={onClick}
      disabled={disabled}
      title={label}
      aria-label={label}
      className={`rounded p-1.5 transition-colors disabled:opacity-40 ${
        danger ? 'text-on-surface-variant hover:text-error' : 'text-on-surface-variant hover:text-on-surface'
      }`}
    >
      <span className="material-symbols-outlined" style={{ fontSize: 18 }}>{icon}</span>
    </button>
  );
}

function ActiveRun({ job, apiFetch, onChanged }) {
  const [cancelling, setCancelling] = useState(false);

  if (!job) {
    return (
      <Card>
        <div className="flex items-center gap-3">
          <span className="material-symbols-outlined text-on-surface-variant" style={{ fontSize: 20 }}>pause_circle</span>
          <div>
            <div className="text-body-md text-on-surface">No active run</div>
            <div className="text-label-sm text-on-surface-variant">
              The worker is idle. A schedule firing, or a manual run, will start one.
            </div>
          </div>
        </div>
      </Card>
    );
  }

  const items = job.items || [];
  const done = items.filter((i) => i.status !== 'pending').length;
  const published = items.filter((i) => i.status === 'published').length;
  const failed = items.filter((i) => i.status === 'failed').length;
  const percent = items.length > 0 ? Math.round((done / items.length) * 100) : 0;

  const cancel = async () => {
    setCancelling(true);
    try {
      await apiFetch('/jobs/cancel', { method: 'POST' });
      onChanged?.();
    } finally {
      setCancelling(false);
    }
  };

  return (
    <Card className="border-primary/40">
      <div className="mb-4 flex flex-wrap items-start justify-between gap-3">
        <div>
          <div className="flex items-center gap-2">
            <span className="h-1.5 w-1.5 animate-pulse rounded-full bg-secondary" />
            <span className="text-label-sm uppercase text-secondary">Active run</span>
          </div>
          <h3 className="mt-1 text-headline-sm font-semibold text-on-surface">
            {job.name || `Job #${job.id}`}
          </h3>
        </div>
        <Button onClick={cancel} variant="danger" icon="close" disabled={cancelling}>
          {cancelling ? 'Cancelling…' : 'Cancel run'}
        </Button>
      </div>

      <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
        <Metric label="Status" value={job.status} pill />
        <Metric label="Started" value={clockTime(job.created_at)} />
        <Metric label="Remaining" value={`${items.length - done} / ${items.length}`} />
        <Metric label="Outcome" value={`${published} ok · ${failed} failed`} />
      </div>

      <div className="mt-4">
        <div className="mb-1 flex items-center justify-between text-label-md">
          <span className="text-on-surface-variant">Progress</span>
          <span className="tabular text-on-surface">{percent}%</span>
        </div>
        <div className="h-2 w-full overflow-hidden rounded-full bg-surface-container-highest">
          <div className="h-full rounded-full bg-primary transition-[width] duration-500" style={{ width: `${percent}%` }} />
        </div>
      </div>
    </Card>
  );
}

function Metric({ label, value, pill }) {
  return (
    <div className="rounded border border-outline-variant/60 bg-surface-container-low px-3 py-2.5">
      <div className="text-label-sm uppercase text-on-surface-variant">{label}</div>
      <div className="mt-1 text-body-md text-on-surface">
        {pill ? <StatusPill status={value} /> : <span className="tabular">{value}</span>}
      </div>
    </div>
  );
}

function ScheduleDialog({ schedule, busy, onCancel, onSave }) {
  const [draft, setDraft] = useState(schedule);

  useEffect(() => {
    const onKey = (e) => { if (e.key === 'Escape') onCancel(); };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [onCancel]);

  const set = (key, value) => setDraft((d) => ({ ...d, [key]: value }));
  const valid = draft.name?.trim() && (draft.kind === 'daily' ? /^\d{2}:\d{2}$/.test(draft.daily_at || '') : Number(draft.interval_minutes) >= 1);

  return (
    <>
      <div className="fixed inset-0 z-40 bg-black/60" onClick={onCancel} aria-hidden="true" />
      <div className="fixed inset-0 z-50 flex items-center justify-center p-4">
        <div className="w-full max-w-lg rounded-md border border-outline-variant bg-surface-container shadow-2xl" role="dialog" aria-label="Schedule">
          <header className="border-b border-outline-variant px-5 py-4">
            <h2 className="text-headline-sm font-semibold text-on-surface">
              {schedule.id ? 'Edit schedule' : 'New schedule'}
            </h2>
          </header>

          <div className="space-y-4 p-5">
            <div>
              <label className="mb-1 block text-label-md text-on-surface" htmlFor="schedule-name">Name</label>
              <input
                id="schedule-name"
                value={draft.name || ''}
                onChange={(e) => set('name', e.target.value)}
                placeholder="Morning deals run"
                className="w-full rounded border border-outline-variant bg-surface-container-lowest px-3 py-2 text-body-md text-on-surface placeholder:text-on-surface-variant focus:border-primary focus:outline-none"
              />
            </div>

            <div>
              <span className="mb-1 block text-label-md text-on-surface">Cadence</span>
              <div className="flex rounded border border-outline-variant p-0.5">
                {[['interval', 'Every N minutes'], ['daily', 'Daily at a time']].map(([id, label]) => (
                  <button
                    key={id}
                    onClick={() => set('kind', id)}
                    className={`flex-1 rounded px-3 py-1.5 text-label-md transition-colors ${
                      draft.kind === id ? 'bg-primary text-on-primary' : 'text-on-surface-variant hover:text-on-surface'
                    }`}
                  >
                    {label}
                  </button>
                ))}
              </div>
            </div>

            {draft.kind === 'daily' ? (
              <div>
                <label className="mb-1 block text-label-md text-on-surface" htmlFor="schedule-time">Time (24-hour, server local)</label>
                <input
                  id="schedule-time"
                  type="time"
                  value={draft.daily_at || ''}
                  onChange={(e) => set('daily_at', e.target.value)}
                  className="rounded border border-outline-variant bg-surface-container-lowest px-3 py-2 text-body-md text-on-surface focus:border-primary focus:outline-none"
                />
              </div>
            ) : (
              <div>
                <label className="mb-1 block text-label-md text-on-surface" htmlFor="schedule-interval">Interval (minutes)</label>
                <input
                  id="schedule-interval"
                  type="number"
                  min="1"
                  value={draft.interval_minutes || ''}
                  onChange={(e) => set('interval_minutes', e.target.value)}
                  className="w-32 rounded border border-outline-variant bg-surface-container-lowest px-3 py-2 text-body-md tabular text-on-surface focus:border-primary focus:outline-none"
                />
                <p className="mt-1 text-label-sm text-on-surface-variant">
                  Counted from the last run. Missed windows are not replayed.
                </p>
              </div>
            )}

            <label className="flex items-start gap-2">
              <input
                type="checkbox"
                checked={Boolean(draft.rotate_old_links)}
                onChange={(e) => set('rotate_old_links', e.target.checked)}
                className="mt-1"
              />
              <span>
                <span className="block text-label-md text-on-surface">Repost old links when pools run dry</span>
                <span className="block text-label-sm text-on-surface-variant">
                  Falls back to least-recently-posted links so the run is not skipped entirely.
                </span>
              </span>
            </label>

            <label className="flex items-center gap-2">
              <input
                type="checkbox"
                checked={Boolean(draft.enabled)}
                onChange={(e) => set('enabled', e.target.checked)}
              />
              <span className="text-label-md text-on-surface">Enabled</span>
            </label>
          </div>

          <footer className="flex justify-end gap-2 border-t border-outline-variant px-5 py-4">
            <Button onClick={onCancel} disabled={busy}>Cancel</Button>
            <Button onClick={() => onSave(draft)} variant="primary" disabled={busy || !valid}>
              {busy ? 'Saving…' : 'Save schedule'}
            </Button>
          </footer>
        </div>
      </div>
    </>
  );
}
