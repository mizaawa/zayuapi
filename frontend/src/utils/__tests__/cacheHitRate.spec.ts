import { describe, expect, it } from 'vitest'
import { formatCacheHitRate, getCacheHitRate, getCacheHitRateClass } from '../cacheHitRate'

describe('usage cache hit rate', () => {
  it('includes cache creation in total input and excludes output', () => {
    const usage = {
      input_tokens: 100,
      cache_creation_tokens: 200,
      cache_read_tokens: 700,
      output_tokens: 9000,
    }

    expect(getCacheHitRate(usage)).toBe(70)
    expect(formatCacheHitRate(usage)).toBe('70.0%')
  })

  it.each([
    [{}, '-'],
    [{ input_tokens: 100, cache_read_tokens: 0 }, '-'],
    [{ input_tokens: 100, cache_creation_tokens: 300, cache_read_tokens: 0 }, '-'],
    [{ input_tokens: 0, cache_read_tokens: 100 }, '100.0%'],
    [{ input_tokens: 99, cache_read_tokens: 1 }, '1.0%'],
    [{ input_tokens: 2, cache_read_tokens: 1 }, '33.3%'],
    [{ input_tokens: 3360, cache_read_tokens: 57000 }, '94.4%'],
  ])('formats %j as %s', (usage, expected) => {
    expect(formatCacheHitRate(usage)).toBe(expected)
  })

  it.each([
    [0, 'text-gray-400 dark:text-gray-500'],
    [1, 'text-red-600 dark:text-red-400'],
    [19.9, 'text-red-600 dark:text-red-400'],
    [20, 'text-yellow-600 dark:text-yellow-400'],
    [69.9, 'text-yellow-600 dark:text-yellow-400'],
    [70, 'text-green-600 dark:text-green-400'],
    [100, 'text-green-600 dark:text-green-400'],
  ])('colors %s percent correctly', (rate, expected) => {
    expect(getCacheHitRateClass({ input_tokens: 100 - rate, cache_read_tokens: rate })).toBe(expected)
  })
})
