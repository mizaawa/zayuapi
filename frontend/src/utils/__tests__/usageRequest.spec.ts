import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { fetchUsageWithRetry } from '@/utils/usageRequest'

describe('fetchUsageWithRetry', () => {
  beforeEach(() => {
    vi.useFakeTimers()
  })

  afterEach(() => {
    vi.restoreAllMocks()
    vi.useRealTimers()
  })

  it('returns a successful read without scheduling retries', async () => {
    const request = vi.fn().mockResolvedValue({ total: 12 })
    await expect(fetchUsageWithRetry(request, new AbortController().signal)).resolves.toEqual({ total: 12 })
    expect(request).toHaveBeenCalledTimes(1)
    expect(vi.getTimerCount()).toBe(0)
  })

  it('recovers an incomplete HTTP-success result with the same bounded backoff', async () => {
    const request = vi.fn()
      .mockResolvedValueOnce({ incomplete: true })
      .mockResolvedValueOnce({ incomplete: false })
    const result = fetchUsageWithRetry(request, new AbortController().signal, (value) => value.incomplete)
    await vi.advanceTimersByTimeAsync(499)
    expect(request).toHaveBeenCalledTimes(1)
    await vi.advanceTimersByTimeAsync(1)
    await expect(result).resolves.toEqual({ incomplete: false })
    expect(request).toHaveBeenCalledTimes(2)
  })

  it('returns the last incomplete result after exhausting retries', async () => {
    const request = vi.fn().mockResolvedValue({ incomplete: true, total: 12 })
    const result = fetchUsageWithRetry(request, new AbortController().signal, (value) => value.incomplete)
    await vi.advanceTimersByTimeAsync(1500)
    await expect(result).resolves.toEqual({ incomplete: true, total: 12 })
    expect(request).toHaveBeenCalledTimes(3)
    expect(vi.getTimerCount()).toBe(0)
  })

  it('cancels an incomplete-result retry without issuing another read', async () => {
    const controller = new AbortController()
    const request = vi.fn().mockResolvedValue({ incomplete: true })
    const result = fetchUsageWithRetry(request, controller.signal, (value) => value.incomplete)
    const rejected = expect(result).rejects.toMatchObject({ name: 'AbortError' })
    await vi.advanceTimersByTimeAsync(0)
    controller.abort()
    await rejected
    await vi.advanceTimersByTimeAsync(1500)
    expect(request).toHaveBeenCalledTimes(1)
    expect(vi.getTimerCount()).toBe(0)
  })

  it.each([
    { status: 0 },
    { status: 500 },
    { status: 502 },
    { status: 503 },
    { status: 504 },
    { code: 'ERR_NETWORK' },
    { code: 'ECONNABORTED' },
    { code: 'ETIMEDOUT' },
  ])('automatically recovers a transient read failure %j', async (error) => {
    const request = vi.fn().mockRejectedValueOnce(error).mockResolvedValueOnce('loaded')
    const result = fetchUsageWithRetry(request, new AbortController().signal)
    await vi.advanceTimersByTimeAsync(499)
    expect(request).toHaveBeenCalledTimes(1)
    await vi.advanceTimersByTimeAsync(1)
    await expect(result).resolves.toBe('loaded')
    expect(request).toHaveBeenCalledTimes(2)
    expect(vi.getTimerCount()).toBe(0)
  })

  it('backs off between retries and exposes the last error after three attempts', async () => {
    const error = { status: 503, message: 'Unavailable' }
    const request = vi.fn().mockRejectedValue(error)
    const result = fetchUsageWithRetry(request, new AbortController().signal)
    const rejected = expect(result).rejects.toBe(error)
    await vi.advanceTimersByTimeAsync(500)
    expect(request).toHaveBeenCalledTimes(2)
    await vi.advanceTimersByTimeAsync(999)
    expect(request).toHaveBeenCalledTimes(2)
    await vi.advanceTimersByTimeAsync(1)
    await rejected
    expect(request).toHaveBeenCalledTimes(3)
    expect(vi.getTimerCount()).toBe(0)
  })

  it.each([429, 503])('honors Retry-After on HTTP %s', async (status) => {
    const request = vi.fn()
      .mockRejectedValueOnce({ status, retryAfter: 2 })
      .mockResolvedValueOnce('loaded')
    const result = fetchUsageWithRetry(request, new AbortController().signal)
    await vi.advanceTimersByTimeAsync(1999)
    expect(request).toHaveBeenCalledTimes(1)
    await vi.advanceTimersByTimeAsync(1)
    await expect(result).resolves.toBe('loaded')
  })

  it('accepts a zero Retry-After without substituting the rate limit default', async () => {
    const request = vi.fn()
      .mockRejectedValueOnce({ status: 429, retryAfter: 0 })
      .mockResolvedValueOnce('loaded')
    const result = fetchUsageWithRetry(request, new AbortController().signal)
    await vi.advanceTimersByTimeAsync(0)
    await expect(result).resolves.toBe('loaded')
    expect(request).toHaveBeenCalledTimes(2)
  })

  it.each([undefined, 'invalid', -1, Number.NaN, Number.POSITIVE_INFINITY])(
    'waits for the rate limit window when Retry-After is invalid: %s',
    async (retryAfter) => {
      const request = vi.fn()
        .mockRejectedValueOnce({ status: 429, retryAfter })
        .mockResolvedValueOnce('loaded')
      const result = fetchUsageWithRetry(request, new AbortController().signal)
      await vi.advanceTimersByTimeAsync(59_999)
      expect(request).toHaveBeenCalledTimes(1)
      await vi.advanceTimersByTimeAsync(1)
      await expect(result).resolves.toBe('loaded')
    }
  )

  it.each([
    { status: 400 },
    { status: 401 },
    { status: 403 },
    { status: 404 },
    { status: 401, code: 'ERR_NETWORK' },
    new Error('Unclassified failure'),
    { status: 0, code: 'ERR_CANCELED' },
    { status: 0, name: 'AbortError' },
    { status: 0, __CANCEL__: true },
  ])('does not retry permanent, unclassified or canceled failures %j', async (error) => {
    const request = vi.fn().mockRejectedValue(error)
    await expect(fetchUsageWithRetry(request, new AbortController().signal)).rejects.toBe(error)
    expect(request).toHaveBeenCalledTimes(1)
    expect(vi.getTimerCount()).toBe(0)
  })

  it('does not issue a read when the signal is already aborted', async () => {
    const controller = new AbortController()
    controller.abort()
    const request = vi.fn()
    await expect(fetchUsageWithRetry(request, controller.signal)).rejects.toMatchObject({ name: 'AbortError' })
    expect(request).not.toHaveBeenCalled()
  })

  it('cancels a pending retry immediately, clearing the timer and abort listener', async () => {
    const controller = new AbortController()
    const removeListener = vi.spyOn(controller.signal, 'removeEventListener')
    const request = vi.fn().mockRejectedValue({ status: 429, retryAfter: 60 })
    const result = fetchUsageWithRetry(request, controller.signal)
    const rejected = expect(result).rejects.toMatchObject({ name: 'AbortError' })
    await vi.advanceTimersByTimeAsync(0)
    expect(vi.getTimerCount()).toBe(1)
    controller.abort()
    await rejected
    expect(vi.getTimerCount()).toBe(0)
    expect(removeListener).toHaveBeenCalledWith('abort', expect.any(Function))
    await vi.advanceTimersByTimeAsync(60_000)
    expect(request).toHaveBeenCalledTimes(1)
  })

  it('removes the abort listener after a retry wait completes', async () => {
    const controller = new AbortController()
    const removeListener = vi.spyOn(controller.signal, 'removeEventListener')
    const request = vi.fn().mockRejectedValueOnce({ status: 503 }).mockResolvedValueOnce('loaded')
    const result = fetchUsageWithRetry(request, controller.signal)
    await vi.advanceTimersByTimeAsync(500)
    await expect(result).resolves.toBe('loaded')
    expect(removeListener).toHaveBeenCalledWith('abort', expect.any(Function))
    expect(vi.getTimerCount()).toBe(0)
  })

  it.each(['resolve', 'reject'])('discards a read that settles by %s after cancellation', async (outcome) => {
    const controller = new AbortController()
    let resolve!: (value: string) => void
    let reject!: (reason: unknown) => void
    const request = vi.fn(() => new Promise<string>((res, rej) => {
      resolve = res
      reject = rej
    }))
    const result = fetchUsageWithRetry(request, controller.signal)
    const rejected = expect(result).rejects.toMatchObject({ name: 'AbortError' })
    controller.abort()
    if (outcome === 'resolve') resolve('obsolete')
    else reject({ status: 503 })
    await rejected
    expect(request).toHaveBeenCalledTimes(1)
    expect(vi.getTimerCount()).toBe(0)
  })
})
