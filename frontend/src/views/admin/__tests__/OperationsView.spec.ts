import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import OperationsView from '../OperationsView.vue'
import type { OperationsDashboardResponse, OperationsUserSpendResponse } from '@/api/admin/dashboard'

const { getOperationsDashboard, getOperationsUserSpend, getOperationsUserSuggestions, getOperationsRechargeOrders, getOperationsRechargeRisks } = vi.hoisted(() => ({
  getOperationsDashboard: vi.fn(), getOperationsUserSpend: vi.fn(), getOperationsUserSuggestions: vi.fn(), getOperationsRechargeOrders: vi.fn(), getOperationsRechargeRisks: vi.fn()
}))
vi.mock('@/api/admin', () => ({ adminAPI: { dashboard: { getOperationsDashboard, getOperationsUserSpend, getOperationsUserSuggestions, getOperationsRechargeOrders, getOperationsRechargeRisks } } }))
vi.mock('vue-i18n', async () => ({
  ...(await vi.importActual<typeof import('vue-i18n')>('vue-i18n')),
  useI18n: () => ({ t: (key: string) => key })
}))

const fixture = (): OperationsDashboardResponse => ({
  metrics: { today_user_consumption: 20, today_user_recharge: 30, today_monitor_consumption: 1, today_revenue: { CNY: 42, USD: 7 }, yesterday_user_consumption: 10, yesterday_user_recharge: 40, yesterday_monitor_consumption: 2, yesterday_revenue: { CNY: 40, USD: 9 }, yesterday_user_balance_change: null, yesterday_admin_consumption: 4, total_user_consumption: 100, total_user_balance: 1000, total_monitor_consumption: 6, admin_balance: 50 },
  trend: [],
  groups: [
    { group_id: 1, group_name: 'Small profit', platform: 'openai', rate_multiplier: 1.2, user_cost: 11, upstream_cost: 10 },
    { group_id: 2, group_name: 'Large loss', platform: 'anthropic', rate_multiplier: 0.5, user_cost: 3, upstream_cost: 20 },
    { group_id: 3, group_name: 'Large profit', platform: 'gemini', rate_multiplier: 2, user_cost: 30, upstream_cost: 10 },
    { group_id: 0, group_name: 'Unassigned', platform: 'custom', rate_multiplier: 1, user_cost: 8, upstream_cost: 10 }
  ],
  models: [
    { model: 'profit-small', user_cost: 11, upstream_cost: 10 },
    { model: 'loss-large', platforms: ['anthropic'], user_cost: 3, upstream_cost: 20 },
    { model: 'profit-large', user_cost: 30, upstream_cost: 10 }
  ]
})
const userSpendFixture = (search = '', userId?: number): OperationsUserSpendResponse => ({
  users: [
    { user_id: 1, email: 'alice@example.com', user_cost: 20, upstream_cost: 10 },
    { user_id: 2, email: 'bob@example.com', user_cost: 12, upstream_cost: 8 }
  ].filter(user => user.email.includes(search)),
  detail: userId ? {
    user_id: userId,
    email: userId === 1 ? 'alice@example.com' : 'bob@example.com',
    groups: [{ group_id: 2, group_name: 'Team', platform: 'anthropic', rate_multiplier: 1.2, user_cost: 12, upstream_cost: 8 }],
    models: [{ model: 'claude-sonnet', platforms: ['anthropic'], user_cost: 12, upstream_cost: 8 }]
  } : null
})
const mountPage = () => mount(OperationsView, { global: { stubs: { AppLayout: { template: '<div><slot /></div>' }, Icon: true, LoadingSpinner: true, OperationsTrendChart: { props: ['points', 'days'], template: '<div data-testid="trend-stub" :data-days="days" />' } } } })

