import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import GroupCapacityBadge from '../GroupCapacityBadge.vue'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, params?: Record<string, unknown>) => {
        if (key === 'admin.groups.concurrencyLabel') return '实时并发'
        if (key === 'admin.groups.concurrencyUnlimited') return '不限'
        return `${key}:${JSON.stringify(params ?? {})}`
      }
    })
  }
})

const badgeText = (wrapper: ReturnType<typeof mount>) =>
  wrapper.get('[data-testid="group-concurrency"]').text().replace(/\s+/g, '')

describe('GroupCapacityBadge', () => {
  it('leads with the requests in flight and keeps the limit secondary', () => {
    const wrapper = mount(GroupCapacityBadge, { props: { concurrencyUsed: 2, concurrencyMax: 30 } })
    expect(wrapper.get('[data-testid="group-concurrency-used"]').text()).toBe('2')
    expect(badgeText(wrapper)).toBe('2/30')
    expect(wrapper.get('[data-testid="group-concurrency"]').classes()).toContain('group-live-busy')
  })

  it('shows ∞ as the limit when any account in the group is unlimited', () => {
    const wrapper = mount(GroupCapacityBadge, { props: { concurrencyUsed: 9, concurrencyMax: 4, concurrencyUnlimited: true } })
    const badge = wrapper.get('[data-testid="group-concurrency"]')
    expect(badgeText(wrapper)).toBe('9/∞')
    expect(badge.classes()).toContain('group-live-busy')
    expect(badge.attributes('title')).toContain('"max":"不限"')
  })

  it('reads as idle when nothing is running', () => {
    const wrapper = mount(GroupCapacityBadge, { props: { concurrencyUsed: 0, concurrencyMax: 12 } })
    expect(wrapper.get('[data-testid="group-concurrency"]').classes()).toContain('group-live-idle')
  })

  it('warns near the limit and marks a group whose limit is reached', () => {
    const high = mount(GroupCapacityBadge, { props: { concurrencyUsed: 4, concurrencyMax: 5 } })
    expect(high.get('[data-testid="group-concurrency"]').classes()).toContain('group-live-high')

    const full = mount(GroupCapacityBadge, { props: { concurrencyUsed: 5, concurrencyMax: 5 } })
    expect(full.get('[data-testid="group-concurrency"]').classes()).toContain('group-live-full')
  })

  it('can carry its own label for card layouts', () => {
    const wrapper = mount(GroupCapacityBadge, { props: { concurrencyUsed: 1, concurrencyMax: 3, showLabel: true } })
    expect(badgeText(wrapper)).toBe('实时并发1/3')
  })
})
