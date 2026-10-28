import { createPinia, setActivePinia } from 'pinia'
import { reactive } from 'vue'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { PAYMENT_ACCESS_HINT_KEY, PAYMENT_ACCESS_WATCH_TIMEOUT_MS, usePaymentAccessStore } from '../paymentAccess'

const mocks = vi.hoisted(() => ({ get: vi.fn(), auth: {} as any }))
vi.mock('@/api/client', () => ({ apiClient: { get: mocks.get } }))
vi.mock('../auth', () => ({ useAuthStore: () => mocks.auth }))

const granted = { enabled: true, allowed: true, exclusive_enabled: true }
const transientFailures = [
  ['a timed out watch', { status: 0, code: 'ECONNABORTED', message: 'timeout of 45000ms exceeded' }],
  ['a dropped connection', { status: 0, message: 'Network error. Please check your connection.' }],
  ['a gateway error', { status: 502, message: 'Bad Gateway' }],
  ['throttling', { status: 429, code: 'RATE_LIMITED', message: 'Too many requests' }],
  ['an unexpected client error', new Error('offline')],
] as const
const refusals = [401, 403, 404].map(status => ({ status, message: 'refused' }))

beforeEach(() => {
  localStorage.clear()
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

  it.each(transientFailures)('keeps the confirmed grant through %s', async (_name, failure) => {
    const store = usePaymentAccessStore()
    mocks.get.mockResolvedValueOnce({ data: granted })
    await store.refresh()
    mocks.get.mockRejectedValueOnce(failure)
    expect(await store.waitForChange()).toBe(false)
    expect(store.allowed).toBe(true)
    mocks.get.mockRejectedValueOnce(failure)
    // Route guards still get no answer, but the open menu and page do not flicker.
    expect(await store.refresh()).toBeNull()
    expect(store.allowed).toBe(true)
    mocks.get.mockResolvedValueOnce({ data: { ...granted, allowed: false } })
    expect(await store.waitForChange()).toBe(true)
    expect(store.allowed).toBe(false)
  })

  it.each(refusals)('withdraws the grant when the permission check is refused with $status', async refusal => {
    const store = usePaymentAccessStore()
    mocks.get.mockResolvedValueOnce({ data: granted })
    await store.refresh()
    mocks.get.mockRejectedValueOnce(refusal)
    expect(await store.refresh()).toBeNull()
    expect(store.allowed).toBe(false)
    expect(localStorage.getItem(PAYMENT_ACCESS_HINT_KEY)).toBeNull()

    mocks.get.mockResolvedValueOnce({ data: granted })
    await store.refresh()
    mocks.get.mockRejectedValueOnce(refusal)
    expect(await store.waitForChange()).toBe(false)
    expect(store.allowed).toBe(false)
  })

  it('gives every watch its own cache key and outlasts the server hold', async () => {
    const store = usePaymentAccessStore()
    mocks.get.mockResolvedValueOnce({ data: granted })
    await store.refresh()
    mocks.get.mockResolvedValue({ data: granted })
    await store.waitForChange()
    await store.waitForChange()
    const [first, second] = mocks.get.mock.calls.slice(1).map(call => call[1])
    expect(first.params.watch).toBe('true:true:true')
    expect(first.params.nonce).toEqual(expect.any(String))
    expect(second.params.nonce).not.toBe(first.params.nonce)
    expect(first.timeout).toBe(PAYMENT_ACCESS_WATCH_TIMEOUT_MS)
    expect(PAYMENT_ACCESS_WATCH_TIMEOUT_MS).toBeGreaterThan(25_000 + 10_000)
  })

  it('shows the remembered grant of the same account before the first check and lets the check withdraw it', async () => {
    mocks.get.mockResolvedValueOnce({ data: granted })
    await usePaymentAccessStore().refresh()

    // A reload creates a new store: the links render immediately instead of popping in later.
    setActivePinia(createPinia())
    const reloaded = usePaymentAccessStore()
    expect(reloaded.allowed).toBe(true)
    expect(reloaded.access).toEqual(granted)
    expect(mocks.get).toHaveBeenCalledTimes(1)

    mocks.get.mockResolvedValueOnce({ data: { enabled: true, allowed: false, exclusive_enabled: true } })
    await reloaded.refresh()
    expect(reloaded.allowed).toBe(false)
    expect(localStorage.getItem(PAYMENT_ACCESS_HINT_KEY)).toBeNull()

    setActivePinia(createPinia())
    expect(usePaymentAccessStore().allowed).toBe(false)
  })

  it('never reuses a remembered grant for another account or after sign-out', async () => {
    mocks.get.mockResolvedValueOnce({ data: granted })
    await usePaymentAccessStore().refresh()

    mocks.auth.user = { id: 11 }
    setActivePinia(createPinia())
    const otherAccount = usePaymentAccessStore()
    expect(otherAccount.allowed).toBe(false)
    let resolve!: (response: unknown) => void
    mocks.get.mockReturnValueOnce(new Promise(done => { resolve = done }))
    const pending = otherAccount.refresh()
    expect(otherAccount.allowed).toBe(false)
    resolve({ data: granted })
    await pending
    expect(otherAccount.allowed).toBe(true)

    mocks.auth.isAuthenticated = false
    mocks.auth.user = null
    otherAccount.reset()
    expect(localStorage.getItem(PAYMENT_ACCESS_HINT_KEY)).toBeNull()
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

  it('does not let a late watcher restore access after a refused independent check', async () => {
    const store = usePaymentAccessStore()
    mocks.get.mockResolvedValueOnce({ data: { enabled: true, allowed: true, exclusive_enabled: true } })
    await store.refresh()
    let resolveWatch!: (response: unknown) => void
    mocks.get.mockReturnValueOnce(new Promise(done => { resolveWatch = done }))
    const watching = store.waitForChange()
    mocks.get.mockRejectedValueOnce({ status: 403, message: 'refused' })
    await store.refresh()
    expect(store.allowed).toBe(false)
    resolveWatch({ data: { enabled: true, allowed: true, exclusive_enabled: true } })
    await watching
    expect(store.allowed).toBe(false)
    expect(store.access).toBeNull()
  })

  it('does not let a late check restore access after a refused watcher', async () => {
    const store = usePaymentAccessStore()
    mocks.get.mockResolvedValueOnce({ data: { enabled: true, allowed: true, exclusive_enabled: true } })
    await store.refresh()
    let rejectWatch!: (error: unknown) => void
    let resolveCheck!: (response: unknown) => void
    mocks.get.mockReturnValueOnce(new Promise((_done, fail) => { rejectWatch = fail }))
    const watching = store.waitForChange()
    mocks.get.mockReturnValueOnce(new Promise(done => { resolveCheck = done }))
    const checking = store.refresh()
    rejectWatch({ status: 404, message: 'refused' })
    await watching
    resolveCheck({ data: { enabled: true, allowed: true, exclusive_enabled: true } })
    expect(await checking).toBeNull()
    expect(store.allowed).toBe(false)
  })

  it('lets an independent check confirm the grant while a watcher is merely reconnecting', async () => {
    const store = usePaymentAccessStore()
    mocks.get.mockResolvedValueOnce({ data: granted })
    await store.refresh()
    let rejectWatch!: (error: unknown) => void
    let resolveCheck!: (response: unknown) => void
    mocks.get.mockReturnValueOnce(new Promise((_done, fail) => { rejectWatch = fail }))
    const watching = store.waitForChange()
    mocks.get.mockReturnValueOnce(new Promise(done => { resolveCheck = done }))
    const checking = store.refresh()
    rejectWatch({ status: 0, code: 'ECONNABORTED', message: 'timeout' })
    expect(await watching).toBe(false)
    expect(store.allowed).toBe(true)
    resolveCheck({ data: granted })
    expect(await checking).toEqual(granted)
    expect(store.allowed).toBe(true)
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
