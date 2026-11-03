import type { GroupPlatform, SubscriptionType } from '@/types'
import { platformLabel } from '@/utils/platformColors'

/** One group the user can bind an API key to. */
export interface KeyGroupOption {
  value: number
  label: string
  description: string | null
  rate: number
  userRate: number | null
  peakRateEnabled: boolean
  peakStart: string
  peakEnd: string
  peakRateMultiplier: number
  subscriptionType: SubscriptionType
  platform: GroupPlatform
  blocked?: boolean
}

export type KeyGroupPlatformFilter = 'all' | GroupPlatform

/**
 * The five platforms groups are created on, in the admin group list's order.
 * Custom groups still travel as the legacy `composite` value.
 */
export const KEY_GROUP_PLATFORMS: readonly GroupPlatform[] = ['anthropic', 'openai', 'gemini', 'grok', 'composite']

/** `custom` and `composite` are the same Custom platform, so they share one filter. */
export function keyGroupPlatform(platform: GroupPlatform | null | undefined): GroupPlatform {
  if (!platform) return 'anthropic'
  return platform === 'custom' ? 'composite' : platform
}

/**
 * Filter chips: always the five platforms, then any older platform that still
 * has groups (e.g. antigravity) so every group stays reachable by platform.
 */
export function keyGroupPlatformChips(options: readonly KeyGroupOption[]): GroupPlatform[] {
  const chips = [...KEY_GROUP_PLATFORMS]
  for (const option of options) {
    const platform = keyGroupPlatform(option.platform)
    if (!chips.includes(platform)) chips.push(platform)
  }
  return chips
}

/**
 * Every whitespace-separated term has to appear in the group's name,
 * description or platform name, so "max 零注入" narrows instead of widening.
 */
export function matchesKeyGroupQuery(option: KeyGroupOption, query: string): boolean {
  const terms = query.toLowerCase().split(/\s+/).filter(Boolean)
  if (terms.length === 0) return true
  const haystack = [option.label, option.description ?? '', platformLabel(keyGroupPlatform(option.platform))]
    .join('\n')
    .toLowerCase()
  return terms.every((term) => haystack.includes(term))
}

export function matchesKeyGroupPlatform(option: KeyGroupOption, filter: KeyGroupPlatformFilter): boolean {
  return filter === 'all' || keyGroupPlatform(option.platform) === filter
}

/** Matches per platform for the current search, so each chip tells where the results are. */
export function countKeyGroupsByPlatform(options: readonly KeyGroupOption[]): Map<GroupPlatform, number> {
  const counts = new Map<GroupPlatform, number>()
  for (const option of options) {
    const platform = keyGroupPlatform(option.platform)
    counts.set(platform, (counts.get(platform) ?? 0) + 1)
  }
  return counts
}
