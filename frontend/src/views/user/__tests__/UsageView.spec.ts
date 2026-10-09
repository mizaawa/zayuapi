import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

import UsageView from '../UsageView.vue'

const {
  query,
  getStats,
  getDashboardModels,
  getDashboardSnapshotV2,
  list,
  getAvailable,
  showError,
  showWarning,
  showSuccess,
  showInfo,
  aoaToSheet,
  saveAs,
  xlsxWrite,
} = vi.hoisted(() => ({
  query: vi.fn(),
  getStats: vi.fn(),
  getDashboardModels: vi.fn(),
  getDashboardSnapshotV2: vi.fn(),
  list: vi.fn(),
  getAvailable: vi.fn(),
  showError: vi.fn(),
  showWarning: vi.fn(),
  showSuccess: vi.fn(),
  showInfo: vi.fn(),
  aoaToSheet: vi.fn((data: (string | number)[][]) => ({ data })),
  saveAs: vi.fn(),
  xlsxWrite: vi.fn(() => new Uint8Array([1, 2, 3])),
}))

const messages: Record<string, string> = {
  'admin.dashboard.timeRange': 'Time range',
  'admin.dashboard.granularity': 'Granularity',
  'admin.dashboard.day': 'Day',
  'admin.dashboard.hour': 'Hour',
  'admin.users.columnSettings': 'Columns',
  'admin.usage.group': 'Group',
  'admin.usage.billingType': 'Billing type',
  'admin.usage.billingMode': 'Billing mode',
  'admin.usage.allTypes': 'All types',
  'admin.usage.allBillingTypes': 'All billing types',
  'admin.usage.billingTypeBalance': 'Balance',
  'admin.usage.billingTypeSubscription': 'Subscription',
  'admin.usage.allBillingModes': 'All billing modes',
  'admin.usage.billingModeToken': 'Token',
  'admin.usage.billingModePerRequest': 'Per request',
  'admin.usage.billingModeImage': 'Image',
  'admin.usage.allGroups': 'All groups',
  'admin.usage.allModels': 'All models',
  'usage.allApiKeys': 'All API Keys',
  'usage.apiKeyFilter': 'API Key',
  'usage.model': 'Model',
  'usage.type': 'Type',
  'usage.ws': 'WS',
  'usage.stream': 'Stream',
  'usage.sync': 'Sync',
  'usage.exporting': 'Exporting',
  'usage.exportCsv': 'Export CSV',
  'usage.exportExcel': 'Export Excel',
  'usage.cacheHitRate': 'Cache hit rate',
  'usage.failedToLoad': 'Failed to load',
  'usage.noDataToExport': 'No data',
  'usage.preparingExport': 'Preparing export',
  'usage.exportSuccess': 'Export success',
  'usage.exportFailed': 'Export failed',
  'common.refresh': 'Refresh',
  'common.reset': 'Reset',
}

vi.mock('@/api', () => ({
  usageAPI: {
    query,
    getStats,
    getDashboardModels,
    getDashboardSnapshotV2,
  },
  keysAPI: {
    list,
  },
  userGroupsAPI: {
    getAvailable,
  },
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showError, showWarning, showSuccess, showInfo }),
}))

vi.mock('file-saver', () => ({ saveAs }))

vi.mock('xlsx', () => ({
  utils: {
    aoa_to_sheet: aoaToSheet,
    book_new: vi.fn(() => ({})),
    book_append_sheet: vi.fn(),
  },
  write: xlsxWrite,
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => messages[key] ?? key,
    }),
  }
})

const simpleStub = { template: '<div><slot /></div>' }
const chartStub = { template: '<div />' }

const usageLog = {
  id: 1,
  request_id: 'req-user-export',
  actual_cost: 0.092883,
  total_cost: 0.092883,
  rate_multiplier: 1,
  service_tier: 'priority',
  input_cost: 0.020285,
  output_cost: 0.00303,
  cache_creation_cost: 0.000001,
  cache_read_cost: 0.069568,
  input_tokens: 4057,
  output_tokens: 101,
  cache_creation_tokens: 4,
  cache_read_tokens: 278272,
  cache_creation_5m_tokens: 0,
  cache_creation_1h_tokens: 0,
  image_count: 0,
  image_size: null,
  first_token_ms: 12,
  duration_ms: 345,
  created_at: '2026-03-08T00:00:00Z',
  model: 'gpt-5.4',
  reasoning_effort: null,
  ip_address: '203.0.113.10',
  api_key: { name: 'demo-key' },
  billing_mode: 'token',
  request_type: 'sync',
  stream: false,
}

