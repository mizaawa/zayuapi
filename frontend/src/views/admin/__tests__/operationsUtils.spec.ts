import { describe, expect, it } from 'vitest'
import { compareProfit, dateRange, formatMoney, profitOf } from '../operationsUtils'

describe('operations financial presentation', () => {
  it('orders largest losses first and then largest profits, with zero last', () => {
    const rows = [-1, 4, -20, 0, 12].map(profit => ({ user_cost: 30 + profit, upstream_cost: 30 }))
    expect(rows.sort(compareProfit).map(profitOf)).toEqual([-20, -1, 12, 4, 0])
  })

  it('includes today in each date range across month boundaries', () => {
    const today = new Date(2026, 10, 2, 12)
    expect(dateRange(1, today)).toEqual({ start: '2026-11-02', end: '2026-11-02' })
    expect(dateRange(7, today)).toEqual({ start: '2026-10-27', end: '2026-11-02' })
    expect(dateRange(30, today)).toEqual({ start: '2026-10-04', end: '2026-11-02' })
  })

  it('retains small costs and distinguishes currencies', () => {
    expect(formatMoney(0.0012)).toBe('$0.0012')
    expect(formatMoney(24, 'CNY')).toContain('24.00')
    expect(formatMoney(24, 'CNY')).not.toBe(formatMoney(24, 'USD'))
  })
})
