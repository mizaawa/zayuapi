import { describe, expect, it } from 'vitest'
import { providerAccountRateMultiplier } from '../providerImport'

describe('providerAccountRateMultiplier', () => {
  it.each([
    [0.2, 0.2],
    [0.1 + 0.2, 0.3],
    [1.23456, 1.2346],
    [0.0625, 0.0625],
    [0, 0],
    [999_999.9999, 999_999.9999]
  ])('uses %s at the account column precision as %s', (value, expected) => {
    expect(providerAccountRateMultiplier(value)).toBe(expected)
  })

  it.each([null, undefined, -0.5, Number.NaN, Number.POSITIVE_INFINITY, 1_000_000])(
    'reports no rate for %s',
    (value) => {
      expect(providerAccountRateMultiplier(value)).toBeNull()
    }
  )
})
