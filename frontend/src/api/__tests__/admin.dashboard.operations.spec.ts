import { beforeEach, describe, expect, it, vi } from 'vitest'

const { get } = vi.hoisted(() => ({ get: vi.fn() }))

vi.mock('@/api/client', () => ({ apiClient: { get } }))

import { getOperationsDashboard } from '@/api/admin/dashboard'

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
})
