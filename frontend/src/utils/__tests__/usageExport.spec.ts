import { afterEach, describe, expect, it, vi } from 'vitest'

import { fetchUsageExportPage } from '@/utils/usageExport'

describe('fetchUsageExportPage', () => {
  afterEach(() => {
    vi.useRealTimers()
  })

  it('waits the supplied retry interval and retries the same page request', async () => {
    vi.useFakeTimers()
    const rateLimited = Object.assign(new Error('Too many requests'), {
      status: 429,
      retryAfter: 2,
    })
    const request = vi.fn()
      .mockRejectedValueOnce(rateLimited)
      .mockResolvedValueOnce({ rows: ['same page'] })

    const result = fetchUsageExportPage(request)
    await vi.advanceTimersByTimeAsync(1999)
    expect(request).toHaveBeenCalledTimes(1)

    await vi.advanceTimersByTimeAsync(1)
    await expect(result).resolves.toEqual({ rows: ['same page'] })
    expect(request).toHaveBeenCalledTimes(2)
  })

  it.each([undefined, 'invalid'])('uses the default wait for invalid retryAfter %s', async (retryAfter) => {
    vi.useFakeTimers()
    const request = vi.fn()
      .mockRejectedValueOnce({ status: 429, retryAfter })
      .mockResolvedValueOnce('page')

    const result = fetchUsageExportPage(request)
    await vi.advanceTimersByTimeAsync(59999)
    expect(request).toHaveBeenCalledTimes(1)

    await vi.advanceTimersByTimeAsync(1)
    await expect(result).resolves.toBe('page')
    expect(request).toHaveBeenCalledTimes(2)
  })

  it('cancels the retry wait immediately and clears its timer', async () => {
    vi.useFakeTimers()
    const controller = new AbortController()
    const request = vi.fn().mockRejectedValue({ status: 429, retryAfter: 60 })

    const result = fetchUsageExportPage(request, controller.signal)
    await vi.waitFor(() => expect(request).toHaveBeenCalledTimes(1))
    controller.abort()

    await expect(result).rejects.toMatchObject({ name: 'AbortError' })
    expect(vi.getTimerCount()).toBe(0)
    expect(request).toHaveBeenCalledTimes(1)
  })

  it('does not retry non-rate-limit errors', async () => {
    const error = { status: 503, message: 'Unavailable' }
    const request = vi.fn().mockRejectedValue(error)

    await expect(fetchUsageExportPage(request)).rejects.toBe(error)
    expect(request).toHaveBeenCalledTimes(1)
  })

  it('makes at most three retries after the initial rate-limited request', async () => {
    vi.useFakeTimers()
    const error = { status: 429, retryAfter: 0 }
    const request = vi.fn().mockRejectedValue(error)

    const result = fetchUsageExportPage(request)
    const rejected = expect(result).rejects.toBe(error)
    await vi.runAllTimersAsync()
    await rejected
    expect(request).toHaveBeenCalledTimes(4)
  })
})
