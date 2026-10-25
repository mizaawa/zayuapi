import { beforeEach, describe, expect, it, vi } from 'vitest'

const { post } = vi.hoisted(() => ({ post: vi.fn() }))

vi.mock('@/api/client', () => ({ apiClient: { post } }))

import { getDashboardApiKeysUsage, type BatchApiKeysUsageResponse } from '@/api/usage'

function usageResponse(ids: number[]): { data: BatchApiKeysUsageResponse } {
  return {
    data: {
      stats: Object.fromEntries(ids.map((id) => [String(id), {
        api_key_id: id,
        today_actual_cost: id / 100,
        total_actual_cost: id,
      }])),
    },
  }
}

describe('user API key usage batches', () => {
  beforeEach(() => {
    post.mockReset()
    post.mockImplementation(async (_url, body) => {
      if (body.api_key_ids.length > 100) throw { status: 400, message: 'Too many API key IDs' }
      return usageResponse(body.api_key_ids)
    })
  })

  it.each([1, 100, 101, 250, 1000])('loads all %s keys without exceeding the backend batch limit', async (count) => {
    const ids = Array.from({ length: count }, (_, index) => index + 1)
    const controller = new AbortController()
    const result = await getDashboardApiKeysUsage(ids, { signal: controller.signal })

    expect(post).toHaveBeenCalledTimes(Math.ceil(count / 100))
    expect(Object.keys(result.stats)).toHaveLength(count)
    expect(result.stats[String(count)]).toEqual(usageResponse([count]).data.stats[String(count)])
    for (const [index, [url, body, options]] of post.mock.calls.entries()) {
      expect(url).toBe('/usage/dashboard/api-keys-usage')
      expect(body.api_key_ids).toEqual(ids.slice(index * 100, (index + 1) * 100))
      expect(body.timezone).toBe(Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC')
      expect(options.signal).toBe(controller.signal)
    }
  })

  it('returns empty stats without a request for an empty key list', async () => {
    expect(await getDashboardApiKeysUsage([])).toEqual({ stats: {} })
    expect(post).not.toHaveBeenCalled()
  })

  it('does not start a request if already canceled', async () => {
    const controller = new AbortController()
    controller.abort()

    await expect(getDashboardApiKeysUsage([1], { signal: controller.signal })).rejects.toMatchObject({ name: 'AbortError' })
    expect(post).not.toHaveBeenCalled()
  })

  it('reads batches sequentially and discards a late result after cancellation', async () => {
    let resolveFirst!: (value: ReturnType<typeof usageResponse>) => void
    post.mockImplementationOnce(() => new Promise((resolve) => { resolveFirst = resolve }))
    const controller = new AbortController()
    const ids = Array.from({ length: 250 }, (_, index) => index + 1)
    const pending = getDashboardApiKeysUsage(ids, { signal: controller.signal })
    const rejected = expect(pending).rejects.toMatchObject({ name: 'AbortError' })

    expect(post).toHaveBeenCalledTimes(1)
    controller.abort()
    resolveFirst(usageResponse(ids.slice(0, 100)))
    await rejected

    expect(post).toHaveBeenCalledTimes(1)
  })

  it('surfaces a failed batch instead of returning partial successful stats', async () => {
    const failure = { status: 503 }
    post.mockResolvedValueOnce(usageResponse([1])).mockRejectedValueOnce(failure)
    const ids = Array.from({ length: 250 }, (_, index) => index + 1)

    await expect(getDashboardApiKeysUsage(ids)).rejects.toBe(failure)
    expect(post).toHaveBeenCalledTimes(2)
  })
})
