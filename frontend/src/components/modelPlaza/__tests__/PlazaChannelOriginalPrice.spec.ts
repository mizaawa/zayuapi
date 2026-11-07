import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import PlazaModelPricingTable from '../PlazaModelPricingTable.vue'
import type { PlazaModel } from '@/api/modelPlaza'
import type { UserSupportedModelPricing } from '@/api/channels'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return { ...actual, useI18n: () => ({ t: (key: string) => key }) }
})

function price(mode: UserSupportedModelPricing['billing_mode'], values: Partial<UserSupportedModelPricing>): UserSupportedModelPricing {
  return {
    billing_mode: mode,
    input_price: null,
    output_price: null,
    cache_write_price: null,
    cache_read_price: null,
    image_input_price: null,
    image_output_price: null,
    per_request_price: null,
    intervals: [],
    ...values
  }
}

function model(paid: UserSupportedModelPricing | null, original: UserSupportedModelPricing | null): PlazaModel {
  return {
    name: 'model',
    platform: 'openai',
    pricing: paid,
    official_pricing: original,
    pricing_source: 'channel'
  }
}

function cells(m: PlazaModel, rate = 0.5) {
  return mount(PlazaModelPricingTable, { props: { models: [m], rateMultiplier: rate } }).findAll('tbody td')
}

describe('model plaza channel original prices', () => {
  it('keeps channel token intervals and zero prices separate from discounted group prices', () => {
    const m = model(
      price('token', { input_price: 1e-6, output_price: 2e-6 }),
      price('token', {
        input_price: 0,
        output_price: 4e-6,
        intervals: [{
          min_tokens: 100000, max_tokens: null, tier_label: '',
          input_price: 8e-6, output_price: 10e-6,
          cache_write_price: null, cache_read_price: null, per_request_price: null
        }]
      })
    )
    m.pricing_source = 'group'

    const row = cells(m)

    expect(row[1].text()).toContain('$0.50')
    expect(row[4].text().replaceAll(' ', '')).toContain('≤100K$0.00')
    expect(row[4].text().replaceAll(' ', '')).toContain('>100K$8.00')
    expect(row[5].text().replaceAll(' ', '')).toContain('≤100K$4.00')
    expect(row[5].text().replaceAll(' ', '')).toContain('>100K$10.00')
    expect(row[0].text()).toContain('modelPlaza.table.groupPriceSource')
  })

  it.each([
    { mode: 'per_request', unit: 'perUnitRequest', tier: 'fast' },
    { mode: 'image', unit: 'perUnitImage', tier: '4K' },
    { mode: 'video', unit: 'perUnitSecond', tier: '720p' }
  ] as const)('shows $mode channel original in its own unit without a second discount', ({ mode, unit, tier }) => {
    const m = model(
      price(mode, { per_request_price: 0.04 }),
      price(mode, {
        per_request_price: 0.08,
        intervals: [{
          min_tokens: 0, max_tokens: null, tier_label: tier,
          input_price: null, output_price: null,
          cache_write_price: null, cache_read_price: null,
          per_request_price: 0.12
        }]
      })
    )

    const row = cells(m)

    expect(row[1].text()).toContain('$0.02')
    expect(row[2].text()).toContain(tier)
    expect(row[2].text()).toContain('$0.12')
    expect(row[2].text()).toContain(`modelPlaza.table.${unit}`)
  })

  it('uses independent official and paid currency switches, and explains missing channel prices', () => {
    const m = model(price('token', { input_price: 2e-6 }), price('token', { input_price: 4e-6 }))
    m.pricing_currency = { official_cny: true, discounted_cny: false, cny_per_usd: 7 }
    const row = cells(m)
    expect(row[1].text()).toBe('$1.00')
    expect(row[4].text()).toBe('¥28.00')

    m.official_pricing = null
    m.pricing_source = 'reference'
    const missing = cells(m)
    expect(missing[0].text()).toContain('modelPlaza.table.referencePriceSource')
    expect(missing[4].text()).toContain('modelPlaza.table.channelPriceMissing')
  })

  it('inherits only the same channel card defaults in partial intervals and preserves image-token units', () => {
    const original = price('token', {
      input_price: 2e-6, output_price: 6e-6, image_input_price: 1e-6,
      intervals: [{
        min_tokens: 0, max_tokens: null, input_price: 0, output_price: null,
        cache_write_price: null, cache_read_price: null, per_request_price: null
      }]
    })
    const m = model(price('token', { input_price: 100e-6, output_price: 200e-6 }), original)
    const row = cells(m)
    expect(row[4].text()).toContain('$0.00')
    expect(row[4].text()).toContain('modelPlaza.table.imageTokens')
    expect(row[4].text()).toContain('$1.00')
    expect(row[5].text()).toContain('$6.00')
    expect(row[5].text()).not.toContain('$200.00')
    expect(original.intervals[0].output_price).toBeNull()
  })

  it.each(['image', 'video'] as const)('applies independent %s rates once to paid prices and never to originals', (mode) => {
    const m = model(price(mode, { per_request_price: 0.04 }), price(mode, { per_request_price: 0.08 }))
    const wrapper = mount(PlazaModelPricingTable, {
      props: {
        models: [m], rateMultiplier: 0.9, userRateMultiplier: 0.8,
        imageRateIndependent: true, imageRateMultiplier: 0.25,
        videoRateIndependent: true, videoRateMultiplier: 0.25
      }
    })
    const row = wrapper.findAll('tbody td')
    expect(row[1].text()).toContain('$0.01')
    expect(row[2].text()).toContain('$0.08')
    expect(wrapper.find('thead').text()).not.toContain('modelPlaza.table.unitPerMillion')
  })
})
