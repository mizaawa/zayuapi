import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import UpstreamView from '../UpstreamView.vue'
import type { Account } from '@/types'

const mocks = vi.hoisted(() => ({
  list: vi.fn(), getOperationsDashboard: vi.fn(), getAllGroups: vi.fn(), getAllProxies: vi.fn(),
  summary: vi.fn(), setBalance: vi.fn(), testAccount: vi.fn(),
  showError: vi.fn(), showSuccess: vi.fn()
}))
vi.mock('@/api/admin', () => ({ adminAPI: {
  accounts: { list: mocks.list, testAccount: mocks.testAccount },
  dashboard: { getOperationsDashboard: mocks.getOperationsDashboard },
  groups: { getAll: mocks.getAllGroups }, proxies: { getAll: mocks.getAllProxies }
} }))
vi.mock('@/api/admin/upstream', () => ({ getUpstreamBalanceSummary: mocks.summary, setUpstreamBalance: mocks.setBalance }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showError: mocks.showError, showSuccess: mocks.showSuccess }) }))
vi.mock('vue-i18n', async () => ({
  ...(await vi.importActual<typeof import('vue-i18n')>('vue-i18n')),
  useI18n: () => ({ t: (key: string) => key })
}))

const account = (id: number): Account => ({
  id, name: `Provider ${id}`, platform: 'openai', type: 'apikey', status: 'active',
  credentials: { base_url: 'https://example.test/v1' }, extra: {}, groups: []
} as unknown as Account)
const mountPage = () => mount(UpstreamView, { global: { stubs: {
  AppLayout: { template: '<div><slot /></div>' }, Icon: true, PlatformIcon: true,
  UpstreamAccountModal: true, EditAccountModal: true, ConfirmDialog: true
} } })

describe('UpstreamView', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mocks.list.mockResolvedValue({ items: Array.from({ length: 10 }, (_, index) => account(index + 1)), total: 19, page: 1, page_size: 10, pages: 2 })
    mocks.summary.mockResolvedValue({ total_usd: 12.34, known_count: 1, unknown_count: 18 })
    mocks.getOperationsDashboard.mockResolvedValue({ metrics: { total_user_balance: 1054.277 } })
    mocks.getAllGroups.mockResolvedValue([])
    mocks.getAllProxies.mockResolvedValue([])
    mocks.setBalance.mockResolvedValue({ account_id: 1, balance_usd: 0 })
  })

  it('limits the page to ten and shows aggregate balances without mistaking unknown for zero', async () => {
    const wrapper = mountPage()
    await flushPromises()
    expect(mocks.list).toHaveBeenCalledWith(1, 10, { search: '', status: '' })
    expect(wrapper.findAll('[data-account-id]')).toHaveLength(10)
    expect(wrapper.text()).toContain('$12.34')
    expect(wrapper.text()).toContain('$1,054.28')
    expect(wrapper.get('[data-account-id="1"]').text()).toContain('—')
    wrapper.unmount()
  })

  it('records a known zero balance through the Go API and reloads the all-account summary', async () => {
    const wrapper = mountPage()
    await flushPromises()
    const row = wrapper.get('[data-account-id="1"]')
    await row.findAll('button').find(button => button.text() === 'admin.upstream.recordBalance')!.trigger('click')
    await row.get('input[type="number"]').setValue('0')
    await row.findAll('button').find(button => button.text() === 'common.save')!.trigger('click')
    await flushPromises()
    expect(mocks.setBalance).toHaveBeenCalledWith(1, 0)
    expect(mocks.summary).toHaveBeenCalledTimes(2)
    wrapper.unmount()
  })

  it('opens the dedicated provider form from the add button', async () => {
    const wrapper = mountPage()
    await flushPromises()
    await wrapper.findAll('button').find(button => button.text() === 'admin.upstream.add')!.trigger('click')
    expect(wrapper.getComponent({ name: 'UpstreamAccountModal' }).attributes('show')).toBe('true')
    wrapper.unmount()
  })
})
