import { mount, type VueWrapper } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'

vi.mock('vue-i18n', async () => ({
  ...(await vi.importActual<typeof import('vue-i18n')>('vue-i18n')),
  useI18n: () => ({ t: (key: string) => key })
}))

import AccountTableFilters from '../AccountTableFilters.vue'
import type { AdminGroup } from '@/types'

const groups = [
  { id: 3, name: 'CodexPro-不降智①', platform: 'openai', rate_multiplier: 0.18 },
  { id: 5, name: 'Media②', platform: 'custom', rate_multiplier: 1 },
  { id: 9, name: 'Gemini-默认通道', platform: 'gemini', rate_multiplier: 0.1 + 0.2 }
] as unknown as AdminGroup[]

let wrapper: VueWrapper | undefined

function mountFilters(group = '') {
  wrapper = mount(AccountTableFilters, {
    props: { searchQuery: '', filters: { platform: '', type: '', status: '', privacy_mode: '', group }, groups },
    global: { stubs: { teleport: true } },
    attachTo: document.body
  })
  return wrapper
}

async function openGroups(target: VueWrapper) {
  await target.get('[data-testid="account-group-filter"] button').trigger('click')
  return target.findAll('[data-testid="account-group-filter"] [role="option"]')
}

afterEach(() => {
  wrapper?.unmount()
  wrapper = undefined
})

describe('AccountTableFilters group filter', () => {
  it('shows each group rate as a tag after the group name', async () => {
    const options = await openGroups(mountFilters())

    expect(options.map((option) => option.text())).toEqual([
      'admin.accounts.allGroups',
      'admin.accounts.ungroupedGroup',
      'CodexPro-不降智①0.18x',
      'Media②1x',
      'Gemini-默认通道0.3x'
    ])
    expect(options[0].find('[data-testid="account-group-rate"]').exists()).toBe(false)
    expect(options[1].find('[data-testid="account-group-rate"]').exists()).toBe(false)
    const tag = options[2].get('[data-testid="account-group-rate"]')
    expect(tag.text()).toBe('0.18x')
    expect(tag.attributes('title')).toBe('admin.groups.columns.rateMultiplier: 0.18x')
    expect(options[2].attributes('title')).toBeUndefined()
    expect(options[2].get('.account-group-option').attributes('title')).toBe('CodexPro-不降智①')
  })

  it('selects a group and keeps its rate beside the selected name', async () => {
    const filters = mountFilters()
    const options = await openGroups(filters)
    await options[3].trigger('click')

    expect(filters.emitted('update:filters')?.[0]?.[0]).toEqual(expect.objectContaining({ group: '5' }))
    expect(filters.emitted('change')).toHaveLength(1)

    await filters.setProps({ filters: { platform: '', type: '', status: '', privacy_mode: '', group: '5' } })
    const trigger = filters.get('[data-testid="account-group-filter"] button')
    expect(trigger.text()).toContain('Media②')
    expect(trigger.get('[data-testid="account-group-selected-rate"]').text()).toBe('1x')
  })

  it('reserves a wide menu so names and tags fit beside the viewport edge', async () => {
    const filters = mountFilters()
    await openGroups(filters)

    const menu = filters.get('[data-testid="account-group-filter"] .select-dropdown-portal').element as HTMLElement
    expect(menu.style.minWidth).toBe('320px')
  })
})
