import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { reactive } from 'vue'
import { useAnnouncementStore } from '@/stores/announcements'
import type { UserAnnouncement } from '@/types'

const api = vi.hoisted(() => ({ list: vi.fn(), markRead: vi.fn(), serverTime: null as number | null }))
vi.mock('@/api', () => ({
  announcementsAPI: {
    listSnapshot: async () => ({ items: await api.list(), fetchedAt: api.serverTime ?? Date.now() }),
    markRead: api.markRead,
  }
}))
const auth = reactive({ isAuthenticated: true, user: { id: 1 } as { id: number } | null })
vi.mock('@/stores/auth', () => ({ useAuthStore: () => auth }))

const FIRST_VISIT = '2026-09-28T10:00:00.000Z'
const OLD_REVISION = '2026-09-01T10:00:00.000Z'
const NEW_REVISION = '2026-09-28T11:00:00.000Z'
const CACHE_KEY = 'announcement_popup_state_v1:1'

function announcement(id: number, overrides: Partial<UserAnnouncement> = {}): UserAnnouncement {
  return {
    id, title: `Announcement ${id}`, content: 'Content', notify_mode: 'popup',
    created_at: OLD_REVISION, updated_at: OLD_REVISION, ...overrides
  }
}

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((done) => { resolve = done })
  return { resolve, promise }
}

