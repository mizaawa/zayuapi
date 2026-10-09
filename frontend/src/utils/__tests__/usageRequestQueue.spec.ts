import { describe, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { createUsageRequestQueue } from '@/utils/usageRequestQueue'

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((res) => { resolve = res })
  return { promise, resolve }
}

describe('usage aggregate request queue', () => {
  it('starts the next read only when the active read settles', async () => {
    const enqueue = createUsageRequestQueue()
    const first = deferred<number>()
    const second = vi.fn().mockResolvedValue(2)
    const one = enqueue(() => first.promise, new AbortController().signal)
    const two = enqueue(second, new AbortController().signal)
    await flushPromises()
    expect(second).not.toHaveBeenCalled()
    first.resolve(1)
    await expect(one).resolves.toBe(1)
    await expect(two).resolves.toBe(2)
  })

  it('does not let canceling a queued read overtake the active read', async () => {
    const enqueue = createUsageRequestQueue()
    const first = deferred<number>()
    const controller = new AbortController()
    const canceled = vi.fn()
    const last = vi.fn().mockResolvedValue(3)
    const one = enqueue(() => first.promise, new AbortController().signal)
    const two = enqueue(canceled, controller.signal)
    const three = enqueue(last, new AbortController().signal)
    controller.abort()
    await expect(two).rejects.toMatchObject({ name: 'AbortError' })
    await flushPromises()
    expect(canceled).not.toHaveBeenCalled()
    expect(last).not.toHaveBeenCalled()
    first.resolve(1)
    await one
    await expect(three).resolves.toBe(3)
  })

  it('releases a canceled active read and ignores its late result', async () => {
    const enqueue = createUsageRequestQueue()
    const first = deferred<number>()
    const controller = new AbortController()
    const one = enqueue(() => first.promise, controller.signal)
    await flushPromises()
    controller.abort()
    await expect(one).rejects.toMatchObject({ name: 'AbortError' })
    await expect(enqueue(() => Promise.resolve(2), new AbortController().signal)).resolves.toBe(2)
    first.resolve(1)
    await flushPromises()
    await expect(one).rejects.toMatchObject({ name: 'AbortError' })
  })

  it.each([false, true])('continues after an error (synchronous: %s)', async (synchronous) => {
    const enqueue = createUsageRequestQueue()
    const error = new Error('Failed query')
    const failed = enqueue(() => {
      if (synchronous) throw error
      return Promise.reject(error)
    }, new AbortController().signal)
    const next = enqueue(() => Promise.resolve(2), new AbortController().signal)
    await expect(failed).rejects.toBe(error)
    await expect(next).resolves.toBe(2)
  })

  it('does not issue reads with an already canceled signal', async () => {
    const controller = new AbortController()
    controller.abort()
    const request = vi.fn()
    await expect(createUsageRequestQueue()(request, controller.signal)).rejects.toMatchObject({ name: 'AbortError' })
    expect(request).not.toHaveBeenCalled()
  })
})
