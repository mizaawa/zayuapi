import { afterEach, describe, expect, it, vi } from 'vitest'
import { list, listSnapshot } from '@/api/announcements'

const { get } = vi.hoisted(() => ({ get: vi.fn() }))
vi.mock('@/api/client', () => ({ apiClient: { get } }))

afterEach(() => vi.useRealTimers())

describe('announcement snapshot', () => {
  it('uses the HTTP server time without changing the list API response', async () => {
    const items = [{ id: 1 }]
    get.mockResolvedValue({ data: items, headers: { date: 'Mon, 28 Sep 2026 09:00:00 GMT' } })
    expect(await listSnapshot()).toEqual({ items, fetchedAt: Date.parse('2026-09-28T09:00:00Z') })
    expect(await list(true)).toEqual(items)
    expect(get).toHaveBeenLastCalledWith('/announcements', { params: { unread_only: 1 } })
  })

  it('falls back to browser time when the server date is unavailable', async () => {
    vi.useFakeTimers()
    vi.setSystemTime('2026-09-28T10:00:00Z')
    get.mockResolvedValue({ data: [], headers: {} })
    expect(await listSnapshot()).toEqual({ items: [], fetchedAt: Date.now() })
  })
})
