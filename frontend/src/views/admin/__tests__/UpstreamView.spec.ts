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
})
