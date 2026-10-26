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

export function dateRange(days: number, now = new Date()): { start: string; end: string } {
  const start = new Date(now)
  start.setDate(start.getDate() - days + 1)
  return { start: localDate(start), end: localDate(now) }
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
