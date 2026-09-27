import { describe, expect, it, vi } from 'vitest'
import { createPinia } from 'pinia'
import { mount } from '@vue/test-utils'
import ModelPlazaContent from '../ModelPlazaContent.vue'
import PlazaFilterBar from '../PlazaFilterBar.vue'
import PlazaGroupSection from '../PlazaGroupSection.vue'
import type { ModelPlazaGroup } from '@/api/modelPlaza'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => key
    })
  }
})

function group(overrides: Partial<ModelPlazaGroup> = {}): ModelPlazaGroup {
  return {
    id: 42,
    name: 'Custom group',
    description: '',
    platform: 'composite',
    subscription_type: 'standard',
    rate_multiplier: 0.5,
    peak_rate_enabled: false,
    peak_start: '',
    peak_end: '',
    peak_rate_multiplier: 1,
    is_exclusive: false,
    image_rate_independent: false,
    image_rate_multiplier: 1,
    models: [{
      name: 'vendor-chat-model',
      platform: 'custom',
      pricing: {
        billing_mode: 'token',
        input_price: 2e-6,
        output_price: 8e-6,
        cache_write_price: null,
        cache_read_price: null,
        image_input_price: null,
        image_output_price: null,
        per_request_price: null,
        intervals: []
      },
      official_pricing: null
    }],
    ...overrides
  }
}

function mountContent(groups: ModelPlazaGroup[]) {
  return mount(ModelPlazaContent, {
    props: { response: { description: '', groups }, loading: false },
    global: {
      plugins: [createPinia()],
      stubs: {
        GroupBadge: { template: '<span>{{ name }}</span>', props: ['name'] },
        PlatformIcon: true,
        Icon: true
      }
    }
  })
}

describe('ModelPlazaContent', () => {
  it.each(['composite', 'custom'])('renders Custom prices for the %s group platform', async (platform) => {
    const wrapper = mountContent([group({ platform })])

    await wrapper.get('[data-testid="group-toggle"]').trigger('click')

    const cells = wrapper.findAll('tbody tr td')
    expect(cells[0].text()).toContain('vendor-chat-model')
    expect(cells[1].text()).toBe('$1.00')
    expect(cells[2].text()).toBe('$4.00')
    expect(cells[4].text()).toBe('-')
    expect(cells[5].text()).toBe('-')
    expect(cells[7].text()).toBe('0.5x')
  })

  it('keeps the Custom group available when selecting its platform', async () => {
    const custom = group()
    const ordinary = group({
      id: 43,
      name: 'OpenAI group',
      platform: 'openai',
      models: [{ ...custom.models[0], name: 'gpt-test', platform: 'openai' }]
    })
    const wrapper = mountContent([custom, ordinary])
    const filters = wrapper.getComponent(PlazaFilterBar)

    expect(filters.props('platforms')).toEqual(['composite', 'openai'])
    const customPlatform = filters.findAll('button').find((button) => button.text() === 'composite')!
    expect(customPlatform.attributes('disabled')).toBeUndefined()
    await customPlatform.trigger('click')

    const sections = wrapper.findAllComponents(PlazaGroupSection)
    expect(sections).toHaveLength(1)
    expect(sections[0].props('group')).toEqual(custom)
  })

  it('retains Custom pricing when searching for its model', async () => {
    const custom = group()
    const wrapper = mountContent([
      custom,
      group({
        id: 43,
        name: 'OpenAI group',
        platform: 'openai',
        models: [{ ...custom.models[0], name: 'gpt-test', platform: 'openai' }]
      })
    ])

    await wrapper.get('input[type="text"]').setValue('vendor-chat')
    await wrapper.get('[data-testid="group-toggle"]').trigger('click')

    expect(wrapper.findAllComponents(PlazaGroupSection)).toHaveLength(1)
    expect(wrapper.findAll('tbody tr')).toHaveLength(1)
    expect(wrapper.text()).toContain('vendor-chat-model')
    expect(wrapper.text()).toContain('$1.00')
    expect(wrapper.text()).toContain('$4.00')
  })
})
