/** Channel prices and balances are persisted in USD. CNY is an input/display unit. */
export interface PlatformPricingCurrency {
  official_cny: boolean
  discounted_cny: boolean
  cny_per_usd: number
}

export const DEFAULT_CNY_PER_USD = 7.15

export function pricingCurrencyFromConfig(config: Record<string, unknown> | undefined, platform: string): PlatformPricingCurrency {
  const all = config?.pricing_currency
  const value = all && typeof all === 'object' && !Array.isArray(all)
    ? (all as Record<string, unknown>)[platform] : undefined
  const settings = value && typeof value === 'object' && !Array.isArray(value)
    ? value as Record<string, unknown> : {}
  const rate = Number(settings.cny_per_usd)
  return {
    official_cny: settings.official_cny === true,
    discounted_cny: settings.discounted_cny === true,
    cny_per_usd: Number.isFinite(rate) && rate > 0 ? rate : DEFAULT_CNY_PER_USD,
  }
}

export function displayPrice(value: number | null | undefined, cny: boolean, rate: number): number | null {
  if (value == null) return null
  return Number((value * (cny ? rate : 1)).toPrecision(12))
}

export function usdPrice(value: number | string | null | undefined, cny: boolean, rate: number): number | null {
  if (value === null || value === undefined || value === '') return null
  const amount = Number(value)
  if (!Number.isFinite(amount) || amount < 0) return null
  return Number((amount / (cny ? rate : 1)).toPrecision(12))
}
