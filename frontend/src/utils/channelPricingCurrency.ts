/** Channel prices stay unchanged; these switches only select displayed symbols. */
export interface PlatformPricingCurrency {
  official_cny: boolean
  discounted_cny: boolean
}

export function pricingCurrencyFromConfig(config: Record<string, unknown> | undefined, platform: string): PlatformPricingCurrency {
  const all = config?.pricing_currency
  const value = all && typeof all === 'object' && !Array.isArray(all)
    ? (all as Record<string, unknown>)[platform] : undefined
  const settings = value && typeof value === 'object' && !Array.isArray(value)
    ? value as Record<string, unknown> : {}
  return {
    official_cny: settings.official_cny === true,
    discounted_cny: settings.discounted_cny === true,
  }
}
