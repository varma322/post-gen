const NAV = [
  { id: 'dashboard', label: 'Generator', icon: 'rocket_launch' },
  { id: 'autopublish', label: 'Auto-Publisher', icon: 'schedule' },
  { id: 'stats', label: 'Pages Dashboard', icon: 'analytics' },
  { id: 'templates', label: 'Templates Manager', icon: 'badge' },
  { id: 'accounts', label: 'Accounts Manager', icon: 'manage_accounts' },
];

export default function Sidebar({ activeTab, onSelectTab, mobileMenuOpen, apiToken, onApiTokenChange, onSaveToken, tokenStatus }) {
  return (
    <nav className={`
      ${mobileMenuOpen ? 'flex' : 'hidden'}
      md:flex flex-col bg-surface-container-low border-r border-outline-variant absolute md:static inset-y-0 left-0 w-64 pt-6 md:pt-8 z-40 duration-300 ease-in-out h-full
    `}>
      <div className="flex flex-col gap-1 px-3 flex-1">
        {NAV.map(item => (
          <button
            key={item.id}
            onClick={() => onSelectTab(item.id)}
            className={`w-full text-left rounded-lg px-4 py-3 flex items-center gap-3 duration-150 ease-in-out font-label font-medium text-sm ${
              activeTab === item.id ? 'bg-secondary-container text-on-secondary-container' : 'text-on-surface-variant hover:bg-surface-container-highest hover:text-on-surface'
            }`}
          >
            <span className="material-symbols-outlined" style={{ fontSize: 20 }}>{item.icon}</span>
            {item.label}
          </button>
        ))}
      </div>

      <div className="p-4 border-t border-outline-variant bg-surface-container-lowest/50">
        <h4 className="text-xs font-bold text-primary tracking-wider uppercase mb-3">API Token Access</h4>
        <div className="space-y-2">
          <input
            type="password"
            value={apiToken}
            onChange={(e) => onApiTokenChange(e.target.value)}
            placeholder="sk-..."
            className="w-full bg-surface-variant border border-outline-variant rounded-lg px-3 py-2 text-on-surface text-xs focus:ring-2 focus:ring-primary focus:border-transparent transition-all"
          />
          <button
            onClick={onSaveToken}
            className="w-full bg-primary-container text-on-primary-container hover:bg-primary font-bold px-3 py-1.5 rounded-lg text-xs transition-colors flex items-center justify-center gap-2 shadow"
          >
            <span className="material-symbols-outlined text-[14px]">save</span>
            Save Token
          </button>
          {tokenStatus.text && (
            <p className={`text-[11px] font-semibold ${tokenStatus.isError ? 'text-error' : 'text-primary'}`}>
              {tokenStatus.text}
            </p>
          )}
        </div>
      </div>
    </nav>
  );
}
