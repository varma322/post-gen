import { chart, shortDate } from './palette';

/**
 * Channel publishing density: one row per account, one cell per day, opacity
 * scaled to volume.
 *
 * Rendered as a CSS grid rather than SVG - the cells are uniform rectangles
 * with text labels, which the grid handles more cleanly than manual x/y
 * placement, and it keeps the row labels selectable.
 *
 * @param {{account_name: string, daily: {date: string, count: number}[]}[]} channels
 */
export default function Heatmap({ channels = [], maxRows = 12 }) {
  const rows = channels.filter((c) => c.daily && c.daily.length > 0).slice(0, maxRows);

  if (rows.length === 0) {
    return (
      <div className="flex h-40 items-center justify-center rounded-md border border-dashed border-outline-variant text-body-sm text-on-surface-variant">
        No channel activity in this window
      </div>
    );
  }

  const dates = rows[0].daily.map((d) => d.date);
  const peak = Math.max(...rows.flatMap((r) => r.daily.map((d) => d.count)), 1);

  return (
    <div className="overflow-x-auto">
      <div className="min-w-[640px]">
        {/* Date header, aligned to the same column template as the rows. */}
        <div
          className="mb-2 grid items-end gap-1"
          style={{ gridTemplateColumns: `140px repeat(${dates.length}, minmax(0, 1fr))` }}
        >
          <div />
          {dates.map((date, index) => (
            <div key={date} className="text-center text-label-sm text-on-surface-variant">
              {/* Label every other column so they don't collide at 14 days. */}
              {index % 2 === 0 ? shortDate(date).split(' ')[1] : ''}
            </div>
          ))}
        </div>

        <div className="flex flex-col gap-1">
          {rows.map((row) => (
            <div
              key={row.account_name}
              className="grid items-center gap-1"
              style={{ gridTemplateColumns: `140px repeat(${dates.length}, minmax(0, 1fr))` }}
            >
              <div className="truncate pr-3 text-body-sm text-on-surface" title={row.account_name}>
                {row.account_name}
              </div>
              {row.daily.map((day) => (
                <div
                  key={day.date}
                  className="h-7 rounded-sm border border-outline-variant/40"
                  style={{ background: cellColour(day.count, peak) }}
                  title={`${row.account_name} — ${shortDate(day.date)}: ${day.count} post${day.count === 1 ? '' : 's'}`}
                />
              ))}
            </div>
          ))}
        </div>

        <ScaleLegend />
      </div>
    </div>
  );
}

/**
 * cellColour maps a count onto a five-step opacity ramp. Zero gets the bare
 * surface rather than a faint tint, so "nothing published" is visually
 * distinct from "barely published" - the difference that matters when hunting
 * for a channel that has stalled.
 */
function cellColour(count, peak) {
  if (count === 0) return chart.surface;

  const steps = [0.2, 0.4, 0.6, 0.8, 1];
  const index = Math.min(steps.length - 1, Math.floor((count / peak) * steps.length));
  return withOpacity(chart.primary, steps[index]);
}

function withOpacity(hex, opacity) {
  const alpha = Math.round(opacity * 255).toString(16).padStart(2, '0');
  return `${hex}${alpha}`;
}

function ScaleLegend() {
  return (
    <div className="mt-4 flex items-center justify-end gap-2">
      <span className="text-label-sm text-on-surface-variant">Less</span>
      <div className="h-3 w-4 rounded-sm border border-outline-variant/40" style={{ background: chart.surface }} />
      {[0.2, 0.4, 0.6, 0.8, 1].map((step) => (
        <div
          key={step}
          className="h-3 w-4 rounded-sm border border-outline-variant/40"
          style={{ background: withOpacity(chart.primary, step) }}
        />
      ))}
      <span className="text-label-sm text-on-surface-variant">More</span>
    </div>
  );
}
