import { chart } from './charts/palette';

/**
 * Shared presentational primitives for the operations screens.
 *
 * These exist because the dashboard, activity log, analytics, queue, and
 * channel screens all render the same handful of shapes - a titled panel, a
 * KPI with a delta, a status pill - and duplicating them across five files is
 * how they drift apart.
 */

/** Card is the standard raised panel: tonal surface plus a hairline border. */
export function Card({ children, className = '', padded = true }) {
  return (
    <div className={`rounded-md border border-outline-variant bg-surface-container ${padded ? 'p-5' : ''} ${className}`}>
      {children}
    </div>
  );
}

/** CardHeader gives a panel its title, optional subtitle, and a right slot. */
export function CardHeader({ title, subtitle, action, icon }) {
  return (
    <div className="mb-4 flex items-start justify-between gap-4">
      <div className="min-w-0">
        <h3 className="flex items-center gap-2 text-headline-sm font-semibold text-on-surface">
          {icon && <span className="material-symbols-outlined text-primary" style={{ fontSize: 20 }}>{icon}</span>}
          {title}
        </h3>
        {subtitle && <p className="mt-0.5 text-body-sm text-on-surface-variant">{subtitle}</p>}
      </div>
      {action && <div className="shrink-0">{action}</div>}
    </div>
  );
}

/**
 * KpiCard is one figure in the dashboard's top row.
 *
 * `delta` is the API's {current, previous, pct_change} shape. A null
 * pct_change means the previous window was empty, which has no meaningful
 * percentage - it renders as a dash rather than an invented number.
 */
export function KpiCard({ label, value, icon, delta, tone = 'default', hint }) {
  const tones = {
    default: 'border-outline-variant',
    warning: 'border-tertiary/60',
    error: 'border-error/60',
  };

  const valueTones = {
    default: 'text-on-surface',
    warning: 'text-tertiary',
    error: 'text-error',
  };

  return (
    <div className={`rounded-md border bg-surface-container p-4 ${tones[tone] || tones.default}`}>
      <div className="flex items-center justify-between">
        <span className="text-body-sm text-on-surface-variant">{label}</span>
        {icon && (
          <span
            className={`material-symbols-outlined ${tone === 'default' ? 'text-on-surface-variant' : valueTones[tone]}`}
            style={{ fontSize: 18 }}
          >
            {icon}
          </span>
        )}
      </div>

      <div className="mt-2 flex items-baseline gap-2">
        <span className={`text-headline-lg font-semibold tabular ${valueTones[tone] || valueTones.default}`}>
          {formatNumber(value)}
        </span>
        {delta && <DeltaPill delta={delta} />}
      </div>

      {hint && <p className="mt-1 text-label-sm text-on-surface-variant">{hint}</p>}
    </div>
  );
}

/** DeltaPill renders a period-over-period change. */
export function DeltaPill({ delta }) {
  if (!delta || delta.pct_change === null || delta.pct_change === undefined) {
    return <span className="text-label-sm text-on-surface-variant" title="No comparable previous period">—</span>;
  }

  const rising = delta.pct_change >= 0;
  const classes = rising
    ? 'bg-secondary/15 text-secondary'
    : 'bg-error/15 text-error';

  return (
    <span
      className={`inline-flex items-center gap-0.5 rounded-full px-1.5 py-0.5 text-label-sm tabular ${classes}`}
      title={`${delta.current} this period vs ${delta.previous} previous`}
    >
      <span className="material-symbols-outlined" style={{ fontSize: 13 }}>
        {rising ? 'arrow_upward' : 'arrow_downward'}
      </span>
      {Math.abs(delta.pct_change)}%
    </span>
  );
}

/**
 * StatusPill encodes state in colour and shape as well as text, so a failed
 * row reads at a glance without parsing the label.
 */
export function StatusPill({ status }) {
  const key = String(status || '').toLowerCase();

  const styles = {
    published: 'bg-secondary/15 text-secondary border-secondary/30',
    success: 'bg-secondary/15 text-secondary border-secondary/30',
    succ: 'bg-secondary/15 text-secondary border-secondary/30',
    active: 'bg-secondary/15 text-secondary border-secondary/30',
    completed: 'bg-secondary/15 text-secondary border-secondary/30',
    publishing: 'bg-primary/15 text-primary border-primary/30',
    running: 'bg-primary/15 text-primary border-primary/30',
    pending: 'bg-surface-container-highest text-on-surface-variant border-outline-variant',
    queued: 'bg-tertiary/15 text-tertiary border-tertiary/30',
    cooldown: 'bg-tertiary/15 text-tertiary border-tertiary/30',
    skipped: 'bg-tertiary/15 text-tertiary border-tertiary/30',
    warn: 'bg-tertiary/15 text-tertiary border-tertiary/30',
    paused: 'bg-tertiary/15 text-tertiary border-tertiary/30',
    failed: 'bg-error/15 text-error border-error/30',
    err: 'bg-error/15 text-error border-error/30',
    error: 'bg-error/15 text-error border-error/30',
    cancelled: 'bg-error/15 text-error border-error/30',
    inactive: 'bg-surface-container-highest text-on-surface-variant border-outline-variant',
    info: 'bg-surface-container-highest text-on-surface-variant border-outline-variant',
    idle: 'bg-surface-container-highest text-on-surface-variant border-outline-variant',
  };

  return (
    <span className={`inline-flex items-center gap-1.5 whitespace-nowrap rounded-full border px-2 py-0.5 text-label-sm ${styles[key] || styles.info}`}>
      <span className="h-1.5 w-1.5 rounded-full bg-current" />
      {status}
    </span>
  );
}

