import { chart, niceMax, shortDate } from './palette';
import { EmptyChart } from './BarChart';

/**
 * Multi-series line/area chart with a smooth curve.
 *
 * The curve is a cubic Bezier fitted through the points via a Catmull-Rom
 * conversion, which passes through every data point rather than smoothing past
 * it - important here, because a chart that visually misses a zero day would
 * misreport a publishing outage.
 *
 * @param {{name: string, color: string, dashed?: boolean, area?: boolean,
 *          points: {date: string, count: number}[]}[]} series
 */
export default function TrendChart({ series = [], height = 300 }) {
  const withData = series.filter((s) => s.points && s.points.length > 1);
  if (withData.length === 0) {
    return <EmptyChart height={height} message="Not enough history to plot a trend yet" />;
  }

  const width = 860;
  const padLeft = 44;
  const padRight = 12;
  const padTop = 12;
  const padBottom = 34;
  const plotWidth = width - padLeft - padRight;
  const plotHeight = height - padTop - padBottom;

  const peak = Math.max(...withData.flatMap((s) => s.points.map((p) => p.count)), 1);
  const max = niceMax(peak);
  const count = withData[0].points.length;

  const xFor = (index) => padLeft + (index / Math.max(count - 1, 1)) * plotWidth;
  const yFor = (value) => padTop + plotHeight - (value / max) * plotHeight;

  const gridLines = [0, 0.25, 0.5, 0.75, 1].map((fraction) => ({
    y: padTop + plotHeight - fraction * plotHeight,
    label: Math.round(max * fraction),
  }));

  // Label roughly six x-positions regardless of series length, so a 30-day
  // window doesn't render 30 overlapping dates.
  const labelEvery = Math.max(1, Math.round(count / 6));

  return (
    <svg
      viewBox={`0 0 ${width} ${height}`}
      className="w-full"
      style={{ height }}
      role="img"
      aria-label={`Trend chart of ${withData.map((s) => s.name).join(', ')}`}
    >
      <defs>
        {withData.map((s) => (
          <linearGradient key={s.name} id={`fill-${slug(s.name)}`} x1="0" y1="0" x2="0" y2="1">
            <stop offset="0%" stopColor={s.color} stopOpacity="0.28" />
            <stop offset="100%" stopColor={s.color} stopOpacity="0" />
          </linearGradient>
        ))}
      </defs>

      {gridLines.map((line) => (
        <g key={line.label}>
          <line x1={padLeft} x2={width - padRight} y1={line.y} y2={line.y} stroke={chart.grid} strokeWidth="1" />
          <text
            x={padLeft - 8} y={line.y + 4}
            textAnchor="end" fontSize="11" fill={chart.textDim}
            style={{ fontVariantNumeric: 'tabular-nums' }}
          >
            {line.label}
          </text>
        </g>
      ))}

      {withData[0].points.map((point, index) =>
        index % labelEvery === 0 ? (
          <text
            key={point.date}
            x={xFor(index)} y={height - 12}
            textAnchor="middle" fontSize="11" fill={chart.textDim}
          >
            {shortDate(point.date)}
          </text>
        ) : null
      )}

      {withData.map((s) => {
        const coords = s.points.map((p, i) => [xFor(i), yFor(p.count)]);
        const line = smoothPath(coords);

        return (
          <g key={s.name}>
            {s.area && (
              <path
                d={`${line} L ${coords[coords.length - 1][0]} ${padTop + plotHeight} L ${coords[0][0]} ${padTop + plotHeight} Z`}
                fill={`url(#fill-${slug(s.name)})`}
              />
            )}
            <path
              d={line}
              fill="none"
              stroke={s.color}
              strokeWidth="2.5"
              strokeLinecap="round"
              strokeDasharray={s.dashed ? '7 5' : undefined}
            />
            {/* Emphasise the latest reading - it is the one people look for. */}
            <circle
              cx={coords[coords.length - 1][0]}
              cy={coords[coords.length - 1][1]}
              r="4"
              fill={s.color}
              stroke={chart.surface}
              strokeWidth="2"
            />
          </g>
        );
      })}
    </svg>
  );
}

/**
 * smoothPath converts points into a cubic Bezier path using Catmull-Rom
 * control points, so the curve passes through every point.
 */
function smoothPath(coords) {
  if (coords.length < 2) return '';

  let path = `M ${coords[0][0]} ${coords[0][1]}`;

  for (let i = 0; i < coords.length - 1; i++) {
    const previous = coords[i - 1] || coords[i];
    const current = coords[i];
    const next = coords[i + 1];
    const after = coords[i + 2] || next;

    // Tension of 6 keeps the curve gentle; lower values overshoot on spiky
    // series and can dip a line below zero.
    const cp1x = current[0] + (next[0] - previous[0]) / 6;
    const cp1y = current[1] + (next[1] - previous[1]) / 6;
    const cp2x = next[0] - (after[0] - current[0]) / 6;
    const cp2y = next[1] - (after[1] - current[1]) / 6;

    path += ` C ${cp1x} ${cp1y}, ${cp2x} ${cp2y}, ${next[0]} ${next[1]}`;
  }

  return path;
}

function slug(name) {
  return name.toLowerCase().replace(/[^a-z0-9]+/g, '-');
}

/** Legend renders the series key beneath a TrendChart. */
export function Legend({ series }) {
  return (
    <div className="flex flex-wrap items-center gap-4">
      {series.map((s) => (
        <div key={s.name} className="flex items-center gap-2">
          <svg width="18" height="8" aria-hidden="true">
            <line
              x1="0" y1="4" x2="18" y2="4"
              stroke={s.color} strokeWidth="2.5" strokeLinecap="round"
              strokeDasharray={s.dashed ? '5 4' : undefined}
            />
          </svg>
          <span className="text-body-sm text-on-surface-variant">{s.name}</span>
        </div>
      ))}
    </div>
  );
}
