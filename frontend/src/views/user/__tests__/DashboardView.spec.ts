import { reactive, defineComponent, h } from 'vue'
import { mount, flushPromises } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import DashboardView from '../DashboardView.vue'
import UserDashboardStats from '@/components/user/dashboard/UserDashboardStats.vue'

const mocks = vi.hoisted(() => ({
  stats: vi.fn(), recent: vi.fn(), trend: vi.fn(), models: vi.fn(), quotas: vi.fn(), refreshUser: vi.fn()
}))
const auth = reactive({ user: { id: 7, balance: 25 }, isSimpleMode: false, isAuthenticated: true, refreshUser: mocks.refreshUser })
vi.mock('@/stores/auth', () => ({ useAuthStore: () => auth }))
vi.mock('@/api/usage', () => ({ usageAPI: { getDashboardStats: mocks.stats, getDashboardRecent: mocks.recent, getDashboardTrend: mocks.trend, getDashboardModels: mocks.models } }))
vi.mock('@/api/user', () => ({ getMyPlatformQuotas: mocks.quotas }))
vi.mock('vue-i18n', async () => ({
  ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'),
  useI18n: () => ({ t: (key: string) => key })
}))

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (error: Error) => void
  const promise = new Promise<T>((res, rej) => { resolve = res; reject = rej })
  return { promise, resolve, reject }
}

const stats = {
  total_api_keys: 2, active_api_keys: 1, today_requests: 5, today_tokens: 123,
  today_cost: 2, today_actual_cost: 1, total_requests: 0, total_cost: 0, total_actual_cost: 0,
  total_tokens: 0, totals_pending: true, rpm: 1, tpm: 30
}
const statsStub = defineComponent({
  props: ['stats', 'balance'],
  setup(props) { return () => h('div', { 'data-testid': 'stats' }, JSON.stringify(props)) }
})
const chartsStub = defineComponent({
  props: ['trend', 'models', 'loading'], emits: ['refresh', 'dateRangeChange'],
  setup(props, { emit }) {
    return () => h('div', { 'data-testid': 'charts' }, [
      JSON.stringify(props),
      h('button', { 'data-testid': 'refresh', onClick: () => emit('refresh') }, 'Refresh'),
      h('button', { 'data-testid': 'range', onClick: () => emit('dateRangeChange') }, 'Range')
    ])
  }
})
function mountDashboard() {
  return mount(DashboardView, { global: { stubs: {
    AppLayout: { template: '<div><slot /></div>' }, LoadingSpinner: true, Icon: true,
    UserDashboardStats: statsStub, UserDashboardCharts: chartsStub,
    UserDashboardRecentUsage: { props: ['data', 'loading'], template: '<div data-testid="recent">{{ data }}</div>' },
    UserDashboardQuickActions: { template: '<div data-testid="actions">Actions</div>' }
  } } })
}

beforeEach(() => {
  vi.clearAllMocks()
  auth.user = { id: 7, balance: 25 }
  auth.isAuthenticated = true
  mocks.refreshUser.mockResolvedValue(auth.user)
  mocks.stats.mockResolvedValue(stats)
  mocks.trend.mockResolvedValue({ trend: [] })
  mocks.models.mockResolvedValue({ models: [] })
  mocks.recent.mockResolvedValue({ items: [] })
  mocks.quotas.mockResolvedValue({ platform_quotas: [] })
})

