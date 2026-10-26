import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import OperationsView from '../OperationsView.vue'
import type { OperationsDashboardResponse } from '@/api/admin/dashboard'

const { getOperationsDashboard } = vi.hoisted(() => ({ getOperationsDashboard: vi.fn() }))
vi.mock('@/api/admin', () => ({ adminAPI: { dashboard: { getOperationsDashboard } } }))
vi.mock('vue-i18n', async () => ({
  ...(await vi.importActual<typeof import('vue-i18n')>('vue-i18n')),
  useI18n: () => ({ t: (key: string) => key })
}))

const fixture = (): OperationsDashboardResponse => ({
  metrics: { today_user_consumption: 20, today_user_recharge: 30, today_monitor_consumption: 1, today_revenue: { CNY: 42, USD: 7 }, total_user_consumption: 100, total_user_balance: 1000, total_monitor_consumption: 6, admin_balance: 50 },
  trend: [],
  groups: [
    { group_id: 1, group_name: 'Small profit', platform: 'openai', rate_multiplier: 1.2, user_cost: 11, upstream_cost: 10 },
    { group_id: 2, group_name: 'Large loss', platform: 'anthropic', rate_multiplier: 0.5, user_cost: 3, upstream_cost: 20 },
    { group_id: 3, group_name: 'Large profit', platform: 'gemini', rate_multiplier: 2, user_cost: 30, upstream_cost: 10 },
    { group_id: 0, group_name: 'Unassigned', platform: 'custom', rate_multiplier: 1, user_cost: 8, upstream_cost: 10 }
  ],
  models: [
    { model: 'profit-small', user_cost: 11, upstream_cost: 10 },
    { model: 'loss-large', user_cost: 3, upstream_cost: 20 },
    { model: 'profit-large', user_cost: 30, upstream_cost: 10 }
  ]
})
const mountPage = () => mount(OperationsView, { global: { stubs: { AppLayout: { template: '<div><slot /></div>' }, Icon: true, LoadingSpinner: true, OperationsTrendChart: { props: ['points', 'days'], template: '<div data-testid="trend-stub" :data-days="days" />' } } } })

describe('OperationsView', () => {
  beforeEach(() => {
    getOperationsDashboard.mockReset().mockResolvedValue(fixture())
    Object.defineProperty(window, 'innerWidth', { configurable: true, writable: true, value: 1280 })
  })

  it('shows eight financial cards, preserves currencies and defaults to today/all groups', async () => {
    const wrapper = mountPage()
    await flushPromises()
    expect(wrapper.findAll('[data-metric]')).toHaveLength(8)
    expect(wrapper.get('[data-metric="today_revenue"]').text()).toContain('42.00')
    expect(wrapper.get('[data-metric="today_revenue"]').text()).toContain('$7.00')
    expect(getOperationsDashboard.mock.calls[0][0].start_date).toBe(getOperationsDashboard.mock.calls[0][0].end_date)
    expect(getOperationsDashboard.mock.calls[0][0].group_id).toBe(0)
    expect(wrapper.get('[data-group="all"]').attributes('aria-pressed')).toBe('true')
    expect(wrapper.get('[data-testid="trend-stub"]').attributes('data-days')).toBe('15')
    wrapper.unmount()
  })

  it('pins the all-groups summary then orders losses ascending and profits descending', async () => {
    const wrapper = mountPage()
    await flushPromises()
    expect(wrapper.findAll('[data-group]').map(row => row.attributes('data-group'))).toEqual(['all', '2', '0', '3', '1'])
    expect(wrapper.get('[data-group="2"] [data-platform]').attributes('data-platform')).toBe('anthropic')
    expect(wrapper.get('[data-group="3"] [data-platform]').attributes('data-platform')).toBe('gemini')
    expect(wrapper.get('[data-testid="groups-scroll-region"]').classes()).toContain('overflow-y-auto')
    expect(wrapper.get('[data-testid="models-scroll-region"]').classes()).toContain('overflow-y-auto')
    expect(wrapper.findAll('[data-model]').map(row => row.attributes('data-model'))).toEqual(['loss-large', 'profit-large', 'profit-small'])
    expect(wrapper.get('[data-group="all"]').text()).toContain('$52.00')
    expect(wrapper.get('[data-group="all"]').text()).toContain('$50.00')
    wrapper.unmount()
  })

  it('filters models by group, uses -1 for unassigned, and updates the period', async () => {
    const wrapper = mountPage()
    await flushPromises()
    getOperationsDashboard.mockResolvedValueOnce({ ...fixture(), models: [{ model: 'selected-model', user_cost: 3, upstream_cost: 20 }] })
    await wrapper.get('[data-group="2"]').trigger('click')
    await flushPromises()
    expect(getOperationsDashboard.mock.lastCall?.[0].group_id).toBe(2)
    expect(wrapper.findAll('[data-model]').map(row => row.attributes('data-model'))).toEqual(['selected-model'])
    await wrapper.get('[data-group="0"]').trigger('click')
    await flushPromises()
    expect(getOperationsDashboard.mock.lastCall?.[0].group_id).toBe(-1)
    await wrapper.get('[data-period="7"]').trigger('click')
    await flushPromises()
    const params = getOperationsDashboard.mock.lastCall?.[0]
    expect((Date.parse(params.end_date) - Date.parse(params.start_date)) / 86400000).toBe(6)
    wrapper.unmount()
  })

  it('uses a seven-day trend on narrow screens and ignores a late response', async () => {
    window.innerWidth = 600
    const wrapper = mountPage()
    await flushPromises()
    expect(wrapper.get('[data-testid="trend-stub"]').attributes('data-days')).toBe('7')
    let resolveFirst!: (value: OperationsDashboardResponse) => void
    getOperationsDashboard.mockImplementationOnce(() => new Promise(resolve => { resolveFirst = resolve }))
    await wrapper.get('[data-group="1"]').trigger('click')
    getOperationsDashboard.mockResolvedValueOnce({ ...fixture(), models: [{ model: 'latest', user_cost: 1, upstream_cost: 0 }] })
    await wrapper.get('[data-group="2"]').trigger('click')
    await flushPromises()
    resolveFirst(fixture())
    await flushPromises()
    expect(wrapper.findAll('[data-model]').map(row => row.attributes('data-model'))).toEqual(['latest'])
    wrapper.unmount()
  })
})
