import { useState } from 'react';
import PageHeader from '../components/PageHeader';
import SectionLabel from '../components/SectionLabel';

export default function AccountsManager({ apiFetch, accounts, templates, onAccountsChanged }) {
  const [showAccountForm, setShowAccountForm] = useState(false);
  const [editingAccount, setEditingAccount] = useState(null); // null if adding new
  const [accFormName, setAccFormName] = useState('');
  const [accFormTemplate, setAccFormTemplate] = useState('');
  const [accFormAffiliateTag, setAccFormAffiliateTag] = useState('');
  const [accFormFbPageId, setAccFormFbPageId] = useState('');
  const [accFormFbToken, setAccFormFbToken] = useState('');
  const [accFormActive, setAccFormActive] = useState(true);
  const [accFormMaxPostsPerDay, setAccFormMaxPostsPerDay] = useState(0);
  const [accFormActiveHoursStart, setAccFormActiveHoursStart] = useState('');
  const [accFormActiveHoursEnd, setAccFormActiveHoursEnd] = useState('');
  const [accFormMinDelayMinutes, setAccFormMinDelayMinutes] = useState(0);
  const [accountStatus, setAccountStatus] = useState({ text: '', isError: false });

  // Per-account link pool panel state
  const [linksAccount, setLinksAccount] = useState(null); // name of account whose pool is open, or null
  const [accountLinks, setAccountLinks] = useState([]);
  const [linksLoading, setLinksLoading] = useState(false);
  const [linksError, setLinksError] = useState('');
  const [newLinkUrls, setNewLinkUrls] = useState('');
  const [linksActionStatus, setLinksActionStatus] = useState('');

  const loadAccountLinks = async (name) => {
    setLinksLoading(true);
    setLinksError('');
    try {
      const resp = await apiFetch(`/accounts/${encodeURIComponent(name)}/links`);
      if (!resp.ok) {
        const data = await resp.json().catch(() => ({}));
        throw new Error(data.error || `Failed to load link pool (HTTP ${resp.status})`);
      }
      const data = await resp.json();
      setAccountLinks(data.links || []);
    } catch (err) {
      setLinksError(err.message);
    } finally {
      setLinksLoading(false);
    }
  };

  const handleToggleLinksPanel = (name) => {
    if (linksAccount === name) {
      setLinksAccount(null);
      return;
    }
    setLinksAccount(name);
    setNewLinkUrls('');
    setLinksActionStatus('');
    loadAccountLinks(name);
  };

  const handleAddLinks = async () => {
    const parsed = newLinkUrls.split(/\r?\n/).map(u => u.trim()).filter(Boolean);
    if (parsed.length === 0 || !linksAccount) return;

    setLinksActionStatus(`Adding ${parsed.length} link(s)...`);

    const failedUrls = [];
    let successCount = 0;

    for (const url of parsed) {
      try {
        const resp = await apiFetch(`/accounts/${encodeURIComponent(linksAccount)}/links`, {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ url })
        });
        if (resp.ok) {
          successCount++;
        } else {
          const data = await resp.json().catch(() => ({}));
          console.error(data.error);
          failedUrls.push(url);
        }
      } catch (err) {
        console.error(err);
        failedUrls.push(url);
      }
    }

    setNewLinkUrls(failedUrls.join('\n'));
    setLinksActionStatus(
      failedUrls.length === 0
        ? `Added ${successCount}/${parsed.length} link(s).`
        : `Added ${successCount}/${parsed.length} link(s), ${failedUrls.length} failed (left in the box for retry).`
    );
    loadAccountLinks(linksAccount);
    setTimeout(() => setLinksActionStatus(""), 4000);
  };

  const handleDeleteLink = async (id) => {
    try {
      const resp = await apiFetch(`/accounts/${encodeURIComponent(linksAccount)}/links/${id}`, { method: "DELETE" });
      if (resp.ok) {
        loadAccountLinks(linksAccount);
      }
    } catch (err) {
      console.error(err);
    }
  };

  const handleOpenAccountForm = (acc = null) => {
    setEditingAccount(acc);
    if (acc) {
      setAccFormName(acc.name);
      setAccFormTemplate(acc.template_path);
      setAccFormAffiliateTag(acc.affiliate_tag || "");
      setAccFormFbPageId(acc.facebook_page_id || "");
      setAccFormFbToken(acc.facebook_access_token || "");
      setAccFormActive(acc.active !== false);
      setAccFormMaxPostsPerDay(acc.max_posts_per_day || 0);
      setAccFormActiveHoursStart(acc.active_hours_start || "");
      setAccFormActiveHoursEnd(acc.active_hours_end || "");
      setAccFormMinDelayMinutes(acc.min_delay_minutes || 0);
    } else {
      setAccFormName("");
      setAccFormTemplate(templates.length > 0 ? templates[0].path : "");
      setAccFormAffiliateTag("");
      setAccFormFbPageId("");
      setAccFormFbToken("");
      setAccFormActive(true);
      setAccFormMaxPostsPerDay(0);
      setAccFormActiveHoursStart("");
      setAccFormActiveHoursEnd("");
      setAccFormMinDelayMinutes(0);
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
      facebook_access_token: accFormFbToken.trim(),
      active: accFormActive,
      max_posts_per_day: Number(accFormMaxPostsPerDay) || 0,
      active_hours_start: accFormActiveHoursStart,
      active_hours_end: accFormActiveHoursEnd,
      min_delay_minutes: Number(accFormMinDelayMinutes) || 0
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
        onAccountsChanged();
      }, 1000);
    } catch (err) {
      setAccountStatus({ text: "Error: " + err.message, isError: true });
    }
  };

  // Quick toggle from the accounts table - resends the account's existing
  // fields with only `active` flipped, rather than requiring the full edit form.
  const handleToggleAccountActive = async (acc) => {
    const nextActive = acc.active === false;
    try {
      const resp = await apiFetch(`/accounts/${encodeURIComponent(acc.name)}`, {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          template_path: acc.template_path,
          affiliate_tag: acc.affiliate_tag || "",
          facebook_page_id: acc.facebook_page_id || "",
          facebook_access_token: acc.facebook_access_token || "",
          active: nextActive,
          max_posts_per_day: acc.max_posts_per_day || 0,
          active_hours_start: acc.active_hours_start || "",
          active_hours_end: acc.active_hours_end || "",
          min_delay_minutes: acc.min_delay_minutes || 0
        })
      });
      if (!resp.ok) {
        const data = await resp.json().catch(() => ({}));
        throw new Error(data.error || "Failed to update account status");
      }
      onAccountsChanged();
    } catch (err) {
      alert("Error: " + err.message);
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
      onAccountsChanged();
    } catch (err) {
      alert("Error: " + err.message);
    }
  };

  return (
    <div className="space-y-8">
      <PageHeader
        title="Accounts Manager"
        subtitle="Manage Affiliate credentials and Facebook destination pages."
        action={!showAccountForm && (
          <button
            onClick={() => handleOpenAccountForm(null)}
            className="bg-primary text-on-secondary hover:bg-primary-fixed-dim px-5 py-2.5 rounded-lg font-bold text-sm transition-all shadow-md flex items-center gap-2 w-fit"
          >
            <span className="material-symbols-outlined">person_add</span>
            Add New Account
          </button>
        )}
      />

      {showAccountForm && (
        <div className="bg-surface-container rounded-xl border border-outline-variant p-6 shadow-md space-y-6 fadein">
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

            <div>
              <label className="block text-sm font-semibold text-on-surface-variant mb-2">Max Posts / Day</label>
              <input
                type="number"
                min="0"
                value={accFormMaxPostsPerDay}
                onChange={(e) => setAccFormMaxPostsPerDay(e.target.value)}
                placeholder="0 = no limit"
                className="w-full bg-surface-variant border border-outline-variant rounded-lg px-4 py-2.5 text-on-surface focus:ring-2 focus:ring-primary focus:border-transparent transition-all text-sm"
              />
            </div>

            <div>
              <label className="block text-sm font-semibold text-on-surface-variant mb-2">Minimum Delay Between Posts (minutes)</label>
              <input
                type="number"
                min="0"
                value={accFormMinDelayMinutes}
                onChange={(e) => setAccFormMinDelayMinutes(e.target.value)}
                placeholder="0 = no minimum"
                className="w-full bg-surface-variant border border-outline-variant rounded-lg px-4 py-2.5 text-on-surface focus:ring-2 focus:ring-primary focus:border-transparent transition-all text-sm"
              />
            </div>

            <div className="md:col-span-2">
              <label className="block text-sm font-semibold text-on-surface-variant mb-2">Active Posting Window</label>
              <div className="flex items-center gap-3">
                <input
                  type="time"
                  value={accFormActiveHoursStart}
                  onChange={(e) => setAccFormActiveHoursStart(e.target.value)}
                  className="flex-1 bg-surface-variant border border-outline-variant rounded-lg px-4 py-2.5 text-on-surface focus:ring-2 focus:ring-primary focus:border-transparent transition-all text-sm"
                />
                <span className="text-on-surface-variant text-sm">to</span>
                <input
                  type="time"
                  value={accFormActiveHoursEnd}
                  onChange={(e) => setAccFormActiveHoursEnd(e.target.value)}
                  className="flex-1 bg-surface-variant border border-outline-variant rounded-lg px-4 py-2.5 text-on-surface focus:ring-2 focus:ring-primary focus:border-transparent transition-all text-sm"
                />
              </div>
              <p className="text-xs text-on-surface-variant mt-1.5">Leave both blank to allow posting at any hour. Times are server-local, 24-hour.</p>
            </div>

            <div className="md:col-span-2 flex items-center gap-3">
              <input
                type="checkbox"
                id="acc-form-active"
                checked={accFormActive}
                onChange={(e) => setAccFormActive(e.target.checked)}
                className="w-4 h-4"
              />
              <label htmlFor="acc-form-active" className="text-sm font-semibold text-on-surface-variant">
                Active (participates in Auto-Publisher candidate selection)
              </label>
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

      <div className="bg-surface-container rounded-xl border border-outline-variant shadow-sm overflow-hidden">
        <div className="overflow-x-auto">
          <table className="w-full text-left border-collapse">
            <thead>
              <tr className="border-b border-outline-variant bg-surface-variant/40 text-on-surface-variant text-xs font-bold uppercase tracking-wider">
                <th className="px-6 py-4">Account Name</th>
                <th className="px-6 py-4">Template Path</th>
                <th className="px-6 py-4">Affiliate Tag</th>
                <th className="px-6 py-4">FB Destination Page ID</th>
                <th className="px-6 py-4">Schedule</th>
                <th className="px-6 py-4">Status</th>
                <th className="px-6 py-4 text-right">Actions</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-outline-variant/60">
              {accounts.length === 0 ? (
                <tr>
                  <td colSpan="7" className="px-6 py-8 text-center text-on-surface-variant text-sm">
                    No accounts configured. Click "Add New Account" to begin.
                  </td>
                </tr>
              ) : (
                accounts.map((acc) => {
                  const isActive = acc.active !== false;
                  const scheduleParts = [];
                  if (acc.max_posts_per_day) scheduleParts.push(`Max ${acc.max_posts_per_day}/day`);
                  if (acc.active_hours_start && acc.active_hours_end) scheduleParts.push(`${acc.active_hours_start}-${acc.active_hours_end}`);
                  if (acc.min_delay_minutes) scheduleParts.push(`${acc.min_delay_minutes}m min delay`);
                  const scheduleBadge = scheduleParts.length > 0 ? scheduleParts.join(' • ') : 'No limits';
                  return (
                    <tr key={acc.name} className="hover:bg-surface-variant/20 transition-colors text-sm text-on-surface">
                      <td className="px-6 py-4 font-bold text-primary">{acc.name}</td>
                      <td className="px-6 py-4 font-mono text-xs text-on-surface-variant">{acc.template_path}</td>
                      <td className="px-6 py-4">{acc.affiliate_tag || '-'}</td>
                      <td className="px-6 py-4 font-mono text-xs">{acc.facebook_page_id || '-'}</td>
                      <td className="px-6 py-4 text-xs text-on-surface-variant">{scheduleBadge}</td>
                      <td className="px-6 py-4">
                        <button
                          onClick={() => handleToggleAccountActive(acc)}
                          title={isActive ? "Click to deactivate" : "Click to activate"}
                          className={`text-xs px-2.5 py-1 rounded-full font-bold uppercase transition-all ${isActive ? 'bg-primary/10 text-primary hover:bg-primary/20' : 'bg-surface-variant text-on-surface-variant hover:bg-surface-container-highest'}`}
                        >
                          {isActive ? 'Active' : 'Inactive'}
                        </button>
                      </td>
                      <td className="px-6 py-4 text-right">
                        <div className="inline-flex gap-2">
                          <button
                            onClick={() => handleToggleLinksPanel(acc.name)}
                            className={`text-xs border px-3 py-1.5 rounded-md font-bold transition-all ${linksAccount === acc.name ? 'bg-primary/10 border-primary/30 text-primary' : 'bg-surface-variant hover:bg-surface-container-high border-outline-variant text-on-surface'}`}
                          >
                            Links
                          </button>
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
                  );
                })
              )}
            </tbody>
          </table>
        </div>
      </div>

      {linksAccount && (
        <div className="bg-surface-container rounded-xl border border-primary/30 p-6 shadow-md space-y-6 fadein">
          <div className="flex justify-between items-center border-b border-outline-variant pb-4">
            <h2 className="text-xl font-bold text-primary">Link Pool: {linksAccount}</h2>
            <button
              onClick={() => setLinksAccount(null)}
              className="text-on-surface-variant hover:text-on-surface p-1 hover:bg-surface-variant rounded-full"
            >
              <span className="material-symbols-outlined">close</span>
            </button>
          </div>

          <p className="text-xs text-on-surface-variant leading-relaxed">
            Links added here are dedicated to this account. When an auto-post job is triggered, this account draws
            its daily quota from this pool first (oldest first), falling back to the shared product pool only if it
            runs out.
          </p>

          <div>
            <SectionLabel icon="add_link">Add Links to Pool</SectionLabel>
            <textarea
              value={newLinkUrls}
              onChange={(e) => setNewLinkUrls(e.target.value)}
              className="w-full h-28 bg-surface-variant border border-outline-variant rounded-lg p-4 text-on-surface placeholder:text-on-surface-variant/40 focus:ring-2 focus:ring-primary focus:border-transparent transition-all resize-none font-mono text-sm leading-relaxed"
              placeholder="Paste product URLs for this account here... One per line."
            />
            <div className="mt-3 flex items-center gap-3">
              <button
                onClick={handleAddLinks}
                className="bg-primary text-on-secondary hover:bg-primary-fixed-dim px-5 py-2.5 rounded-lg font-label font-bold text-sm transition-colors shadow flex items-center gap-2"
              >
                <span className="material-symbols-outlined text-lg">add_link</span>
                Add to Pool
              </button>
              {linksActionStatus && (
                <span className="text-xs text-primary font-semibold">{linksActionStatus}</span>
              )}
            </div>
          </div>

          <div className="space-y-3">
            <div className="flex justify-between items-center">
              <span className="text-xs text-on-surface-variant font-semibold uppercase">
                Pool Contents ({accountLinks.length})
              </span>
              <button
                onClick={() => loadAccountLinks(linksAccount)}
                className="text-xs text-primary hover:text-primary-fixed-dim font-bold flex items-center gap-1.5"
              >
                <span className="material-symbols-outlined text-sm">refresh</span>
                Refresh
              </button>
            </div>

            {linksError && (
              <div className="text-error bg-error-container/10 p-3 rounded-lg border border-error/20 text-sm">{linksError}</div>
            )}

            {linksLoading ? (
              <p className="text-center py-6 text-on-surface-variant text-sm">Loading pool...</p>
            ) : accountLinks.length === 0 ? (
              <div className="text-center py-8 border border-dashed border-outline-variant bg-surface-variant/20 rounded-lg">
                <p className="text-on-surface-variant text-sm">No links in this account's pool yet.</p>
              </div>
            ) : (
              <div className="border border-outline-variant rounded-lg divide-y divide-outline-variant/60 max-h-80 overflow-y-auto">
                {accountLinks.map((link) => (
                  <div key={link.id} className="flex items-center gap-3 px-4 py-2.5 hover:bg-surface-variant/20 transition-colors">
                    <span className={`text-[10px] px-2 py-0.5 rounded-full font-bold uppercase shrink-0 ${link.posted ? 'bg-surface-container-highest text-on-surface-variant' : 'bg-primary/10 text-primary'}`}>
                      {link.posted ? 'Posted' : 'Available'}
                    </span>
                    <a
                      href={link.url}
                      target="_blank"
                      rel="noopener noreferrer"
                      className="flex-1 min-w-0 text-xs text-on-surface-variant hover:underline font-mono truncate"
                    >
                      {link.url}
                    </a>
                    <button
                      onClick={() => handleDeleteLink(link.id)}
                      className="text-error bg-red-950/15 hover:bg-red-950/30 p-1.5 rounded-md border border-red-900/20 shrink-0"
                      title="Remove from pool"
                    >
                      <span className="material-symbols-outlined text-sm">delete</span>
                    </button>
                  </div>
                ))}
              </div>
            )}
          </div>
        </div>
      )}
    </div>
  );
}