describe('announcement popup delivery', () => {
  let store: ReturnType<typeof useAnnouncementStore>

  beforeEach(() => {
    vi.useFakeTimers()
    vi.setSystemTime(FIRST_VISIT)
    vi.clearAllMocks()
    api.serverTime = null
    localStorage.clear()
    auth.isAuthenticated = true
    auth.user = { id: 1 }
    setActivePinia(createPinia())
    store = useAnnouncementStore()
    api.markRead.mockResolvedValue({ message: 'ok' })
    api.list.mockResolvedValue([])
  })

  afterEach(() => {
    store.reset()
    store.$dispose()
    vi.restoreAllMocks()
    vi.useRealTimers()
  })

  function refreshBrowser() {
    store.reset()
    store.$dispose()
    setActivePinia(createPinia())
    store = useAnnouncementStore()
  }

  it('shows only the first pinned announcement and retains all historical announcements', async () => {
    const history = Array.from({ length: 25 }, (_, index) => announcement(index + 1))
    history[24].is_pinned = true
    api.list.mockResolvedValue(history)

    await store.fetchAnnouncements()

    expect(store.currentPopup?.id).toBe(25)
    expect(store.announcements).toHaveLength(25)
    expect(api.markRead).not.toHaveBeenCalled()
    await store.dismissPopup()
    await vi.advanceTimersByTimeAsync(300)
    expect(store.currentPopup).toBeNull()
    expect(api.markRead).toHaveBeenCalledTimes(1)
    expect(api.markRead).toHaveBeenCalledWith(25)
  })

  it('shows the first pinned announcement even if silent and already read', async () => {
    api.list.mockResolvedValue([announcement(1, {
      is_pinned: true, notify_mode: 'silent', read_at: OLD_REVISION
    })])
    await store.fetchAnnouncements()
    expect(store.currentPopup?.id).toBe(1)
  })

  it('initializes without a popup when there is no pin and does not show history after refresh', async () => {
    api.list.mockResolvedValue([announcement(1)])
    await store.fetchAnnouncements()
    expect(store.currentPopup).toBeNull()
    expect(localStorage.getItem(CACHE_KEY)).not.toBeNull()

    refreshBrowser()
    await store.fetchAnnouncements()
    expect(store.currentPopup).toBeNull()
    expect(store.announcements.map((item) => item.id)).toEqual([1])
  })

  it('does not repeat a displayed pin on refresh even when it was not dismissed', async () => {
    api.list.mockResolvedValue([announcement(1, { is_pinned: true })])
    await store.fetchAnnouncements()
    expect(store.currentPopup?.id).toBe(1)
    refreshBrowser()
    await store.fetchAnnouncements()
    expect(store.currentPopup).toBeNull()
    expect(api.markRead).not.toHaveBeenCalled()
  })

  it('preserves pending new announcements across refresh without repeating displayed versions', async () => {
    await store.fetchAnnouncements()
    vi.setSystemTime(NEW_REVISION)
    api.list.mockResolvedValue([announcement(2, { updated_at: NEW_REVISION }), announcement(3, { updated_at: NEW_REVISION })])
    await store.fetchAnnouncements(true)
    expect(store.currentPopup?.id).toBe(2)

    refreshBrowser()
    await store.fetchAnnouncements()
    expect(store.currentPopup?.id).toBe(3)
    refreshBrowser()
    await store.fetchAnnouncements()
    expect(store.currentPopup).toBeNull()
  })

  it('automatically shows edited announcements once per version regardless of server read status', async () => {
    api.list.mockResolvedValue([announcement(1, { read_at: OLD_REVISION })])
    await store.fetchAnnouncements()
    api.list.mockResolvedValue([announcement(1, { read_at: OLD_REVISION, updated_at: NEW_REVISION })])
    await store.fetchAnnouncements(true)
    expect(store.currentPopup?.updated_at).toBe(NEW_REVISION)
    expect(api.markRead).not.toHaveBeenCalled()

    refreshBrowser()
    await store.fetchAnnouncements()
    expect(store.currentPopup).toBeNull()
  })

  it('detects edits to known announcements even if the client clock is ahead of the server', async () => {
    api.list.mockResolvedValue([announcement(1)])
    await store.fetchAnnouncements()
    api.list.mockResolvedValue([announcement(1, { updated_at: '2026-09-27T10:00:00.000Z' })])
    await store.fetchAnnouncements(true)
    expect(store.currentPopup?.id).toBe(1)
  })

  it('uses the server snapshot time to detect new announcements when the browser clock is ahead', async () => {
    api.serverTime = Date.parse('2026-09-28T09:00:00.000Z')
    await store.fetchAnnouncements()
    api.list.mockResolvedValue([announcement(2, { updated_at: '2026-09-28T09:30:00.000Z' })])
    await store.fetchAnnouncements(true)
    expect(store.currentPopup?.id).toBe(2)
  })

  it('does not automatically show new or edited silent announcements', async () => {
    api.list.mockResolvedValue([announcement(1)])
    await store.fetchAnnouncements()
    api.list.mockResolvedValue([
      announcement(1, { notify_mode: 'silent', updated_at: NEW_REVISION }),
      announcement(2, { notify_mode: 'silent', updated_at: NEW_REVISION })
    ])
    await store.fetchAnnouncements(true)
    expect(store.currentPopup).toBeNull()
    expect(store.announcements).toHaveLength(2)
  })

  it('keeps newly visible historical announcements and pin-only changes from triggering popups', async () => {
    api.list.mockResolvedValue([announcement(1)])
    await store.fetchAnnouncements()
    api.list.mockResolvedValue([announcement(1, { is_pinned: true }), announcement(2)])
    await store.fetchAnnouncements(true)
    expect(store.currentPopup).toBeNull()
    expect(store.announcements).toHaveLength(2)
  })

  it('waits between popups and removes pending announcements that are no longer visible', async () => {
    await store.fetchAnnouncements()
    api.list.mockResolvedValue([announcement(1, { updated_at: NEW_REVISION }), announcement(2, { updated_at: NEW_REVISION })])
    await store.fetchAnnouncements(true)
    await store.dismissPopup()
    expect(store.currentPopup).toBeNull()
    api.list.mockResolvedValue([announcement(1, { updated_at: NEW_REVISION })])
    await store.fetchAnnouncements(true)
    await vi.advanceTimersByTimeAsync(300)
    expect(store.currentPopup).toBeNull()
  })

  it('shows queued announcements after the delay even when an intervening fetch runs', async () => {
    await store.fetchAnnouncements()
    api.list.mockResolvedValue([announcement(1, { updated_at: NEW_REVISION }), announcement(2, { updated_at: NEW_REVISION })])
    await store.fetchAnnouncements(true)
    await store.dismissPopup()
    await store.fetchAnnouncements(true)
    expect(store.currentPopup).toBeNull()
    await vi.advanceTimersByTimeAsync(299)
    expect(store.currentPopup).toBeNull()
    await vi.advanceTimersByTimeAsync(1)
    expect(store.currentPopup?.id).toBe(2)
  })

  it('deduplicates concurrent forced fetches and throttles later ordinary fetches', async () => {
    const request = deferred<UserAnnouncement[]>()
    api.list.mockReturnValue(request.promise)
    const first = store.fetchAnnouncements(true)
    const second = store.fetchAnnouncements(true)
    expect(api.list).toHaveBeenCalledTimes(1)
    request.resolve([])
    await Promise.all([first, second])
    await store.fetchAnnouncements()
    expect(api.list).toHaveBeenCalledTimes(1)
  })

  it('does not initialize the browser visit on failure and permits an immediate retry', async () => {
    vi.spyOn(console, 'error').mockImplementation(() => {})
    api.list.mockRejectedValueOnce(new Error('offline'))
    await store.fetchAnnouncements()
    expect(localStorage.getItem(CACHE_KEY)).toBeNull()
    expect(store.loading).toBe(false)

    api.list.mockResolvedValue([announcement(1, { is_pinned: true })])
    await store.fetchAnnouncements()
    expect(api.list).toHaveBeenCalledTimes(2)
    expect(store.currentPopup?.id).toBe(1)
  })

  it('discards an old response after reset and does not clear a newer in-flight request', async () => {
    const oldRequest = deferred<UserAnnouncement[]>()
    const newRequest = deferred<UserAnnouncement[]>()
    api.list.mockReturnValueOnce(oldRequest.promise).mockReturnValueOnce(newRequest.promise)
    const oldFetch = store.fetchAnnouncements()
    store.reset()
    const newFetch = store.fetchAnnouncements()
    oldRequest.resolve([announcement(1, { is_pinned: true })])
    await oldFetch
    expect(store.announcements).toEqual([])
    expect(store.loading).toBe(true)
    expect(localStorage.getItem(CACHE_KEY)).toBeNull()
    newRequest.resolve([announcement(2, { is_pinned: true })])
    await newFetch
    expect(store.currentPopup?.id).toBe(2)
    expect(store.loading).toBe(false)
  })

  it('cancels delayed popup display when the user logs out', async () => {
    await store.fetchAnnouncements()
    api.list.mockResolvedValue([announcement(1, { updated_at: NEW_REVISION }), announcement(2, { updated_at: NEW_REVISION })])
    await store.fetchAnnouncements(true)
    await store.dismissPopup()
    auth.isAuthenticated = false
    await vi.advanceTimersByTimeAsync(300)
    expect(store.currentPopup).toBeNull()
    expect(store.announcements).toEqual([])
    await store.fetchAnnouncements(true)
    expect(api.list).toHaveBeenCalledTimes(2)
  })

  it('isolates account caches and ignores responses from the previous account', async () => {
    const oldRequest = deferred<UserAnnouncement[]>()
    api.list.mockReturnValueOnce(oldRequest.promise)
    const oldFetch = store.fetchAnnouncements()
    auth.user = { id: 2 }
    api.list.mockResolvedValue([announcement(2, { is_pinned: true })])
    await store.fetchAnnouncements()
    oldRequest.resolve([announcement(1, { is_pinned: true })])
    await oldFetch
    expect(store.currentPopup?.id).toBe(2)
    expect(localStorage.getItem(CACHE_KEY)).toBeNull()
    expect(localStorage.getItem('announcement_popup_state_v1:2')).not.toBeNull()
  })

  it('ignores read responses from the previous account', async () => {
    api.list.mockResolvedValue([announcement(1)])
    await store.fetchAnnouncements()
    const readRequest = deferred<{ message: string }>()
    api.markRead.mockReturnValueOnce(readRequest.promise)
    const read = store.markAsRead(1)
    auth.user = { id: 2 }
    api.list.mockResolvedValue([announcement(1)])
    await store.fetchAnnouncements()
    readRequest.resolve({ message: 'ok' })
    await read
    expect(store.announcements[0].read_at).toBeUndefined()
  })

  it('recovers from malformed browser cache', async () => {
    localStorage.setItem(CACHE_KEY, '{broken')
    api.list.mockResolvedValue([announcement(1, { is_pinned: true })])
    await store.fetchAnnouncements()
    expect(store.currentPopup?.id).toBe(1)
    expect(() => JSON.parse(localStorage.getItem(CACHE_KEY)!)).not.toThrow()
  })
})