const modelStats = [{ model: 'gpt-5.4', requests: 1, input_tokens: 10, output_tokens: 20, cache_creation_tokens: 0, cache_read_tokens: 0, total_tokens: 30, cost: 0.1, actual_cost: 0.08 }]
const groupStats = [{ group_id: 1, group_name: 'default', requests: 1, total_tokens: 30, cost: 0.1, actual_cost: 0.08 }]
const trend = [{ date: '2026-03-08', requests: 1, total_tokens: 30 }]

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (error: unknown) => void
  const promise = new Promise<T>((res, rej) => { resolve = res; reject = rej })
  return { promise, resolve, reject }
}

function mountUsageView() {
  return mount(UsageView, {
    global: {
      stubs: {
        AppLayout: simpleStub,
        Pagination: true,
        Select: true,
        DateRangePicker: true,
        Icon: true,
        UsageStatsCards: chartStub,
        UsageTable: chartStub,
        ModelDistributionChart: chartStub,
        GroupDistributionChart: chartStub,
        EndpointDistributionChart: chartStub,
        TokenUsageTrend: chartStub,
      },
    },
  })
}

describe('user UsageView', () => {
  afterEach(() => {
    vi.useRealTimers()
  })

  beforeEach(() => {
    query.mockReset()
    getStats.mockReset()
    getDashboardModels.mockReset()
    getDashboardSnapshotV2.mockReset()
    list.mockReset()
    getAvailable.mockReset()
    showError.mockReset()
    showWarning.mockReset()
    showSuccess.mockReset()
    showInfo.mockReset()
    aoaToSheet.mockClear()
    saveAs.mockClear()
    xlsxWrite.mockClear()
    localStorage.clear()

    query.mockResolvedValue({ items: [usageLog], total: 1, pages: 1 })
    getStats.mockResolvedValue({
      total_requests: 1,
      total_input_tokens: 10,
      total_output_tokens: 20,
      total_cache_tokens: 0,
      total_tokens: 30,
      total_cost: 0.1,
      total_actual_cost: 0.08,
      average_duration_ms: 12,
      endpoints: [],
      upstream_endpoints: [],
      endpoint_paths: [],
    })
    getDashboardModels.mockResolvedValue({
      models: [{ model: 'gpt-5.4', requests: 1, input_tokens: 10, output_tokens: 20, cache_creation_tokens: 0, cache_read_tokens: 0, total_tokens: 30, cost: 0.1, actual_cost: 0.08 }],
      start_date: '2026-03-08',
      end_date: '2026-03-08',
    })
    getDashboardSnapshotV2.mockResolvedValue({
      generated_at: '2026-03-08T00:00:00Z',
      start_date: '2026-03-08',
      end_date: '2026-03-08',
      granularity: 'hour',
      models: modelStats,
      trend,
      groups: groupStats,
    })
    list.mockResolvedValue({ items: [{ id: 1, name: 'demo-key' }] })
    getAvailable.mockResolvedValue([{ id: 1, name: 'default' }])
  })

  it('shows cache hit rate between tokens and cost and persists its column setting', async () => {
    localStorage.setItem('user-usage-hidden-columns', JSON.stringify(['model']))
    const wrapper = mountUsageView()
    await flushPromises()

    const vm = wrapper.vm as any
    const columnKeys = vm.visibleColumns.map((column: { key: string }) => column.key)
    const tokensIndex = columnKeys.indexOf('tokens')
    expect(columnKeys.slice(tokensIndex, tokensIndex + 3)).toEqual(['tokens', 'cache_hit_rate', 'cost'])

    await wrapper.get('button[title="Columns"]').trigger('click')
    const cacheHitRateButton = wrapper.findAll('button').find((button) => button.text() === 'Cache hit rate')
    expect(cacheHitRateButton).toBeDefined()
    await cacheHitRateButton!.trigger('click')

    expect(vm.visibleColumns.map((column: { key: string }) => column.key)).not.toContain('cache_hit_rate')
    expect(JSON.parse(localStorage.getItem('user-usage-hidden-columns')!)).toContain('cache_hit_rate')
    wrapper.unmount()

    const restored = mountUsageView()
    await flushPromises()
    expect((restored.vm as any).visibleColumns.map((column: { key: string }) => column.key)).not.toContain('cache_hit_rate')
    restored.unmount()
  })

  it('loads logs without counting history and loads each chart independently', async () => {
    const wrapper = mountUsageView()
    await flushPromises()

    expect(query).toHaveBeenCalledWith(expect.objectContaining({ exact_total: false }), expect.objectContaining({ signal: expect.any(AbortSignal) }))
    expect(getStats).toHaveBeenCalledWith(expect.any(Object), undefined, expect.objectContaining({ signal: expect.any(AbortSignal) }))
    expect(getDashboardModels).not.toHaveBeenCalled()
    expect(getDashboardSnapshotV2).toHaveBeenCalledTimes(3)
    for (const key of ['include_trend', 'include_model_stats', 'include_group_stats']) {
      expect(getDashboardSnapshotV2).toHaveBeenCalledWith(expect.objectContaining({
        include_trend: key === 'include_trend',
        include_model_stats: key === 'include_model_stats',
        include_group_stats: key === 'include_group_stats',
      }), expect.objectContaining({ signal: expect.any(AbortSignal) }))
    }
    expect(list).toHaveBeenCalledWith(1, 100)
    expect(getAvailable).toHaveBeenCalled()
    wrapper.unmount()
  })

  it('recovers first-entry statistics and distributions automatically without reloading healthy sections', async () => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
    getStats.mockRejectedValueOnce({ status: 0, code: 'ECONNABORTED' })
    let modelAttempts = 0
    let groupAttempts = 0
    getDashboardSnapshotV2.mockImplementation((params) => {
      if (params.include_model_stats && ++modelAttempts === 1) return Promise.reject({ status: 503 })
      if (params.include_group_stats && ++groupAttempts === 1) return Promise.reject({ status: 504 })
      return Promise.resolve({ models: modelStats, groups: groupStats, trend })
    })
    const wrapper = mountUsageView()
    await flushPromises()
    const vm = wrapper.vm as any

    expect(wrapper.findAll('[data-usage-error]')).toHaveLength(0)
    expect(vm.endpointStatsLoading).toBe(true)
    expect(vm.modelStatsLoading).toBe(true)
    expect(vm.groupStatsLoading).toBe(true)
    expect(vm.usageLogs).toEqual([usageLog])
    expect(vm.trendData).toEqual(trend)

    await vi.advanceTimersByTimeAsync(500)
    await flushPromises()

    expect(vm.usageStats.total_requests).toBe(1)
    expect(vm.requestedModelStats).toEqual(modelStats)
    expect(vm.groupStats).toEqual(groupStats)
    expect(vm.endpointStatsLoading).toBe(false)
    expect(vm.modelStatsLoading).toBe(false)
    expect(vm.groupStatsLoading).toBe(false)
    expect(wrapper.findAll('[data-usage-error]')).toHaveLength(0)
    expect(showError).not.toHaveBeenCalled()
    expect(getStats).toHaveBeenCalledTimes(2)
    expect(modelAttempts).toBe(2)
    expect(groupAttempts).toBe(2)
    expect(query).toHaveBeenCalledTimes(1)
    expect(getDashboardSnapshotV2.mock.calls.filter(([params]) => params.include_trend)).toHaveLength(1)
    wrapper.unmount()
  })

  it('loads all first-entry aggregates without overlapping database requests', async () => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
    let active = 0
    let maxActive = 0
    const read = (data: unknown) => {
      active++
      maxActive = Math.max(maxActive, active)
      if (active > 1) {
        active--
        return Promise.reject({ status: 500, message: 'internal error' })
      }
      return new Promise((resolve) => setTimeout(() => { active--; resolve(data) }, 10))
    }
    getStats.mockImplementation(() => read({ total_requests: 1, endpoints: [] }))
    getDashboardSnapshotV2.mockImplementation(() => read({ models: modelStats, groups: groupStats, trend }))
    const wrapper = mountUsageView()
    await flushPromises()
    expect((wrapper.vm as any).usageLogs).toEqual([usageLog])
    expect(getStats).toHaveBeenCalledTimes(1)
    expect(getDashboardSnapshotV2).not.toHaveBeenCalled()

    await vi.advanceTimersByTimeAsync(40)
    await flushPromises()
    const vm = wrapper.vm as any
    expect(maxActive).toBe(1)
    expect(vm.usageStats.total_requests).toBe(1)
    expect(vm.requestedModelStats).toEqual(modelStats)
    expect(vm.groupStats).toEqual(groupStats)
    expect(vm.trendData).toEqual(trend)
    expect(getStats).toHaveBeenCalledTimes(1)
    expect(getDashboardSnapshotV2).toHaveBeenCalledTimes(3)
    expect(wrapper.findAll('[data-usage-error]')).toHaveLength(0)
    expect(vi.getTimerCount()).toBe(0)
    wrapper.unmount()
  })

  it('shows a section error only after automatic retries are exhausted and still supports manual retry', async () => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
    getDashboardSnapshotV2.mockImplementation((params) => params.include_group_stats
      ? Promise.reject({ status: 503 })
      : Promise.resolve({ models: modelStats, trend }))
    const wrapper = mountUsageView()
    await flushPromises()
    const vm = wrapper.vm as any
    const groupCalls = () => getDashboardSnapshotV2.mock.calls.filter(([params]) => params.include_group_stats)

    expect(vm.groupStatsLoading).toBe(true)
    expect(wrapper.find('[data-usage-error="groups"]').exists()).toBe(false)
    await vi.advanceTimersByTimeAsync(500)
    await flushPromises()
    expect(groupCalls()).toHaveLength(2)
    expect(wrapper.find('[data-usage-error="groups"]').exists()).toBe(false)
    await vi.advanceTimersByTimeAsync(999)
    expect(groupCalls()).toHaveLength(2)
    await vi.advanceTimersByTimeAsync(1)
    await flushPromises()

    expect(groupCalls()).toHaveLength(3)
    expect(vm.groupStatsLoading).toBe(false)
    expect(wrapper.findAll('[data-usage-error]')).toHaveLength(1)
    expect(wrapper.find('[data-usage-error="groups"]').exists()).toBe(true)
    expect(vm.usageLogs).toEqual([usageLog])
    expect(vm.requestedModelStats).toEqual(modelStats)
    expect(vm.trendData).toEqual(trend)
    await vi.advanceTimersByTimeAsync(10000)
    expect(groupCalls()).toHaveLength(3)

    getDashboardSnapshotV2.mockResolvedValue({ groups: groupStats })
    await wrapper.get('[data-usage-error="groups"] button').trigger('click')
    await flushPromises()
    expect(vm.groupStats).toEqual(groupStats)
    expect(wrapper.findAll('[data-usage-error]')).toHaveLength(0)
    expect(groupCalls()).toHaveLength(4)
    expect(query).toHaveBeenCalledTimes(1)
    expect(getStats).toHaveBeenCalledTimes(1)
    wrapper.unmount()
  })

  it('cancels pending retries when filters change and loads only the new filter values', async () => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
    getStats.mockRejectedValueOnce({ status: 503 })
    getDashboardSnapshotV2.mockImplementation((params) => params.model === 'new-model'
      ? Promise.resolve({ models: [{ ...modelStats[0], model: 'new-model' }], groups: groupStats, trend })
      : Promise.reject({ status: 504 }))
    const wrapper = mountUsageView()
    await flushPromises()
    const oldSignals = [getStats.mock.calls[0][2].signal,
      ...getDashboardSnapshotV2.mock.calls.map((call) => call[1].signal)] as AbortSignal[]
    const vm = wrapper.vm as any

    vm.filters.model = 'new-model'
    vm.applyFilters()
    await flushPromises()
    expect(oldSignals.every((signal) => signal.aborted)).toBe(true)
    expect(vm.requestedModelStats[0].model).toBe('new-model')
    expect(vm.usageStats.total_requests).toBe(1)
    expect(wrapper.findAll('[data-usage-error]')).toHaveLength(0)
    expect(getStats).toHaveBeenLastCalledWith(expect.objectContaining({ model: 'new-model' }), undefined, expect.anything())
    expect(getDashboardSnapshotV2.mock.calls.slice(3).every(([params]) => params.model === 'new-model')).toBe(true)

    await vi.advanceTimersByTimeAsync(10000)
    await flushPromises()
    expect(getStats).toHaveBeenCalledTimes(2)
    expect(getDashboardSnapshotV2).toHaveBeenCalledTimes(6)
    expect(query).toHaveBeenCalledTimes(2)
    expect(showError).not.toHaveBeenCalled()
    expect(wrapper.findAll('[data-usage-error]')).toHaveLength(0)
    wrapper.unmount()
  })

  it('clears pending retries on unmount without issuing follow-up requests', async () => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
    query.mockRejectedValue({ status: 503 })
    getStats.mockRejectedValue({ status: 503 })
    getDashboardSnapshotV2.mockRejectedValue({ status: 504 })
    const wrapper = mountUsageView()
    await flushPromises()

    expect(vi.getTimerCount()).toBe(5)
    expect(showError).not.toHaveBeenCalled()
    wrapper.unmount()
    await flushPromises()
    expect(vi.getTimerCount()).toBe(0)
    await vi.advanceTimersByTimeAsync(10000)
    expect(query).toHaveBeenCalledTimes(1)
    expect(getStats).toHaveBeenCalledTimes(1)
    expect(getDashboardSnapshotV2).toHaveBeenCalledTimes(3)
    expect(showError).not.toHaveBeenCalled()
  })

  it('recovers a failed list without reloading successful statistics and charts', async () => {
    query.mockRejectedValueOnce({ status: 400 })
    const wrapper = mountUsageView()
    await flushPromises()
    const vm = wrapper.vm as any
    expect(wrapper.find('[data-usage-error="logs"]').exists()).toBe(true)
    expect(vm.usageStats.total_requests).toBe(1)
    expect(vm.requestedModelStats).toEqual(modelStats)
    expect(vm.groupStats).toEqual(groupStats)
    expect(vm.trendData).toEqual(trend)
    await wrapper.get('[data-usage-error="logs"] button').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-usage-error="logs"]').exists()).toBe(false)
    expect(vm.usageLogs).toEqual([usageLog])
    expect(vm.loading).toBe(false)
    expect(getStats).toHaveBeenCalledTimes(1)
    expect(getDashboardSnapshotV2).toHaveBeenCalledTimes(3)
    wrapper.unmount()
  })

  it('keeps healthy charts visible when one section fails and retries just that section', async () => {
    getDashboardSnapshotV2.mockImplementation((params) => params.include_group_stats
      ? Promise.reject({ status: 400 })
      : Promise.resolve({ models: modelStats, trend }))
    const wrapper = mountUsageView()
    await flushPromises()
    const vm = wrapper.vm as any
    expect(vm.usageLogs).toEqual([usageLog])
    expect(vm.requestedModelStats).toEqual(modelStats)
    expect(vm.trendData).toEqual(trend)
    expect(wrapper.findAll('[data-usage-error]')).toHaveLength(1)
    expect(wrapper.find('[data-usage-error="groups"]').exists()).toBe(true)
    getDashboardSnapshotV2.mockResolvedValue({ groups: groupStats })
    await wrapper.get('[data-usage-error="groups"] button').trigger('click')
    await flushPromises()
    expect(vm.groupStats).toEqual(groupStats)
    expect(vm.requestedModelStats).toEqual(modelStats)
    expect(vm.trendData).toEqual(trend)
    expect(wrapper.findAll('[data-usage-error]')).toHaveLength(0)
    expect(getDashboardSnapshotV2).toHaveBeenCalledTimes(4)
    expect(query).toHaveBeenCalledTimes(1)
    wrapper.unmount()
  })

  it('uses the matching stats count after logs arrive and still permits paging if stats fail', async () => {
    const stats = deferred<{ total_requests: number }>()
    getStats.mockReturnValueOnce(stats.promise)
    query.mockResolvedValue({ items: [usageLog], total: 21, total_is_exact: false })
    const wrapper = mountUsageView()
    await flushPromises()
    const vm = wrapper.vm as any
    expect(vm.paginationTotal).toBe(21)
    expect(vm.paginationTotalIsExact).toBe(false)
    stats.resolve({ total_requests: 85 })
    await flushPromises()
    expect(vm.paginationTotal).toBe(85)
    expect(vm.paginationTotalIsExact).toBe(true)

    getStats.mockRejectedValueOnce({ status: 400 })
    vm.filters.model = 'another-model'
    vm.applyFilters()
    await flushPromises()
    expect(vm.paginationTotal).toBe(21)
    expect(vm.paginationTotalIsExact).toBe(false)
    expect(wrapper.find('[data-usage-error="stats"]').exists()).toBe(true)
    query.mockResolvedValueOnce({ items: [usageLog], total: 41, total_is_exact: false })
    vm.handlePageChange(2)
    await flushPromises()
    expect(vm.pagination.page).toBe(2)
    expect(vm.paginationTotal).toBe(41)
    wrapper.unmount()
  })

  it('aborts obsolete requests and ignores late failures after filters change', async () => {
    const oldLogs = deferred<unknown>()
    const oldStats = deferred<unknown>()
    query.mockReturnValueOnce(oldLogs.promise)
    getStats.mockReturnValueOnce(oldStats.promise)
    const wrapper = mountUsageView()
    await flushPromises()
    expect(getDashboardSnapshotV2).not.toHaveBeenCalled()
    const oldSignals = [query.mock.calls[0][1].signal, getStats.mock.calls[0][2].signal,
      ...getDashboardSnapshotV2.mock.calls.map((call) => call[1].signal)] as AbortSignal[]
    const vm = wrapper.vm as any
    vm.filters.model = 'gpt-5.4'
    vm.applyFilters()
    await flushPromises()
    expect(oldSignals.every((signal) => signal.aborted)).toBe(true)
    oldLogs.reject(new Error('late network failure'))
    oldStats.resolve({ total_requests: 999 })
    await flushPromises()
    expect(getDashboardSnapshotV2).toHaveBeenCalledTimes(3)
    expect(getDashboardSnapshotV2.mock.calls.every(([params]) => params.model === 'gpt-5.4')).toBe(true)
    expect(showError).not.toHaveBeenCalled()
    expect(wrapper.findAll('[data-usage-error]')).toHaveLength(0)
    expect(vm.usageStats.total_requests).toBe(1)
    expect(vm.requestedModelStats).toEqual(modelStats)
    expect(vm.trendData).toEqual(trend)
    expect(vm.loading).toBe(false)
    wrapper.unmount()
  })

  it('cancels page requests on unmount and suppresses late errors', async () => {
    const logs = deferred<unknown>()
    const stats = deferred<unknown>()
    query.mockReturnValue(logs.promise)
    getStats.mockReturnValue(stats.promise)
    const wrapper = mountUsageView()
    await flushPromises()
    const signals = [query.mock.calls[0][1].signal, getStats.mock.calls[0][2].signal,
      ...getDashboardSnapshotV2.mock.calls.map((call) => call[1].signal)] as AbortSignal[]
    wrapper.unmount()
    expect(signals.every((signal) => signal.aborted)).toBe(true)
    logs.reject(new Error('after navigation'))
    stats.reject(new Error('after navigation'))
    await flushPromises()
    expect(getDashboardSnapshotV2).not.toHaveBeenCalled()
    expect(showError).not.toHaveBeenCalled()
  })

  it.each(['keys', 'groups'])('preserves available filter options when %s fail', async (failed) => {
    const failingRequest = failed === 'keys' ? list : getAvailable
    failingRequest.mockRejectedValueOnce(new Error('temporary failure'))
    const wrapper = mountUsageView()
    await flushPromises()
    const vm = wrapper.vm as any
    expect(failed === 'keys' ? vm.groups : vm.apiKeys).toHaveLength(1)
    expect(wrapper.find('[data-usage-error="filters"]').exists()).toBe(true)
    await wrapper.get('[data-usage-error="filters"] button').trigger('click')
    await flushPromises()
    expect(vm.groups).toHaveLength(1)
    expect(vm.apiKeys).toHaveLength(1)
    expect(wrapper.find('[data-usage-error="filters"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('preserves existing chart data after a refresh failure', async () => {
    const wrapper = mountUsageView()
    await flushPromises()
    getDashboardSnapshotV2.mockRejectedValue({ status: 400 })
    await (wrapper.vm as any).loadGroupStats()
    expect((wrapper.vm as any).groupStats).toEqual(groupStats)
    expect(wrapper.find('[data-usage-error="groups"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('retains totals and retries when the server reports incomplete endpoint statistics', async () => {
    getStats.mockResolvedValueOnce({ total_requests: 1, endpoints: [], endpoints_unavailable: true })
    const wrapper = mountUsageView()
    await flushPromises()
    expect((wrapper.vm as any).usageStats.total_requests).toBe(1)
    expect(wrapper.get('[data-usage-error="stats"]').text()).toContain('usage.endpointStatsLoadFailed')
    await wrapper.get('[data-usage-error="stats"] button').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-usage-error="stats"]').exists()).toBe(false)
    expect(query).toHaveBeenCalledTimes(1)
    wrapper.unmount()
  })

  it('preserves loaded endpoints on partial failure and clears them for a successful empty result', async () => {
    const endpoints = [{ endpoint: '/v1/responses', requests: 1, total_tokens: 30, cost: 0.1, actual_cost: 0.08 }]
    getStats.mockResolvedValueOnce({ total_requests: 1, endpoints })
    const wrapper = mountUsageView()
    await flushPromises()
    const vm = wrapper.vm as any
    getStats.mockResolvedValueOnce({ total_requests: 1, endpoints: [], endpoints_unavailable: true })
    await vm.loadStats()
    expect(vm.inboundEndpointStats).toEqual(endpoints)
    expect(wrapper.get('[data-usage-error="stats"]').text()).toContain('usage.endpointStatsLoadFailed')
    await vm.loadStats()
    expect(vm.inboundEndpointStats).toEqual([])
    expect(wrapper.find('[data-usage-error="stats"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('exports csv with current filters and without admin-only fields', async () => {
    const wrapper = mountUsageView()
    await flushPromises()

    let exportedBlob: Blob | null = null
    let csvContent = ''
    const OriginalBlob = globalThis.Blob
    vi.stubGlobal('Blob', vi.fn((parts: BlobPart[], options?: BlobPropertyBag) => {
      csvContent = parts.map((part) => String(part)).join('')
      return new OriginalBlob(parts, options)
    }))
    const originalCreateObjectURL = window.URL.createObjectURL
    const originalRevokeObjectURL = window.URL.revokeObjectURL
    window.URL.createObjectURL = vi.fn((blob: Blob | MediaSource) => {
      exportedBlob = blob as Blob
      return 'blob:usage-export'
    }) as typeof window.URL.createObjectURL
    window.URL.revokeObjectURL = vi.fn(() => {}) as typeof window.URL.revokeObjectURL
    const clickSpy = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {})

    await (wrapper.vm as any).exportToCSV()

    expect(exportedBlob).not.toBeNull()
    expect(query).toHaveBeenCalledWith(expect.objectContaining({
      page_size: 1000,
      exact_total: true,
      sort_by: 'created_at',
      sort_order: 'desc',
    }), expect.objectContaining({ signal: expect.any(AbortSignal), timeout: 120000 }))
    expect(clickSpy).toHaveBeenCalled()
    expect(showSuccess).toHaveBeenCalled()
    expect(csvContent.startsWith('\uFEFF')).toBe(true)
    expect(csvContent.slice(1)).toBe([
      'Time,API Key Name,Model,Reasoning Effort,Inbound Endpoint,IP Address,Type,Billing Mode,Input Tokens,Output Tokens,Cache Read Tokens,Cache Creation Tokens,Cache Hit Rate,Rate Multiplier,Billed Cost,Original Cost,First Token (ms),Duration (ms),TPS (tok/s)',
      '2026-03-08T00:00:00Z,demo-key,gpt-5.4,-,,203.0.113.10,Sync,Token,4057,101,278272,4,98.6%,1,0.09288300,0.09288300,12,345,292.8',
    ].join('\n'))
    expect(csvContent).toContain('IP Address')
    expect(csvContent).toContain('203.0.113.10')
    expect(csvContent).toContain('Billed Cost')
    expect(csvContent).toContain('Original Cost')
    expect(csvContent).not.toContain('Upstream Endpoint')
    expect(csvContent).not.toContain('account_cost')
    expect(csvContent).not.toContain('account_rate_multiplier')

    window.URL.createObjectURL = originalCreateObjectURL
    window.URL.revokeObjectURL = originalRevokeObjectURL
    vi.unstubAllGlobals()
    clickSpy.mockRestore()
  })

  it('exports historical image rows with image billing mode derived from image_count', async () => {
    query.mockResolvedValue({
      items: [
        {
          ...usageLog,
          request_id: 'req-user-export-legacy-image',
          actual_cost: 0.2,
          total_cost: 0.2,
          input_cost: 0,
          output_cost: 0,
          cache_creation_cost: 0,
          cache_read_cost: 0,
          input_tokens: 0,
          output_tokens: 0,
          cache_creation_tokens: 0,
          cache_read_tokens: 0,
          image_count: 1,
          model: 'gpt-image-2',
          billing_mode: null,
          ip_address: null,
        },
      ],
      total: 1,
      pages: 1,
    })

    const wrapper = mountUsageView()
    await flushPromises()

    let csvContent = ''
    const OriginalBlob = globalThis.Blob
    vi.stubGlobal('Blob', vi.fn((parts: BlobPart[], options?: BlobPropertyBag) => {
      csvContent = parts.map((part) => String(part)).join('')
      return new OriginalBlob(parts, options)
    }))
    const originalCreateObjectURL = window.URL.createObjectURL
    const originalRevokeObjectURL = window.URL.revokeObjectURL
    window.URL.createObjectURL = vi.fn(() => 'blob:usage-export') as typeof window.URL.createObjectURL
    window.URL.revokeObjectURL = vi.fn(() => {}) as typeof window.URL.revokeObjectURL
    const clickSpy = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {})

    await (wrapper.vm as any).exportToCSV()

    expect(csvContent).toContain('Billing Mode')
    expect(csvContent).toContain('Image')
    expect(csvContent).toContain(',Image,0,0,0,0,-,')
    expect(csvContent).not.toContain(',Token,0,0,0,0,')
    const [headers, row] = csvContent.slice(1).split('\n').map(line => line.split(','))
    const speedIndex = headers!.indexOf('TPS (tok/s)')
    expect(speedIndex).toBeGreaterThan(-1)
    expect(row![speedIndex]).toBe('')

    window.URL.createObjectURL = originalCreateObjectURL
    window.URL.revokeObjectURL = originalRevokeObjectURL
    vi.unstubAllGlobals()
    clickSpy.mockRestore()
  })

  it('exports Excel across filtered pages with formatted cache hit rates and missing-cache placeholders', async () => {
    query.mockResolvedValue({ items: [usageLog], total: 1, pages: 1 })
    const wrapper = mountUsageView()
    await flushPromises()
    const vm = wrapper.vm as any
    vm.filters.api_key_id = 1
    vm.filters.model = 'gpt-5.4'
    vm.toggleColumn('cache_hit_rate')

    query.mockReset()
    query.mockResolvedValueOnce({ items: Array.from({ length: 1000 }, () => usageLog), total: 1001, pages: 2 })
    query.mockResolvedValueOnce({
      items: [{ ...usageLog, cache_read_tokens: 0, cache_creation_tokens: 25, duration_ms: null }],
      total: 1001,
      pages: 2,
    })

    const excelButton = wrapper.findAll('button').find((button) => button.text() === 'Export Excel')
    expect(excelButton).toBeDefined()
    await excelButton!.trigger('click')
    await flushPromises()

    expect(query).toHaveBeenCalledTimes(2)
    for (let page = 1; page <= 2; page++) {
      expect(query).toHaveBeenNthCalledWith(page, expect.objectContaining({
        page,
        page_size: 1000,
        api_key_id: 1,
        model: 'gpt-5.4',
        sort_by: 'created_at',
        sort_order: 'desc',
      }), expect.objectContaining({ signal: expect.any(AbortSignal), timeout: 120000 }))
    }
    const data = aoaToSheet.mock.calls[0]![0] as (string | number)[][]
    const cacheHitRateIndex = data[0]!.indexOf('Cache Hit Rate')
    expect(cacheHitRateIndex).toBeGreaterThan(-1)
    expect(data).toHaveLength(1002)
    expect(data[1]![cacheHitRateIndex]).toBe('98.6%')
    expect(data[1001]![cacheHitRateIndex]).toBe('-')
    const speedIndex = data[0]!.indexOf('TPS (tok/s)')
    expect(speedIndex).toBeGreaterThan(-1)
    expect(data[0]![speedIndex - 1]).toBe('Duration (ms)')
    expect(data[1]![speedIndex]).toBe(292.8)
    expect(data[1001]![speedIndex]).toBe('')
    expect(data[0]).not.toContain('Upstream Endpoint')
    expect(xlsxWrite).toHaveBeenCalledWith(expect.anything(), { bookType: 'xlsx', type: 'array' })
    expect(saveAs).toHaveBeenCalledWith(expect.any(Blob), expect.stringMatching(/^usage_.*\.xlsx$/))
    expect(showSuccess).toHaveBeenCalledWith('Export success')
    wrapper.unmount()
  })

  it('keeps the original filters and filename when filters change during export', async () => {
    const wrapper = mountUsageView()
    await flushPromises()
    const vm = wrapper.vm as any
    const originalStartDate = vm.startDate
    const originalEndDate = vm.endDate
    vm.filters.model = 'gpt-5.4'
    query.mockReset()
    query.mockImplementationOnce(() => {
      vm.filters.model = 'changed-model'
      vm.startDate = '2026-01-01'
      return Promise.resolve({ items: Array.from({ length: 1000 }, () => usageLog), total: 1001 })
    })
    query.mockResolvedValueOnce({ items: [usageLog], total: 1001 })

    await vm.exportToExcel()

    expect(query).toHaveBeenNthCalledWith(2, expect.objectContaining({
      page: 2, model: 'gpt-5.4', start_date: originalStartDate,
    }), expect.anything())
    expect(saveAs).toHaveBeenCalledWith(expect.any(Blob), `usage_${originalStartDate}_to_${originalEndDate}.xlsx`)
    wrapper.unmount()
  })

  it('does not download a partial export after a later page fails and shows the reason', async () => {
    const wrapper = mountUsageView()
    await flushPromises()
    query.mockReset()
    query.mockResolvedValueOnce({ items: Array.from({ length: 1000 }, () => usageLog), total: 1001 })
    query.mockRejectedValueOnce({ status: 503, message: 'Database unavailable' })
    const errorSpy = vi.spyOn(console, 'error').mockImplementation(() => {})

    await (wrapper.vm as any).exportToExcel()

    expect(saveAs).not.toHaveBeenCalled()
    expect(showSuccess).not.toHaveBeenCalled()
    expect(showError).toHaveBeenCalledWith('Export failed: Database unavailable')
    expect((wrapper.vm as any).exporting).toBe(false)
    errorSpy.mockRestore()
    wrapper.unmount()
  })
})
