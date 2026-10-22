import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import PaymentPermissionsPanel from '../PaymentPermissionsPanel.vue'
import Pagination from '@/components/common/Pagination.vue'
import SearchInput from '@/components/common/SearchInput.vue'

const { getConfig, updateConfig, listUsers } = vi.hoisted(() => ({
  getConfig: vi.fn(), updateConfig: vi.fn(), listUsers: vi.fn(),
}))
vi.mock('@/api/admin', () => ({ adminAPI: {
  payment: { getConfig, updateConfig }, users: { list: listUsers },
} }))
vi.mock('vue-i18n', async (importOriginal) => ({
  ...await importOriginal<typeof import('vue-i18n')>(),
  useI18n: () => ({ t: (key: string) => key }),
}))

const user = (id: number) => ({ id, email: `user${id}@example.com`, notes: `Notes ${id}`, balance: 25.5, concurrency: 3 })
const response = (ids: number[], total = ids.length, page = 1) => ({ items: ids.map(user), total, page, page_size: 10, pages: Math.ceil(total / 10) })
const wrappers: ReturnType<typeof mount>[] = []
const render = (props: { enabled?: boolean; pendingRemovals?: number[] } = {}) => {
  const wrapper = mount(PaymentPermissionsPanel, { props: {
    active: true, enabled: false, pendingRemovals: [], ...props,
  } })
  wrappers.push(wrapper)
  return wrapper
}

describe('PaymentPermissionsPanel', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    getConfig.mockResolvedValue({ data: { payment_exclusive_enabled: true } })
    listUsers.mockResolvedValue(response([1]))
  })
  afterEach(() => wrappers.splice(0).forEach(wrapper => wrapper.unmount()))

  it('keeps the channel toggle as a draft and hides grants while disabled', async () => {
    getConfig.mockResolvedValueOnce({ data: { payment_exclusive_enabled: false } })
    const wrapper = render()
    await flushPromises()
    expect(listUsers).not.toHaveBeenCalled()
    expect(wrapper.get('#exclusive-payment-hint').text()).toBe('admin.settings.payment.permissions.exclusiveHint')
    expect(wrapper.get('[role="switch"]').attributes('aria-describedby')).toBe('exclusive-payment-hint')
    expect(wrapper.findComponent(SearchInput).exists()).toBe(false)
    await wrapper.get('[role="switch"]').trigger('click')
    expect(updateConfig).not.toHaveBeenCalled()
    expect(wrapper.emitted('update:enabled')).toEqual([[false], [true]])
  })

  it('marks permission revocations as pending without updating the user', async () => {
    const wrapper = render({ enabled: true })
    await flushPromises()
    await wrapper.get('button[aria-label$="user1@example.com"]').trigger('click')
    expect(wrapper.emitted('update:pendingRemovals')).toEqual([[ [1] ]])
  })

  it('uses ten rows per page, searches email only, and resets the search page', async () => {
    listUsers.mockResolvedValue(response(Array.from({ length: 10 }, (_, i) => i + 1), 21))
    const wrapper = render({ enabled: true })
    await flushPromises()
    expect(wrapper.findAll('tbody tr')).toHaveLength(10)
    expect(wrapper.getComponent(Pagination).props('showPageSizeSelector')).toBe(false)
    wrapper.getComponent(Pagination).vm.$emit('update:page', 3)
    await flushPromises()
    expect(listUsers).toHaveBeenLastCalledWith(3, 10, expect.any(Object), expect.any(Object))
    wrapper.getComponent(SearchInput).vm.$emit('search', ' user2@ ')
    await flushPromises()
    expect(listUsers).toHaveBeenLastCalledWith(1, 10, { exclusive_recharge_enabled: true, email: 'user2@' }, expect.any(Object))
    expect(wrapper.findAll('th').map(column => column.text())).toEqual([
      'admin.settings.payment.permissions.email', 'admin.users.notes', 'admin.users.columns.balance',
      'admin.settings.payment.permissions.concurrency', 'admin.users.columns.actions',
    ])
  })

  it('does not remove a grant until its pending change is committed', async () => {
    listUsers.mockResolvedValueOnce(response([1], 11))
    const wrapper = render({ enabled: true })
    await flushPromises()
    listUsers.mockResolvedValueOnce(response([11], 11, 2))
    wrapper.getComponent(Pagination).vm.$emit('update:page', 2)
    await flushPromises()
    await wrapper.get('button[aria-label$="user11@example.com"]').trigger('click')
    expect(wrapper.emitted('update:pendingRemovals')).toEqual([[ [11] ]])
  })

  it('lets a pending permission revocation be cancelled', async () => {
    const wrapper = render({ enabled: true, pendingRemovals: [1] })
    await flushPromises()
    await wrapper.get('button[aria-label$="user1@example.com"]').trigger('click')
    expect(wrapper.emitted('update:pendingRemovals')).toEqual([[ [] ]])
  })

  it('discards stale search responses and stale responses after disabling', async () => {
    let resolveOld!: (value: ReturnType<typeof response>) => void
    listUsers.mockImplementationOnce(() => new Promise(resolve => { resolveOld = resolve }))
    const wrapper = render({ enabled: true })
    await flushPromises()
    listUsers.mockResolvedValueOnce(response([2]))
    wrapper.getComponent(SearchInput).vm.$emit('search', 'user2@')
    await flushPromises()
    resolveOld(response([99]))
    await flushPromises()
    expect(wrapper.text()).not.toContain('user99@example.com')
    await wrapper.setProps({ enabled: false })
    await flushPromises()
    expect(wrapper.findComponent(SearchInput).exists()).toBe(false)
  })

  it('refreshes grants and the master setting after changes from another panel', async () => {
    const wrapper = render({ enabled: true })
    await flushPromises()
    listUsers.mockResolvedValueOnce(response([2]))
    window.dispatchEvent(new Event('payment-access-changed'))
    await flushPromises()
    expect(wrapper.text()).toContain('user2@example.com')
    expect(wrapper.text()).not.toContain('user1@example.com')
    getConfig.mockResolvedValueOnce({ data: { payment_exclusive_enabled: false } })
    window.dispatchEvent(new Event('focus'))
    await flushPromises()
    await wrapper.setProps({ enabled: false })
    await flushPromises()
    expect(wrapper.findComponent(SearchInput).exists()).toBe(false)
    expect(wrapper.get('[role="switch"]').attributes('aria-checked')).toBe('false')
  })
})
