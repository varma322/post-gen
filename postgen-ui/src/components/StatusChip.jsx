const STATUS_MAP = {
  success: { cls: 'bg-primary-container/20 text-primary border border-primary/20', icon: 'check_circle', label: 'Success' },
  published: { cls: 'bg-primary-container/20 text-primary border border-primary/20', icon: 'check_circle', label: 'Published' },
  error: { cls: 'bg-error-container/30 text-error border border-error/20', icon: 'error', label: 'Error' },
  failed: { cls: 'bg-error-container/30 text-error border border-error/20', icon: 'error', label: 'Failed' },
  publishing: { cls: 'bg-orange-500/20 text-orange-500 border border-orange-500/20 animate-pulse', icon: 'autorenew', label: 'Publishing' },
  pending: { cls: 'bg-secondary-container text-on-secondary-container border border-transparent', icon: 'schedule', label: 'Pending' },
  skipped: { cls: 'bg-surface-container-highest text-on-surface-variant border border-outline-variant', icon: 'remove_circle', label: 'Skipped' },
};

export default function StatusChip({ status, label }) {
  const s = STATUS_MAP[status] || STATUS_MAP.pending;
  return (
    <span className={`px-2.5 py-1 rounded-full text-xs font-bold uppercase tracking-wider inline-flex items-center gap-1.5 w-fit ${s.cls}`}>
      <span className="material-symbols-outlined text-[14px]">{s.icon}</span>
      {label || s.label}
    </span>
  );
}
