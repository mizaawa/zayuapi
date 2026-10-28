import { mount, type VueWrapper } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'

import GroupBadge from '../GroupBadge.vue'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key }),
  }
})

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ cachedPublicSettings: null }),
}))

const mountBadge = (props: { name: string; maxNameLength?: number }) =>
  mount(GroupBadge, {
    props: { platform: 'openai', ...props },
    global: {
      stubs: {
        PlatformIcon: { template: '<i data-test="platform-icon" />' },
      },
    },
  })

const nameOf = (wrapper: VueWrapper) => wrapper.get('span.truncate')

describe('GroupBadge name length limit', () => {
  it('shows the full name without a tooltip when no limit is set', () => {
    const name = '超长分组名称测试'.repeat(10)
    const wrapper = mountBadge({ name })

    expect(nameOf(wrapper).text()).toBe(name)
    expect(nameOf(wrapper).attributes('title')).toBeUndefined()
  })

  it('leaves names within the limit untouched', () => {
    const wrapper = mountBadge({ name: '默认分组', maxNameLength: 4 })

    expect(nameOf(wrapper).text()).toBe('默认分组')
    expect(nameOf(wrapper).attributes('title')).toBeUndefined()
  })

  it('cuts names over the limit with an ellipsis and keeps the full name as a tooltip', () => {
    const name = '超长分组名称测试高速稳定专线'.repeat(2)
    const wrapper = mountBadge({ name, maxNameLength: 20 })

    expect(nameOf(wrapper).text()).toBe(`${Array.from(name).slice(0, 20).join('')}…`)
    expect(nameOf(wrapper).attributes('title')).toBe(name)
  })

  it('counts Unicode code points so emoji are never split in half', () => {
    const fire = String.fromCodePoint(0x1f525)
    const wrapper = mountBadge({ name: `${fire.repeat(3)}分组`, maxNameLength: 3 })

    expect(nameOf(wrapper).text()).toBe(`${fire.repeat(3)}…`)
  })

  it('drops trailing spaces before the ellipsis', () => {
    const wrapper = mountBadge({ name: 'Claude Code Max', maxNameLength: 7 })

    expect(nameOf(wrapper).text()).toBe('Claude…')
  })

  it('keeps the platform icon from shrinking when the badge is width-capped', () => {
    const wrapper = mountBadge({ name: 'Group' })

    expect(wrapper.get('[data-test="platform-icon"]').classes()).toContain('shrink-0')
  })
})
