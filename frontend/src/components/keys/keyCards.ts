import type { ApiKey, Group } from '@/types'

const MINUTE_MS = 60_000
const DAY_MS = 86_400_000

// 到期前这么多天内提醒用户续期
export const EXPIRY_WARNING_DAYS = 7

/** Card sections the user can hide from the "card fields" menu. Name, key, status, group and actions always show. */
export const KEY_CARD_FIELDS = [
  'id',
  'usage',
  'current_concurrency',
  'tokens',
  'rate_limit',
  'failover',
  'expires_at',
  'last_used_at',
  'last_used_ip',
  'created_at'
] as const
export type KeyCardField = (typeof KEY_CARD_FIELDS)[number]

export const DEFAULT_HIDDEN_KEY_CARD_FIELDS: readonly KeyCardField[] = ['id', 'last_used_ip']
export const KEY_CARD_FIELDS_STORAGE_KEY = 'api-key-card-hidden-fields'
// Column choices of the old table layout. Cards start from their own defaults,
// so these are dropped instead of carried over.
const LEGACY_COLUMN_STORAGE_KEYS = ['api-key-hidden-columns', 'api-key-column-settings-version']

const isKeyCardField = (value: unknown): value is KeyCardField =>
  typeof value === 'string' && (KEY_CARD_FIELDS as readonly string[]).includes(value)

export function loadHiddenKeyCardFields(storage: Storage = localStorage): Set<KeyCardField> {
  try {
    for (const key of LEGACY_COLUMN_STORAGE_KEYS) storage.removeItem(key)
    const saved = storage.getItem(KEY_CARD_FIELDS_STORAGE_KEY)
    if (saved !== null) {
      const parsed: unknown = JSON.parse(saved)
      if (Array.isArray(parsed)) return new Set(parsed.filter(isKeyCardField))
    }
  } catch (error) {
    console.error('Failed to load API key card fields:', error)
  }
  return new Set(DEFAULT_HIDDEN_KEY_CARD_FIELDS)
}

export function saveHiddenKeyCardFields(fields: Iterable<KeyCardField>, storage: Storage = localStorage) {
  try {
    storage.setItem(KEY_CARD_FIELDS_STORAGE_KEY, JSON.stringify([...fields]))
  } catch (error) {
    console.error('Failed to save API key card fields:', error)
  }
}

/** Server-side sort fields, each with the direction people usually want first. */
export const KEY_SORT_FIELDS = {
  created_at: 'desc',
  name: 'asc',
  last_used_at: 'desc',
  expires_at: 'asc',
  current_concurrency: 'desc',
  status: 'asc',
  id: 'desc'
} as const satisfies Record<string, KeySortOrder>
export type KeySortField = keyof typeof KEY_SORT_FIELDS
export type KeySortOrder = 'asc' | 'desc'
export interface KeySort {
  sort_by: KeySortField
  sort_order: KeySortOrder
}

export const DEFAULT_KEY_SORT: KeySort = { sort_by: 'created_at', sort_order: 'desc' }
export const KEY_SORT_STORAGE_KEY = 'api-key-sort'

export const isKeySortField = (value: unknown): value is KeySortField =>
  typeof value === 'string' && Object.prototype.hasOwnProperty.call(KEY_SORT_FIELDS, value)

export function loadKeySort(storage: Storage = localStorage): KeySort {
  try {
    const saved = storage.getItem(KEY_SORT_STORAGE_KEY)
    if (saved) {
      const parsed = JSON.parse(saved) as Partial<KeySort>
      if (isKeySortField(parsed.sort_by) && (parsed.sort_order === 'asc' || parsed.sort_order === 'desc')) {
        return { sort_by: parsed.sort_by, sort_order: parsed.sort_order }
      }
    }
  } catch (error) {
    console.error('Failed to load API key sort:', error)
  }
  return { ...DEFAULT_KEY_SORT }
}

