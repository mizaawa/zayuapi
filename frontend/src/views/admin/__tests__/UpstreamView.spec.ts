import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import UpstreamView from '../UpstreamView.vue'
import type { Provider } from '@/api/admin/upstream'

const mocks = vi.hoisted(() => ({
  list: vi.fn(), refresh: vi.fn(), delete: vi.fn(), dashboard: vi.fn(), success: vi.fn(), error: vi.fn()
}))
vi.mock('@/api/admin/upstream', () => ({
  listProviders: mocks.list, refreshProvider: mocks.refresh, deleteProvider: mocks.delete
}))
vi.mock('@/api/admin', () => ({ adminAPI: { dashboard: { getOperationsDashboard: mocks.dashboard } } }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showSuccess: mocks.success, showError: mocks.error }) }))
vi.mock('vue-i18n', async () => ({
  ...(await vi.importActual<typeof import('vue-i18n')>('vue-i18n')),
  useI18n: () => ({ t: (key: string) => key })
}))

const provider: Provider = {
  id: 'provider-1', name: '杂鱼AI', site_type: 'sub2api', base_url: 'https://www.zayuapi.com',
  auth_mode: 'password', account: 'demo@example.com', exchange_rate: 1, status: 'connected',
  metrics: { balance: 15.79, today_spend: 0.7, total_recharge: 0, total_spend: 9.21, currency: 'USD' },
  updated_at: '2026-10-27T00:00:00Z'
}
const mountPage = () => mount(UpstreamView, { global: { stubs: {
  AppLayout: { template: '<div><slot /></div>' }, Icon: true, ProviderDialog: true
} } })

