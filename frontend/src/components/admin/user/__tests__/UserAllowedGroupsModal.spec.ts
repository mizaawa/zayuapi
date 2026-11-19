import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

const { listGroups, updateUser, showSuccess, showError } = vi.hoisted(() => ({
  listGroups: vi.fn(),
  updateUser: vi.fn(),
  showSuccess: vi.fn(),
  showError: vi.fn()
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    groups: { list: listGroups },
    users: { update: updateUser }
  }
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showSuccess, showError })
}))

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string) => key
  })
}))

import UserAllowedGroupsModal from '../UserAllowedGroupsModal.vue'

const user = {
  id: 7,
  email: 'user@example.com',
  allowed_groups: [],
  blocked_groups: [],
  group_rates: {}
} as any

const exclusiveGroup = {
  id: 10,
  name: 'VIP',
  platform: 'openai',
  is_exclusive: true,
  status: 'active',
  subscription_type: 'standard',
  rate_multiplier: 1
} as any

describe('UserAllowedGroupsModal', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    listGroups.mockResolvedValue({ items: [exclusiveGroup] })
    updateUser.mockResolvedValue({})
  })

  async function mountModal() {
    const wrapper = mount(UserAllowedGroupsModal, {
      props: { show: false, user },
      global: {
        stubs: {
          BaseDialog: {
            props: ['show'],
            template: '<div v-if="show"><slot /><slot name="footer" /></div>'
          },
          PlatformIcon: true
        }
      }
    })
    await wrapper.setProps({ show: true })
    await flushPromises()
    return wrapper
  }

  it('keeps an unselected exclusive group checkbox visible', async () => {
    const wrapper = await mountModal()
    const checkbox = wrapper.get('[data-test="exclusive-group-checkbox-10"]')

    expect(checkbox.attributes('role')).toBe('checkbox')
    expect(checkbox.attributes('aria-checked')).toBe('false')
    expect(checkbox.classes()).toContain('ring-1')
    expect(checkbox.classes()).toContain('ring-[var(--control-focus)]')
    expect(checkbox.classes()).toContain('control-quiet')
  })

  it('toggles the visible checkbox and saves the selected group', async () => {
    const wrapper = await mountModal()
    const checkbox = wrapper.get('[data-test="exclusive-group-checkbox-10"]')

    await checkbox.trigger('click')
    expect(checkbox.attributes('aria-checked')).toBe('true')
    expect(checkbox.classes()).toContain('control-selected')

    await wrapper.get('button.btn-primary').trigger('click')
    await flushPromises()
    expect(updateUser).toHaveBeenCalledWith(7, expect.objectContaining({ allowed_groups: [10] }))
  })
})