export function saveKeySort(sort: KeySort, storage: Storage = localStorage) {
  try {
    storage.setItem(KEY_SORT_STORAGE_KEY, JSON.stringify(sort))
  } catch (error) {
    console.error('Failed to save API key sort:', error)
  }
}

export const supportsFailover = (group?: Group | null) =>
  Boolean(group && group.platform !== 'custom' && group.platform !== 'composite')

export const isFailoverActive = (key: ApiKey, now: number) => Boolean(
  key.failover_enabled && key.failover_cooldown_until &&
  new Date(key.failover_cooldown_until).getTime() > now
)

export const daysUntil = (date: string, now: number) =>
  Math.max(Math.ceil((new Date(date).getTime() - now) / DAY_MS), 0)

export function expiryState(date: string, now: number): 'expired' | 'soon' | 'normal' {
  const remaining = new Date(date).getTime() - now
  if (remaining <= 0) return 'expired'
  if (remaining <= EXPIRY_WARNING_DAYS * DAY_MS) return 'soon'
  return 'normal'
}

export interface RateLimitWindow {
  key: '5h' | '1d' | '7d'
  used: number
  limit: number
  resetAt: string | null
}

export const rateLimitWindows = (key: ApiKey): RateLimitWindow[] => ([
  { key: '5h', used: key.usage_5h ?? 0, limit: key.rate_limit_5h, resetAt: key.reset_5h_at },
  { key: '1d', used: key.usage_1d ?? 0, limit: key.rate_limit_1d, resetAt: key.reset_1d_at },
  { key: '7d', used: key.usage_7d ?? 0, limit: key.rate_limit_7d, resetAt: key.reset_7d_at }
] satisfies RateLimitWindow[]).filter((window) => window.limit > 0)

export const usagePercent = (used: number | null | undefined, limit: number) =>
  limit > 0 ? Math.min(((used ?? 0) / limit) * 100, 100) : 0

export function usageTone(used: number | null | undefined, limit: number): 'normal' | 'warning' | 'danger' {
  const ratio = limit > 0 ? (used ?? 0) / limit : 0
  if (ratio >= 1) return 'danger'
  if (ratio >= 0.8) return 'warning'
  return 'normal'
}

/** Time left until a rate-limit window resets: "3d 4h", "2h 12m", "45m"; resetNow once it has passed. */
export function formatResetCountdown(resetAt: string | null, now: number, resetNow: string): string {
  if (!resetAt) return ''
  const diff = new Date(resetAt).getTime() - now
  if (Number.isNaN(diff)) return ''
  if (diff <= 0) return resetNow
  const days = Math.floor(diff / DAY_MS)
  const hours = Math.floor((diff % DAY_MS) / 3_600_000)
  const mins = Math.floor((diff % 3_600_000) / MINUTE_MS)
  if (days > 0) return `${days}d ${hours}h`
  if (hours > 0) return `${hours}h ${mins}m`
  return `${mins}m`
}

/** Spend on a card: four decimals while small, two with grouping once it reaches four digits. */
export function formatKeyCost(amount: number | null | undefined): string {
  const value = Number(amount)
  if (!Number.isFinite(value)) return '$0.0000'
  if (Math.abs(value) >= 1000) {
    return `$${value.toLocaleString('en-US', { minimumFractionDigits: 2, maximumFractionDigits: 2 })}`
  }
  return `$${value.toFixed(4)}`
}

/**
 * When the cards next need a fresh clock: the moment a failover cooldown, an
 * expiry or a rate-limit reset passes, otherwise the next minute (countdowns
 * and "last used" times only show minutes).
 */
export function nextKeyClockTick(keys: readonly ApiKey[], now: number): number {
  let next = now - (now % MINUTE_MS) + MINUTE_MS
  for (const key of keys) {
    for (const stamp of [key.failover_cooldown_until, key.expires_at, key.reset_5h_at, key.reset_1d_at, key.reset_7d_at]) {
      const at = stamp ? new Date(stamp).getTime() : Number.NaN
      if (at > now && at < next) next = at
    }
  }
  return next
}
