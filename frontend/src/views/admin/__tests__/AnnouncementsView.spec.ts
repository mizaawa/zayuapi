import { defineComponent } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { Announcement } from '@/types'
import AnnouncementsView from '@/views/admin/AnnouncementsView.vue'

const { listAnnouncements, togglePin, showSuccess, showError } = vi.hoisted(() => ({
  listAnnouncements: vi.fn(),
  togglePin: vi.fn(),
  showSuccess: vi.fn(),
  showError: vi.fn(),
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    announcements: { list: listAnnouncements, togglePin },
    groups: { getAll: vi.fn().mockResolvedValue([]) },
  },
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showSuccess, showError }),
}))

vi.mock('vue-i18n', async () => ({
  ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'),
  useI18n: () => ({ t: (key: string) => key }),
}))

const DataTableStub = defineComponent({
  props: ['data'],
  template: '<div><div v-for="row in data" :key="row.id"><slot name="cell-title" :value="row.title" :row="row" /><slot name="cell-actions" :row="row" /></div></div>',
})

function announcement(id: number, isPinned = false): Announcement {
  return {
    id,
    title: `Announcement ${id}`,
    content: 'Announcement content',
    status: 'active',
    notify_mode: 'popup',
    is_pinned: isPinned,
    targeting: {},
    created_at: '2026-09-28T00:00:00Z',
    updated_at: '2026-09-28T00:00:00Z',
  }
}

function page(items: Announcement[]) {
  return { items, total: items.length, page: 1, page_size: 20, pages: 1 }
}

function mountView() {
  return mount(AnnouncementsView, {
    global: {
      stubs: {
        AppLayout: { template: '<main><slot /></main>' },
        TablePageLayout: { template: '<section><slot name="filters" /><slot name="table" /></section>' },
        DataTable: DataTableStub,
        Select: true,
        Pagination: true,
        BaseDialog: true,
        ConfirmDialog: true,
        EmptyState: true,
        AnnouncementTargetingEditor: true,
        AnnouncementReadStatusDialog: true,
        AnnouncementPopup: true,
      },
    },
  })
}

describe('AnnouncementsView pin action', () => {
  beforeEach(() => {
    localStorage.clear()
    vi.clearAllMocks()
    listAnnouncements.mockResolvedValue(page([announcement(1, true), announcement(2)]))
  })

  it('replaces the previous pin, updates both controls, and reports the replacement', async () => {
    togglePin.mockResolvedValue({ announcement: announcement(2, true), replaced_announcement_id: 1 })
    listAnnouncements.mockResolvedValueOnce(page([announcement(1, true), announcement(2)]))
      .mockResolvedValueOnce(page([announcement(1), announcement(2, true)]))
    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.get('[data-testid="announcement-pin-1"]').attributes('aria-pressed')).toBe('true')
    await wrapper.get('[data-testid="announcement-pin-2"]').trigger('click')
    await flushPromises()

    expect(togglePin).toHaveBeenCalledWith(2)
    expect(wrapper.get('[data-testid="announcement-pin-1"]').attributes('aria-pressed')).toBe('false')
    expect(wrapper.get('[data-testid="announcement-pin-2"]').attributes('aria-pressed')).toBe('true')
    expect(showSuccess).toHaveBeenCalledWith('admin.announcements.pinReplaced')
    wrapper.unmount()
  })

  it('unpins the selected announcement and reports success', async () => {
    togglePin.mockResolvedValue({ announcement: announcement(1) })
    listAnnouncements.mockResolvedValueOnce(page([announcement(1, true)]))
      .mockResolvedValueOnce(page([announcement(1)]))
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="announcement-pin-1"]').trigger('click')
    await flushPromises()

    expect(togglePin).toHaveBeenCalledWith(1)
    expect(wrapper.get('[data-testid="announcement-pin-1"]').attributes('aria-pressed')).toBe('false')
    expect(showSuccess).toHaveBeenCalledWith('admin.announcements.unpinSuccess')
    wrapper.unmount()
  })

  it('disables every pin control while an update is in flight', async () => {
    let resolvePin!: (value: { announcement: Announcement }) => void
    togglePin.mockImplementationOnce(() => new Promise(resolve => { resolvePin = resolve }))
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="announcement-pin-2"]').trigger('click')
    expect(wrapper.get('[data-testid="announcement-pin-1"]').attributes('disabled')).toBeDefined()
    expect(wrapper.get('[data-testid="announcement-pin-2"]').attributes('disabled')).toBeDefined()
    await wrapper.get('[data-testid="announcement-pin-1"]').trigger('click')
    expect(togglePin).toHaveBeenCalledTimes(1)

    resolvePin({ announcement: announcement(2, true) })
    await flushPromises()
    expect(wrapper.get('[data-testid="announcement-pin-2"]').attributes('disabled')).toBeUndefined()
    wrapper.unmount()
  })

  it('preserves current pin state and reports a failed update', async () => {
    togglePin.mockRejectedValueOnce({ response: { data: { detail: 'Pin failed' } } })
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="announcement-pin-2"]').trigger('click')
    await flushPromises()

    expect(wrapper.get('[data-testid="announcement-pin-1"]').attributes('aria-pressed')).toBe('true')
    expect(showError).toHaveBeenCalledWith('Pin failed')
    expect(showSuccess).not.toHaveBeenCalled()
    wrapper.unmount()
  })
})