describe('Dashboard loading', () => {
  it('keeps other sections usable while statistics and profile refresh are pending', async () => {
    const pending = deferred<typeof stats>()
    mocks.stats.mockReturnValue(pending.promise)
    mocks.refreshUser.mockReturnValue(new Promise(() => {}))
    const wrapper = mountDashboard()
    await flushPromises()
    expect(wrapper.find('[data-testid="actions"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="recent"]').exists()).toBe(true)
    expect(mocks.stats).toHaveBeenCalledWith({ include_totals: false }, expect.objectContaining({ signal: expect.any(AbortSignal) }))
    expect(mocks.stats).toHaveBeenCalledTimes(1)
    pending.resolve(stats)
    await flushPromises()
    expect(wrapper.find('[data-testid="stats"]').text()).toContain('"today_requests":5')
    expect(mocks.stats).toHaveBeenCalledWith({ include_totals: true }, expect.any(Object))
    wrapper.unmount()
  })

  it('retains current-day statistics and allows retry when historical statistics fail', async () => {
    mocks.stats.mockResolvedValueOnce(stats).mockRejectedValueOnce(new Error('query timeout'))
    const wrapper = mountDashboard()
    await flushPromises()
    expect(wrapper.find('[role="alert"]').text()).toContain('dashboard.totalsLoadFailed')
    expect(wrapper.find('[data-testid="stats"]').text()).toContain('"today_requests":5')
    mocks.stats.mockResolvedValueOnce({ ...stats, total_requests: 100, totals_pending: false })
    await wrapper.find('[aria-label="common.refresh"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="stats"]').text()).toContain('"total_requests":100')
    wrapper.unmount()
  })

  it('shows recoverable errors instead of a blank dashboard', async () => {
    mocks.stats.mockRejectedValue(new Error('unavailable'))
    mocks.recent.mockRejectedValue(new Error('unavailable'))
    mocks.trend.mockRejectedValue(new Error('unavailable'))
    const wrapper = mountDashboard()
    await flushPromises()
    expect(wrapper.findAll('[role="alert"]')).toHaveLength(3)
    expect(wrapper.find('[data-testid="actions"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('ignores stale history and cancels requests when refreshing', async () => {
    const old = deferred<typeof stats>()
    mocks.stats.mockResolvedValueOnce(stats).mockReturnValueOnce(old.promise)
    const wrapper = mountDashboard()
    await flushPromises()
    const oldSignal = mocks.stats.mock.calls[1][1].signal as AbortSignal
    mocks.stats.mockResolvedValueOnce({ ...stats, today_requests: 10, totals_pending: false })
    await wrapper.find('[data-testid="refresh"]').trigger('click')
    await flushPromises()
    expect(oldSignal.aborted).toBe(true)
    old.resolve({ ...stats, today_requests: 1 })
    await flushPromises()
    expect(wrapper.find('[data-testid="stats"]').text()).toContain('"today_requests":10')
    wrapper.unmount()
  })

  it('clears the prior account and discards its pending responses', async () => {
    const oldRecent = deferred<{ items: { id: number }[] }>()
    const oldStats = deferred<typeof stats>()
    mocks.stats.mockReturnValueOnce(oldStats.promise)
    mocks.recent.mockReturnValueOnce(oldRecent.promise)
    const wrapper = mountDashboard()
    await flushPromises()
    const oldSignal = mocks.stats.mock.calls[0][1].signal as AbortSignal
    mocks.stats.mockResolvedValue({ ...stats, today_requests: 20, totals_pending: false })
    auth.user = { id: 8, balance: 9 }
    await flushPromises()
    expect(oldSignal.aborted).toBe(true)
    oldStats.resolve({ ...stats, today_requests: 999 })
    oldRecent.resolve({ items: [{ id: 999 }] })
    await flushPromises()
    expect(wrapper.find('[data-testid="stats"]').text()).toContain('"today_requests":20')
    expect(wrapper.find('[data-testid="recent"]').text()).not.toContain('999')
    wrapper.unmount()
  })

  it('ignores older chart results after changing the range', async () => {
    const old = deferred<{ trend: { date: string }[] }>()
    mocks.trend.mockReturnValueOnce(old.promise)
    const wrapper = mountDashboard()
    await flushPromises()
    const oldSignal = mocks.trend.mock.calls[0][1].signal as AbortSignal
    mocks.trend.mockResolvedValueOnce({ trend: [{ date: 'new' }] })
    await wrapper.find('[data-testid="range"]').trigger('click')
    await flushPromises()
    expect(oldSignal.aborted).toBe(true)
    old.resolve({ trend: [{ date: 'old' }] })
    await flushPromises()
    expect(wrapper.find('[data-testid="charts"]').text()).toContain('new')
    expect(wrapper.find('[data-testid="charts"]').text()).not.toContain('old')
    wrapper.unmount()
  })

  it('cancels requests on unmount', async () => {
    mocks.stats.mockReturnValue(new Promise(() => {}))
    const wrapper = mountDashboard()
    await flushPromises()
    wrapper.unmount()
    for (const mock of [mocks.stats, mocks.trend, mocks.models, mocks.recent, mocks.quotas]) {
      const options = mock === mocks.recent || mock === mocks.quotas ? mock.mock.calls[0][0] : mock.mock.calls[0][1]
      expect(options.signal.aborted).toBe(true)
    }
  })
})

it('shows placeholders for unloaded lifetime amounts without masking current-day values', () => {
  const wrapper = mount(UserDashboardStats, { props: { stats: stats as never, balance: 25, isSimple: false }, global: { stubs: { Icon: true } } })
  expect(wrapper.text()).toContain('dashboard.todayRequests')
  expect(wrapper.text()).toContain('common.total: -')
  expect(wrapper.text()).not.toContain('$0.00')
  wrapper.unmount()
})
