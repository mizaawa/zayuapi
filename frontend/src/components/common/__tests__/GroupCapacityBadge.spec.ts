import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import GroupCapacityBadge from '../GroupCapacityBadge.vue'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, params?: Record<string, unknown>) =>
        key === 'admin.groups.concurrencyLabel' ? '并发数：' : `${key}:${JSON.stringify(params ?? {})}`
    })
  }
})

describe('GroupCapacityBadge', () => {
  it('shows the group concurrency limit as 并发数：X', () => {
    const wrapper = mount(GroupCapacityBadge, { props: { concurrencyUsed: 2, concurrencyMax: 30 } })
    expect(wrapper.get('[data-testid="group-concurrency"]').text().replace(/\s+/g, '')).toBe('并发数：30')
  })

  it('shows ∞ when any account in the group is unlimited', () => {
    const wrapper = mount(GroupCapacityBadge, { props: { concurrencyUsed: 9, concurrencyMax: 4, concurrencyUnlimited: true } })
    const badge = wrapper.get('[data-testid="group-concurrency"]')
    expect(badge.text().replace(/\s+/g, '')).toBe('并发数：∞')
    expect(badge.classes()).not.toContain('group-concurrency-full')
  })

  it('marks a group whose limit is reached', () => {
    const wrapper = mount(GroupCapacityBadge, { props: { concurrencyUsed: 5, concurrencyMax: 5 } })
    expect(wrapper.get('[data-testid="group-concurrency"]').classes()).toContain('group-concurrency-full')
  })
})
