import { useState, useEffect } from 'react';
import './index.css';
import Header from './components/Header';
import Sidebar from './components/Sidebar';
import Generator from './screens/Generator';
import AutoPublisher from './screens/AutoPublisher';
import PagesDashboard from './screens/PagesDashboard';
import TemplatesManager from './screens/TemplatesManager';
import AccountsManager from './screens/AccountsManager';

export default function App() {
  const [activeTab, setActiveTab] = useState('dashboard');
  const [mobileMenuOpen, setMobileMenuOpen] = useState(false);
  const [apiToken, setApiToken] = useState(() => localStorage.getItem("postgen_api_token") || "");
  const [accounts, setAccounts] = useState([]);
  const [templates, setTemplates] = useState([]);
  const [tokenStatus, setTokenStatus] = useState({ text: '', isError: false });

  // Helper for API fetches with Bearer Token auth
  const apiFetch = async (url, options = {}) => {
    const token = apiToken.trim();
    if (token) {
      options.headers = options.headers || {};
      options.headers["Authorization"] = "Bearer " + token;
    }
    const resp = await fetch(url, options);
    if (resp.status === 401) {
      throw new Error("Unauthorized: Please verify your API Bearer Token");
    }
    return resp;
  };

  const loadAccounts = async () => {
    try {
      const resp = await apiFetch("/accounts");
      if (!resp.ok) throw new Error("Failed to fetch accounts");
      const data = await resp.json();
      setAccounts(data.accounts || []);
    } catch (err) {
      console.error(err);
      setTokenStatus({ text: "Error loading accounts: " + err.message, isError: true });
    }
  };

  const loadTemplates = async () => {
    try {
      const resp = await apiFetch("/templates");
      if (!resp.ok) throw new Error("Failed to fetch templates");
      const data = await resp.json();
      setTemplates(data.templates || []);
    } catch (err) {
      console.error(err);
      setTokenStatus({ text: "Error loading templates: " + err.message, isError: true });
    }
  };

  // Load accounts and templates on mount & token change
  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect -- fetch-on-mount/token-change is intentional
    loadAccounts();
    loadTemplates();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [apiToken]);

  const saveToken = () => {
    localStorage.setItem("postgen_api_token", apiToken);
    setTokenStatus({ text: "Token saved to local storage!", isError: false });
    setTimeout(() => {
      setTokenStatus({ text: '', isError: false });
      loadAccounts();
      loadTemplates();
    }, 1200);
  };

  return (
    <div className="bg-surface text-on-surface antialiased min-h-screen flex flex-col font-body">
      <Header
        mobileMenuOpen={mobileMenuOpen}
        onToggleMobileMenu={() => setMobileMenuOpen(v => !v)}
        accounts={accounts}
      />

      <div className="flex-1 flex relative">
        <Sidebar
          activeTab={activeTab}
          onSelectTab={(id) => { setActiveTab(id); setMobileMenuOpen(false); }}
          mobileMenuOpen={mobileMenuOpen}
          apiToken={apiToken}
          onApiTokenChange={setApiToken}
          onSaveToken={saveToken}
          tokenStatus={tokenStatus}
        />

        <div className="flex-1 flex flex-col min-w-0">
          {/* All screens stay mounted (matching the original single-component
              app's behavior) so switching tabs never discards in-progress
              work like typed URLs or a live results feed. Visibility is
              toggled with CSS instead of conditional rendering. */}
          <main className="flex-1 p-4 md:p-8 overflow-y-auto">
            <div className={activeTab === 'dashboard' ? 'fadein' : 'hidden'}>
              <Generator apiFetch={apiFetch} accounts={accounts} onNavigateToAccounts={() => setActiveTab('accounts')} />
            </div>
            <div className={activeTab === 'autopublish' ? 'fadein' : 'hidden'}>
              <AutoPublisher apiFetch={apiFetch} accounts={accounts} active={activeTab === 'autopublish'} />
            </div>
            <div className={activeTab === 'stats' ? 'fadein' : 'hidden'}>
              <PagesDashboard apiFetch={apiFetch} />
            </div>
            <div className={activeTab === 'templates' ? 'fadein' : 'hidden'}>
              <TemplatesManager apiFetch={apiFetch} templates={templates} onTemplatesChanged={loadTemplates} />
            </div>
            <div className={activeTab === 'accounts' ? 'fadein' : 'hidden'}>
              <AccountsManager apiFetch={apiFetch} accounts={accounts} templates={templates} onAccountsChanged={loadAccounts} />
            </div>
          </main>
        </div>
      </div>
    </div>
  );
}
