import { describe, expect, it } from 'vitest'

import type { Account } from '@/types'
import { accountQuotaBar, accountQuotaBars } from '../accountQuota'

const account = (overrides: Partial<Account> = {}) => ({ id: 1, type: 'apikey', extra: {}, ...overrides }) as Account

describe('accountQuota', () => {
  it('returns only the windows that have a limit, in daily, weekly, total order', () => {
    const bars = accountQuotaBars(account({
      quota_limit: 500,
      quota_used: 470,
      quota_daily_limit: 20,
      quota_daily_used: 13.2,
      quota_weekly_limit: 0,
      quota_weekly_used: 99
    }))

    expect(bars.map((bar) => bar.period)).toEqual(['daily', 'total'])
    expect(bars[0]).toMatchObject({ used: 13.2, limit: 20, resetsAt: null })
    expect(bars[0].utilization).toBeCloseTo(66)
    expect(bars[1]).toMatchObject({ used: 470, limit: 500, utilization: 94, resetsAt: null })
    expect(accountQuotaBars(account())).toEqual([])
  })

  it('ends a rolling window one period after it started and takes a fixed window from extra', () => {
    const rolling = account({
      quota_daily_limit: 10,
      quota_weekly_limit: 10,
      extra: { quota_daily_start: '2026-11-08T00:00:00Z', quota_weekly_start: '2026-11-02T00:00:00Z' }
    })
    expect(accountQuotaBar(rolling, 'daily')?.resetsAt).toBe('2026-11-09T00:00:00.000Z')
    expect(accountQuotaBar(rolling, 'weekly')?.resetsAt).toBe('2026-11-09T00:00:00.000Z')

    const fixed = account({
      quota_daily_limit: 10,
      extra: { quota_daily_reset_mode: 'fixed', quota_daily_reset_at: '2026-11-08T16:00:00Z', quota_daily_start: '2026-11-01T00:00:00Z' }
    })
    expect(accountQuotaBar(fixed, 'daily')?.resetsAt).toBe('2026-11-08T16:00:00Z')
  })

  it('leaves the reset unknown for a missing or unreadable window start', () => {
    expect(accountQuotaBar(account({ quota_daily_limit: 10 }), 'daily')?.resetsAt).toBeNull()
    expect(accountQuotaBar(account({ quota_daily_limit: 10, extra: { quota_daily_start: 'not a date' } }), 'daily')?.resetsAt).toBeNull()
  })
})
