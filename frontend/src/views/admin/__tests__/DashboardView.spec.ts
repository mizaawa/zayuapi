import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'

import type { DashboardStats } from '@/types'
import DashboardView from '../DashboardView.vue'

const { getSnapshotV2, getUserUsageTrend, getUserSpendingRanking } = vi.hoisted(() => ({
  getSnapshotV2: vi.fn(),
  getUserUsageTrend: vi.fn(),
  getUserSpendingRanking: vi.fn()
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    dashboard: {
      getSnapshotV2,
      getUserUsageTrend,
      getUserSpendingRanking
    }
  }
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError: vi.fn()
  })
}))

vi.mock('vue-router', () => ({
  useRouter: () => ({
    push: vi.fn()
  })
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => key
    })
  }
})

const formatLocalDate = (date: Date): string => {
  const year = date.getFullYear()
  const month = String(date.getMonth() + 1).padStart(2, '0')
  const day = String(date.getDate()).padStart(2, '0')
  return `${year}-${month}-${day}`
}

const createDashboardStats = (): DashboardStats => ({
  total_users: 0,
  today_new_users: 0,
  active_users: 0,
  hourly_active_users: 0,
  stats_updated_at: '',
  stats_stale: false,
  total_consumption: 0,
  total_balance: 0,
  total_recharged: 0,
  total_api_keys: 0,
  active_api_keys: 0,
  total_accounts: 0,
  normal_accounts: 0,
  error_accounts: 0,
  ratelimit_accounts: 0,
  overload_accounts: 0,
  total_requests: 0,
  total_input_tokens: 0,
  total_output_tokens: 0,
  total_cache_creation_tokens: 0,
  total_cache_read_tokens: 0,
  total_tokens: 0,
  total_cost: 0,
  total_actual_cost: 0,
  total_account_cost: 0,
  today_requests: 0,
  today_input_tokens: 0,
  today_output_tokens: 0,
  today_cache_creation_tokens: 0,
  today_cache_read_tokens: 0,
  today_tokens: 0,
  today_cost: 0,
  today_actual_cost: 0,
  today_account_cost: 0,
  average_duration_ms: 0,
  uptime: 0,
  rpm: 0,
  tpm: 0
})

const mountDashboard = () => mount(DashboardView, {
  global: {
    stubs: {
      AppLayout: { template: '<div><slot /></div>' },
      LoadingSpinner: true,
      Icon: true,
      DateRangePicker: true,
      Select: true,
      ModelDistributionChart: true,
      TokenUsageTrend: true,
      Line: true
    }
  }
})

describe('admin DashboardView', () => {
  beforeEach(() => {
    setActivePinia(createPinia())

    getSnapshotV2.mockReset()
    getUserUsageTrend.mockReset()
    getUserSpendingRanking.mockReset()

    getSnapshotV2.mockResolvedValue({
      stats: createDashboardStats(),
      trend: [],
      models: []
    })
    getUserUsageTrend.mockResolvedValue({
      trend: [],
      start_date: '',
      end_date: '',
      granularity: 'hour'
    })
    getUserSpendingRanking.mockResolvedValue({
      ranking: [],
      total_actual_cost: 0,
      total_requests: 0,
      total_tokens: 0,
      start_date: '',
      end_date: ''
    })
  })

  it('uses last 24 hours as default dashboard range', async () => {
    const wrapper = mountDashboard()

    await flushPromises()

    const now = new Date()
    const yesterday = new Date(now.getTime() - 24 * 60 * 60 * 1000)

    expect(getSnapshotV2).toHaveBeenCalledTimes(1)
    expect(getSnapshotV2).toHaveBeenCalledWith(expect.objectContaining({
      start_date: formatLocalDate(yesterday),
      end_date: formatLocalDate(now),
      granularity: 'hour'
    }))
    wrapper.unmount()
  })

  it('shows actual daily spending and all-time financial totals from the snapshot', async () => {
    getSnapshotV2.mockResolvedValueOnce({
      stats: {
        ...createDashboardStats(),
        today_cost: 9999,
        today_actual_cost: 12.34,
        total_actual_cost: 500,
        total_consumption: 225012.34,
        total_balance: -42.5,
        total_recharged: 1000000
      },
      trend: [],
      models: []
    })
    const wrapper = mountDashboard()
    await flushPromises()

    const finances = wrapper.get('[data-testid="dashboard-financial-stats"]')
    const expected = [
      ['today_actual_cost', 'todayCost', '$12.34', '$12.34'],
      ['total_consumption', 'totalCost', '$225.01K', '$225,012.34'],
      ['total_balance', 'totalBalance', '-$42.50', '-$42.50'],
      ['total_recharged', 'totalRecharged', '$1.00M', '$1,000,000.00']
    ]
    for (const [key, label, amount, fullAmount] of expected) {
      const card = finances.get(`[data-metric="${key}"]`)
      expect(card.text()).toContain(`admin.dashboard.${label}`)
      expect(card.get('p[title]').text()).toBe(amount)
      expect(card.get('p[title]').attributes('title')).toBe(fullAmount)
      expect(card.get('svg path').attributes('d')).toBeTruthy()
    }
    wrapper.unmount()
  })

  it('distinguishes zero amounts from fields missing in an older server response', async () => {
    const stats: Partial<DashboardStats> = createDashboardStats()
    delete stats.total_recharged
    getSnapshotV2.mockResolvedValueOnce({ stats, trend: [], models: [] })
    const wrapper = mountDashboard()
    await flushPromises()

    expect(wrapper.get('[data-metric="total_balance"] p[title]').text()).toBe('$0.00')
    expect(wrapper.get('[data-metric="total_recharged"] p[title]').text()).toBe('--')
    wrapper.unmount()
  })
})
