import { useState } from 'react';

/**
 * The application frame: a fixed sidebar plus a top bar, with the active
 * screen rendered into the content area.
 *
 * Nav order follows the PostGen V2 mockups. The sidebar collapses to icons at
 * the tablet breakpoint and becomes a slide-over on mobile.
 */

export const NAV_ITEMS = [
  { id: 'dashboard', label: 'Dashboard', icon: 'dashboard' },
  { id: 'publisher', label: 'Publisher', icon: 'upload' },
  { id: 'channels', label: 'Channels', icon: 'hub' },
  { id: 'analytics', label: 'Analytics', icon: 'analytics' },
  { id: 'queue', label: 'Content Queue', icon: 'view_list' },
  { id: 'scheduler', label: 'Scheduler', icon: 'calendar_month' },
  { id: 'activity', label: 'Activity Log', icon: 'history' },
  { id: 'templates', label: 'Templates', icon: 'description' },
  { id: 'accounts', label: 'Accounts', icon: 'manage_accounts' },
  { id: 'settings', label: 'Settings', icon: 'settings' },
];

export default function AppShell({
  activeTab,
  onSelectTab,
  apiToken,
  onApiTokenChange,
  onSaveToken,
  tokenStatus,
  workerStatus,
  children,
}) {
  const [collapsed, setCollapsed] = useState(false);
  const [mobileOpen, setMobileOpen] = useState(false);

  const select = (id) => {
    onSelectTab(id);
    setMobileOpen(false);
  };

  return (
    // A fixed viewport frame rather than a page that grows: the shell is
    // exactly one screen tall and never scrolls, so the sidebar and top bar
    // stay put while only the content column moves. Without this the sidebar
    // scrolls away on long screens like the activity log.
    <div className="flex h-screen overflow-hidden bg-background font-body text-on-surface antialiased">
      {/* Mobile scrim */}
      {mobileOpen && (
        <div
          className="fixed inset-0 z-30 bg-black/60 lg:hidden"
          onClick={() => setMobileOpen(false)}
          aria-hidden="true"
        />
      )}

      <aside
        className={`fixed inset-y-0 left-0 z-40 flex h-screen shrink-0 flex-col border-r border-outline-variant bg-surface-container-low transition-transform lg:static lg:h-full lg:translate-x-0 ${
          mobileOpen ? 'translate-x-0' : '-translate-x-full'
        }`}
        style={{ width: collapsed ? 64 : 240 }}
      >
        <div className="flex items-center gap-3 border-b border-outline-variant px-4 py-4">
          <div className="grid h-9 w-9 shrink-0 place-items-center rounded bg-primary text-on-primary">
            <span className="material-symbols-outlined" style={{ fontSize: 20 }}>bolt</span>
          </div>
          {!collapsed && (
            <div className="min-w-0">
              <div className="truncate text-body-lg font-semibold text-primary">PostGen V2</div>
              <div className="truncate text-label-sm text-on-surface-variant">Marketing Ops</div>
            </div>
          )}
        </div>

        <nav className="flex-1 space-y-1 overflow-y-auto p-2" aria-label="Main">
          {NAV_ITEMS.map((item) => {
            const isActive = activeTab === item.id;
            return (
              <button
                key={item.id}
                onClick={() => select(item.id)}
                title={collapsed ? item.label : undefined}
                aria-current={isActive ? 'page' : undefined}
                className={`flex w-full items-center gap-3 rounded px-3 py-2.5 text-body-md transition-colors ${
                  isActive
                    ? 'bg-primary text-on-primary font-semibold'
                    : 'text-on-surface-variant hover:bg-surface-container-high hover:text-on-surface'
                }`}
              >
                <span className="material-symbols-outlined shrink-0" style={{ fontSize: 20 }}>{item.icon}</span>
                {!collapsed && <span className="truncate">{item.label}</span>}
              </button>
            );
          })}
        </nav>

        {!collapsed && (
          <div className="border-t border-outline-variant p-3">
            <label className="mb-1.5 block text-label-sm text-on-surface-variant" htmlFor="api-token">
              API Bearer Token
            </label>
            <input
              id="api-token"
              type="password"
              value={apiToken}
              onChange={(e) => onApiTokenChange(e.target.value)}
              placeholder="Paste token"
              className="w-full rounded border border-outline-variant bg-surface-container-lowest px-2.5 py-1.5 text-body-sm text-on-surface placeholder:text-on-surface-variant focus:border-primary focus:outline-none"
            />
            <button
              onClick={onSaveToken}
              className="mt-2 w-full rounded bg-surface-container-high px-3 py-1.5 text-label-md text-on-surface hover:bg-surface-container-highest"
            >
              Save token
            </button>
            {tokenStatus?.text && (
              <p className={`mt-1.5 text-label-sm ${tokenStatus.isError ? 'text-error' : 'text-secondary'}`}>
                {tokenStatus.text}
              </p>
            )}
          </div>
        )}

        <button
          onClick={() => setCollapsed((v) => !v)}
          className="hidden items-center justify-center border-t border-outline-variant py-2 text-on-surface-variant hover:bg-surface-container-high hover:text-on-surface lg:flex"
          title={collapsed ? 'Expand sidebar' : 'Collapse sidebar'}
          aria-label={collapsed ? 'Expand sidebar' : 'Collapse sidebar'}
        >
          <span className="material-symbols-outlined" style={{ fontSize: 20 }}>
            {collapsed ? 'chevron_right' : 'chevron_left'}
          </span>
        </button>
      </aside>

      <div className="flex min-w-0 flex-1 flex-col overflow-hidden">
        <header className="z-20 flex shrink-0 items-center gap-4 border-b border-outline-variant bg-surface-container-low px-4 py-3">
          <button
            onClick={() => setMobileOpen(true)}
            className="text-on-surface-variant hover:text-on-surface lg:hidden"
            aria-label="Open navigation"
          >
            <span className="material-symbols-outlined">menu</span>
          </button>

          <div className="min-w-0 flex-1">
            <h1 className="truncate text-headline-sm font-semibold text-on-surface">
              {NAV_ITEMS.find((item) => item.id === activeTab)?.label || 'PostGen'}
            </h1>
          </div>

          <WorkerBadge status={workerStatus} />
        </header>

        {/* min-h-0 is what actually lets this shrink and scroll. A flex child
            defaults to min-height:auto, which refuses to shrink below its
            content, pushing the overflow back onto the page. */}
        <main className="min-h-0 flex-1 overflow-y-auto">
          <div className="mx-auto w-full max-w-container p-4 md:p-6">
            {children}
          </div>
        </main>
      </div>
    </div>
  );
}

/**
 * WorkerBadge surfaces publisher state in the top bar, so an operator on any
 * screen can see whether the background worker is running, mid-publish, or
 * cooling down.
 */
function WorkerBadge({ status }) {
  if (!status) return null;

  const phase = status.phase || 'idle';
  const running = status.running;

  const tone = !running
    ? 'border-outline-variant text-on-surface-variant'
    : phase === 'cooldown'
      ? 'border-tertiary/40 text-tertiary'
      : phase === 'idle'
        ? 'border-outline-variant text-on-surface-variant'
        : 'border-primary/40 text-primary';

  return (
    <div
      className={`hidden items-center gap-2 rounded-full border px-3 py-1 text-label-md sm:flex ${tone}`}
      title={status.current_account ? `Working on ${status.current_account}` : 'Background publisher'}
    >
      <span className={`h-1.5 w-1.5 rounded-full bg-current ${running && phase !== 'idle' ? 'animate-pulse' : ''}`} />
      Worker {running ? phase : 'stopped'}
    </div>
  );
}
