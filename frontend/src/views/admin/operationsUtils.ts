export interface ProfitAmounts { user_cost: number; upstream_cost: number }

export const profitOf = (row: ProfitAmounts): number => row.user_cost - row.upstream_cost

// Put the largest losses first, followed by the largest profits.
export function compareProfit(a: ProfitAmounts, b: ProfitAmounts): number {
  const aProfit = profitOf(a)
  const bProfit = profitOf(b)
  if ((aProfit < 0) !== (bProfit < 0)) return aProfit < 0 ? -1 : 1
  return aProfit < 0 ? aProfit - bProfit : bProfit - aProfit
}

export function localDate(date: Date): string {
  return `${date.getFullYear()}-${String(date.getMonth() + 1).padStart(2, '0')}-${String(date.getDate()).padStart(2, '0')}`
}

/** The IANA zone if this browser can format it, else undefined (the browser's own zone). */
export function supportedTimeZone(name?: string | null): string | undefined {
  if (!name) return undefined
  try {
    new Intl.DateTimeFormat('en-US', { timeZone: name })
    return name
  } catch {
    return undefined
  }
}

function zonedParts(instant: number, timeZone: string): Record<'year' | 'month' | 'day' | 'hour' | 'minute' | 'second', number> {
  const parts = { year: 0, month: 0, day: 0, hour: 0, minute: 0, second: 0 }
  const format = new Intl.DateTimeFormat('en-US', { timeZone, hourCycle: 'h23', year: 'numeric', month: 'numeric', day: 'numeric', hour: 'numeric', minute: 'numeric', second: 'numeric' })
  for (const part of format.formatToParts(new Date(instant))) {
    if (part.type in parts) parts[part.type as keyof typeof parts] = Number(part.value)
  }
  return parts
}

/** Calendar date (YYYY-MM-DD) of `now` in `timeZone`, or in the browser's zone. */
export function zonedDate(now: Date, timeZone?: string): string {
  if (!timeZone) return localDate(now)
  const { year, month, day } = zonedParts(now.getTime(), timeZone)
  return `${year}-${String(month).padStart(2, '0')}-${String(day).padStart(2, '0')}`
}

/** Moves a YYYY-MM-DD date by whole days; malformed input comes back unchanged. */
export function shiftDate(date: string, days: number): string {
  const [year, month, day] = date.split('-').map(Number)
  const shifted = new Date(Date.UTC(year, month - 1, day + days))
  return Number.isNaN(shifted.getTime()) ? date : shifted.toISOString().slice(0, 10)
}

/** The instant a YYYY-MM-DD day starts (00:00) in `timeZone`, or in the browser's zone. */
export function zonedDayStart(date: string, timeZone?: string): Date {
  const [year, month, day] = date.split('-').map(Number)
  if (!timeZone) return new Date(year, month - 1, day)
  const wall = Date.UTC(year, month - 1, day)
  if (Number.isNaN(wall)) return new Date(NaN)
  const offset = (instant: number) => {
    const p = zonedParts(instant, timeZone)
    return Date.UTC(p.year, p.month - 1, p.day, p.hour, p.minute, p.second) - Math.floor(instant / 1000) * 1000
  }
  // Take the zone's offset at the guess, then once more in case midnight sits
  // next to a daylight-saving change.
  const guess = wall - offset(wall)
  return new Date(wall - offset(guess))
}

/** The last `days` calendar days up to today, in `timeZone` or the browser's zone. */
export function dateRange(days: number, now = new Date(), timeZone?: string): { start: string; end: string } {
  const end = zonedDate(now, timeZone)
  return { start: shiftDate(end, 1 - days), end }
}

export function formatMoney(value: number, currency = 'USD'): string {
  const safeValue = Number.isFinite(value) ? value : 0
  try {
    return new Intl.NumberFormat('en-US', {
      style: 'currency', currency, minimumFractionDigits: 2,
      maximumFractionDigits: Math.abs(safeValue) > 0 && Math.abs(safeValue) < 0.01 ? 4 : 2
    }).format(safeValue)
  } catch {
    return `${currency} ${safeValue.toFixed(2)}`
  }
}
