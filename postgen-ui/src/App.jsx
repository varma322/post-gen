import { useState, useEffect, useRef } from 'react';
import './index.css';

export default function App() {
  const [activeTab, setActiveTab] = useState('dashboard');
  const [apiToken, setApiToken] = useState(() => localStorage.getItem("postgen_api_token") || "");
  const [accounts, setAccounts] = useState([]);
  const [selectedAccounts, setSelectedAccounts] = useState([]);
  const [templates, setTemplates] = useState([]);
  
  // Template Manager state
  const [selectedTemplate, setSelectedTemplate] = useState('');
  const [templateContent, setTemplateContent] = useState('');
  const [templateSaving, setTemplateSaving] = useState(false);
  const [templateStatus, setTemplateStatus] = useState({ text: '', isError: false });

  // Accounts Manager state
  const [showAccountForm, setShowAccountForm] = useState(false);
  const [editingAccount, setEditingAccount] = useState(null); // null if adding new
  const [accFormName, setAccFormName] = useState('');
  const [accFormTemplate, setAccFormTemplate] = useState('');
  const [accFormAffiliateTag, setAccFormAffiliateTag] = useState('');
  const [accFormFbPageId, setAccFormFbPageId] = useState('');
  const [accFormFbToken, setAccFormFbToken] = useState('');
  const [accountStatus, setAccountStatus] = useState({ text: '', isError: false });

  // Generation state
  const [urls, setUrls] = useState('');
  const [publishToFb, setPublishToFb] = useState(false);
  const [publishDelay, setPublishDelay] = useState(5);
  const [isGenerating, setIsGenerating] = useState(false);
  const [statusMessage, setStatusMessage] = useState('');
  const [cooldownSeconds, setCooldownSeconds] = useState(0);
  const [streamedResults, setStreamedResults] = useState([]);

  // Sidebar mobile state
  const [mobileMenuOpen, setMobileMenuOpen] = useState(false);

  const cooldownIntervalRef = useRef(null);

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

  // Load accounts and templates on mount & token change
  useEffect(() => {
    loadAccounts();
    loadTemplates();
  }, [apiToken]);

  // Handle FB Cooldown timer decrement
  useEffect(() => {
    if (cooldownSeconds > 0) {
      cooldownIntervalRef.current = setInterval(() => {
        setCooldownSeconds(prev => {
          if (prev <= 1) {
            clearInterval(cooldownIntervalRef.current);
            setStatusMessage("Resuming...");
            return 0;
          }
          return prev - 1;
        });
      }, 1000);
    }
    return () => clearInterval(cooldownIntervalRef.current);
  }, [cooldownSeconds]);

  const loadAccounts = async () => {
    try {
      const resp = await apiFetch("/accounts");
      if (!resp.ok) throw new Error("Failed to fetch accounts");
      const data = await resp.json();
      const list = data.accounts || [];
      setAccounts(list);
      
      // Auto select all accounts by default if none are selected yet
      if (selectedAccounts.length === 0) {
        setSelectedAccounts(list.map(a => a.name));
      }
    } catch (err) {
      console.error(err);
      setStatusMessage("Error loading accounts: " + err.message);
    }
  };

  const loadTemplates = async () => {
    try {
      const resp = await apiFetch("/templates");
      if (!resp.ok) throw new Error("Failed to fetch templates");
      const data = await resp.json();
      const list = data.templates || [];
      setTemplates(list);

      // Select first template by default if not set
      if (list.length > 0 && !selectedTemplate) {
        setSelectedTemplate(list[0].name);
      }
    } catch (err) {
      console.error(err);
      setTemplateStatus({ text: "Error loading templates: " + err.message, isError: true });
    }
  };

  // Fetch individual template content when selected template changes
  useEffect(() => {
    if (!selectedTemplate) return;
    const fetchTemplateContent = async () => {
      setTemplateStatus({ text: `Loading ${selectedTemplate}...`, isError: false });
      try {
        const resp = await apiFetch(`/templates/${encodeURIComponent(selectedTemplate)}`);
        if (!resp.ok) {
          const data = await resp.json();
          throw new Error(data.error || "Failed to load template content");
        }
        const data = await resp.json();
        setTemplateContent(data.content || "");
        setTemplateStatus({ text: `Loaded ${selectedTemplate} successfully.`, isError: false });
      } catch (err) {
        setTemplateStatus({ text: err.message, isError: true });
      }
    };
    fetchTemplateContent();
  }, [selectedTemplate]);

  const saveToken = () => {
    localStorage.setItem("postgen_api_token", apiToken);
    setStatusMessage("Token saved to local storage!");
    setTimeout(() => {
      setStatusMessage("");
      loadAccounts();
      loadTemplates();
    }, 1200);
  };

  const handleSaveTemplate = async () => {
    if (!selectedTemplate) return;
    setTemplateSaving(true);
    setTemplateStatus({ text: `Saving ${selectedTemplate}...`, isError: false });
    try {
      const resp = await apiFetch(`/templates/${encodeURIComponent(selectedTemplate)}`, {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ content: templateContent })
      });
      if (!resp.ok) {
        const data = await resp.json();
        throw new Error(data.error || "Failed to save template");
      }
      setTemplateStatus({ text: `Saved ${selectedTemplate} successfully!`, isError: false });
      loadTemplates();
    } catch (err) {
      setTemplateStatus({ text: "Error: " + err.message, isError: true });
    } finally {
      setTemplateSaving(false);
    }
  };

  // CRUD for Accounts
  const handleOpenAccountForm = (acc = null) => {
    setEditingAccount(acc);
    if (acc) {
      setAccFormName(acc.name);
      setAccFormTemplate(acc.template_path);
      setAccFormAffiliateTag(acc.affiliate_tag || "");
      setAccFormFbPageId(acc.facebook_page_id || "");
      setAccFormFbToken(acc.facebook_access_token || "");
    } else {
      setAccFormName("");
      setAccFormTemplate(templates.length > 0 ? templates[0].path : "");
      setAccFormAffiliateTag("");
      setAccFormFbPageId("");
      setAccFormFbToken("");
    }
    setAccountStatus({ text: '', isError: false });
    setShowAccountForm(true);
  };

  const handleSaveAccount = async () => {
    if (!accFormName.trim()) {
      setAccountStatus({ text: "Account name is required", isError: true });
      return;
    }

    setAccountStatus({ text: "Saving account...", isError: false });
    const payload = {
      name: accFormName.trim(),
      template_path: accFormTemplate,
      affiliate_tag: accFormAffiliateTag.trim(),
      facebook_page_id: accFormFbPageId.trim(),
      facebook_access_token: accFormFbToken.trim()
    };

    try {
      const method = editingAccount ? "PUT" : "POST";
      const url = editingAccount ? `/accounts/${encodeURIComponent(editingAccount.name)}` : "/accounts";
      const resp = await apiFetch(url, {
        method,
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(payload)
      });

      if (!resp.ok) {
        const data = await resp.json();
        throw new Error(data.error || "Failed to save account");
      }

      setAccountStatus({ text: "Account saved successfully!", isError: false });
      setTimeout(() => {
        setShowAccountForm(false);
        loadAccounts();
      }, 1000);
    } catch (err) {
      setAccountStatus({ text: "Error: " + err.message, isError: true });
    }
  };

  const handleDeleteAccount = async (name) => {
    if (!confirm(`Are you sure you want to delete account "${name}"?`)) return;
    try {
      const resp = await apiFetch(`/accounts/${encodeURIComponent(name)}`, {
        method: "DELETE"
      });
      if (!resp.ok) {
        const data = await resp.json();
        throw new Error(data.error || "Failed to delete account");
      }
      loadAccounts();
    } catch (err) {
      alert("Error: " + err.message);
    }
  };

  // Generation process
  const parseURLs = () => {
    return urls
      .split(/\r?\n/)
      .map(v => v.trim())
      .filter(Boolean);
  };

  const handleClear = () => {
    setUrls('');
    setStreamedResults([]);
    setStatusMessage('Cleared.');
  };

  const handleSelectAllAccounts = () => {
    setSelectedAccounts(accounts.map(a => a.name));
  };

  const handleToggleAccountSelection = (name) => {
    setSelectedAccounts(prev => 
      prev.includes(name) ? prev.filter(x => x !== name) : [...prev, name]
    );
  };

  const parseSSEEvent = (block) => {
    const lines = block.split("\n");
    let eventType = "message";
    let data = "";

    lines.forEach((line) => {
      if (line.startsWith("event:")) {
        eventType = line.slice(6).trim();
      }
      if (line.startsWith("data:")) {
        data += line.slice(5).trim();
      }
    });

    if (!data) return null;

    try {
      return { type: eventType, data: JSON.parse(data) };
    } catch (_) {
      return null;
    }
  };

  const handleGenerate = async () => {
    const parsedUrls = parseURLs();
    if (parsedUrls.length === 0) {
      setStatusMessage("Please enter at least one URL.");
      return;
    }

    if (selectedAccounts.length === 0) {
      setStatusMessage("Please select at least one account.");
      return;
    }

    setIsGenerating(true);
    setStreamedResults([]);
    setCooldownSeconds(0);
    setStatusMessage(`Processing ${parsedUrls.length} URL(s)... Scraping in progress.`);

    const currentResults = [];

    try {
      const resp = await apiFetch("/generate/stream", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          urls: parsedUrls,
          accounts: selectedAccounts,
          publish: publishToFb,
          publish_delay_minutes: Number(publishDelay)
        })
      });

      if (!resp.ok) {
        const data = await resp.json();
        throw new Error(data.error || "Generation failed");
      }

      if (!resp.body || !resp.body.getReader) {
        throw new Error("Streaming is not supported by your browser");
      }

      const reader = resp.body.getReader();
      const decoder = new TextDecoder();
      let buffer = "";

      while (true) {
        const read = await reader.read();
        if (read.done) break;

        buffer += decoder.decode(read.value, { stream: true });

        let splitIndex = buffer.indexOf("\n\n");
        while (splitIndex !== -1) {
          const rawEvent = buffer.slice(0, splitIndex);
          buffer = buffer.slice(splitIndex + 2);

          const event = parseSSEEvent(rawEvent);
          if (event) {
            if (event.type === "progress") {
              setStatusMessage(`Processing ${event.data.current}/${event.data.total}: ${event.data.url}`);
            }

            if (event.type === "result") {
              currentResults.push(event.data.result);
              setStreamedResults([...currentResults]);
            }

            if (event.type === "error") {
              setStatusMessage(`Error: ${event.data.error}`);
            }

            if (event.type === "cooldown") {
              setCooldownSeconds(event.data.duration_seconds);
            }

            if (event.type === "done") {
              setStatusMessage(`Completed! Success: ${event.data.success}, Failed: ${event.data.failed}, Total: ${event.data.totalResults}.`);
            }
          }

          splitIndex = buffer.indexOf("\n\n");
        }
      }
    } catch (err) {
      setStatusMessage("Error during generation: " + err.message);
    } finally {
      setIsGenerating(false);
    }
  };

  const copyToClipboard = async (text, id) => {
    try {
      await navigator.clipboard.writeText(text);
      setStreamedResults(prev => prev.map(r => r.id === id || r.output === text ? { ...r, copied: true } : r));
      setTimeout(() => {
        setStreamedResults(prev => prev.map(r => r.id === id || r.output === text ? { ...r, copied: false } : r));
      }, 1500);
    } catch (_) {
      alert("Failed to copy content.");
    }
  };

  return (
    <div className="bg-surface-dim text-on-surface antialiased min-h-screen flex flex-col font-body">
      
      {/* TopAppBar */}
      <header className="bg-surface-dim border-b border-outline-variant shadow-sm top-0 sticky z-50 flex items-center justify-between px-6 py-4 w-full">
        <div className="flex items-center gap-4 cursor-pointer hover:opacity-90 transition-all">
          <span className="material-symbols-outlined text-primary text-2xl animate-pulse">auto_awesome</span>
          <span className="text-2xl font-black tracking-tight text-primary font-headline">PostGen</span>
          <span className="hidden sm:inline bg-secondary-container text-on-secondary-container px-2 py-0.5 text-xs font-bold rounded-full border border-primary/20">
            v2.0 Slate Mode
          </span>
        </div>
        
        {/* Dynamic connection indicator */}
        <div className="flex items-center gap-4">
          <div className="hidden md:flex items-center gap-2 bg-surface-container border border-outline-variant px-3 py-1.5 rounded-full text-xs text-on-surface-variant font-medium">
            <span className={`w-2 h-2 rounded-full ${accounts.length > 0 ? 'bg-primary' : 'bg-orange-500 animate-ping'}`} />
            {accounts.length > 0 ? `${accounts.length} Accounts Connected` : 'Configuring'}
          </div>
          
          <button 
            className="md:hidden text-on-surface p-2 hover:bg-surface-container rounded-full"
            onClick={() => setMobileMenuOpen(!mobileMenuOpen)}
          >
            <span className="material-symbols-outlined">{mobileMenuOpen ? 'close' : 'menu'}</span>
          </button>
        </div>
      </header>

      <div className="flex-1 flex relative">
        
        {/* Navigation Drawer (SideNav) */}
        <nav className={`
          ${mobileMenuOpen ? 'flex' : 'hidden'} 
          md:flex flex-col bg-surface-container-low border-r border-outline-variant fixed md:static inset-y-0 left-0 w-64 pt-6 md:pt-8 z-40 duration-300 ease-in-out h-full
        `}>
          <div className="flex flex-col gap-2 px-2">
            <button 
              onClick={() => { setActiveTab('dashboard'); setMobileMenuOpen(false); }}
              className={`w-full text-left rounded-lg px-4 py-3 flex items-center gap-3 duration-200 ease-in-out font-label font-medium text-label-large ${activeTab === 'dashboard' ? 'bg-secondary-container text-on-secondary-container border border-primary/20 shadow-md' : 'text-on-surface-variant hover:bg-surface-container-highest hover:text-on-surface'}`}
            >
              <span className="material-symbols-outlined">dashboard</span>
              Dashboard
            </button>
            <button 
              onClick={() => { setActiveTab('templates'); setMobileMenuOpen(false); }}
              className={`w-full text-left rounded-lg px-4 py-3 flex items-center gap-3 duration-200 ease-in-out font-label font-medium text-label-large ${activeTab === 'templates' ? 'bg-secondary-container text-on-secondary-container border border-primary/20 shadow-md' : 'text-on-surface-variant hover:bg-surface-container-highest hover:text-on-surface'}`}
            >
              <span className="material-symbols-outlined">badge</span>
              Templates Manager
            </button>
            <button 
              onClick={() => { setActiveTab('accounts'); setMobileMenuOpen(false); }}
              className={`w-full text-left rounded-lg px-4 py-3 flex items-center gap-3 duration-200 ease-in-out font-label font-medium text-label-large ${activeTab === 'accounts' ? 'bg-secondary-container text-on-secondary-container border border-primary/20 shadow-md' : 'text-on-surface-variant hover:bg-surface-container-highest hover:text-on-surface'}`}
            >
              <span className="material-symbols-outlined">manage_accounts</span>
              Accounts Manager
            </button>
          </div>

          <div className="mt-auto p-4 border-t border-outline-variant bg-surface-container-lowest/50">
            <h4 className="text-xs font-bold text-primary tracking-wider uppercase mb-3">API Token Access</h4>
            <div className="space-y-2">
              <input 
                type="password" 
                value={apiToken} 
                onChange={(e) => setApiToken(e.target.value)}
                placeholder="sk-..." 
                className="w-full bg-surface-variant border border-outline-variant rounded-lg px-3 py-2 text-on-surface text-xs focus:ring-2 focus:ring-primary focus:border-transparent transition-all"
              />
              <button 
                onClick={saveToken}
                className="w-full bg-primary-container text-on-primary-container hover:bg-primary font-bold px-3 py-1.5 rounded-lg text-xs transition-colors flex items-center justify-center gap-2 shadow"
              >
                <span className="material-symbols-outlined text-[14px]">save</span>
                Save Token
              </button>
            </div>
          </div>
        </nav>

        {/* Main Content Area */}
        <div className="flex-1 flex flex-col min-w-0">
          <main className="flex-1 p-4 md:p-8 overflow-y-auto space-y-8">
            <div className="max-w-5xl mx-auto space-y-8">
              
              {/* TAB 1: DASHBOARD */}
              {activeTab === 'dashboard' && (
                <div className="space-y-8 animate-fadeIn">
                  
                  {/* Title & Global Info */}
                  <div className="flex flex-col md:flex-row md:items-center justify-between gap-4 border-b border-outline-variant pb-6">
                    <div>
                      <h1 className="text-3xl font-headline font-bold text-on-surface">Dashboard</h1>
                      <p className="text-on-surface-variant mt-1 font-body">Manage URL queue, monitor live progress, and review Facebook outputs.</p>
                    </div>
                    {statusMessage && (
                      <div className="flex items-center gap-3 bg-surface-container border border-outline-variant px-4 py-2 rounded-xl text-sm max-w-md shadow-sm">
                        <span className="material-symbols-outlined text-primary text-spin animate-spin">sync</span>
                        <span className="font-medium text-primary-fixed-dim line-clamp-1">{statusMessage}</span>
                      </div>
                    )}
                  </div>

                  {/* Cooldown notice if active */}
                  {cooldownSeconds > 0 && (
                    <div className="bg-primary/10 border border-primary/30 p-4 rounded-xl flex items-center justify-between shadow-lg text-primary animate-bounce">
                      <div className="flex items-center gap-3">
                        <span className="material-symbols-outlined text-xl animate-pulse">timer</span>
                        <div>
                          <strong className="font-semibold block">Facebook Cooldown Active</strong>
                          <span className="text-xs text-on-surface-variant">Cooling down to stay safely within Facebook API rate limits.</span>
                        </div>
                      </div>
                      <div className="text-2xl font-bold bg-secondary-container px-4 py-2 rounded-lg border border-primary/20">
                        {cooldownSeconds}s
                      </div>
                    </div>
                  )}

                  {/* Grid: URLs & Config */}
                  <div className="grid grid-cols-1 lg:grid-cols-3 gap-6">
                    
                    {/* URLs Panel */}
                    <div className="lg:col-span-2 bg-surface-container rounded-xl border border-outline-variant p-6 shadow-sm flex flex-col justify-between">
                      <div>
                        <div className="flex items-center gap-2 mb-4">
                          <span className="material-symbols-outlined text-primary">link</span>
                          <h2 className="text-xl font-headline font-semibold text-on-surface">Product URLs Queue</h2>
                        </div>
                        <textarea 
                          value={urls}
                          onChange={(e) => setUrls(e.target.value)}
                          className="w-full h-56 bg-surface-variant border border-outline-variant rounded-lg p-4 text-on-surface placeholder:text-on-surface-variant/40 focus:ring-2 focus:ring-primary focus:border-transparent transition-all resize-none font-mono text-sm leading-relaxed" 
                          placeholder="Paste Amazon, Flipkart, or other product URLs here... One per line."
                        />
                      </div>
                      <div className="mt-4 flex gap-3">
                        <button 
                          onClick={handleGenerate}
                          disabled={isGenerating}
                          className="bg-primary text-on-secondary hover:bg-primary-fixed-dim disabled:bg-surface-container-high disabled:text-on-surface-variant/50 px-6 py-3 rounded-lg font-label font-bold transition-colors shadow-lg flex items-center gap-2"
                        >
                          <span className="material-symbols-outlined text-lg">rocket_launch</span>
                          {isGenerating ? 'Processing Live...' : 'Generate Affiliate Posts'}
                        </button>
                        <button 
                          onClick={handleClear}
                          disabled={isGenerating}
                          className="bg-surface-container-high text-on-surface-variant hover:text-on-surface hover:bg-surface-container-highest px-4 py-3 rounded-lg border border-outline-variant font-label font-medium transition-colors"
                        >
                          Clear Queue
                        </button>
                      </div>
                    </div>
                    
                    {/* Config Panel */}
                    <div className="bg-surface-container rounded-xl border border-outline-variant p-6 shadow-sm flex flex-col gap-6">
                      <div>
                        <div className="flex items-center gap-2 mb-4">
                          <span className="material-symbols-outlined text-primary">settings</span>
                          <h2 className="text-xl font-headline font-semibold text-on-surface">Publish Config</h2>
                        </div>
                        
                        <div className="space-y-4">
                          <div className="pt-2">
                            <label className="flex items-center cursor-pointer gap-3 bg-surface-variant hover:bg-surface-container-highest p-3 rounded-lg border border-outline-variant/60 transition-colors">
                              <input 
                                type="checkbox"
                                checked={publishToFb}
                                onChange={(e) => setPublishToFb(e.target.checked)}
                                className="form-checkbox bg-surface border-outline text-primary rounded focus:ring-primary focus:ring-offset-surface"
                              />
                              <div>
                                <span className="text-sm font-semibold text-on-surface block">Post directly to Facebook</span>
                                <span className="text-xs text-on-surface-variant">Automatically publish generated post to account's page</span>
                              </div>
                            </label>
                          </div>

                          {publishToFb && (
                            <div className="animate-slideDown">
                              <label className="block text-sm font-label text-on-surface-variant mb-2">
                                Post Inter-delay (minutes)
                              </label>
                              <div className="flex items-center gap-3">
                                <input 
                                  type="number" 
                                  value={publishDelay}
                                  onChange={(e) => setPublishDelay(e.target.value)}
                                  min="0"
                                  className="w-full bg-surface-variant border border-outline-variant rounded-lg px-3 py-2.5 text-primary focus:ring-2 focus:ring-primary focus:border-transparent transition-all text-sm font-bold"
                                />
                                <span className="text-xs text-on-surface-variant font-semibold">Min</span>
                              </div>
                              <span className="text-[11px] text-on-surface-variant block mt-2">
                                Sets delay between subsequent posts to avoid API spam limits.
                              </span>
                            </div>
                          )}
                        </div>
                      </div>
                    </div>
                  </div>
                  
                  {/* Account Selection Bento */}
                  <div className="bg-surface-container rounded-xl border border-outline-variant p-6 shadow-sm">
                    <div className="flex items-center justify-between mb-4">
                      <div className="flex items-center gap-2">
                        <span className="material-symbols-outlined text-primary">group</span>
                        <h2 className="text-xl font-headline font-semibold text-on-surface">Target Accounts ({selectedAccounts.length} selected)</h2>
                      </div>
                      <button 
                        onClick={handleSelectAllAccounts}
                        className="text-sm text-primary hover:text-primary-fixed-dim font-label font-bold transition-colors"
                      >
                        Select All
                      </button>
                    </div>

                    {accounts.length === 0 ? (
                      <div className="text-center py-6 border border-dashed border-outline-variant rounded-lg bg-surface-variant/40">
                        <p className="text-on-surface-variant text-sm mb-2">No accounts available.</p>
                        <button 
                          onClick={() => setActiveTab('accounts')}
                          className="text-xs text-primary hover:underline font-bold"
                        >
                          Add one in Accounts Manager
                        </button>
                      </div>
                    ) : (
                      <div className="grid grid-cols-1 sm:grid-cols-3 gap-4">
                        {accounts.map((account) => {
                          const isSelected = selectedAccounts.includes(account.name);
                          return (
                            <label 
                              key={account.name}
                              className={`flex items-center gap-3 p-4 border rounded-lg cursor-pointer transition-colors ${isSelected ? 'border-primary bg-primary/5' : 'border-outline-variant bg-surface-variant hover:border-outline'}`}
                            >
                              <input 
                                type="checkbox"
                                checked={isSelected}
                                onChange={() => handleToggleAccountSelection(account.name)}
                                className="form-checkbox bg-surface border-outline text-primary rounded focus:ring-primary"
                              />
                              <div className="flex flex-col min-w-0">
                                <span className="font-body text-on-surface font-semibold truncate text-sm">{account.name}</span>
                                <span className="text-[11px] text-on-surface-variant truncate">{account.affiliate_tag || 'No tag'}</span>
                              </div>
                            </label>
                          );
                        })}
                      </div>
                    )}
                  </div>
                  
                  {/* Results Feed */}
                  <div className="space-y-4">
                    <h2 className="text-xl font-headline font-semibold text-on-surface flex items-center gap-2">
                      <span className="material-symbols-outlined text-primary">list_alt</span>
                      Live Results Feed ({streamedResults.length} posts generated)
                    </h2>

                    {streamedResults.length === 0 ? (
                      <div className="text-center py-12 border border-dashed border-outline-variant bg-surface-container rounded-xl">
                        <span className="material-symbols-outlined text-on-surface-variant/40 text-4xl mb-2">post_add</span>
                        <p className="text-on-surface-variant text-sm">Generated posts will appear here in real-time as they process.</p>
                      </div>
                    ) : (
                      <div className="space-y-6">
                        
                        {/* Summary Bar */}
                        <div className="bg-surface-container border border-outline-variant rounded-xl p-4 flex flex-wrap gap-4 items-center justify-between shadow-sm">
                          <div className="flex gap-4">
                            <span className="bg-primary-container text-on-primary-container px-3 py-1 rounded-full text-xs font-bold flex items-center gap-1.5">
                              <span className="material-symbols-outlined text-sm">check_circle</span>
                              Success: {streamedResults.filter(r => !r.error).length}
                            </span>
                            <span className="bg-error-container text-error px-3 py-1 rounded-full text-xs font-bold flex items-center gap-1.5">
                              <span className="material-symbols-outlined text-sm">error</span>
                              Failed: {streamedResults.filter(r => r.error).length}
                            </span>
                          </div>
                          <span className="text-xs text-on-surface-variant font-semibold">
                            Total Processed: {streamedResults.length}
                          </span>
                        </div>

                        {/* Streamed Items */}
                        {streamedResults.map((result, idx) => {
                          const isError = !!result.error;
                          return (
                            <div 
                              key={idx}
                              className={`bg-surface-container rounded-xl border p-6 shadow-sm transition-colors ${isError ? 'border-red-950/60 bg-red-950/5' : 'border-outline-variant hover:border-outline'}`}
                            >
                              <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4 border-b border-outline-variant pb-4 mb-4">
                                <div className="flex flex-wrap items-center gap-3">
                                  <span className={`px-2.5 py-1 rounded-full text-xs font-bold uppercase tracking-wider flex items-center gap-1.5 ${isError ? 'bg-error-container/30 text-error border border-error/20' : 'bg-primary-container/20 text-primary border border-primary/20'}`}>
                                    <span className="material-symbols-outlined text-[14px]">{isError ? 'error' : 'check_circle'}</span>
                                    {isError ? 'Error' : 'Success'}
                                  </span>
                                  <span className="font-semibold text-on-surface text-sm">Account: {result.account}</span>
                                </div>
                                
                                {!isError && (
                                  <button 
                                    onClick={() => copyToClipboard(result.output, idx)}
                                    className="text-xs bg-surface-variant hover:bg-surface-container-high border border-outline-variant text-on-surface px-4 py-2 rounded-lg font-bold transition-all flex items-center gap-2 w-fit active:scale-95"
                                  >
                                    <span className="material-symbols-outlined text-sm">
                                      {result.copied ? 'done' : 'content_copy'}
                                    </span>
                                    {result.copied ? 'Copied!' : 'Copy Post Content'}
                                  </button>
                                )}
                              </div>

                              {/* URL & Meta */}
                              <div className="mb-4">
                                <span className="text-xs text-on-surface-variant font-semibold block mb-1">Source URL</span>
                                <a 
                                  href={result.url} 
                                  target="_blank" 
                                  rel="noopener noreferrer" 
                                  className="text-sm text-primary hover:underline font-mono break-all line-clamp-1 inline-flex items-center gap-1"
                                >
                                  {result.url}
                                  <span className="material-symbols-outlined text-xs">open_in_new</span>
                                </a>
                              </div>

                              {/* FB Post status badge */}
                              {(result.publish_id || result.publish_error) && (
                                <div className="mb-4 p-3 rounded-lg border bg-surface-variant/40 border-outline-variant flex items-center gap-2">
                                  <span className="material-symbols-outlined text-base">facebook</span>
                                  {result.publish_id ? (
                                    <span className="text-xs text-primary font-bold">
                                      Posted successfully to Facebook (Post ID: {result.publish_id})
                                    </span>
                                  ) : (
                                    <span className="text-xs text-error font-bold">
                                      Facebook Posting Failed: {result.publish_error}
                                    </span>
                                  )}
                                </div>
                              )}

                              {/* Content output */}
                              <div>
                                <span className="text-xs text-on-surface-variant font-semibold block mb-2">Generated Output</span>
                                {isError ? (
                                  <p className="text-error text-sm font-mono p-3 bg-error-container/10 border border-error-container/20 rounded-lg">
                                    {result.error}
                                  </p>
                                ) : (
                                  <pre className="whitespace-pre-wrap font-mono text-sm bg-surface-variant p-4 rounded-lg border border-outline-variant max-h-72 overflow-y-auto leading-relaxed text-on-surface select-all">
                                    {result.output}
                                  </pre>
                                )}
                              </div>
                            </div>
                          );
                        })}
                      </div>
                    )}
                  </div>
                </div>
              )}

              {/* TAB 2: TEMPLATES MANAGER */}
              {activeTab === 'templates' && (
                <div className="space-y-8 animate-fadeIn">
                  <div className="flex flex-col md:flex-row md:items-center justify-between gap-4 border-b border-outline-variant pb-6">
                    <div>
                      <h1 className="text-3xl font-headline font-bold text-on-surface">Templates Manager</h1>
                      <p className="text-on-surface-variant mt-1 font-body">Customize generator layouts using Go HTML template tags.</p>
                    </div>
                  </div>

                  <div className="grid grid-cols-1 lg:grid-cols-4 gap-6">
                    
                    {/* Template list */}
                    <div className="bg-surface-container rounded-xl border border-outline-variant p-5 shadow-sm lg:col-span-1 space-y-4">
                      <h3 className="font-semibold text-on-surface text-base mb-2">Available Templates</h3>
                      <div className="space-y-2">
                        {templates.map(tmpl => (
                          <button 
                            key={tmpl.name}
                            onClick={() => setSelectedTemplate(tmpl.name)}
                            className={`w-full text-left p-3 rounded-lg text-sm transition-all border flex flex-col gap-1 ${selectedTemplate === tmpl.name ? 'bg-secondary-container border-primary/30 text-on-secondary-container font-semibold shadow-md' : 'bg-surface-variant border-outline-variant hover:border-outline text-on-surface-variant hover:text-on-surface'}`}
                          >
                            <span>{tmpl.name}</span>
                            {tmpl.accounts && tmpl.accounts.length > 0 && (
                              <span className="text-[10px] opacity-80 block truncate">
                                Used by: {tmpl.accounts.join(", ")}
                              </span>
                            )}
                          </button>
                        ))}
                      </div>
                    </div>

                    {/* Template Editor */}
                    <div className="bg-surface-container rounded-xl border border-outline-variant p-6 shadow-sm lg:col-span-3 flex flex-col gap-4">
                      <div className="flex items-center justify-between border-b border-outline-variant pb-4">
                        <div>
                          <h2 className="text-lg font-bold text-on-surface">{selectedTemplate || 'No Template Selected'}</h2>
                          <span className="text-xs text-on-surface-variant">Path: templates/{selectedTemplate}</span>
                        </div>
                        <button 
                          onClick={handleSaveTemplate}
                          disabled={templateSaving || !selectedTemplate}
                          className="bg-primary text-on-secondary hover:bg-primary-fixed-dim disabled:bg-surface-container-high px-5 py-2 rounded-lg font-bold text-sm transition-colors flex items-center gap-2 shadow"
                        >
                          <span className="material-symbols-outlined text-base">save</span>
                          {templateSaving ? 'Saving...' : 'Save Template'}
                        </button>
                      </div>

                      {templateStatus.text && (
                        <div className={`p-3 rounded-lg border text-xs font-semibold ${templateStatus.isError ? 'bg-error-container/10 border-error/20 text-error' : 'bg-primary/5 border-primary/20 text-primary'}`}>
                          {templateStatus.text}
                        </div>
                      )}

                      <div className="relative">
                        <textarea 
                          value={templateContent}
                          onChange={(e) => setTemplateContent(e.target.value)}
                          disabled={!selectedTemplate}
                          className="w-full h-[460px] bg-surface-variant text-on-surface border border-outline-variant rounded-lg p-4 font-mono text-sm leading-relaxed focus:ring-2 focus:ring-primary focus:border-transparent transition-all disabled:opacity-40"
                          placeholder="Loading template content..."
                        />
                      </div>

                      {/* Template syntax quick guide */}
                      <div className="bg-surface-container-low border border-outline-variant/60 rounded-lg p-4">
                        <h4 className="text-xs font-bold text-primary tracking-wider uppercase mb-2">Go Template Helper Guide</h4>
                        <p className="text-xs text-on-surface-variant leading-relaxed">
                          Use these placeholders: <code className="text-primary font-bold">.Title</code> (Scraped product title), <code className="text-primary font-bold">.ShortURL</code> (Amazon/Flipkart affiliate link), <code className="text-primary font-bold">.Specs</code> (Product attributes key-value map), <code className="text-primary font-bold">.Features</code> (List of Bullet features).
                        </p>
                      </div>
                    </div>
                  </div>
                </div>
              )}

              {/* TAB 3: ACCOUNTS MANAGER */}
              {activeTab === 'accounts' && (
                <div className="space-y-8 animate-fadeIn">
                  
                  {/* Header & Add Button */}
                  <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4 border-b border-outline-variant pb-6">
                    <div>
                      <h1 className="text-3xl font-headline font-bold text-on-surface">Accounts Manager</h1>
                      <p className="text-on-surface-variant mt-1 font-body">Manage Affiliate credentials and Facebook destination pages.</p>
                    </div>
                    {!showAccountForm && (
                      <button 
                        onClick={() => handleOpenAccountForm(null)}
                        className="bg-primary text-on-secondary hover:bg-primary-fixed-dim px-5 py-2.5 rounded-lg font-bold text-sm transition-all shadow-md flex items-center gap-2 w-fit"
                      >
                        <span className="material-symbols-outlined">person_add</span>
                        Add New Account
                      </button>
                    )}
                  </div>

                  {/* Add / Edit Account Form Form Block */}
                  {showAccountForm && (
                    <div className="bg-surface-container rounded-xl border border-outline-variant p-6 shadow-md space-y-6 animate-slideDown">
                      <div className="flex justify-between items-center border-b border-outline-variant pb-4">
                        <h2 className="text-xl font-bold text-primary">
                          {editingAccount ? `Edit Account: ${editingAccount.name}` : 'Add New Account'}
                        </h2>
                        <button 
                          onClick={() => setShowAccountForm(false)}
                          className="text-on-surface-variant hover:text-on-surface p-1 hover:bg-surface-variant rounded-full"
                        >
                          <span className="material-symbols-outlined">close</span>
                        </button>
                      </div>

                      {accountStatus.text && (
                        <div className={`p-3 rounded-lg border text-xs font-semibold ${accountStatus.isError ? 'bg-error-container/10 border-error/20 text-error' : 'bg-primary/5 border-primary/20 text-primary'}`}>
                          {accountStatus.text}
                        </div>
                      )}

                      <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
                        <div>
                          <label className="block text-sm font-semibold text-on-surface-variant mb-2">Account Name</label>
                          <input 
                            type="text" 
                            disabled={!!editingAccount}
                            value={accFormName}
                            onChange={(e) => setAccFormName(e.target.value)}
                            placeholder="e.g. afficart"
                            className="w-full bg-surface-variant border border-outline-variant rounded-lg px-4 py-2.5 text-on-surface focus:ring-2 focus:ring-primary focus:border-transparent transition-all text-sm disabled:opacity-50"
                          />
                        </div>

                        <div>
                          <label className="block text-sm font-semibold text-on-surface-variant mb-2">Template File</label>
                          <select 
                            value={accFormTemplate}
                            onChange={(e) => setAccFormTemplate(e.target.value)}
                            className="w-full bg-surface-variant border border-outline-variant rounded-lg px-4 py-2.5 text-on-surface focus:ring-2 focus:ring-primary focus:border-transparent transition-all text-sm"
                          >
                            {templates.map(t => (
                              <option key={t.path} value={t.path}>{t.name}</option>
                            ))}
                          </select>
                        </div>

                        <div>
                          <label className="block text-sm font-semibold text-on-surface-variant mb-2">Amazon Affiliate Tag</label>
                          <input 
                            type="text" 
                            value={accFormAffiliateTag}
                            onChange={(e) => setAccFormAffiliateTag(e.target.value)}
                            placeholder="e.g. afficart-21"
                            className="w-full bg-surface-variant border border-outline-variant rounded-lg px-4 py-2.5 text-on-surface focus:ring-2 focus:ring-primary focus:border-transparent transition-all text-sm"
                          />
                        </div>

                        <div>
                          <label className="block text-sm font-semibold text-on-surface-variant mb-2">Facebook Page ID</label>
                          <input 
                            type="text" 
                            value={accFormFbPageId}
                            onChange={(e) => setAccFormFbPageId(e.target.value)}
                            placeholder="e.g. 102938475610293"
                            className="w-full bg-surface-variant border border-outline-variant rounded-lg px-4 py-2.5 text-on-surface focus:ring-2 focus:ring-primary focus:border-transparent transition-all text-sm"
                          />
                        </div>

                        <div className="md:col-span-2">
                          <label className="block text-sm font-semibold text-on-surface-variant mb-2">Facebook Page Access Token</label>
                          <input 
                            type="password" 
                            value={accFormFbToken}
                            onChange={(e) => setAccFormFbToken(e.target.value)}
                            placeholder="EAAiT..."
                            className="w-full bg-surface-variant border border-outline-variant rounded-lg px-4 py-2.5 text-on-surface focus:ring-2 focus:ring-primary focus:border-transparent transition-all text-sm"
                          />
                        </div>
                      </div>

                      <div className="flex gap-3 justify-end pt-4 border-t border-outline-variant">
                        <button 
                          onClick={() => setShowAccountForm(false)}
                          className="bg-surface-variant hover:bg-surface-container-highest border border-outline-variant px-5 py-2.5 rounded-lg text-sm text-on-surface font-semibold transition-colors"
                        >
                          Cancel
                        </button>
                        <button 
                          onClick={handleSaveAccount}
                          className="bg-primary text-on-secondary hover:bg-primary-fixed-dim px-6 py-2.5 rounded-lg text-sm font-bold transition-all shadow shadow-primary/20"
                        >
                          {editingAccount ? 'Update Account' : 'Create Account'}
                        </button>
                      </div>
                    </div>
                  )}

                  {/* Accounts Table */}
                  <div className="bg-surface-container rounded-xl border border-outline-variant shadow-sm overflow-hidden">
                    <div className="overflow-x-auto">
                      <table className="w-full text-left border-collapse">
                        <thead>
                          <tr className="border-b border-outline-variant bg-surface-variant/40 text-on-surface-variant text-xs font-bold uppercase tracking-wider">
                            <th className="px-6 py-4">Account Name</th>
                            <th className="px-6 py-4">Template Path</th>
                            <th className="px-6 py-4">Affiliate Tag</th>
                            <th className="px-6 py-4">FB Destination Page ID</th>
                            <th className="px-6 py-4 text-right">Actions</th>
                          </tr>
                        </thead>
                        <tbody className="divide-y divide-outline-variant/60">
                          {accounts.length === 0 ? (
                            <tr>
                              <td colSpan="5" className="px-6 py-8 text-center text-on-surface-variant text-sm">
                                No accounts configured. Click "Add New Account" to begin.
                              </td>
                            </tr>
                          ) : (
                            accounts.map((acc) => (
                              <tr key={acc.name} className="hover:bg-surface-variant/20 transition-colors text-sm text-on-surface">
                                <td className="px-6 py-4 font-bold text-primary">{acc.name}</td>
                                <td className="px-6 py-4 font-mono text-xs text-on-surface-variant">{acc.template_path}</td>
                                <td className="px-6 py-4">{acc.affiliate_tag || '-'}</td>
                                <td className="px-6 py-4 font-mono text-xs">{acc.facebook_page_id || '-'}</td>
                                <td className="px-6 py-4 text-right">
                                  <div className="inline-flex gap-2">
                                    <button 
                                      onClick={() => handleOpenAccountForm(acc)}
                                      className="text-xs bg-surface-variant hover:bg-surface-container-high border border-outline-variant text-on-surface px-3 py-1.5 rounded-md font-bold transition-all"
                                    >
                                      Edit
                                    </button>
                                    <button 
                                      onClick={() => handleDeleteAccount(acc.name)}
                                      className="text-xs bg-red-950/20 hover:bg-red-950/40 border border-red-900/40 text-red-200 px-3 py-1.5 rounded-md font-bold transition-all"
                                    >
                                      Delete
                                    </button>
                                  </div>
                                </td>
                              </tr>
                            ))
                          )}
                        </tbody>
                      </table>
                    </div>
                  </div>
                </div>
              )}

            </div>
          </main>
        </div>
      </div>
    </div>
  );
}
