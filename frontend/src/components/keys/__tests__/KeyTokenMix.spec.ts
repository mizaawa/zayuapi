import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'

vi.mock('vue-i18n', async () => ({
  ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'),
  useI18n: () => ({ t: (key: string) => key }),
}))

import KeyTokenMix from '../KeyTokenMix.vue'

describe('KeyTokenMix', () => {
  it('shows the cache hit rate against every prompt token', () => {
    const wrapper = mount(KeyTokenMix, {
      props: { usage: { input_tokens: 200, output_tokens: 50, cache_creation_tokens: 300, cache_read_tokens: 500 } },
    })

    // 500 / (200 + 300 + 500) = 50%
    expect(wrapper.get('[data-testid="key-cache-hit-rate"]').text()).toContain('50.0%')
    expect(wrapper.get('[data-testid="key-cache-hit-rate"]').classes()).toContain('is-mid')
    expect(wrapper.text()).toContain('1.1K')
    expect(wrapper.findAll('.key-mix-segment')).toHaveLength(4)
  })

  it('marks a key without traffic instead of showing a zero hit rate', () => {
    const wrapper = mount(KeyTokenMix, { props: { usage: null } })

    expect(wrapper.get('[data-testid="key-cache-hit-rate"]').text()).toContain('—')
    expect(wrapper.get('[data-testid="key-cache-hit-rate"]').classes()).toContain('is-none')
    expect(wrapper.text()).toContain('keys.tokenMixEmpty')
    expect(wrapper.findAll('.key-mix-segment')).toHaveLength(0)
  })

  it('ignores negative or invalid counts from older responses', () => {
    const wrapper = mount(KeyTokenMix, {
      props: { usage: { input_tokens: -5, output_tokens: Number.NaN, cache_read_tokens: 0 } },
    })

    expect(wrapper.findAll('.key-mix-segment')).toHaveLength(0)
    expect(wrapper.get('[data-testid="key-cache-hit-rate"]').classes()).toContain('is-none')
  })

  it('uses short legend labels inside the table cell', () => {
    const wrapper = mount(KeyTokenMix, { props: { variant: 'cell', usage: { input_tokens: 1 } } })
    expect(wrapper.text()).toContain('keys.tokenParts.input')
    expect(wrapper.text()).not.toContain('dashboard.input')
  })
})
