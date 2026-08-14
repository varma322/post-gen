import { useCallback, useEffect, useState } from 'react';
import './index.css';
import AppShell from './layout/AppShell';

import Dashboard from './screens/Dashboard';
import ActivityLog from './screens/ActivityLog';
import Analytics from './screens/Analytics';
import Channels from './screens/Channels';
import ContentQueue from './screens/ContentQueue';
import Publisher from './screens/Publisher';
import AutoPublisher from './screens/AutoPublisher';
import TemplatesManager from './screens/TemplatesManager';
import AccountsManager from './screens/AccountsManager';
import Settings from './screens/Settings';

const WORKER_POLL_MS = 15000;

/**
 * Screens are addressed by URL hash, so a reload keeps you where you were and
 * a screen can be linked to directly. Falls back to the dashboard for an
 * unknown or absent hash.
 */
const VALID_TABS = new Set([
  'dashboard', 'publisher', 'channels', 'analytics', 'queue',
  'scheduler', 'activity', 'templates', 'accounts', 'settings',
]);

function tabFromHash() {
  const hash = window.location.hash.replace(/^#\/?/, '');
  return VALID_TABS.has(hash) ? hash : 'dashboard';
}

export default function App() {
  const [activeTab, setActiveTab] = useState(tabFromHash);
  const [apiToken, setApiToken] = useState(() => localStorage.getItem('postgen_api_token') || '');
  const [accounts, setAccounts] = useState([]);
  const [templates, setTemplates] = useState([]);
  const [tokenStatus, setTokenStatus] = useState({ text: '', isError: false });
  const [workerStatus, setWorkerStatus] = useState(null);

  const apiFetch = useCallback(async (url, options = {}) => {
    const token = apiToken.trim();
    if (token) {
      options.headers = { ...(options.headers || {}), Authorization: `Bearer ${token}` };
    }
    const resp = await fetch(url, options);
    if (resp.status === 401) {
      throw new Error('Unauthorized: check the API bearer token in the sidebar');
    }
    return resp;
  }, [apiToken]);

  const loadAccounts = useCallback(async () => {
    try {
      const resp = await apiFetch('/accounts');
      if (!resp.ok) throw new Error('Failed to fetch accounts');
      setAccounts((await resp.json()).accounts || []);
    } catch (err) {
      setTokenStatus({ text: err.message, isError: true });
    }
  }, [apiFetch]);

  const loadTemplates = useCallback(async () => {
    try {
      const resp = await apiFetch('/templates');
      if (!resp.ok) throw new Error('Failed to fetch templates');
      setTemplates((await resp.json()).templates || []);
    } catch (err) {
      setTokenStatus({ text: err.message, isError: true });
    }
  }, [apiFetch]);

  const loadWorkerStatus = useCallback(async () => {
    try {
      const resp = await apiFetch('/worker/status');
      if (!resp.ok) return;
      setWorkerStatus(await resp.json());
    } catch {
      // The worker badge is ambient; a failed poll should not raise an error
      // banner over whatever screen the operator is actually using.
    }
  }, [apiFetch]);

  useEffect(() => {
    loadAccounts();
    loadTemplates();
    loadWorkerStatus();
  }, [loadAccounts, loadTemplates, loadWorkerStatus]);

  // Worker state drives the badge in the top bar on every screen, so it polls
  // globally rather than per-screen. Paused while the tab is hidden.
  useEffect(() => {
    const timer = setInterval(() => {
      if (!document.hidden) loadWorkerStatus();
    }, WORKER_POLL_MS);
    return () => clearInterval(timer);
  }, [loadWorkerStatus]);

  // Keep the hash and the active screen in step, in both directions, so the
  // back button and a pasted link both work.
  const selectTab = useCallback((id) => {
    setActiveTab(id);
    if (tabFromHash() !== id) window.location.hash = `#/${id}`;
  }, []);

  useEffect(() => {
    const onHashChange = () => setActiveTab(tabFromHash());
    window.addEventListener('hashchange', onHashChange);
    return () => window.removeEventListener('hashchange', onHashChange);
  }, []);

  const saveToken = () => {
    localStorage.setItem('postgen_api_token', apiToken);
    setTokenStatus({ text: 'Token saved', isError: false });
    setTimeout(() => {
      setTokenStatus({ text: '', isError: false });
      loadAccounts();
      loadTemplates();
      loadWorkerStatus();
    }, 1000);
  };

  return (
    <AppShell
      activeTab={activeTab}
      onSelectTab={selectTab}
      apiToken={apiToken}
      onApiTokenChange={setApiToken}
      onSaveToken={saveToken}
      tokenStatus={tokenStatus}
      workerStatus={workerStatus}
    >
      {/* Screens stay mounted so switching tabs never discards in-progress
          work - a typed URL list or a live results feed survives navigation.
          Visibility is toggled with CSS rather than conditional rendering. */}
      <Pane active={activeTab === 'dashboard'}>
        <Dashboard
          apiFetch={apiFetch}
          workerStatus={workerStatus}
          onRefreshWorker={loadWorkerStatus}
          onNavigate={selectTab}
        />
      </Pane>

      <Pane active={activeTab === 'publisher'}>
        <Publisher apiFetch={apiFetch} accounts={accounts} onNavigate={selectTab} />
      </Pane>

      <Pane active={activeTab === 'channels'}>
        <Channels apiFetch={apiFetch} onNavigate={selectTab} />
      </Pane>

      <Pane active={activeTab === 'analytics'}>
        <Analytics apiFetch={apiFetch} />
      </Pane>

      <Pane active={activeTab === 'queue'}>
        <ContentQueue apiFetch={apiFetch} accounts={accounts} />
      </Pane>

      <Pane active={activeTab === 'scheduler'}>
        <AutoPublisher apiFetch={apiFetch} accounts={accounts} active={activeTab === 'scheduler'} />
      </Pane>

      <Pane active={activeTab === 'activity'}>
        <ActivityLog apiFetch={apiFetch} accounts={accounts} />
      </Pane>

      <Pane active={activeTab === 'templates'}>
        <TemplatesManager apiFetch={apiFetch} templates={templates} onTemplatesChanged={loadTemplates} />
      </Pane>

      <Pane active={activeTab === 'accounts'}>
        <AccountsManager apiFetch={apiFetch} accounts={accounts} templates={templates} onAccountsChanged={loadAccounts} />
      </Pane>

      <Pane active={activeTab === 'settings'}>
        <Settings apiFetch={apiFetch} accounts={accounts} />
      </Pane>
    </AppShell>
  );
}

function Pane({ active, children }) {
  return <div className={active ? 'fadein' : 'hidden'}>{children}</div>;
}
