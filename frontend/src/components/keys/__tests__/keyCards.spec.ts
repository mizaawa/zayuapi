import { beforeEach, describe, expect, it } from 'vitest'

import type { ApiKey } from '@/types'
import {
  DEFAULT_KEY_SORT,
  KEY_CARD_FIELDS_STORAGE_KEY,
  KEY_SORT_STORAGE_KEY,
  daysUntil,
  expiryState,
  formatKeyCost,
  formatResetCountdown,
  isFailoverActive,
  loadHiddenKeyCardFields,
  loadKeySort,
  nextKeyClockTick,
  rateLimitWindows,
  saveHiddenKeyCardFields,
  saveKeySort,
  supportsFailover,
  usagePercent,
  usageTone
} from '../keyCards'

const MINUTE = 60_000
const DAY = 86_400_000
const NOW = Date.parse('2026-11-04T08:30:15.000Z')

const key = (overrides: Partial<ApiKey> = {}): ApiKey => ({
  id: 1,
  user_id: 1,
  key: 'sk-test',
  name: 'test',
  group_id: null,
  status: 'active',
  ip_whitelist: [],
  ip_blacklist: [],
  last_used_at: null,
  last_used_ip: null,
  quota: 0,
  quota_used: 0,
  expires_at: null,
  created_at: '2026-10-01T00:00:00Z',
  updated_at: '2026-10-01T00:00:00Z',
  current_concurrency: 0,
  rate_limit_5h: 0,
  rate_limit_1d: 0,
  rate_limit_7d: 0,
  usage_5h: 0,
  usage_1d: 0,
  usage_7d: 0,
  window_5h_start: null,
  window_1d_start: null,
  window_7d_start: null,
  reset_5h_at: null,
  reset_1d_at: null,
  reset_7d_at: null,
  ...overrides
})

describe('key card field preferences', () => {
  beforeEach(() => localStorage.clear())

  it('starts with the ID and last used IP hidden', () => {
    expect([...loadHiddenKeyCardFields()]).toEqual(['id', 'last_used_ip'])
  })

  it('round-trips saved fields and drops unknown names', () => {
    saveHiddenKeyCardFields(['tokens', 'created_at'])
    expect([...loadHiddenKeyCardFields()]).toEqual(['tokens', 'created_at'])

    localStorage.setItem(KEY_CARD_FIELDS_STORAGE_KEY, JSON.stringify(['tokens', 'name', 42, 'actions']))
    expect([...loadHiddenKeyCardFields()]).toEqual(['tokens'])
  })

  it('keeps an empty selection instead of falling back to the defaults', () => {
    saveHiddenKeyCardFields([])
    expect(loadHiddenKeyCardFields().size).toBe(0)
  })

  it('falls back to the defaults on unreadable data', () => {
    localStorage.setItem(KEY_CARD_FIELDS_STORAGE_KEY, '{not json')
    expect([...loadHiddenKeyCardFields()]).toEqual(['id', 'last_used_ip'])
  })

  it('removes the column settings of the old table layout', () => {
    localStorage.setItem('api-key-hidden-columns', JSON.stringify(['group']))
    localStorage.setItem('api-key-column-settings-version', '3')
    loadHiddenKeyCardFields()
    expect(localStorage.getItem('api-key-hidden-columns')).toBeNull()
    expect(localStorage.getItem('api-key-column-settings-version')).toBeNull()
  })
})

describe('key sort preference', () => {
  beforeEach(() => localStorage.clear())

  it('defaults to the newest keys first', () => {
    expect(loadKeySort()).toEqual({ sort_by: 'created_at', sort_order: 'desc' })
    expect(DEFAULT_KEY_SORT).toEqual({ sort_by: 'created_at', sort_order: 'desc' })
  })

  it('restores a saved sort and rejects fields the server cannot sort by', () => {
    saveKeySort({ sort_by: 'current_concurrency', sort_order: 'asc' })
    expect(loadKeySort()).toEqual({ sort_by: 'current_concurrency', sort_order: 'asc' })

    localStorage.setItem(KEY_SORT_STORAGE_KEY, JSON.stringify({ sort_by: 'usage', sort_order: 'asc' }))
    expect(loadKeySort()).toEqual({ sort_by: 'created_at', sort_order: 'desc' })
    localStorage.setItem(KEY_SORT_STORAGE_KEY, JSON.stringify({ sort_by: 'name', sort_order: 'sideways' }))
    expect(loadKeySort()).toEqual({ sort_by: 'created_at', sort_order: 'desc' })
  })
})

