import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import SupportedModelChip from '../SupportedModelChip.vue'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return { ...actual, useI18n: () => ({ t: (key: string) => key }) }
})

const basePricing = {
  input_price: null, output_price: null, cache_write_price: null, cache_read_price: null,
  image_input_price: null, image_output_price: null, per_request_price: 0.25, intervals: [],
}

describe('SupportedModelChip currency display', () => {
  it.each(['image', 'video'] as const)('shows a CNY %s price with its billing unit', async (mode) => {
    const wrapper = mount(SupportedModelChip, {
      props: {
        model: { name: 'media-model', platform: 'openai', pricing: { ...basePricing, billing_mode: mode } },
        currency: { official_cny: false, discounted_cny: true, cny_per_usd: 8 },
      },
      global: { stubs: { Teleport: true, PlatformIcon: true } },
    })
    await wrapper.get('[tabindex="0"]').trigger('mouseenter')
    expect(wrapper.text()).toContain('¥2')
    expect(wrapper.text()).toContain(mode === 'image' ? 'unitPerImage' : 'unitPerSecond')
    wrapper.unmount()
  })
})
