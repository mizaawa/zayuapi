import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import UserEditModal from '../UserEditModal.vue'
import type { AdminUser } from '@/types'

const { getConfig, getUser, updateUser, showError, showSuccess } = vi.hoisted(() => ({
  getConfig: vi.fn(), getUser: vi.fn(), updateUser: vi.fn(), showError: vi.fn(), showSuccess: vi.fn(),
}))
vi.mock('@/api/admin', () => ({ adminAPI: {
  payment: { getConfig }, users: { getById: getUser, update: updateUser },
} }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showError, showSuccess }) }))
vi.mock('vue-i18n', async (importOriginal) => ({
  ...await importOriginal<typeof import('vue-i18n')>(),
  useI18n: () => ({ t: (key: string) => key }),
}))
vi.mock('@/composables/useClipboard', () => ({ useClipboard: () => ({ copyToClipboard: vi.fn() }) }))
vi.mock('@/composables/useStepUp', () => ({
  useStepUp: () => ({ run: (fn: () => Promise<unknown>) => fn() }),
  isStepUpBlocked: () => false, isStepUpCancelled: () => false, stepUpBlockReason: () => '',
}))

const user = { id: 4, email: 'test@example.com', role: 'user', concurrency: 2, notes: 'Notes', exclusive_recharge_enabled: true } as AdminUser
const wrappers: ReturnType<typeof mount>[] = []
const render = () => {
  const wrapper = mount(UserEditModal, {
    props: { show: true, user },
    global: { stubs: {
      BaseDialog: { props: ['show'], template: '<div v-if="show"><slot /><slot name="footer" /></div>' },
      UserAttributeForm: true, TotpStepUpDialog: true,
    } },
  })
  wrappers.push(wrapper)
  return wrapper
}

describe('UserEditModal exclusive recharge permission', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    getConfig.mockResolvedValue({ data: { payment_exclusive_enabled: true } })
    getUser.mockResolvedValue(user)
    updateUser.mockImplementation(async (_id, data) => {
      const updated = { ...user, ...data }
      getUser.mockResolvedValue(updated)
      return updated
    })
  })
  afterEach(() => wrappers.splice(0).forEach(wrapper => wrapper.unmount()))

  it('keeps the authorization as a draft and submits it with the user form', async () => {
    const wrapper = render()
    await flushPromises()
    const labels = wrapper.findAll('label').map(label => label.text())
    const index = labels.indexOf('admin.users.exclusiveRecharge')
    expect(labels[index - 1]).toBe('admin.users.notes')
    expect(labels[index + 1]).toBe('admin.users.columns.concurrency')
    expect(wrapper.text()).toContain('admin.users.exclusiveRechargeHint')
    expect(wrapper.get('[role="switch"]').attributes('aria-checked')).toBe('true')
    await wrapper.get('[role="switch"]').trigger('click')
    expect(updateUser).not.toHaveBeenCalled()
    expect(wrapper.get('[role="switch"]').attributes('aria-checked')).toBe('false')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(updateUser).toHaveBeenCalledWith(4, expect.objectContaining({
      email: user.email,
      role: user.role,
      exclusive_recharge_enabled: false,
    }))
    expect(wrapper.emitted('success')).toHaveLength(1)
    expect(wrapper.emitted('close')).toHaveLength(1)
  })

  it('hides the grant when disabled and never changes permission on regular save', async () => {
    getConfig.mockResolvedValue({ data: { payment_exclusive_enabled: false } })
    const wrapper = render()
    await flushPromises()
    expect(wrapper.find('[role="switch"]').exists()).toBe(false)
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(updateUser).toHaveBeenCalledTimes(1)
    expect(updateUser.mock.calls[0][1]).not.toHaveProperty('exclusive_recharge_enabled')
  })

  it('does not overwrite an unsaved draft when focus or payment events refresh access', async () => {
    const wrapper = render()
    await flushPromises()
    await wrapper.get('[role="switch"]').trigger('click')
    getUser.mockResolvedValue({ ...user, exclusive_recharge_enabled: false })
    getConfig.mockResolvedValue({ data: { payment_exclusive_enabled: true } })
    window.dispatchEvent(new Event('payment-access-changed'))
    window.dispatchEvent(new Event('payment-mode-changed'))
    window.dispatchEvent(new Event('focus'))
    await flushPromises()
    expect(wrapper.get('[role="switch"]').attributes('aria-checked')).toBe('false')
    expect(getUser).toHaveBeenCalledTimes(2)
    expect(updateUser).not.toHaveBeenCalled()
  })

  it('discards the draft on close and reloads the server value on reopen', async () => {
    const wrapper = render()
    await flushPromises()
    await wrapper.get('[role="switch"]').trigger('click')
    const cancel = wrapper.findAll('button').find(button => button.text() === 'common.cancel')!
    await cancel.trigger('click')
    expect(wrapper.emitted('close')).toHaveLength(1)
    await wrapper.setProps({ show: false })
    await wrapper.setProps({ show: true })
    await flushPromises()
    expect(wrapper.get('[role="switch"]').attributes('aria-checked')).toBe('true')
    expect(getUser).toHaveBeenCalledTimes(2)
  })

  it('keeps the draft after a failed form save', async () => {
    updateUser.mockRejectedValueOnce(new Error('Failed to update'))
    const wrapper = render()
    await flushPromises()
    await wrapper.get('[role="switch"]').trigger('click')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(wrapper.get('[role="switch"]').attributes('aria-checked')).toBe('false')
    expect(showError).toHaveBeenCalledWith('Failed to update')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(updateUser).toHaveBeenCalledTimes(2)
    expect(updateUser.mock.calls[1][1].exclusive_recharge_enabled).toBe(false)
  })

  it('does not carry a draft over when switching to another user', async () => {
    const wrapper = render()
    await flushPromises()
    await wrapper.get('[role="switch"]').trigger('click')
    const otherUser = { ...user, id: 9, email: 'other@example.com', exclusive_recharge_enabled: false } as AdminUser
    getUser.mockImplementation(async id => id === otherUser.id ? otherUser : user)
    await wrapper.setProps({ user: otherUser })
    await flushPromises()
    expect(wrapper.get('[role="switch"]').attributes('aria-checked')).toBe('false')
    await wrapper.get('[role="switch"]').trigger('click')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(updateUser).toHaveBeenCalledWith(9, expect.objectContaining({ exclusive_recharge_enabled: true }))
  })

  it('omits the permission when saving before its server value has loaded', async () => {
    let resolveUser!: (value: AdminUser) => void
    getUser.mockReturnValueOnce(new Promise<AdminUser>(resolve => { resolveUser = resolve }))
    const wrapper = render()
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(updateUser).toHaveBeenCalledTimes(1)
    expect(updateUser.mock.calls[0][1]).not.toHaveProperty('exclusive_recharge_enabled')
    resolveUser(user)
    await flushPromises()
  })
})
