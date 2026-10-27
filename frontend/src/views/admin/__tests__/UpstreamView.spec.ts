import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import UpstreamView from '../UpstreamView.vue'
import type { Provider } from '@/api/admin/upstream'

const mocks = vi.hoisted(() => ({
  list: vi.fn(), refresh: vi.fn(), dashboard: vi.fn(), success: vi.fn(), error: vi.fn()
}))
vi.mock('@/api/admin/upstream', () => ({
  listProviders: mocks.list, refreshProvider: mocks.refresh
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
  })

  it('shows only site statistics and the added user balance', async () => {
    const wrapper = mountPage()
    await flushPromises()
    expect(mocks.list).toHaveBeenCalledWith(1, '', '')
    const text = wrapper.text()
    expect(text).toContain('杂鱼AI')
    expect(text).toContain('$15.79')
    expect(text).toContain('$1,054.28')
    expect(text).toContain('$0.70')
    expect(text).toContain('$9.21')
    expect(text).not.toContain('admin.upstream.recordBalance')
    expect(text).not.toContain('admin.upstream.disable')
    expect(text).not.toContain('admin.upstream.discover')
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

  it('automatically refreshes balances while visible and stops after unmount', async () => {
    vi.useFakeTimers()
    try {
      const wrapper = mountPage()
      await flushPromises()
      await vi.waitFor(() => expect(mocks.refresh).toHaveBeenCalledWith('provider-1'))
      await flushPromises()
      const initialLoads = mocks.list.mock.calls.length
      expect(initialLoads).toBeGreaterThan(1)

      await vi.advanceTimersByTimeAsync(60_000)
      await flushPromises()
      expect(mocks.list.mock.calls.length).toBeGreaterThan(initialLoads)

      const beforeUnmount = mocks.refresh.mock.calls.length
      wrapper.unmount()
      await vi.advanceTimersByTimeAsync(60_000)
      expect(mocks.refresh.mock.calls.length).toBe(beforeUnmount)
    } finally {
      vi.useRealTimers()
    }
  })

  it('replaces the displayed provider and user balances after a live probe', async () => {
    let balance = 15.79
    let userBalance = 1054.28
    mocks.list.mockImplementation(async () => ({
      items: [{ ...provider, metrics: { ...provider.metrics, balance } }],
      total: 1, page: 1, pages: 1,
      overview: { provider_count: 1, connected_count: 1, upstream_total_balance: balance }
    }))
    mocks.dashboard.mockImplementation(async () => ({ metrics: { total_user_balance: userBalance } }))
    mocks.refresh.mockImplementation(async () => {
      balance = 20.5
      userBalance = 1070
      return provider
    })
    const wrapper = mountPage()
    await flushPromises()
    expect(wrapper.text()).toContain('$20.50')
    expect(wrapper.text()).toContain('$1,070.00')
    wrapper.unmount()
  })

  it('pauses balance probes in a hidden tab and refreshes on return', async () => {
    vi.useFakeTimers()
    const hidden = vi.spyOn(document, 'hidden', 'get').mockReturnValue(true)
    try {
      const wrapper = mountPage()
      await flushPromises()
      await vi.advanceTimersByTimeAsync(60_000)
      expect(mocks.refresh).not.toHaveBeenCalled()
      hidden.mockReturnValue(false)
      document.dispatchEvent(new Event('visibilitychange'))
      await flushPromises()
      expect(mocks.refresh).toHaveBeenCalledWith('provider-1')
      wrapper.unmount()
    } finally {
      hidden.mockRestore()
      vi.useRealTimers()
    }
  })
})
