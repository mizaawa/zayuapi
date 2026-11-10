import { beforeEach, describe, expect, it, vi } from 'vitest'

const { get } = vi.hoisted(() => ({ get: vi.fn() }))

vi.mock('@/api/client', () => ({ apiClient: { get } }))

import { getOperationsDashboard, getOperationsRechargeOrders, getOperationsRechargeRanking, getOperationsRechargeSummary, getOperationsUserSpend, getOperationsUserSuggestions } from '@/api/admin/dashboard'

describe('admin operations dashboard API', () => {
  beforeEach(() => {
    get.mockReset()
    get.mockResolvedValue({
      data: { metrics: {}, trend: [], groups: [], models: [] }
    })
  })

  it('requests the selected period, trend range, timezone, and optional group', async () => {
    const params = {
      start_date: '2026-10-20',
      end_date: '2026-10-26',
      trend_days: 15,
      timezone: 'Asia/Shanghai',
      group_id: 7
    }

    await getOperationsDashboard(params)

    expect(get).toHaveBeenCalledWith('/admin/dashboard/operations', { params })
  })

  it('requests ranked users, optional details, and email suggestions', async () => {
    const params = { start_date: '2026-10-20', end_date: '2026-10-26', timezone: 'Asia/Shanghai', search: 'alice', user_id: 7 }
    await getOperationsUserSpend(params)
    expect(get).toHaveBeenCalledWith('/admin/dashboard/operations/users', { params })
    await getOperationsUserSuggestions('alice')
    expect(get).toHaveBeenCalledWith('/admin/dashboard/operations/users/suggest', { params: { q: 'alice' } })
  })

  it("requests today's top-up ranking", async () => {
    get.mockResolvedValueOnce({ data: { users: [], total_amount: 0, total_users: 0, date: '2026-11-10', start_time: '', end_time: '', site_timezone: 'Asia/Shanghai' } })
    const result = await getOperationsRechargeRanking()
    expect(get).toHaveBeenCalledWith('/admin/dashboard/operations/recharge-ranking')
    expect(result.date).toBe('2026-11-10')
  })

  it('turns whole site-timezone days into instants for recharge orders and totals', async () => {
    get.mockResolvedValueOnce({ data: { items: [{ id: 'redeem:1', channel: 'redeem' }], total: 1, page: 1, pages: 1 } })
    const page = await getOperationsRechargeOrders({ page: 2, page_size: 25, start_date: '2026-11-04', end_date: '2026-11-10', timezone: 'Asia/Shanghai', status: 'paid', channel: 'code', search: 'alice' })
    expect(get).toHaveBeenLastCalledWith('/admin/payment/recharge-activity', { params: {
      page: 2, page_size: 25, email: 'alice', status: 'paid', channel: 'redeem',
      start_time: '2026-11-03T16:00:00.000Z', end_time: '2026-11-10T16:00:00.000Z'
    } })
    expect(page.items[0].channel).toBe('code')

    get.mockResolvedValueOnce({ data: { amount: 980, count: 41 } })
    const summary = await getOperationsRechargeSummary({ start_date: '2026-11-10', end_date: '2026-11-10', timezone: 'Asia/Shanghai', channel: 'direct' })
    expect(get).toHaveBeenLastCalledWith('/admin/payment/recharge-activity/summary', { params: {
      email: undefined, status: undefined, channel: 'direct',
      start_time: '2026-11-09T16:00:00.000Z', end_time: '2026-11-10T16:00:00.000Z'
    } })
    expect(summary).toEqual({ amount: 980, count: 41 })

    await expect(getOperationsRechargeSummary({ start_date: 'bad', end_date: '2026-11-10', timezone: 'Asia/Shanghai' })).rejects.toThrow('Invalid recharge date range')
  })
})
