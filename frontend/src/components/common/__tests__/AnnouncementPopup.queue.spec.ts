import { afterEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick, reactive } from 'vue'

import AnnouncementPopup from '../AnnouncementPopup.vue'

const store = vi.hoisted(() => ({
  state: null as null | {
    currentPopup: Record<string, unknown> | null
    queuedPopupCount: number
    dismissPopup: () => void
  },
}))

vi.mock('@/stores/announcements', async () => {
  const { reactive: makeReactive } = await import('vue')
  store.state = makeReactive({
    currentPopup: null,
    queuedPopupCount: 0,
    dismissPopup: vi.fn(),
  })
  return { useAnnouncementStore: () => store.state }
})

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, params?: Record<string, unknown>) => (
        key === 'announcements.moreQueued' ? `${params?.count} more waiting` : key
      ),
    }),
  }
})

const popup = {
  id: 7,
  title: 'Maintenance tonight',
  content: 'Short **notice**',
  notify_mode: 'popup',
  created_at: '2026-10-30T03:00:00Z',
  updated_at: '2026-10-30T03:00:00Z',
}

describe('AnnouncementPopup queue and focus', () => {
  afterEach(() => {
    store.state!.currentPopup = null
    store.state!.queuedPopupCount = 0
    document.body.innerHTML = ''
    document.body.style.overflow = ''
  })

  it('tells the user how many announcements are still waiting', async () => {
    store.state!.currentPopup = { ...popup }
    store.state!.queuedPopupCount = 2
    const wrapper = mount(AnnouncementPopup, { attachTo: document.body })
    await nextTick()

    expect(document.body.querySelector('[data-testid="announcement-queue"]')?.textContent?.trim())
      .toBe('2 more waiting')

    store.state!.queuedPopupCount = 0
    await nextTick()
    expect(document.body.querySelector('[data-testid="announcement-queue"]')).toBeNull()
    wrapper.unmount()
  })

  it('never shows a queue count in an admin preview', async () => {
    store.state!.queuedPopupCount = 3
    const wrapper = mount(AnnouncementPopup, {
      attachTo: document.body,
      props: { preview: true, announcement: reactive({ ...popup, is_pinned: false }) },
    })
    await nextTick()

    expect(document.body.querySelector('[data-testid="announcement-queue"]')).toBeNull()
    wrapper.unmount()
  })

  it('labels the dialog by its title and moves focus to the dismiss button', async () => {
    store.state!.currentPopup = { ...popup }
    const wrapper = mount(AnnouncementPopup, { attachTo: document.body })
    await nextTick()
    await nextTick()

    const dialog = document.body.querySelector('[role="dialog"]')
    const titleId = dialog?.getAttribute('aria-labelledby')
    expect(titleId).toBeTruthy()
    expect(document.getElementById(titleId!)?.textContent?.trim()).toBe('Maintenance tonight')
    expect(document.activeElement?.getAttribute('data-testid')).toBe('announcement-popup-dismiss')
    wrapper.unmount()
  })
})
