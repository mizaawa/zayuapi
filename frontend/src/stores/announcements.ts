import { defineStore } from 'pinia'
import { ref, computed, watch } from 'vue'
import { announcementsAPI } from '@/api'
import { useAuthStore } from './auth'
import type { UserAnnouncement } from '@/types'

const THROTTLE_MS = 20 * 60 * 1000 // 20 minutes
const POPUP_CACHE_PREFIX = 'announcement_popup_state_v1:'

interface PopupCache {
  initializedAt: number
  handled: Record<string, string>
  pending: Record<string, string>
}

function isRevisionMap(value: unknown): value is Record<string, string> {
  return value !== null && typeof value === 'object' && !Array.isArray(value)
    && Object.entries(value).every(([id, revision]) => /^\d+$/.test(id) && typeof revision === 'string')
}

function readPopupCache(userId: number): PopupCache | null {
  try {
    const raw = localStorage.getItem(`${POPUP_CACHE_PREFIX}${userId}`)
    if (!raw) return null
    const cache: unknown = JSON.parse(raw)
    if (cache && typeof cache === 'object' && 'initializedAt' in cache
      && typeof cache.initializedAt === 'number' && Number.isFinite(cache.initializedAt)
      && 'handled' in cache && isRevisionMap(cache.handled)
      && 'pending' in cache && isRevisionMap(cache.pending)) {
      return { initializedAt: cache.initializedAt, handled: cache.handled, pending: cache.pending }
    }
  } catch {
    // Storage can be unavailable in private browsing; retain session behavior.
  }
  return null
}

