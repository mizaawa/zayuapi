import type { Account } from '@/types'

export type AccountQuotaPeriod = 'daily' | 'weekly' | 'total'

export interface AccountQuotaBar {
  period: AccountQuotaPeriod
  used: number
  limit: number
  /** Percent of the limit used; may exceed 100. */
  utilization: number
  resetsAt: string | null
}

const DAY_MS = 24 * 60 * 60 * 1000

// A fixed window carries its precomputed next reset; a rolling window ends one
// period after it started.
function quotaResetsAt(account: Account, period: 'daily' | 'weekly'): string | null {
  const extra = account.extra as Record<string, unknown> | undefined
  const mode = (extra?.[`quota_${period}_reset_mode`] as string) || 'rolling'
  if (mode === 'fixed') return (extra?.[`quota_${period}_reset_at`] as string) || null
  const start = extra?.[`quota_${period}_start`]
  if (typeof start !== 'string' || !start) return null
  const startMs = new Date(start).getTime()
  if (!Number.isFinite(startMs)) return null
  return new Date(startMs + (period === 'daily' ? DAY_MS : 7 * DAY_MS)).toISOString()
}

/** The account's configured spending quota for one window, if any. */
export function accountQuotaBar(account: Account, period: AccountQuotaPeriod): AccountQuotaBar | null {
  const limit = Number(
    period === 'daily' ? account.quota_daily_limit : period === 'weekly' ? account.quota_weekly_limit : account.quota_limit
  ) || 0
  if (limit <= 0) return null
  const used = Number(
    period === 'daily' ? account.quota_daily_used : period === 'weekly' ? account.quota_weekly_used : account.quota_used
  ) || 0
  return {
    period,
    used,
    limit,
    utilization: (used / limit) * 100,
    resetsAt: period === 'total' ? null : quotaResetsAt(account, period)
  }
}

export function accountQuotaBars(account: Account): AccountQuotaBar[] {
  return (['daily', 'weekly', 'total'] as const)
    .map((period) => accountQuotaBar(account, period))
    .filter((bar): bar is AccountQuotaBar => bar !== null)
}