describe('OperationsView', () => {
  beforeEach(() => {
    getOperationsDashboard.mockReset().mockResolvedValue(fixture())
    getOperationsUserSpend.mockReset().mockImplementation(({ search, user_id }) => Promise.resolve(userSpendFixture(search, user_id)))
    getOperationsUserSuggestions.mockReset().mockResolvedValue([{ user_id: 1, email: 'alice@example.com' }])
    getOperationsRechargeOrders.mockReset().mockResolvedValue({ items: [], total: 0, page: 1, pages: 1 })
    getOperationsRechargeRisks.mockReset().mockResolvedValue([])
    Object.defineProperty(window, 'innerWidth', { configurable: true, writable: true, value: 1280 })
  })

  it('loads only the first recharge page until next is selected and resets on filters', async () => {
    getOperationsRechargeOrders.mockImplementation(async ({ page }) => ({
      items: [{ id: `payment:${page}`, user_email: 'alice@example.com', amount: 15, currency: 'USD', status: 'paid', channel: 'direct', created_at: '2026-10-27T12:00:00Z' }],
      total: 26, page, pages: 2
    }))
    const wrapper = mountPage()
    await flushPromises()
    expect(getOperationsRechargeOrders).toHaveBeenCalledTimes(1)
    expect(getOperationsRechargeOrders.mock.calls[0][0]).toMatchObject({ page: 1, page_size: 25 })
    expect(wrapper.get('[data-recharge-order="payment:1"]').text()).toContain('alice@example.com')
    await wrapper.get('[data-testid="recharge-orders-panel"]').findAll('button').find(button => button.text() === 'admin.operations.recharge.next')!.trigger('click')
    await flushPromises()
    expect(getOperationsRechargeOrders).toHaveBeenCalledTimes(2)
    expect(getOperationsRechargeOrders.mock.calls[1][0].page).toBe(2)
    await wrapper.get('[data-order-status="unpaid"]').trigger('click')
    await flushPromises()
    expect(getOperationsRechargeOrders.mock.lastCall?.[0]).toMatchObject({ page: 1, status: 'unpaid' })
    wrapper.unmount()
  })

  it('opens read-only risk details and closes the dialog', async () => {
    getOperationsRechargeRisks.mockResolvedValue([{ id: 'rapid_orders:1', type: 'rapid_orders', user_email: 'alice@example.com', count: 10, order_count: 10, user_balance: 18, total_consumption: 34, first_at: '2026-10-27T10:00:00Z', last_at: '2026-10-27T10:20:00Z', result: 'failed' }])
    const wrapper = mountPage()
    await flushPromises()
    await wrapper.get('[data-risk-id="rapid_orders:1"]').trigger('click')
    await flushPromises()
    expect(document.body.textContent).toContain('alice@example.com')
    expect(document.body.textContent).toContain('$18.00')
    expect(document.body.textContent).toContain('$34.00')
    const close = Array.from(document.body.querySelectorAll('button')).find(button => button.textContent?.includes('common.close'))
    expect(close).toBeTruthy()
    close!.click()
    await flushPromises()
    expect(wrapper.getComponent({ name: 'OperationsRechargeRiskPanel' }).getComponent({ name: 'BaseDialog' }).props('show')).toBe(false)
    wrapper.unmount()
  })

  it('shows eight financial cards, preserves currencies and defaults to today/all groups', async () => {
    const wrapper = mountPage()
    await flushPromises()
    expect(wrapper.findAll('[data-metric]')).toHaveLength(8)
    expect(wrapper.get('[data-metric="today_revenue"]').text()).toContain('42.00')
    expect(wrapper.get('[data-metric="today_revenue"]').text()).toContain('$7.00')
    expect(wrapper.get('[data-metric="today_user_consumption"]').text()).toContain('+$10.00')
    expect(wrapper.get('[data-metric="today_user_consumption"] .text-rose-500').exists()).toBe(true)
    expect(wrapper.get('[data-metric="today_user_recharge"]').text()).toContain('-$10.00')
    expect(wrapper.get('[data-metric="today_user_recharge"] .text-emerald-600').exists()).toBe(true)
    expect(wrapper.get('[data-metric="today_revenue"]').text()).toContain('-$2.00')
    expect(wrapper.get('[data-metric="total_user_balance"]').text()).toContain('—')
    expect(wrapper.get('[data-metric="admin_balance"]').text()).toContain('$4.00')
    expect(getOperationsDashboard.mock.calls[0][0].start_date).toBe(getOperationsDashboard.mock.calls[0][0].end_date)
    expect(getOperationsDashboard.mock.calls[0][0].group_id).toBe(0)
    expect(wrapper.get('[data-group="all"]').attributes('aria-pressed')).toBe('true')
    expect(wrapper.findAll('[data-user]')).toHaveLength(2)
    expect(wrapper.get('[data-user="1"]').attributes('aria-pressed')).toBe('true')
    expect(wrapper.get('[data-testid="user-detail"]').text()).toContain('alice@example.com')
    expect(getOperationsUserSpend.mock.lastCall?.[0].user_id).toBe(1)
    expect(wrapper.get('[data-testid="trend-stub"]').attributes('data-days')).toBe('15')
    wrapper.unmount()
  })

  it('shows the selected user breakdown with group and model tabs', async () => {
    const wrapper = mountPage()
    await flushPromises()
    await wrapper.get('[data-user="2"]').trigger('click')
    await flushPromises()
    expect(getOperationsUserSpend.mock.lastCall?.[0].user_id).toBe(2)
    expect(wrapper.get('[data-testid="user-detail"]').text()).toContain('bob@example.com')
    expect(wrapper.get('[data-user-group="2"]').text()).toContain('$4.00')
    await wrapper.get('[data-detail-tab="models"]').trigger('click')
    expect(wrapper.get('[data-user-model="claude-sonnet"]').text()).toContain('$12.00')
    expect(wrapper.get('[data-user-model="claude-sonnet"] .text-orange-600').exists()).toBe(true)
    expect(wrapper.get('[data-user="2"]').attributes('aria-pressed')).toBe('true')
    await wrapper.get('[data-period="7"]').trigger('click')
    await flushPromises()
    expect(getOperationsUserSpend.mock.lastCall?.[0].user_id).toBe(2)
    expect(wrapper.get('[data-user="2"]').attributes('aria-pressed')).toBe('true')
    wrapper.unmount()
  })

  it('follows the new top spender on refresh until a user is chosen manually', async () => {
    let topUserId = 1
    getOperationsUserSpend.mockImplementation(({ search, user_id }) => {
      const result = userSpendFixture(search, user_id)
      if (topUserId === 2) result.users.reverse()
      return Promise.resolve(result)
    })
    const wrapper = mountPage()
    await flushPromises()
    expect(wrapper.get('[data-user="1"]').attributes('aria-pressed')).toBe('true')
    topUserId = 2
    await wrapper.get('.btn-secondary').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-user="2"]').attributes('aria-pressed')).toBe('true')
    expect(getOperationsUserSpend.mock.lastCall?.[0].user_id).toBe(2)
    wrapper.unmount()
  })

  it('suggests user emails while typing and filters the ranking', async () => {
    const wrapper = mountPage()
    await flushPromises()
    await wrapper.get('[data-testid="user-search"]').setValue('alice')
    await new Promise(resolve => setTimeout(resolve, 300))
    await flushPromises()
    expect(getOperationsUserSuggestions).toHaveBeenCalledWith('alice')
    expect(getOperationsUserSpend.mock.lastCall?.[0].search).toBe('alice')
    expect(wrapper.findAll('[data-user]')).toHaveLength(1)
    expect(wrapper.get('[data-user="1"]').attributes('aria-pressed')).toBe('true')
    expect(wrapper.get('[data-testid="user-suggestions"]').text()).toContain('alice@example.com')
    await wrapper.get('[data-testid="user-suggestions"] [role="option"]').trigger('click')
    await flushPromises()
    expect(getOperationsUserSpend.mock.lastCall?.[0].user_id).toBe(1)
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
    expect(wrapper.get('[data-model="loss-large"] .text-orange-600').exists()).toBe(true)
    expect(wrapper.get('[data-group="all"]').text()).toContain('$52.00')
    expect(wrapper.get('[data-group="all"]').text()).toContain('$50.00')
    wrapper.unmount()
  })

  it('keeps site groups and models on the first row, then users and detail on the second', async () => {
    const wrapper = mountPage()
    await flushPromises()
    const site = wrapper.get('[data-testid="site-spend-grid"]')
    const users = wrapper.get('[data-testid="user-spend-grid"]')
    expect(site.element.compareDocumentPosition(users.element) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    expect(site.element.children[0]?.textContent).toContain('admin.operations.siteGroupSpend')
    expect(site.element.children[1]?.textContent).toContain('admin.operations.siteModelSpend')
    expect(users.element.children[0]?.textContent).toContain('admin.operations.userRanking')
    expect(users.element.children[1]?.textContent).toContain('admin.operations.userDetails')
    for (const region of ['groups-scroll-region', 'models-scroll-region', 'users-scroll-region', 'user-detail-groups']) {
      expect(wrapper.get(`[data-testid="${region}"]`).classes()).toContain('overflow-y-auto')
      expect(wrapper.get(`[data-testid="${region}"]`).classes().some(name => name.startsWith('max-h-'))).toBe(true)
    }
    wrapper.unmount()
  })

  it('shows progress next to the selected group, user and period during delayed requests', async () => {
    const wrapper = mountPage()
    await flushPromises()
    let resolveDashboard!: (value: OperationsDashboardResponse) => void
    getOperationsDashboard.mockImplementationOnce(() => new Promise(resolve => { resolveDashboard = resolve }))
    await wrapper.get('[data-group="2"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-group="2"] [data-testid="group-selection-loading"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid="model-filter-loading"]').exists()).toBe(true)
    resolveDashboard(fixture())
    await flushPromises()

    let resolveUsers!: (value: OperationsUserSpendResponse) => void
    getOperationsUserSpend.mockImplementationOnce(() => new Promise(resolve => { resolveUsers = resolve }))
    await wrapper.get('[data-user="2"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-user="2"] [data-testid="selected-user-loading"]').exists()).toBe(true)
    expect(wrapper.get('[data-detail-tab="groups"] [data-testid="detail-tab-loading"]').exists()).toBe(true)
    resolveUsers(userSpendFixture('', 2))
    await flushPromises()
    getOperationsDashboard.mockImplementationOnce(() => new Promise(resolve => { resolveDashboard = resolve }))
    await wrapper.get('[data-period="7"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-period="7"] [data-testid="period-loading"]').exists()).toBe(true)
    resolveDashboard(fixture())
    await flushPromises()
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
    expect(wrapper.get('[data-testid="groups-loading"]').attributes('role')).toBe('status')
    expect(wrapper.get('[data-testid="models-loading"]').attributes('role')).toBe('status')
    getOperationsDashboard.mockResolvedValueOnce({ ...fixture(), models: [{ model: 'latest', user_cost: 1, upstream_cost: 0 }] })
    await wrapper.get('[data-group="2"]').trigger('click')
    await flushPromises()
    resolveFirst(fixture())
    await flushPromises()
    expect(wrapper.findAll('[data-model]').map(row => row.attributes('data-model'))).toEqual(['latest'])
    expect(wrapper.find('[data-testid="groups-loading"]').exists()).toBe(false)
    wrapper.unmount()
  })
})
