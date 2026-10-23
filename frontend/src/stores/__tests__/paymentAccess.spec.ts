import { createPinia, setActivePinia } from 'pinia'
import { reactive } from 'vue'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { usePaymentAccessStore } from '../paymentAccess'

const mocks = vi.hoisted(() => ({ get: vi.fn(), auth: {} as any }))
vi.mock('@/api/client', () => ({ apiClient: { get: mocks.get } }))
vi.mock('../auth', () => ({ useAuthStore: () => mocks.auth }))

beforeEach(() => {
  setActivePinia(createPinia())
  mocks.auth = reactive({ isAuthenticated: true, user: { id: 10 } })
  mocks.get.mockReset()
})

describe('payment access', () => {
  it('defaults to hidden, grants from the server, and immediately clears a denied grant', async () => {
    const store = usePaymentAccessStore()
    expect(store.allowed).toBe(false)
    mocks.get.mockResolvedValue({ data: { enabled: true, allowed: true, exclusive_enabled: true } })
    await store.refresh()
    expect(store.allowed).toBe(true)
    store.deny()
    expect(store.allowed).toBe(false)
  })

  it('does not retain a grant after a failed refresh', async () => {
    const store = usePaymentAccessStore()
    mocks.get.mockResolvedValueOnce({ data: { enabled: true, allowed: true, exclusive_enabled: false } })
    await store.refresh()
    mocks.get.mockRejectedValueOnce(new Error('offline'))
    await store.refresh()
    expect(store.allowed).toBe(false)
  })

  it('hides access as soon as authentication ends, even before the user object is cleared', async () => {
    const store = usePaymentAccessStore()
    mocks.get.mockResolvedValueOnce({ data: { enabled: true, allowed: true, exclusive_enabled: true } })
    await store.refresh()
    mocks.auth.isAuthenticated = false
    expect(store.allowed).toBe(false)
  })

  it('ignores an old response after logout or account replacement', async () => {
    const store = usePaymentAccessStore()
    let resolve!: (response: unknown) => void
    mocks.get.mockReturnValue(new Promise(done => { resolve = done }))
    const pending = store.refresh()
    mocks.auth.user = { id: 11 }
    store.reset()
    resolve({ data: { enabled: true, allowed: true, exclusive_enabled: true } })
    await pending
    expect(store.allowed).toBe(false)
    expect(store.access).toBeNull()
  })

  it('shares concurrent checks and prevents a late success from undoing revocation', async () => {
    const store = usePaymentAccessStore()
    let resolve!: (response: unknown) => void
    mocks.get.mockReturnValue(new Promise(done => { resolve = done }))
    const first = store.refresh()
    const second = store.refresh()
    expect(mocks.get).toHaveBeenCalledTimes(1)
    store.deny()
    resolve({ data: { enabled: true, allowed: true, exclusive_enabled: true } })
    await Promise.all([first, second])
    expect(store.allowed).toBe(false)
  })

  it('confirms a resource 404 with a fresh check and ignores older in-flight grants', async () => {
    const store = usePaymentAccessStore()
    mocks.get.mockResolvedValueOnce({ data: { enabled: true, allowed: true, exclusive_enabled: true } })
    await store.refresh()
    let resolveWatch!: (response: unknown) => void
    let resolveCheck!: (response: unknown) => void
    let resolveFresh!: (response: unknown) => void
    mocks.get.mockReturnValueOnce(new Promise(done => { resolveWatch = done }))
    const watching = store.waitForChange()
    const watchSignal = mocks.get.mock.lastCall?.[1].signal as AbortSignal
    mocks.get.mockReturnValueOnce(new Promise(done => { resolveCheck = done }))
    const checking = store.refresh()
    const checkSignal = mocks.get.mock.lastCall?.[1].signal as AbortSignal
    mocks.get.mockReturnValueOnce(new Promise(done => { resolveFresh = done }))
    const fresh = store.refresh(true)
    expect(mocks.get).toHaveBeenCalledTimes(4)
    expect(watchSignal.aborted).toBe(true)
    expect(checkSignal.aborted).toBe(true)
    expect(store.allowed).toBe(true)
    resolveFresh({ data: { enabled: true, allowed: false, exclusive_enabled: true } })
    await fresh
    resolveWatch({ data: { enabled: true, allowed: true, exclusive_enabled: true } })
    resolveCheck({ data: { enabled: true, allowed: true, exclusive_enabled: true } })
    await Promise.all([watching, checking])
    expect(store.allowed).toBe(false)
  })

  it('retains a legitimate grant after a missing-order check and fails closed if confirmation fails', async () => {
    const store = usePaymentAccessStore()
    mocks.get.mockResolvedValue({ data: { enabled: true, allowed: true, exclusive_enabled: true } })
    await store.refresh()
    await store.refresh(true)
    expect(store.allowed).toBe(true)
    mocks.get.mockRejectedValueOnce(new Error('permission endpoint unavailable'))
    await store.refresh(true)
    expect(store.allowed).toBe(false)
  })

  it('receives an open-page revocation without delaying an independent route check', async () => {
    const store = usePaymentAccessStore()
    mocks.get.mockResolvedValueOnce({ data: { enabled: true, allowed: true, exclusive_enabled: true } })
    await store.refresh()
    let resolve!: (response: unknown) => void
    mocks.get.mockReturnValueOnce(new Promise(done => { resolve = done }))
    const watching = store.waitForChange()
    expect(mocks.get.mock.lastCall?.[1].params.watch).toBe('true:true:true')
    mocks.get.mockResolvedValueOnce({ data: { enabled: true, allowed: true, exclusive_enabled: true } })
    await store.refresh()
    expect(mocks.get).toHaveBeenCalledTimes(3)
    resolve({ data: { enabled: true, allowed: false, exclusive_enabled: true } })
    await watching
    expect(store.allowed).toBe(false)
  })

  it('does not let an aborted watcher overwrite the next session', async () => {
    const store = usePaymentAccessStore()
    mocks.get.mockResolvedValueOnce({ data: { enabled: true, allowed: true, exclusive_enabled: true } })
    await store.refresh()
    let reject!: (error: Error) => void
    mocks.get.mockReturnValueOnce(new Promise((_resolve, fail) => { reject = fail }))
    const watching = store.waitForChange()
    store.stopWatching()
    reject(new Error('cancelled'))
    await watching
    expect(store.allowed).toBe(true)
  })

  it('does not let a slower route check restore a grant revoked by the watcher', async () => {
    const store = usePaymentAccessStore()
    mocks.get.mockResolvedValueOnce({ data: { enabled: true, allowed: true, exclusive_enabled: true } })
    await store.refresh()
    let resolveWatch!: (response: unknown) => void
    let resolveCheck!: (response: unknown) => void
    mocks.get.mockReturnValueOnce(new Promise(done => { resolveWatch = done }))
    const watching = store.waitForChange()
    mocks.get.mockReturnValueOnce(new Promise(done => { resolveCheck = done }))
    const checking = store.refresh()
    resolveWatch({ data: { enabled: true, allowed: false, exclusive_enabled: true } })
    await watching
    resolveCheck({ data: { enabled: true, allowed: true, exclusive_enabled: true } })
    expect((await checking)?.allowed).toBe(false)
    expect(store.allowed).toBe(false)
  })

  it('does not let a late watcher restore access after a failed independent check', async () => {
    const store = usePaymentAccessStore()
    mocks.get.mockResolvedValueOnce({ data: { enabled: true, allowed: true, exclusive_enabled: true } })
    await store.refresh()
    let resolveWatch!: (response: unknown) => void
    mocks.get.mockReturnValueOnce(new Promise(done => { resolveWatch = done }))
    const watching = store.waitForChange()
    mocks.get.mockRejectedValueOnce(new Error('offline'))
    await store.refresh()
    expect(store.allowed).toBe(false)
    resolveWatch({ data: { enabled: true, allowed: true, exclusive_enabled: true } })
    await watching
    expect(store.allowed).toBe(false)
    expect(store.access).toBeNull()
  })

  it('does not let a late check restore access after a failed watcher', async () => {
    const store = usePaymentAccessStore()
    mocks.get.mockResolvedValueOnce({ data: { enabled: true, allowed: true, exclusive_enabled: true } })
    await store.refresh()
    let rejectWatch!: (error: Error) => void
    let resolveCheck!: (response: unknown) => void
    mocks.get.mockReturnValueOnce(new Promise((_done, fail) => { rejectWatch = fail }))
    const watching = store.waitForChange()
    mocks.get.mockReturnValueOnce(new Promise(done => { resolveCheck = done }))
    const checking = store.refresh()
    rejectWatch(new Error('offline'))
    await watching
    resolveCheck({ data: { enabled: true, allowed: true, exclusive_enabled: true } })
    expect(await checking).toBeNull()
    expect(store.allowed).toBe(false)
  })

  it.each([
    null,
    { enabled: true, allowed: 'true', exclusive_enabled: true },
    { enabled: true, allowed: true },
  ])('fails closed on malformed permission data %j', async data => {
    const store = usePaymentAccessStore()
    mocks.get.mockResolvedValue({ data })
    expect(await store.refresh()).toBeNull()
    expect(store.allowed).toBe(false)
  })
})
