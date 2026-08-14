// Chart colours, mirroring the design-system tokens in tailwind.config.js.
//
// SVG attributes can't read Tailwind classes for every case (gradient stops
// and interpolated fills in particular), so the values live here rather than
// being scattered through the chart components. Changing a token means
// changing it in two places - this file and the Tailwind config - which is the
// price of not shipping a charting library.
export const chart = {
  primary: '#c0c1ff',
  primaryDim: '#8083ff',
  success: '#4edea3',
  warning: '#ffb95f',
  error: '#ffb4ab',
  grid: '#2a2a2c',
  axis: '#464554',
  text: '#c7c4d7',
  textDim: '#908fa0',
  surface: '#201f22',
};

// severityFor maps a success percentage to the semantic colour the dashboard
// uses for rate indicators.
export function severityFor(rate) {
  if (rate >= 98) return chart.success;
  if (rate >= 90) return chart.warning;
  return chart.error;
}

// niceMax rounds a maximum up to a readable axis bound, so the y-axis reads
// 0/50/100 rather than 0/37/74.
export function niceMax(value) {
  if (value <= 0) return 1;
  const magnitude = Math.pow(10, Math.floor(Math.log10(value)));
  const normalised = value / magnitude;
  const step = normalised <= 1 ? 1 : normalised <= 2 ? 2 : normalised <= 5 ? 5 : 10;
  return step * magnitude;
}

// shortDate turns "2026-08-14" into "Aug 14" for axis labels.
export function shortDate(iso) {
  const [, month, day] = iso.split('-');
  const months = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec'];
  return `${months[Number(month) - 1]} ${Number(day)}`;
}

// weekday turns an ISO date into "Mon", for the 7-day view where the day of
// week is more legible than the date.
export function weekday(iso) {
  const days = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'];
  return days[new Date(`${iso}T00:00:00`).getDay()];
}