describe('UpstreamView provider management', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mocks.list.mockResolvedValue({ items: [provider], total: 1, page: 1, pages: 1,
      overview: { provider_count: 1, connected_count: 1, upstream_total_balance: 15.79 } })
    mocks.dashboard.mockResolvedValue({ metrics: { total_user_balance: 1054.28 } })
    mocks.refresh.mockResolvedValue(provider)
    mocks.delete.mockResolvedValue(undefined)
  })

  it('shows only site statistics and the added user balance', async () => {
    const wrapper = mountPage()
    await flushPromises()
    expect(mocks.list).toHaveBeenCalledWith(1, '', '')
    const text = wrapper.text()
    expect(text).toContain('杂鱼AI')
    expect(wrapper.get('[data-testid="upstream-metric"]').text()).toContain('¥15.79')
    expect(text).toContain('$1,054.28')
    expect(text).toContain('$0.70')
    expect(text).toContain('$9.21')
    expect(text).not.toContain('admin.upstream.recordBalance')
    expect(text).not.toContain('admin.upstream.disable')
    expect(text).not.toContain('admin.upstream.discover')
    wrapper.unmount()
  })

  it('shows a low-balance badge from the saved provider state', async () => {
    mocks.list.mockResolvedValue({ items: [{ ...provider, low_balance_alert: true }], total: 1, page: 1, pages: 1,
      overview: { provider_count: 1, connected_count: 1, upstream_total_balance: 15.79 } })
    const wrapper = mountPage()
    await flushPromises()
    expect(wrapper.get('[data-testid="provider-low-balance-alert"]').text()).toBe('admin.upstream.lowBalanceAlert')
    wrapper.unmount()
  })

  it('renders cached provider balances without remote probes while the user total loads independently', async () => {
    let resolveUser!: (value: { metrics: { total_user_balance: number } }) => void
    const other: Provider = { ...provider, id: 'provider-2', name: '备用上游', metrics: { ...provider.metrics, balance: 4 } }
    mocks.list.mockResolvedValue({ items: [provider, other], total: 2, page: 1, pages: 1,
      overview: { provider_count: 2, connected_count: 2, upstream_total_balance: 19.79 } })
    mocks.dashboard.mockReturnValue(new Promise(resolve => { resolveUser = resolve }))
    const wrapper = mountPage()
    expect(wrapper.find('[data-testid="users-balance-loading"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="upstream-balance-loading"]').exists()).toBe(true)
    await flushPromises()
    expect(wrapper.get('[data-testid="upstream-metric"]').text()).toContain('¥19.79')
    expect(wrapper.text()).toContain('¥15.79')
    expect(wrapper.text()).toContain('¥4.00')
    expect(wrapper.find('[data-testid="upstream-balance-loading"]').exists()).toBe(false)
    expect(mocks.refresh).not.toHaveBeenCalled()

    resolveUser({ metrics: { total_user_balance: 1054.28 } })
    await flushPromises()
    expect(wrapper.text()).toContain('$1,054.28')
    expect(wrapper.find('[data-testid="users-balance-loading"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('opens the provider login form and refreshes a card', async () => {
    const wrapper = mountPage()
    await flushPromises()
    await wrapper.findAll('button').find(button => button.text() === 'admin.upstream.add')!.trigger('click')
    expect(wrapper.getComponent({ name: 'ProviderDialog' }).attributes('show')).toBe('true')
    await wrapper.get('[data-provider-id="provider-1"]').findAll('button').find(button => button.text() === 'admin.upstream.refresh')!.trigger('click')
    await flushPromises()
    expect(mocks.refresh).toHaveBeenCalledWith('provider-1')
    wrapper.unmount()
  })

  it('confirms deletion, removes the provider, and reloads the overview', async () => {
    let deleted = false
    mocks.delete.mockImplementation(async () => { deleted = true })
    mocks.list.mockImplementation(async () => ({
      items: deleted ? [] : [provider], total: deleted ? 0 : 1, page: 1, pages: 1,
      overview: { provider_count: deleted ? 0 : 1, connected_count: deleted ? 0 : 1, upstream_total_balance: deleted ? 0 : 15.79 }
    }))
    const wrapper = mountPage()
    await flushPromises()
    await wrapper.get('[data-provider-id="provider-1"] button[aria-label="admin.upstream.delete"]').trigger('click')
    const dialog = wrapper.getComponent({ name: 'ConfirmDialog' })
    expect(dialog.props('show')).toBe(true)
    dialog.vm.$emit('cancel')
    await flushPromises()
    expect(mocks.delete).not.toHaveBeenCalled()

    await wrapper.get('[data-provider-id="provider-1"] button[aria-label="admin.upstream.delete"]').trigger('click')
    dialog.vm.$emit('confirm')
    await flushPromises()
    expect(mocks.delete).toHaveBeenCalledExactlyOnceWith('provider-1')
    expect(wrapper.find('[data-provider-id="provider-1"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('admin.upstream.empty')
    expect(mocks.success).toHaveBeenCalledWith('admin.upstream.deleted')
    wrapper.unmount()
  })

  it('keeps the provider visible if deletion fails', async () => {
    mocks.delete.mockRejectedValue(new Error('delete failed'))
    const wrapper = mountPage()
    await flushPromises()
    await wrapper.get('[data-provider-id="provider-1"] button[aria-label="admin.upstream.delete"]').trigger('click')
    wrapper.getComponent({ name: 'ConfirmDialog' }).vm.$emit('confirm')
    await flushPromises()
    expect(wrapper.find('[data-provider-id="provider-1"]').exists()).toBe(true)
    expect(mocks.error).toHaveBeenCalledWith('admin.upstream.deleteFailed')
    wrapper.unmount()
  })

  it('filters providers from the themed status menu', async () => {
    const wrapper = mountPage()
    await flushPromises()
    await wrapper.get('button[aria-label="admin.upstream.filterStatus"]').trigger('click')
    const choices = document.body.querySelectorAll<HTMLElement>('[role="option"]')
    expect(Array.from(choices, choice => choice.textContent)).toEqual([
      'admin.upstream.allStatus', 'admin.upstream.connected', 'admin.upstream.disconnected'
    ])
    choices[1]?.click()
    await flushPromises()
    expect(mocks.list).toHaveBeenLastCalledWith(1, '', 'connected')
    wrapper.unmount()
  })

  it('silently polls saved balances while visible and stops after unmount', async () => {
    vi.useFakeTimers()
    try {
      const wrapper = mountPage()
      await flushPromises()
      const initialLoads = mocks.list.mock.calls.length
      expect(initialLoads).toBe(1)
      expect(mocks.refresh).not.toHaveBeenCalled()

      await vi.advanceTimersByTimeAsync(10_000)
      await flushPromises()
      expect(mocks.list.mock.calls.length).toBeGreaterThan(initialLoads)
      expect(wrapper.find('[data-testid="upstream-balance-loading"]').exists()).toBe(false)

      const beforeUnmount = mocks.list.mock.calls.length
      wrapper.unmount()
      await vi.advanceTimersByTimeAsync(10_000)
      expect(mocks.list.mock.calls.length).toBe(beforeUnmount)
    } finally {
      vi.useRealTimers()
    }
  })

  it('shows a newer saved balance on the next silent poll', async () => {
    vi.useFakeTimers()
    try {
    let balance = 15.79
    let userBalance = 1054.28
    mocks.list.mockImplementation(async () => ({
      items: [{ ...provider, metrics: { ...provider.metrics, balance } }],
      total: 1, page: 1, pages: 1,
      overview: { provider_count: 1, connected_count: 1, upstream_total_balance: balance }
    }))
    mocks.dashboard.mockImplementation(async () => ({ metrics: { total_user_balance: userBalance } }))
    const wrapper = mountPage()
    await flushPromises()
    expect(wrapper.get('[data-testid="upstream-metric"]').text()).toContain('¥15.79')
    expect(wrapper.text()).toContain('$1,054.28')
    balance = 20.5
    userBalance = 1070
    await vi.advanceTimersByTimeAsync(10_000)
    await flushPromises()
    expect(wrapper.get('[data-testid="upstream-metric"]').text()).toContain('¥20.50')
    expect(mocks.refresh).not.toHaveBeenCalled()
    wrapper.unmount()
    } finally { vi.useRealTimers() }
  })

  it('refreshes the user total silently while the page remains open', async () => {
    vi.useFakeTimers()
    try {
      let userBalance = 1054.28
      mocks.dashboard.mockImplementation(async () => ({ metrics: { total_user_balance: userBalance } }))
      const wrapper = mountPage()
      await flushPromises()
      expect(wrapper.get('[data-testid="users-metric"]').text()).toContain('$1,054.28')
      userBalance = 1070
      await vi.advanceTimersByTimeAsync(30_000)
      await flushPromises()
      expect(wrapper.get('[data-testid="users-metric"]').text()).toContain('$1,070.00')
      expect(wrapper.find('[data-testid="users-balance-loading"]').exists()).toBe(false)
      wrapper.unmount()
    } finally { vi.useRealTimers() }
  })

  it('pauses polling in a hidden tab and reloads cached values on return', async () => {
    vi.useFakeTimers()
    const hidden = vi.spyOn(document, 'hidden', 'get').mockReturnValue(true)
    try {
      const wrapper = mountPage()
      await flushPromises()
      const initialLoads = mocks.list.mock.calls.length
      await vi.advanceTimersByTimeAsync(10_000)
      expect(mocks.list.mock.calls.length).toBe(initialLoads)
      expect(mocks.refresh).not.toHaveBeenCalled()
      hidden.mockReturnValue(false)
      document.dispatchEvent(new Event('visibilitychange'))
      await flushPromises()
      expect(mocks.list.mock.calls.length).toBeGreaterThan(initialLoads)
      wrapper.unmount()
    } finally {
      hidden.mockRestore()
      vi.useRealTimers()
    }
  })
})