export const useAnnouncementStore = defineStore('announcements', () => {
  const authStore = useAuthStore()
  const announcements = ref<UserAnnouncement[]>([])
  const loading = ref(false)
  const lastFetchTime = ref(0)
  const popupQueue = ref<UserAnnouncement[]>([])
  const currentPopup = ref<UserAnnouncement | null>(null)

  let activeUserId: number | null = null
  let popupCache: PopupCache | null = null
  let sessionGeneration = 0
  let fetchRequest: Promise<void> | null = null
  let nextPopupTimer: ReturnType<typeof setTimeout> | null = null

  // Getters
  const unreadCount = computed(() =>
    announcements.value.filter((a) => !a.read_at).length
  )

  function authenticatedUserId() {
    return authStore.isAuthenticated ? authStore.user?.id ?? null : null
  }

  function isCurrentSession(generation: number, userId: number | null) {
    return userId !== null && generation === sessionGeneration
      && userId === activeUserId && userId === authenticatedUserId()
  }

  function persistPopupCache() {
    if (activeUserId === null || !popupCache) return
    try {
      localStorage.setItem(`${POPUP_CACHE_PREFIX}${activeUserId}`, JSON.stringify(popupCache))
    } catch {
      // A failed write must not prevent the announcement from being displayed.
    }
  }

  function fetchAnnouncements(force = false): Promise<void> {
    const userId = authenticatedUserId()
    if (userId === null) return Promise.resolve()
    if (activeUserId !== userId) {
      reset()
      activeUserId = userId
      popupCache = readPopupCache(userId)
    }
    if (fetchRequest) return fetchRequest

    const now = Date.now()
    if (!force && lastFetchTime.value > 0 && now - lastFetchTime.value < THROTTLE_MS) {
      return Promise.resolve()
    }

    const generation = sessionGeneration
    loading.value = true
    fetchRequest = (async () => {
      try {
        const snapshot = await announcementsAPI.listSnapshot(false)
        if (!isCurrentSession(generation, userId)) return
        announcements.value = snapshot.items
        lastFetchTime.value = Date.now()
        enqueueNewPopups(snapshot.fetchedAt)
      } catch (err: unknown) {
        if (!isCurrentSession(generation, userId)) return
        lastFetchTime.value = 0
        console.error('Failed to fetch announcements:', err)
      } finally {
        if (isCurrentSession(generation, userId)) {
          loading.value = false
          fetchRequest = null
        }
      }
    })()
    return fetchRequest
  }

  function enqueueNewPopups(fetchedAt: number) {
    const firstVisit = popupCache === null
    if (!popupCache) {
      popupCache = { initializedAt: fetchedAt, handled: {}, pending: {} }
    }
    const firstPinned = firstVisit ? announcements.value.find((a) => a.is_pinned) : undefined
    const pending: Record<string, string> = {}
    const nextQueue: UserAnnouncement[] = []

    for (const announcement of announcements.value) {
      const id = String(announcement.id)
      const revision = announcement.updated_at
      const isPending = popupCache.pending[id] === revision
      // A scheduled or targeted historical announcement may only become visible later.
      const updatedSinceFirstVisit = Date.parse(revision) > popupCache.initializedAt
      const changedSinceFirstVisit = popupCache.handled[id] === undefined
        ? updatedSinceFirstVisit : popupCache.handled[id] !== revision
      const shouldShow = announcement === firstPinned || (isPending && announcement.notify_mode === 'popup')
        || (!firstVisit && announcement.notify_mode === 'popup' && changedSinceFirstVisit)

      if (shouldShow) {
        pending[id] = revision
        nextQueue.push(announcement)
      } else {
        popupCache.handled[id] = revision
      }
    }

    popupCache.pending = pending
    popupQueue.value = nextQueue
    persistPopupCache()
    if (!currentPopup.value && nextPopupTimer === null) showNextPopup()
  }

  function showNextPopup() {
    if (authenticatedUserId() !== activeUserId || !popupCache) return
    currentPopup.value = popupQueue.value.shift() ?? null
    if (!currentPopup.value) return

    const { id, updated_at } = currentPopup.value
    popupCache.handled[String(id)] = updated_at
    delete popupCache.pending[String(id)]
    persistPopupCache()
  }

  async function dismissPopup() {
    if (!currentPopup.value) return
    const id = currentPopup.value.id
    const generation = sessionGeneration
    const userId = activeUserId
    currentPopup.value = null
    void markAsRead(id)

    if (popupQueue.value.length > 0 && nextPopupTimer === null) {
      nextPopupTimer = setTimeout(() => {
        nextPopupTimer = null
        if (isCurrentSession(generation, userId)) showNextPopup()
      }, 300)
    }
  }

  async function markAsRead(id: number) {
    const generation = sessionGeneration
    const userId = activeUserId
    if (!isCurrentSession(generation, userId)) return
    try {
      await announcementsAPI.markRead(id)
      if (!isCurrentSession(generation, userId)) return
      const ann = announcements.value.find((a) => a.id === id)
      if (ann) ann.read_at = new Date().toISOString()
    } catch (err: unknown) {
      console.error('Failed to mark announcement as read:', err)
    }
  }

  async function markAllAsRead() {
    const generation = sessionGeneration
    const userId = activeUserId
    if (!isCurrentSession(generation, userId)) return
    const unread = announcements.value.filter((a) => !a.read_at)
    if (unread.length === 0) return

    try {
      loading.value = true
      await Promise.all(unread.map((a) => announcementsAPI.markRead(a.id)))
      if (!isCurrentSession(generation, userId)) return
      const readIds = new Set(unread.map((a) => a.id))
      announcements.value.forEach((a) => {
        if (readIds.has(a.id)) a.read_at = new Date().toISOString()
      })
    } catch (err: unknown) {
      console.error('Failed to mark all as read:', err)
      throw err
    } finally {
      if (isCurrentSession(generation, userId)) loading.value = false
    }
  }

  function reset() {
    sessionGeneration++
    if (nextPopupTimer !== null) clearTimeout(nextPopupTimer)
    nextPopupTimer = null
    fetchRequest = null
    activeUserId = null
    popupCache = null
    announcements.value = []
    lastFetchTime.value = 0
    popupQueue.value = []
    currentPopup.value = null
    loading.value = false
  }

  watch(authenticatedUserId, reset, { flush: 'sync' })

  return {
    // State
    announcements,
    loading,
    currentPopup,
    // Getters
    unreadCount,
    // Actions
    fetchAnnouncements,
    dismissPopup,
    markAsRead,
    markAllAsRead,
    reset,
  }
})
