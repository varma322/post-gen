import { useState } from 'react';
import { chart, niceMax, weekday, shortDate } from './palette';

/**
 * Stacked publish/failure bars over a date series.
 *
 * Renders pure SVG against a viewBox so it scales with its container without
 * a resize observer. Days with no activity arrive from the API as explicit
 * zeroes, so the axis stays continuous rather than collapsing gaps.
 *
 * @param {{date: string, count: number}[]} published
 * @param {{date: string, count: number}[]} failed
 */
export default function BarChart({ published = [], failed = [], height = 260, labelMode = 'weekday' }) {
  const [hover, setHover] = useState(null);

  if (published.length === 0) {
    return <EmptyChart height={height} message="No publishing activity yet" />;
  }

  const failureByDate = new Map(failed.map((d) => [d.date, d.count]));
  const points = published.map((d) => ({
    date: d.date,
    published: d.count,
    failed: failureByDate.get(d.date) || 0,
  }));

  const peak = Math.max(...points.map((p) => p.published + p.failed), 1);
  const max = niceMax(peak);

  // A fixed viewBox with generous padding for the axis gutters; the SVG
  // itself stretches to whatever width the card gives it.
  const width = 720;
  const padLeft = 38;
  const padBottom = 26;
  const padTop = 10;
  const plotWidth = width - padLeft;
  const plotHeight = height - padBottom - padTop;

  const slot = plotWidth / points.length;
  const barWidth = Math.min(slot * 0.55, 42);

  const yFor = (value) => padTop + plotHeight - (value / max) * plotHeight;

  const gridLines = [0, 0.5, 1].map((fraction) => ({
    y: padTop + plotHeight - fraction * plotHeight,
    label: Math.round(max * fraction),
  }));

  return (
    <div className="relative">
      <svg
        viewBox={`0 0 ${width} ${height}`}
        className="w-full"
        style={{ height }}
        role="img"
        aria-label={`Publishing activity across ${points.length} days`}
      >
        {gridLines.map((line) => (
          <g key={line.label}>
            <line
              x1={padLeft} x2={width} y1={line.y} y2={line.y}
              stroke={chart.grid} strokeWidth="1"
            />
            <text
              x={padLeft - 8} y={line.y + 4}
              textAnchor="end" fontSize="11" fill={chart.textDim}
              style={{ fontVariantNumeric: 'tabular-nums' }}
            >
              {line.label}
            </text>
          </g>
        ))}

        {points.map((point, index) => {
          const centre = padLeft + slot * index + slot / 2;
          const x = centre - barWidth / 2;
          const publishedHeight = (point.published / max) * plotHeight;
          const failedHeight = (point.failed / max) * plotHeight;
          const isHovered = hover === index;

          return (
            <g
              key={point.date}
              onMouseEnter={() => setHover(index)}
              onMouseLeave={() => setHover(null)}
            >
              {/* Full-height hit area so the tooltip doesn't require pixel
                  accuracy on a short bar. */}
              <rect
                x={padLeft + slot * index} y={padTop}
                width={slot} height={plotHeight}
                fill={isHovered ? '#ffffff08' : 'transparent'}
              />

              {point.failed > 0 && (
                <rect
                  x={x} y={yFor(point.published + point.failed)}
                  width={barWidth} height={Math.max(failedHeight, 2)}
                  fill={chart.error} rx="2"
                />
              )}
              <rect
                x={x} y={yFor(point.published)}
                width={barWidth} height={Math.max(publishedHeight, point.published > 0 ? 2 : 0)}
                fill={isHovered ? chart.primary : chart.primaryDim}
                rx="2"
              />

              <text
                x={centre} y={height - 8}
                textAnchor="middle" fontSize="11"
                fill={isHovered ? chart.text : chart.textDim}
              >
                {labelMode === 'weekday' ? weekday(point.date) : shortDate(point.date)}
              </text>
            </g>
          );
        })}
      </svg>

      {hover !== null && (
        <Tooltip point={points[hover]} index={hover} total={points.length} />
      )}
    </div>
  );
}

function Tooltip({ point, index, total }) {
  // Anchor by percentage so the tooltip tracks the bar as the SVG scales.
  const leftPct = ((index + 0.5) / total) * 100;

  return (
    <div
      className="pointer-events-none absolute -top-2 z-10 -translate-x-1/2 rounded-md border border-outline-variant bg-surface-container-high px-3 py-2 shadow-lg"
      style={{ left: `${leftPct}%` }}
    >
      <div className="text-label-sm text-on-surface-variant">{shortDate(point.date)}</div>
      <div className="mt-1 flex items-center gap-2 text-body-sm">
        <span className="inline-block h-2 w-2 rounded-full" style={{ background: chart.primaryDim }} />
        <span className="text-on-surface tabular">{point.published}</span>
        <span className="text-on-surface-variant">published</span>
      </div>
      {point.failed > 0 && (
        <div className="flex items-center gap-2 text-body-sm">
          <span className="inline-block h-2 w-2 rounded-full" style={{ background: chart.error }} />
          <span className="text-on-surface tabular">{point.failed}</span>
          <span className="text-on-surface-variant">failed</span>
        </div>
      )}
    </div>
  );
}

export function EmptyChart({ height, message }) {
  return (
    <div
      className="flex items-center justify-center rounded-md border border-dashed border-outline-variant text-body-sm text-on-surface-variant"
      style={{ height }}
    >
      {message}
    </div>
  );
}