describe('key card values', () => {
  it('keeps four decimals for small spend and groups large spend', () => {
    expect(formatKeyCost(0)).toBe('$0.0000')
    expect(formatKeyCost(12.4821)).toBe('$12.4821')
    expect(formatKeyCost(999.99994)).toBe('$999.9999')
    expect(formatKeyCost(1234.5678)).toBe('$1,234.57')
    expect(formatKeyCost(98765.4321)).toBe('$98,765.43')
    expect(formatKeyCost(Number.NaN)).toBe('$0.0000')
    expect(formatKeyCost(undefined)).toBe('$0.0000')
  })

  it('counts down to a rate-limit reset', () => {
    const at = (offset: number) => new Date(NOW + offset).toISOString()
    expect(formatResetCountdown(null, NOW, 'now')).toBe('')
    expect(formatResetCountdown(at(-1000), NOW, 'now')).toBe('now')
    expect(formatResetCountdown(at(45 * MINUTE), NOW, 'now')).toBe('45m')
    expect(formatResetCountdown(at(2 * 3_600_000 + 12 * MINUTE), NOW, 'now')).toBe('2h 12m')
    expect(formatResetCountdown(at(3 * DAY + 4 * 3_600_000), NOW, 'now')).toBe('3d 4h')
    expect(formatResetCountdown('not a date', NOW, 'now')).toBe('')
  })

  it('warns a week ahead of expiry', () => {
    const at = (offset: number) => new Date(NOW + offset).toISOString()
    expect(expiryState(at(-1), NOW)).toBe('expired')
    expect(expiryState(at(3 * DAY), NOW)).toBe('soon')
    expect(expiryState(at(7 * DAY), NOW)).toBe('soon')
    expect(expiryState(at(8 * DAY), NOW)).toBe('normal')
    expect(daysUntil(at(2.5 * DAY), NOW)).toBe(3)
    expect(daysUntil(at(-DAY), NOW)).toBe(0)
  })

  it('lists only configured rate-limit windows', () => {
    const windows = rateLimitWindows(key({ rate_limit_5h: 5, usage_5h: 4, rate_limit_7d: 100, usage_7d: 100, reset_7d_at: '2026-11-06T00:00:00Z' }))
    expect(windows.map((window) => window.key)).toEqual(['5h', '7d'])
    expect(windows[1]).toEqual({ key: '7d', used: 100, limit: 100, resetAt: '2026-11-06T00:00:00Z' })
  })

  it('grades usage against a limit', () => {
    expect(usagePercent(25, 100)).toBe(25)
    expect(usagePercent(150, 100)).toBe(100)
    expect(usagePercent(5, 0)).toBe(0)
    expect(usageTone(79, 100)).toBe('normal')
    expect(usageTone(80, 100)).toBe('warning')
    expect(usageTone(100, 100)).toBe('danger')
    expect(usageTone(null, 100)).toBe('normal')
  })

  it('only offers failover outside the custom wire platforms', () => {
    expect(supportsFailover(undefined)).toBe(false)
    expect(supportsFailover({ platform: 'openai' } as never)).toBe(true)
    expect(supportsFailover({ platform: 'custom' } as never)).toBe(false)
    expect(supportsFailover({ platform: 'composite' } as never)).toBe(false)
  })

  it('treats a failover as active until its cooldown passes', () => {
    const cooling = key({ failover_enabled: true, failover_cooldown_until: new Date(NOW + 2000).toISOString() })
    expect(isFailoverActive(cooling, NOW)).toBe(true)
    expect(isFailoverActive(cooling, NOW + 2000)).toBe(false)
    expect(isFailoverActive({ ...cooling, failover_enabled: false }, NOW)).toBe(false)
  })
})

describe('nextKeyClockTick', () => {
  const nextMinute = Date.parse('2026-11-04T08:31:00.000Z')

  it('waits for the next minute when nothing changes sooner', () => {
    expect(nextKeyClockTick([], NOW)).toBe(nextMinute)
    expect(nextKeyClockTick([key({ expires_at: new Date(NOW + 5 * DAY).toISOString() })], NOW)).toBe(nextMinute)
  })

  it('wakes exactly when a cooldown, expiry or reset passes within the minute', () => {
    const cooldown = NOW + 2000
    const reset = NOW + 9000
    const keys = [
      key({ id: 1, failover_enabled: true, failover_cooldown_until: new Date(cooldown).toISOString() }),
      key({ id: 2, reset_5h_at: new Date(reset).toISOString() })
    ]
    expect(nextKeyClockTick(keys, NOW)).toBe(cooldown)
    expect(nextKeyClockTick([keys[1]], NOW)).toBe(reset)
    expect(nextKeyClockTick([key({ expires_at: new Date(NOW + 30_000).toISOString() })], NOW)).toBe(NOW + 30_000)
  })

  it('ignores moments that already passed or cannot be read', () => {
    const keys = [
      key({ failover_cooldown_until: new Date(NOW - 1000).toISOString(), reset_1d_at: 'garbage', reset_7d_at: new Date(NOW).toISOString() })
    ]
    expect(nextKeyClockTick(keys, NOW)).toBe(nextMinute)
  })
})
