import { chart, severityFor } from './palette';

/**
 * Compact inline trace of a daily series, for table rows and channel cards.
 * No axes or labels - it exists to show shape, not values.
 */
export function Sparkline({ points = [], width = 96, height = 26, color = chart.primaryDim }) {
  if (points.length < 2) {
    return <div style={{ width, height }} aria-hidden="true" />;
  }

  const counts = points.map((p) => p.count);
  const peak = Math.max(...counts, 1);
  const step = width / (counts.length - 1);

  const coords = counts.map((count, index) => [
    index * step,
    // Inset by 2px top and bottom so a peak or a zero isn't clipped by the
    // viewBox edge.
    height - 2 - (count / peak) * (height - 4),
  ]);

  const line = coords.map(([x, y], i) => `${i === 0 ? 'M' : 'L'} ${x} ${y}`).join(' ');
  const [lastX, lastY] = coords[coords.length - 1];

  return (
    <svg
      width={width} height={height}
      viewBox={`0 0 ${width} ${height}`}
      role="img"
      aria-label={`Trend: ${counts.join(', ')}`}
    >
      <path d={line} fill="none" stroke={color} strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" />
      <circle cx={lastX} cy={lastY} r="2.5" fill={color} />
    </svg>
  );
}

/**
 * Horizontal proportion bar for a success rate, coloured by severity so the
 * number and the bar carry the same signal.
 */
export function SuccessBar({ rate = 0, showLabel = true }) {
  const clamped = Math.max(0, Math.min(100, rate));
  const colour = severityFor(clamped);

  return (
    <div className="w-full">
      {showLabel && (
        <div className="mb-1 flex items-center justify-between">
          <span className="text-label-md text-on-surface-variant">Success Rate</span>
          <span className="text-label-md tabular" style={{ color: colour }}>
            {clamped.toFixed(1)}%
          </span>
        </div>
      )}
      <div
        className="h-1.5 w-full overflow-hidden rounded-full bg-surface-container-highest"
        role="meter"
        aria-valuenow={clamped}
        aria-valuemin={0}
        aria-valuemax={100}
        aria-label="Publish success rate"
      >
        <div
          className="h-full rounded-full transition-[width] duration-500"
          style={{ width: `${clamped}%`, background: colour }}
        />
      </div>
    </div>
  );
}
