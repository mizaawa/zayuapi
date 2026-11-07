import { describe, expect, it } from 'vitest'
import { exportPricingTransfer, parsePricingTransfer, previewPricingTransfer } from '../pricingTransfer'
import type { PricingFormEntry } from '../types'
import { displayPrice, usdPrice } from '@/utils/channelPricingCurrency'

const currency = { official_cny: true, discounted_cny: true, cny_per_usd: 7.2 }
const entry: PricingFormEntry = {
  models: ['model-a'], billing_mode: 'token', input_price: 7.2, output_price: 14.4,
  cache_write_price: null, cache_read_price: null, image_input_price: null,
  image_output_price: null, per_request_price: null, intervals: [],
}

describe('platform pricing JSON transfer', () => {
  it('round trips exported prices and their currency settings', () => {
    const exported = exportPricingTransfer('openai', currency, [entry])
    expect(parsePricingTransfer(exported, 'openai')).toEqual({
      format: 'zayuapi-channel-pricing', version: 1, platform: 'openai', currency, entries: [entry],
    })
    expect(usdPrice(entry.input_price, true, currency.cny_per_usd)).toBe(1)
    expect(displayPrice(1, true, currency.cny_per_usd)).toBe(7.2)
  })

  it('rejects other platforms, duplicates, invalid prices and malformed JSON', () => {
    const exported = exportPricingTransfer('openai', currency, [entry])
    expect(() => parsePricingTransfer(exported, 'anthropic')).toThrow(/Platform mismatch/)
    const parsed = JSON.parse(exported)
    parsed.entries.push({ ...entry, models: ['MODEL-A'] })
    expect(() => parsePricingTransfer(JSON.stringify(parsed), 'openai')).toThrow(/Duplicate/)
    parsed.entries.pop()
    parsed.entries[0].input_price = -1
    expect(() => parsePricingTransfer(JSON.stringify(parsed), 'openai')).toThrow(/input_price/)
    expect(() => parsePricingTransfer('{', 'openai')).toThrow(/syntax/)
    expect(() => exportPricingTransfer('openai', currency, [{ ...entry, input_price: 'invalid' }])).toThrow(/input_price/)
  })

  it('shows added, removed and field-level price changes without applying them', () => {
    const before = [entry, { ...entry, models: ['old-model'] }]
    const after = [{ ...entry, output_price: 20 }, { ...entry, models: ['new-model'] }]
    expect(previewPricingTransfer(before, after)).toEqual({
      added: ['new-model'], removed: ['old-model'],
      changed: [{ model: 'model-a', fields: [{ field: 'output_price', before: 14.4, after: 20 }] }],
    })
    expect(before[0].output_price).toBe(14.4)
  })

  it('accepts an explicit empty replacement only after parsing and previewing deletions', () => {
    const data = parsePricingTransfer(exportPricingTransfer('custom', currency, []), 'custom')
    expect(data.entries).toEqual([])
    expect(previewPricingTransfer([entry], data.entries).removed).toEqual(['model-a'])
  })

  it('round trips token tiers and per-request, image and video forms', () => {
    const tier = { min_tokens: 0, max_tokens: 1000, tier_label: '', input_price: 1.5,
      output_price: 3, cache_write_price: null, cache_read_price: null, per_request_price: null, sort_order: 0 }
    const mediaTier = { ...tier, max_tokens: null, tier_label: '720p', input_price: null,
      output_price: null, per_request_price: 0.25 }
    const entries: PricingFormEntry[] = [
      { ...entry, models: ['token-model'], intervals: [tier] },
      { ...entry, models: ['request-model'], billing_mode: 'per_request', input_price: null,
        output_price: null, per_request_price: 0.1, intervals: [] },
      { ...entry, models: ['image-model'], billing_mode: 'image', input_price: null,
        output_price: null, per_request_price: 0.2, intervals: [{ ...mediaTier, tier_label: '1K' }] },
      { ...entry, models: ['video-model'], billing_mode: 'video', input_price: null,
        output_price: null, per_request_price: 0.3, intervals: [mediaTier] },
    ]
    const exported = exportPricingTransfer('grok', currency, entries)
    expect(parsePricingTransfer(exported, 'grok').entries).toEqual(entries)
    expect(previewPricingTransfer(entries, parsePricingTransfer(exported, 'grok').entries)).toEqual({ added: [], removed: [], changed: [] })
  })
})