/** Button covers the three variants the design system defines. */
export function Button({ children, onClick, variant = 'secondary', icon, disabled, type = 'button', className = '' }) {
  const variants = {
    primary: 'bg-primary text-on-primary hover:bg-primary-fixed disabled:bg-primary/40',
    secondary: 'border border-outline-variant bg-transparent text-on-surface hover:bg-surface-container-high',
    danger: 'border border-error/40 bg-error/10 text-error hover:bg-error/20',
  };

  return (
    <button
      type={type}
      onClick={onClick}
      disabled={disabled}
      className={`inline-flex items-center justify-center gap-2 rounded px-4 py-2 text-body-md font-medium transition-colors disabled:cursor-not-allowed disabled:opacity-50 ${variants[variant]} ${className}`}
    >
      {icon && <span className="material-symbols-outlined" style={{ fontSize: 18 }}>{icon}</span>}
      {children}
    </button>
  );
}

/** EmptyState explains an empty panel rather than leaving a blank box. */
export function EmptyState({ icon = 'inbox', title, hint }) {
  return (
    <div className="flex flex-col items-center justify-center gap-2 rounded-md border border-dashed border-outline-variant px-6 py-12 text-center">
      <span className="material-symbols-outlined text-on-surface-variant" style={{ fontSize: 32 }}>{icon}</span>
      <p className="text-body-md text-on-surface">{title}</p>
      {hint && <p className="max-w-sm text-body-sm text-on-surface-variant">{hint}</p>}
    </div>
  );
}

/** ErrorNotice reports a failed load with the actual reason. */
export function ErrorNotice({ message, onRetry }) {
  return (
    <div className="flex items-start gap-3 rounded-md border border-error/30 bg-error/10 px-4 py-3">
      <span className="material-symbols-outlined text-error" style={{ fontSize: 20 }}>error</span>
      <div className="min-w-0 flex-1">
        <p className="text-body-md text-on-surface">Could not load this data</p>
        <p className="mt-0.5 break-words text-body-sm text-on-surface-variant">{message}</p>
      </div>
      {onRetry && <Button onClick={onRetry} icon="refresh">Retry</Button>}
    </div>
  );
}

/** Skeleton is the loading placeholder, sized to the content it replaces. */
export function Skeleton({ height = 80, className = '' }) {
  return (
    <div
      className={`animate-pulse rounded-md bg-surface-container-high ${className}`}
      style={{ height }}
      aria-hidden="true"
    />
  );
}

/** MetricRow is a labelled figure in a side panel. */
export function MetricRow({ label, value, tone, dot }) {
  const tones = {
    success: 'text-secondary',
    warning: 'text-tertiary',
    error: 'text-error',
    primary: 'text-primary',
  };

  return (
    <div className="flex items-center justify-between rounded border border-outline-variant/60 bg-surface-container-low px-3 py-2.5">
      <span className="flex items-center gap-2 text-body-md text-on-surface-variant">
        {dot && <span className={`h-2 w-2 rounded-full bg-current ${tones[tone] || 'text-on-surface-variant'}`} />}
        {label}
      </span>
      <span className={`text-body-md font-semibold tabular ${tones[tone] || 'text-on-surface'}`}>
        {formatNumber(value)}
      </span>
    </div>
  );
}

/** formatNumber adds thousands separators; non-numeric values pass through. */
export function formatNumber(value) {
  if (typeof value !== 'number') return value ?? '—';
  return value.toLocaleString('en-IN');
}

/** relativeTime renders a timestamp as "3m ago", which is how the operator
 *  reads recency on a dashboard. */
export function relativeTime(iso) {
  if (!iso) return '—';

  const seconds = Math.floor((Date.now() - new Date(iso).getTime()) / 1000);
  if (seconds < 0) return 'just now';
  if (seconds < 60) return `${seconds}s ago`;

  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes}m ago`;

  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours}h ago`;

  const days = Math.floor(hours / 24);
  if (days < 30) return `${days}d ago`;

  return new Date(iso).toLocaleDateString();
}

/** clockTime renders an absolute wall-clock time, for scheduled moments where
 *  "in 12m" is less useful than "12:05 PM". */
export function clockTime(iso) {
  if (!iso) return '—';
  return new Date(iso).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
}

export { chart };
