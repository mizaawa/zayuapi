export const tokenFields = {
  input_price: 'inputPrice', output_price: 'outputPrice', cache_write_price: 'cacheWritePrice',
  cache_read_price: 'cacheReadPrice', image_input_price: 'imageInputPrice', image_output_price: 'imageTokenPrice'
} as const
export type TokenField = keyof typeof tokenFields
export const tokenFieldKeys = Object.keys(tokenFields) as TokenField[]
export type TokenPrices<T> = Record<TokenField, T>
export type TokenWindow<T = number | null> = { start: string; end: string } & TokenPrices<T>

export function mapTokenPrices<T, U>(values: TokenPrices<T>, convert: (value: T) => U): TokenPrices<U> {
  return Object.fromEntries(tokenFieldKeys.map(key => [key, convert(values[key])])) as TokenPrices<U>
}
const emptyPrices: TokenPrices<null> = { input_price: null, output_price: null, cache_read_price: null, cache_write_price: null, image_input_price: null, image_output_price: null }
export const emptyWindow = (): TokenWindow<null> => ({start: '', end: '', ...emptyPrices})

// Mirrors the backend: Shanghai civil time, inclusive start and exclusive end, overnight allowed.
export function validateTokenWindows(card: TokenPrices<number | string | null> & { time_windows?: TokenWindow<number | string | null>[] }): string | null {
  const set = (value: unknown) => value != null && value !== ''
  if (!set(card.input_price) || !set(card.output_price)) return 'fallbackRequired'
  const occupied = new Uint8Array(1440)
  const minute = (s: string) => /^(?:[01]\d|2[0-3]):[0-5]\d$/.test(s) ? Number(s.slice(0, 2)) * 60 + Number(s.slice(3)) : -1
  for (const w of card.time_windows || []) {
    const a = minute(w.start), b = minute(w.end)
    if (a < 0 || b < 0 || a === b) return 'invalidWindow'
    const prices = tokenFieldKeys.map(k => w[k]).filter(set)
    if (!prices.length) return 'windowPriceRequired'
    if (prices.some(v => !Number.isFinite(Number(v)) || Number(v) < 0 || Number(v) > 1e9)) return 'invalidPrice'
    for (let m = a; m !== b; m = (m + 1) % 1440) {
      if (occupied[m]) return 'overlappingWindows'
      occupied[m] = 1
    }
  }
  return null
}
