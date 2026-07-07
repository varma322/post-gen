import { useState, useEffect } from 'react';
import PageHeader from '../components/PageHeader';

export default function TemplatesManager({ apiFetch, templates, onTemplatesChanged }) {
  const [selectedTemplate, setSelectedTemplate] = useState('');
  const [templateContent, setTemplateContent] = useState('');
  const [templateSaving, setTemplateSaving] = useState(false);
  const [templateStatus, setTemplateStatus] = useState({ text: '', isError: false });

  // Select first template by default once the list loads.
  useEffect(() => {
    if (templates.length > 0 && !selectedTemplate) {
      // eslint-disable-next-line react-hooks/set-state-in-effect -- one-time default-selection seed
      setSelectedTemplate(templates[0].name);
    }
  }, [templates, selectedTemplate]);

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
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [selectedTemplate]);

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
      onTemplatesChanged();
    } catch (err) {
      setTemplateStatus({ text: "Error: " + err.message, isError: true });
    } finally {
      setTemplateSaving(false);
    }
  };

  return (
    <div className="space-y-8">
      <PageHeader title="Templates Manager" subtitle="Customize generator layouts using Go HTML template tags." />

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
  );
}
