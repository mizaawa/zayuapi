import { describe, expect, it } from 'vitest'
import { compareProfit, dateRange, formatMoney, profitOf, shiftDate, supportedTimeZone, zonedDate, zonedDayStart } from '../operationsUtils'

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

  it('counts days in the site timezone from its 00:00', () => {
    // 2026-11-09 17:30 UTC is already 2026-11-10 01:30 in Shanghai.
    const instant = new Date(Date.UTC(2026, 10, 9, 17, 30))
    expect(zonedDate(instant, 'Asia/Shanghai')).toBe('2026-11-10')
    expect(zonedDate(instant, 'America/Los_Angeles')).toBe('2026-11-09')
    expect(dateRange(1, instant, 'Asia/Shanghai')).toEqual({ start: '2026-11-10', end: '2026-11-10' })
    expect(dateRange(7, instant, 'Asia/Shanghai')).toEqual({ start: '2026-11-04', end: '2026-11-10' })
    expect(zonedDayStart('2026-11-10', 'Asia/Shanghai').toISOString()).toBe('2026-11-09T16:00:00.000Z')
    expect(zonedDayStart('2026-11-10', 'UTC').toISOString()).toBe('2026-11-10T00:00:00.000Z')
  })

  it('finds midnight across daylight-saving changes and rejects bad input', () => {
    expect(zonedDayStart('2026-03-08', 'America/New_York').toISOString()).toBe('2026-03-08T05:00:00.000Z')
    expect(zonedDayStart('2026-03-09', 'America/New_York').toISOString()).toBe('2026-03-09T04:00:00.000Z')
    expect(zonedDayStart('2026-11-01', 'America/New_York').toISOString()).toBe('2026-11-01T04:00:00.000Z')
    expect(Number.isNaN(zonedDayStart('not-a-date', 'Asia/Shanghai').getTime())).toBe(true)
    expect(shiftDate('2026-12-31', 1)).toBe('2027-01-01')
    expect(shiftDate('2026-03-01', -1)).toBe('2026-02-28')
    expect(shiftDate('not-a-date', 1)).toBe('not-a-date')
    expect(supportedTimeZone('Asia/Shanghai')).toBe('Asia/Shanghai')
    expect(supportedTimeZone('Mars/Olympus')).toBeUndefined()
    expect(supportedTimeZone('')).toBeUndefined()
  })

  it('retains small costs and distinguishes currencies', () => {
    expect(formatMoney(0.0012)).toBe('$0.0012')
    expect(formatMoney(24, 'CNY')).toContain('24.00')
    expect(formatMoney(24, 'CNY')).not.toBe(formatMoney(24, 'USD'))
  })
})
