import type { UserPricingInterval, UserSupportedModelPricing } from '@/api/channels'

// A partial interval overrides only the fields configured on it. Never borrow
// prices from another card (channel original and group/paid cards are separate).
export function tokenPricingIntervals(pricing: (Pick<UserSupportedModelPricing, 'input_price' | 'output_price' | 'cache_write_price' | 'cache_read_price'> & Partial<UserSupportedModelPricing>) | null): UserPricingInterval[] {
  if (!pricing?.intervals?.length) return []
  const intervals = [...pricing.intervals].sort((a, b) => a.min_tokens - b.min_tokens).map((iv) => ({
    ...iv,
    input_price: iv.input_price ?? pricing.input_price,
    output_price: iv.output_price ?? pricing.output_price,
    cache_write_price: iv.cache_write_price ?? pricing.cache_write_price,
    cache_read_price: iv.cache_read_price ?? pricing.cache_read_price
  }))
  if (intervals[0].min_tokens > 0) {
    intervals.unshift({
      min_tokens: 0,
      max_tokens: intervals[0].min_tokens,
      input_price: pricing.input_price,
      output_price: pricing.output_price,
      cache_write_price: pricing.cache_write_price,
      cache_read_price: pricing.cache_read_price,
      per_request_price: null
    })
  }
  return intervals
}

export function pricingTierLabel(iv: Pick<UserPricingInterval, 'min_tokens' | 'max_tokens' | 'tier_label'>): string {
  if (iv.tier_label) return iv.tier_label
  const { min_tokens: min, max_tokens: max } = iv
  if (max == null) return `>${formatTokenCount(min)}`
  if (min === 0) return `≤${formatTokenCount(max)}`
  return `${formatTokenCount(min)}–${formatTokenCount(max)}`
}

function formatTokenCount(n: number): string {
  if (n >= 1_000_000) return `${Math.round(n / 1_000_000 * 100) / 100}M`
  if (n >= 1_000) return `${Math.round(n / 1_000 * 100) / 100}K`
  return String(n)
}
