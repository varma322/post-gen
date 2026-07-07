import StatusChip from './StatusChip';

export default function ResultsFeed({ results, onCopy, onPublish }) {
  if (results.length === 0) {
    return (
      <div className="text-center py-12 border border-dashed border-outline-variant bg-surface-container rounded-xl">
        <span className="material-symbols-outlined text-on-surface-variant/40 text-4xl mb-2">post_add</span>
        <p className="text-on-surface-variant text-sm">Generated posts will appear here in real-time as they process.</p>
      </div>
    );
  }

  return (
    <div className="space-y-6">
      {/* Summary Bar */}
      <div className="bg-surface-container border border-outline-variant rounded-xl p-4 flex flex-wrap gap-4 items-center justify-between shadow-sm">
        <div className="flex gap-4">
          <span className="bg-primary-container text-on-primary-container px-3 py-1 rounded-full text-xs font-bold flex items-center gap-1.5">
            <span className="material-symbols-outlined text-sm">check_circle</span>
            Success: {results.filter(r => !r.error).length}
          </span>
          <span className="bg-error-container text-error px-3 py-1 rounded-full text-xs font-bold flex items-center gap-1.5">
            <span className="material-symbols-outlined text-sm">error</span>
            Failed: {results.filter(r => r.error).length}
          </span>
        </div>
        <span className="text-xs text-on-surface-variant font-semibold">
          Total Processed: {results.length}
        </span>
      </div>

      {results.map((result, idx) => {
        const isError = !!result.error;
        return (
          <div
            key={idx}
            className={`bg-surface-container rounded-xl border p-6 shadow-sm transition-colors ${isError ? 'border-red-950/60 bg-red-950/5' : 'border-outline-variant hover:border-outline'}`}
          >
            <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4 border-b border-outline-variant pb-4 mb-4">
              <div className="flex flex-wrap items-center gap-3">
                <StatusChip status={isError ? 'error' : 'success'} />
                <span className="font-semibold text-on-surface text-sm">Account: {result.account}</span>
              </div>

              {!isError && (
                <div className="flex gap-2">
                  <button
                    onClick={() => onCopy(result.output, idx)}
                    className="text-xs bg-surface-variant hover:bg-surface-container-high border border-outline-variant text-on-surface px-4 py-2 rounded-lg font-bold transition-all flex items-center gap-2 w-fit active:scale-95"
                  >
                    <span className="material-symbols-outlined text-sm">
                      {result.copied ? 'done' : 'content_copy'}
                    </span>
                    {result.copied ? 'Copied!' : 'Copy Post Content'}
                  </button>

                  {!result.publish_id && (
                    <button
                      onClick={() => onPublish(result.account, result.output, idx)}
                      disabled={result.publishing}
                      className="text-xs bg-primary text-on-secondary hover:bg-primary-fixed-dim disabled:bg-surface-container-high disabled:text-on-surface-variant/50 px-4 py-2 rounded-lg font-bold transition-all flex items-center gap-2 w-fit active:scale-95"
                    >
                      <span className="material-symbols-outlined text-sm">
                        {result.publishing ? 'hourglass_empty' : 'send'}
                      </span>
                      {result.publishing ? 'Publishing...' : 'Publish to Facebook'}
                    </button>
                  )}
                </div>
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
  );
}
