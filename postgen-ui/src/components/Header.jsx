export default function Header({ mobileMenuOpen, onToggleMobileMenu, accounts }) {
  return (
    <header className="bg-surface-container-low border-b border-outline-variant shadow-sm top-0 sticky z-50 flex items-center justify-between px-4 md:px-6 py-3 w-full">
      <div className="flex items-center gap-3">
        <button
          className="md:hidden text-on-surface p-2 hover:bg-surface-container rounded-full"
          onClick={onToggleMobileMenu}
        >
          <span className="material-symbols-outlined">{mobileMenuOpen ? 'close' : 'menu'}</span>
        </button>
        <div className="w-8 h-8 rounded-lg bg-primary flex items-center justify-center">
          <span className="material-symbols-outlined text-on-primary-container" style={{ fontSize: 18 }}>bolt</span>
        </div>
        <span className="font-headline font-bold text-lg text-on-surface">PostGen</span>
        <span className="hidden sm:inline text-xs font-label bg-surface-container-high text-on-surface-variant px-2 py-1 rounded-full">
          v2.0 · Redesign
        </span>
      </div>

      <div className="flex items-center gap-3">
        <div className="hidden sm:inline-flex items-center gap-1.5 text-xs font-semibold text-primary bg-primary/10 px-3 py-1.5 rounded-full">
          <span className={`w-1.5 h-1.5 rounded-full bg-primary ${accounts.length === 0 ? 'animate-ping' : ''}`} />
          {accounts.length > 0 ? `${accounts.length} Accounts Connected` : 'Configuring'}
        </div>
        <div className="w-8 h-8 rounded-full bg-surface-container-high flex items-center justify-center text-xs font-bold text-on-surface-variant">
          Y
        </div>
      </div>
    </header>
  );
}
