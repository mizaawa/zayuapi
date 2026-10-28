import { computed, ref } from 'vue'
import { defineStore } from 'pinia'
import { apiClient } from '@/api/client'
import { useAuthStore } from './auth'

export interface PaymentAccess {
  enabled: boolean
  allowed: boolean
  exclusive_enabled: boolean
}

/**
 * The server answers a watch after at most 25 seconds. The extra headroom covers time the
 * request spends queued in the browser, so a healthy watch never times out on the client.
 */
export const PAYMENT_ACCESS_WATCH_TIMEOUT_MS = 45_000

/** Last confirmed grant of the signed-in account, used to render the links immediately on reload. */
export const PAYMENT_ACCESS_HINT_KEY = 'payment_access_hint'

export function isUserPaymentPath(path: string): boolean {
  const normalized = path.toLowerCase().replace(/\/+$/, '')
  return normalized === '/purchase' || normalized === '/orders' || normalized.startsWith('/payment/') ||
    normalized === '/auth/wechat/payment/callback'
}

function isPaymentAccess(value: unknown): value is PaymentAccess {
  const candidate = value as Partial<PaymentAccess> | null
  return !!candidate && typeof candidate === 'object' && typeof candidate.enabled === 'boolean' &&
    typeof candidate.allowed === 'boolean' && typeof candidate.exclusive_enabled === 'boolean'
}

/**
 * Lost connections, timeouts, throttling and server errors say nothing about the grant
 * itself. Only a refusal from the permission endpoint may hide the payment links;
 * everything else keeps the confirmed snapshot and is retried.
 */
function isTransientFailure(error: unknown): boolean {
  const status = (error as { status?: unknown } | null)?.status
  return typeof status !== 'number' || status === 0 || status === 408 || status === 429 || status >= 500
}

function readRememberedAccess(userId: number): PaymentAccess | null {
  try {
    const stored = JSON.parse(localStorage.getItem(PAYMENT_ACCESS_HINT_KEY) || 'null') as
      (Partial<PaymentAccess> & { user_id?: unknown }) | null
    if (!stored || stored.user_id !== userId || !isPaymentAccess(stored)) return null
    return { enabled: stored.enabled, allowed: stored.allowed, exclusive_enabled: stored.exclusive_enabled }
  } catch {
    return null
  }
}

function forgetRememberedAccess() {
  try {
    localStorage.removeItem(PAYMENT_ACCESS_HINT_KEY)
  } catch {
    // Storage can be unavailable in private browsing; the hint is only an optimization.
  }
}

function rememberAccess(userId: number, value: PaymentAccess) {
  if (!value.enabled || !value.allowed) {
    forgetRememberedAccess()
    return
  }
  try {
    localStorage.setItem(PAYMENT_ACCESS_HINT_KEY, JSON.stringify({ user_id: userId, ...value }))
  } catch {
    // See forgetRememberedAccess.
  }
}

const fingerprint = (value: PaymentAccess) => `${value.enabled}:${value.allowed}:${value.exclusive_enabled}`

