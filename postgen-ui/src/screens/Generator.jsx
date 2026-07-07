import { useState, useEffect, useRef } from 'react';
import PageHeader from '../components/PageHeader';
import SectionLabel from '../components/SectionLabel';
import AccountChip from '../components/AccountChip';
import ResultsFeed from '../components/ResultsFeed';

export default function Generator({ apiFetch, accounts, onNavigateToAccounts }) {
  const [urls, setUrls] = useState('');
  const [publishToFb, setPublishToFb] = useState(false);
  const [publishDelay, setPublishDelay] = useState(5);
  const [isGenerating, setIsGenerating] = useState(false);
  const [statusMessage, setStatusMessage] = useState('');
  const [cooldownSeconds, setCooldownSeconds] = useState(0);
  const [streamedResults, setStreamedResults] = useState([]);
  const [selectedAccounts, setSelectedAccounts] = useState([]);
  const cooldownIntervalRef = useRef(null);

  // Auto-select all accounts by default once the account list loads.
  useEffect(() => {
    if (selectedAccounts.length === 0 && accounts.length > 0) {
      setSelectedAccounts(accounts.map(a => a.name));
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [accounts]);

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
    } catch {
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
    } catch {
      alert("Failed to copy content.");
    }
  };

  const publishPost = async (accountName, outputText, index) => {
    setStreamedResults(prev => prev.map((r, idx) => idx === index ? { ...r, publishing: true } : r));
    try {
      const resp = await apiFetch("/publish", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          account: accountName,
          content: outputText
        })
      });
      const data = await resp.json();
      if (!resp.ok) {
        throw new Error(data.error || "Failed to publish post");
      }
      setStreamedResults(prev => prev.map((r, idx) => idx === index ? { ...r, publish_id: data.publish_id, publish_error: null, publishing: false } : r));
    } catch (err) {
      setStreamedResults(prev => prev.map((r, idx) => idx === index ? { ...r, publish_error: err.message, publishing: false } : r));
    }
  };

  return (
    <div className="space-y-6">
      <PageHeader title="Generator" subtitle="Turn product links into ready-to-publish affiliate posts." />

      {statusMessage && (
        <div className="flex items-center gap-3 bg-surface-container border border-outline-variant px-4 py-2.5 rounded-xl text-sm shadow-sm fadein">
          <span className="material-symbols-outlined text-primary animate-spin">sync</span>
          <span className="font-medium text-primary-fixed-dim line-clamp-1">{statusMessage}</span>
        </div>
      )}

      {cooldownSeconds > 0 && (
        <div className="bg-primary/10 border border-primary/30 p-4 rounded-xl flex items-center justify-between shadow-lg text-primary fadein">
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

      <div className="grid grid-cols-1 lg:grid-cols-[420px_1fr] gap-6 items-start">
        <div className="flex flex-col gap-5 lg:sticky lg:top-8">
          {/* Product URLs */}
          <div className="bg-surface-container rounded-xl border border-outline-variant p-5 shadow-sm">
            <SectionLabel icon="link">Product URLs</SectionLabel>
            <textarea
              value={urls}
              onChange={(e) => setUrls(e.target.value)}
              className="w-full h-36 bg-surface-variant border border-outline-variant rounded-lg p-3 text-on-surface placeholder:text-on-surface-variant/40 focus:ring-2 focus:ring-primary focus:border-transparent transition-all resize-none font-mono text-sm leading-relaxed"
              placeholder="Paste Amazon, Flipkart, or other product URLs here... One per line."
            />
            <div className="text-xs text-on-surface-variant mt-2">{parseURLs().length} link{parseURLs().length === 1 ? '' : 's'} queued</div>
          </div>

          {/* Publish config */}
          <div className="bg-surface-container rounded-xl border border-outline-variant p-5 shadow-sm">
            <SectionLabel icon="tune">Publish config</SectionLabel>
            <label className="flex items-center gap-3 cursor-pointer mb-4">
              <input
                type="checkbox"
                checked={publishToFb}
                onChange={(e) => setPublishToFb(e.target.checked)}
                className="form-checkbox bg-surface border-outline text-primary rounded focus:ring-primary focus:ring-offset-surface"
              />
              <span className="text-sm text-on-surface">Post directly to Facebook</span>
            </label>

            {publishToFb && (
              <div className="flex items-center gap-3 mb-5 fadein">
                <span className="text-xs text-on-surface-variant">Stagger by</span>
                <input
                  type="number"
                  value={publishDelay}
                  onChange={(e) => setPublishDelay(e.target.value)}
                  min="0"
                  className="w-16 bg-surface-variant border border-outline-variant rounded-lg px-2 py-1.5 text-primary text-sm font-bold text-center focus:ring-2 focus:ring-primary"
                />
                <span className="text-xs text-on-surface-variant">min</span>
              </div>
            )}

            <div className="flex items-center justify-between mb-2">
              <span className="text-xs font-bold text-on-surface-variant uppercase tracking-wide">Accounts ({selectedAccounts.length} selected)</span>
              <button
                onClick={handleSelectAllAccounts}
                className="text-xs text-primary hover:text-primary-fixed-dim font-bold transition-colors"
              >
                Select all
              </button>
            </div>

            {accounts.length === 0 ? (
              <div className="text-center py-6 border border-dashed border-outline-variant rounded-lg bg-surface-variant/40">
                <p className="text-on-surface-variant text-sm mb-2">No accounts available.</p>
                <button
                  onClick={onNavigateToAccounts}
                  className="text-xs text-primary hover:underline font-bold"
                >
                  Add one in Accounts Manager
                </button>
              </div>
            ) : (
              <div className="flex flex-col gap-2">
                {accounts.map(account => (
                  <AccountChip
                    key={account.name}
                    account={account}
                    selected={selectedAccounts.includes(account.name)}
                    onToggle={() => handleToggleAccountSelection(account.name)}
                  />
                ))}
              </div>
            )}
          </div>

          <div className="flex gap-3">
            <button
              onClick={handleGenerate}
              disabled={isGenerating}
              className="flex-1 bg-primary text-on-secondary hover:bg-primary-fixed-dim disabled:bg-surface-container-high disabled:text-on-surface-variant/50 px-6 py-3.5 rounded-lg font-bold text-sm transition-all flex items-center justify-center gap-2 shadow-sm"
            >
              <span className="material-symbols-outlined text-lg">rocket_launch</span>
              {isGenerating ? 'Processing live…' : 'Generate affiliate posts'}
            </button>
            <button
              onClick={handleClear}
              disabled={isGenerating}
              className="bg-surface-container-high text-on-surface-variant hover:text-on-surface hover:bg-surface-container-highest px-4 py-3.5 rounded-lg border border-outline-variant font-medium text-sm transition-colors"
            >
              Clear
            </button>
          </div>
        </div>

        <div>
          <SectionLabel icon="dynamic_feed">Live results feed ({streamedResults.length} generated)</SectionLabel>
          <ResultsFeed results={streamedResults} onCopy={copyToClipboard} onPublish={publishPost} />
        </div>
      </div>
    </div>
  );
}
