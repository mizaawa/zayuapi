import { describe, expect, it, vi } from 'vitest'
import { createPinia } from 'pinia'
import { mount } from '@vue/test-utils'
import PlazaGroupSection from '../PlazaGroupSection.vue'
import type { ModelPlazaGroup } from '@/api/modelPlaza'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, params?: Record<string, unknown>) => {
        if (key === 'modelPlaza.detail.modelCount') return `${params?.count ?? 0} models`
        return key
      }
    })
  }
})

const group: ModelPlazaGroup = {
  id: 42,
  name: 'Public models',
  description: 'Available to everyone',
  platform: 'openai',
  subscription_type: 'standard',
  rate_multiplier: 1,
  user_rate_multiplier: undefined,
  peak_rate_enabled: false,
  peak_start: '',
  peak_end: '',
  peak_rate_multiplier: 1,
  is_exclusive: false,
  image_rate_independent: false,
  image_rate_multiplier: 1,
  video_rate_independent: false,
  video_rate_multiplier: 1,
  models: [
    {
      name: 'gpt-test',
      platform: 'openai',
      pricing: null,
      official_pricing: null
    }
  ]
}

function mountSection(groupOverride: Partial<ModelPlazaGroup> = {}) {
  return mount(PlazaGroupSection, {
    props: { group: { ...group, ...groupOverride } },
    global: {
      plugins: [createPinia()],
      stubs: {
        GroupBadge: { template: '<span data-group-badge>{{ name }}</span>', props: ['name'] },
        PlazaModelPricingTable: {
          name: 'PlazaModelPricingTable',
          template: '<div data-pricing-table>pricing</div>',
          props: ['videoRateIndependent', 'videoRateMultiplier']
        },
        Icon: { template: '<i :data-icon="name" />', props: ['name'] }
      }
    }
  })
}

describe('PlazaGroupSection', () => {
  it('starts collapsed and does not render price details', () => {
    const wrapper = mountSection()
    const toggle = wrapper.get('[data-testid="group-toggle"]')

    expect(toggle.attributes('type')).toBe('button')
    expect(toggle.attributes('aria-expanded')).toBe('false')
    expect(toggle.attributes('aria-controls')).toBeUndefined()
    expect(toggle.attributes('aria-label')).toBe('Public models: modelPlaza.detail.expandPricing')
    expect(wrapper.find('[data-testid="group-pricing-details"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('1 models')
  })

  it('toggles the pricing card details on click', async () => {
    const wrapper = mountSection()
    const toggle = wrapper.get('[data-testid="group-toggle"]')

    await toggle.trigger('click')
    expect(toggle.attributes('aria-expanded')).toBe('true')
    expect(toggle.attributes('aria-controls')).toBe('model-plaza-group-42-pricing')
    expect(toggle.attributes('aria-label')).toBe('Public models: modelPlaza.detail.collapsePricing')
    expect(wrapper.get('[data-testid="group-pricing-details"] [data-pricing-table]').exists()).toBe(true)

    await toggle.trigger('click')
    expect(toggle.attributes('aria-expanded')).toBe('false')
    expect(toggle.attributes('aria-controls')).toBeUndefined()
    expect(toggle.attributes('aria-label')).toBe('Public models: modelPlaza.detail.expandPricing')
    expect(wrapper.find('[data-testid="group-pricing-details"]').exists()).toBe(false)
  })

  it('shows the no-models state only after an empty group is expanded', async () => {
    const wrapper = mountSection({ id: 43, models: [] })
    const toggle = wrapper.get('[data-testid="group-toggle"]')

    expect(wrapper.text()).not.toContain('modelPlaza.detail.noModels')
    expect(wrapper.find('[data-testid="group-pricing-details"]').exists()).toBe(false)

    await toggle.trigger('click')

    expect(wrapper.get('[data-testid="group-pricing-details"]').text()).toContain(
      'modelPlaza.detail.noModels'
    )
    expect(wrapper.find('[data-pricing-table]').exists()).toBe(false)
  })

  it('renders the exclusive badge when the server marks the group as exclusive', () => {
    const wrapper = mountSection({ id: 44, is_exclusive: true })

    expect(wrapper.text()).toContain('modelPlaza.badges.exclusive')
  })

  it('passes video independent pricing to the expanded table', async () => {
    const wrapper = mountSection({ video_rate_independent: true, video_rate_multiplier: 0.25 })
    await wrapper.get('[data-testid="group-toggle"]').trigger('click')

    expect(wrapper.getComponent({ name: 'PlazaModelPricingTable' }).props()).toMatchObject({
      videoRateIndependent: true,
      videoRateMultiplier: 0.25
    })
  })
})