export const usePaymentAccessStore = defineStore('paymentAccess', () => {
  const auth = useAuthStore()
  const access = ref<PaymentAccess | null>(null)
  const ownerId = ref<number | null>(null)
  let generation = 0
  let snapshotVersion = 0
  let watchSequence = 0
  let request: Promise<PaymentAccess | null> | null = null
  let controller: AbortController | null = null
  let watchController: AbortController | null = null
  const allowed = computed(() => auth.isAuthenticated && ownerId.value === auth.user?.id &&
    access.value?.enabled === true && access.value.allowed === true)

  // Render the last grant this account confirmed right away; the first check confirms or withdraws it.
  function adoptRememberedAccess() {
    const userId = auth.isAuthenticated ? auth.user?.id : undefined
    const remembered = userId ? readRememberedAccess(userId) : null
    if (!userId || !remembered) return
    ownerId.value = userId
    access.value = remembered
  }

  function invalidateAccess() {
    // Invalidate every older in-flight response, including a long-poll that could restore a grant.
    snapshotVersion++
    access.value = null
    forgetRememberedAccess()
  }

  function reset() {
    generation++
    stopWatching()
    controller?.abort()
    controller = null
    request = null
    ownerId.value = null
    access.value = null
    if (!auth.isAuthenticated) forgetRememberedAccess()
  }

  function stopWatching() {
    watchController?.abort()
    watchController = null
  }

  function applyAccess(value: PaymentAccess) {
    const current = access.value
    if (current && fingerprint(current) === fingerprint(value)) return
    const modeChanged = current !== null && current.exclusive_enabled !== value.exclusive_enabled
    snapshotVersion++
    access.value = value
    if (ownerId.value !== null) rememberAccess(ownerId.value, value)
    if (modeChanged) window.dispatchEvent(new Event('payment-mode-changed'))
  }

  // Every watch gets its own URL. Chrome's HTTP cache makes identical in-flight GETs wait for
  // each other (up to 20 seconds), so watches from two open windows pushed each other past
  // the client timeout and the payment links disappeared every 30 seconds.
  const watchNonce = () => `${Date.now().toString(36)}${(++watchSequence).toString(36)}${Math.random().toString(36).slice(2, 8)}`

  async function waitForChange(): Promise<boolean> {
    if (!auth.isAuthenticated || !access.value) return (await refresh()) !== null
    stopWatching()
    const previous = access.value
    const requestGeneration = generation
    const requestSnapshotVersion = snapshotVersion
    const requestOwner = auth.user?.id
    const watcher = new AbortController()
    watchController = watcher
    const isCurrent = () => !watcher.signal.aborted && generation === requestGeneration &&
      snapshotVersion === requestSnapshotVersion
    try {
      const { data } = await apiClient.get<unknown>('/payment/access', {
        params: { watch: fingerprint(previous), nonce: watchNonce() },
        signal: watcher.signal,
        timeout: PAYMENT_ACCESS_WATCH_TIMEOUT_MS,
      })
      if (!isCurrent() || auth.user?.id !== requestOwner) return true
      if (!isPaymentAccess(data)) {
        invalidateAccess()
        return false
      }
      applyAccess(data)
      return true
    } catch (error) {
      // A dropped or slow watch is not a revocation; keep the confirmed snapshot and retry.
      if (isCurrent() && !isTransientFailure(error)) invalidateAccess()
      return false
    } finally {
      if (watchController === watcher) watchController = null
    }
  }

  async function refresh(force = false): Promise<PaymentAccess | null> {
    if (force) {
      // A resource 404 needs a fresh decision, independent of older checks and long-polls.
      generation++
      stopWatching()
      controller?.abort()
      controller = null
      request = null
    }
    if (!auth.isAuthenticated || !auth.user) {
      reset()
      return null
    }
    if (ownerId.value !== auth.user.id) {
      reset()
      adoptRememberedAccess()
    }
    if (request) return request
    ownerId.value = auth.user.id
    const requestGeneration = generation
    const requestSnapshotVersion = snapshotVersion
    const requestOwner = auth.user.id
    controller = new AbortController()
    const signal = controller.signal
    const pending = (async () => {
      try {
        const { data } = await apiClient.get<unknown>('/payment/access', { signal })
        if (generation !== requestGeneration || auth.user?.id !== requestOwner) return null
        if (snapshotVersion !== requestSnapshotVersion) return access.value
        if (!isPaymentAccess(data)) {
          invalidateAccess()
          return null
        }
        applyAccess(data)
        return data
      } catch (error) {
        // A refusal always clears the grant, and so does any failed confirmation of a payment 404.
        // Callers that need a fresh answer (route guards) still receive null for transient failures.
        if (generation === requestGeneration && snapshotVersion === requestSnapshotVersion &&
          (force || !isTransientFailure(error))) invalidateAccess()
        return null
      } finally {
        if (generation === requestGeneration) {
          request = null
          controller = null
        }
      }
    })()
    request = pending
    return pending
  }

  function deny() {
    reset()
    forgetRememberedAccess()
    ownerId.value = auth.user?.id ?? null
    access.value = { enabled: true, allowed: false, exclusive_enabled: true }
  }

  adoptRememberedAccess()

  return { access, allowed, refresh, waitForChange, stopWatching, reset, deny }
})
