import { computed, ref } from 'vue'
import { defineStore } from 'pinia'
import { apiClient } from '@/api/client'
import { useAuthStore } from './auth'

export interface PaymentAccess {
  enabled: boolean
  allowed: boolean
  exclusive_enabled: boolean
}

export function isUserPaymentPath(path: string): boolean {
  const normalized = path.toLowerCase().replace(/\/+$/, '')
  return normalized === '/purchase' || normalized === '/orders' || normalized.startsWith('/payment/') ||
    normalized === '/auth/wechat/payment/callback'
}

export const usePaymentAccessStore = defineStore('paymentAccess', () => {
  const auth = useAuthStore()
  const access = ref<PaymentAccess | null>(null)
  const ownerId = ref<number | null>(null)
  let generation = 0
  let snapshotVersion = 0
  let request: Promise<PaymentAccess | null> | null = null
  let controller: AbortController | null = null
  let watchController: AbortController | null = null
  const allowed = computed(() => auth.isAuthenticated && ownerId.value === auth.user?.id &&
    access.value?.enabled === true && access.value.allowed === true)

  function invalidateAccess() {
    // Invalidate every older in-flight response, including a long-poll that could restore a grant.
    snapshotVersion++
    access.value = null
  }

  function reset() {
    generation++
    stopWatching()
    controller?.abort()
    controller = null
    request = null
    ownerId.value = null
    access.value = null
  }

  function stopWatching() {
    watchController?.abort()
    watchController = null
  }

  function applyAccess(value: PaymentAccess) {
    if (!value || typeof value.enabled !== 'boolean' || typeof value.allowed !== 'boolean' ||
      typeof value.exclusive_enabled !== 'boolean') throw new Error('Invalid payment access response')
    const modeChanged = access.value !== null && access.value.exclusive_enabled !== value.exclusive_enabled
    if (!access.value || access.value.enabled !== value.enabled || access.value.allowed !== value.allowed || modeChanged) snapshotVersion++
    access.value = value
    if (modeChanged) window.dispatchEvent(new Event('payment-mode-changed'))
  }

  async function waitForChange(): Promise<boolean> {
    if (!auth.isAuthenticated || !access.value) return (await refresh()) !== null
    stopWatching()
    const previous = access.value
    const fingerprint = (value: PaymentAccess) => `${value.enabled}:${value.allowed}:${value.exclusive_enabled}`
    const requestGeneration = generation
    const requestSnapshotVersion = snapshotVersion
    const requestOwner = auth.user?.id
    const watcher = new AbortController()
    watchController = watcher
    try {
      const { data } = await apiClient.get<PaymentAccess>('/payment/access', {
        params: { watch: fingerprint(previous) }, signal: watcher.signal, timeout: 30000,
      })
      if (!watcher.signal.aborted && generation === requestGeneration && auth.user?.id === requestOwner &&
        snapshotVersion === requestSnapshotVersion) applyAccess(data)
      return true
    } catch {
      if (!watcher.signal.aborted && generation === requestGeneration && snapshotVersion === requestSnapshotVersion) invalidateAccess()
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
    if (ownerId.value !== auth.user.id) reset()
    if (request) return request
    ownerId.value = auth.user.id
    const requestGeneration = generation
    const requestSnapshotVersion = snapshotVersion
    const requestOwner = auth.user.id
    controller = new AbortController()
    const signal = controller.signal
    const pending = (async () => {
      try {
        const { data } = await apiClient.get<PaymentAccess>('/payment/access', { signal })
        if (generation !== requestGeneration || auth.user?.id !== requestOwner) return null
        if (snapshotVersion !== requestSnapshotVersion) return access.value
        applyAccess(data)
        return data
      } catch {
        // Never keep an earlier grant usable after a failed permission check.
        if (generation === requestGeneration && snapshotVersion === requestSnapshotVersion) invalidateAccess()
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
    ownerId.value = auth.user?.id ?? null
    access.value = { enabled: true, allowed: false, exclusive_enabled: true }
  }

  return { access, allowed, refresh, waitForChange, stopWatching, reset, deny }
})
