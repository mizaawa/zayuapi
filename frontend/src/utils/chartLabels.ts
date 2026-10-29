/**
 * Trend endpoints label buckets as "YYYY-MM-DD" (daily) or "YYYY-MM-DD HH:mm"
 * (hourly). Axis ticks only need the part that changes between neighbours, so
 * hourly ticks keep the clock and daily ticks drop the year; tooltips still
 * show the full label.
 */
export function shortenTrendLabel(label: string): string {
  const hourly = /^\d{4}-\d{2}-\d{2} (\d{2}:\d{2})/.exec(label)
  if (hourly) return hourly[1]
  const daily = /^\d{4}-(\d{2}-\d{2})$/.exec(label)
  return daily ? daily[1] : label
}

/** Chart.js tick callback that shortens category labels with shortenTrendLabel. */
export function trendTickCallback(
  this: { getLabelForValue: (value: number) => string | number },
  value: string | number
): string {
  return shortenTrendLabel(String(this.getLabelForValue(Number(value))))
}
