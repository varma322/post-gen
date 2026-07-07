export default function AccountChip({ account, selected, onToggle }) {
  const initials = account.name.split(' ').map(w => w[0]).slice(0, 2).join('').toUpperCase();
  return (
    <button
      type="button"
      onClick={onToggle}
      className={`flex items-center gap-3 p-3 rounded-lg border text-left transition-colors w-full ${
        selected ? 'border-primary bg-primary/5' : 'border-outline-variant bg-surface-variant hover:border-outline'
      }`}
    >
      <div className={`w-8 h-8 rounded-full flex items-center justify-center text-xs font-bold flex-shrink-0 ${
        selected ? 'bg-primary text-on-primary-container' : 'bg-surface-container-high text-on-surface-variant'
      }`}>
        {initials}
      </div>
      <div className="min-w-0 flex-1">
        <div className="text-sm font-semibold text-on-surface truncate">{account.name}</div>
        <div className="text-xs text-on-surface-variant truncate">{account.affiliate_tag || 'No tag'}</div>
      </div>
      {selected && <span className="material-symbols-outlined text-primary flex-shrink-0" style={{ fontSize: 18 }}>check_circle</span>}
    </button>
  );
}
