import { describe, expect, it } from 'vitest'

import { shortenTrendLabel, trendTickCallback } from '../chartLabels'

describe('shortenTrendLabel', () => {
  it('keeps only the clock for hourly buckets', () => {
    expect(shortenTrendLabel('2026-10-29 14:00')).toBe('14:00')
    expect(shortenTrendLabel('2026-10-29 09:30:00')).toBe('09:30')
  })

  it('drops the year for daily buckets', () => {
    expect(shortenTrendLabel('2026-10-29')).toBe('10-29')
  })

  it('leaves other labels untouched', () => {
    expect(shortenTrendLabel('Week 43')).toBe('Week 43')
    expect(shortenTrendLabel('')).toBe('')
  })
})

describe('trendTickCallback', () => {
  it('shortens the category label Chart.js resolves for a tick', () => {
    const scale = { getLabelForValue: (value: number) => ['2026-10-28', '2026-10-29'][value] }
    expect(trendTickCallback.call(scale, 1)).toBe('10-29')
    expect(trendTickCallback.call(scale, '0')).toBe('10-28')
  })
})
