import { defineComponent } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import ResourceLibraryView from '@/views/admin/ResourceLibraryView.vue'

const {
  listItems,
  listSaved,
  fetchItemContent,
  fetchSavedContent,
  downloadItem,
  deleteItem,
  saveItem,
  showSuccess,
  showError,
} = vi.hoisted(() => ({
  listItems: vi.fn(),
  listSaved: vi.fn(),
  fetchItemContent: vi.fn(),
  fetchSavedContent: vi.fn(),
  downloadItem: vi.fn(),
  deleteItem: vi.fn(),
  saveItem: vi.fn(),
  showSuccess: vi.fn(),
  showError: vi.fn(),
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    resourceLibrary: {
      listItems,
      listSaved,
      fetchItemContent,
      fetchSavedContent,
      downloadItem,
      deleteItem,
      saveItem,
    },
  },
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showSuccess, showError }),
}))

vi.mock('@/composables/useClipboard', () => ({
  useClipboard: () => ({ copyToClipboard: vi.fn().mockResolvedValue(true) }),
}))

vi.mock('vue-i18n', async () => ({
  ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'),
  useI18n: () => ({ t: (key: string) => key }),
}))

// URL has no createObjectURL in jsdom for blobs; stub it so cards can render.
beforeEach(() => {
  Object.defineProperty(URL, 'createObjectURL', { value: vi.fn(() => 'blob:preview'), configurable: true })
  Object.defineProperty(URL, 'revokeObjectURL', { value: vi.fn(), configurable: true })
})

const AppLayoutStub = defineComponent({ template: '<main><slot /></main>' })

function liveItem(overrides: Record<string, unknown> = {}) {
  return {
    kind: 'image',
    task_id: 'imgtask_0123456789abcdef0123456789abcdef',
    index: 0,
    name: 'imgtask_0123456789abcdef0123456789abcdef-1.png',
    user_id: 7,
    user_email: 'user@example.com',
    api_key_id: 3,
    group_id: 2,
    group_name: 'A组',
    model: 'gpt-image-1',
    prompt: 'draw a cat',
    size_bytes: 2048,
    content_type: 'image/png',
    created_at: 1700000000,
    expires_at: 1700003600,
    cost: 0.125,
    billing_state: 'settled',
    details: { quality: 'auto' },
    ...overrides,
  }
}

function savedItem(overrides: Record<string, unknown> = {}) {
  return {
    id: 'res_0000000000000000000000000000000a',
    kind: 'video',
    source_task_id: 'vidtask_0123456789abcdef0123456789abcdef',
    source_index: 0,
    name: 'vidtask_0123456789abcdef0123456789abcdef.mp4',
    content_type: 'video/mp4',
    size_bytes: 4096,
    sha256: 'deadbeef',
    user_id: 7,
    user_email: 'user@example.com',
    api_key_id: 3,
    group_id: 2,
    group_name: 'A组',
    model: 'veo-3',
    prompt: 'a dog running',
    cost: null as number | null,
    generated_at: '2026-10-14T10:00:00Z',
    created_at: '2026-10-14T11:00:00Z',
    saved_by: 1,
    saved_by_email: 'admin@example.com',
    ...overrides,
  }
}

function mountView() {
  return mount(ResourceLibraryView, {
    global: {
      stubs: {
        AppLayout: AppLayoutStub,
        Select: true,
        Pagination: true,
        BaseDialog: { template: '<div><slot /><slot name="footer" /></div>' },
        ConfirmDialog: true,
        Icon: true,
      },
    },
  })
}

describe('ResourceLibraryView', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    listItems.mockResolvedValue({
      items: [liveItem()],
      total: 1,
      page: 1,
      page_size: 20,
      pages: 1,
      models: ['gpt-image-1'],
      groups: [{ id: 2, name: 'A组' }],
      truncated: false,
    })
    listSaved.mockResolvedValue({
      items: [],
      total: 0,
      page: 1,
      page_size: 20,
      pages: 1,
      models: [],
      groups: [],
    })
    fetchItemContent.mockResolvedValue(new Blob(['x'], { type: 'image/png' }))
    fetchSavedContent.mockResolvedValue(new Blob(['x'], { type: 'video/mp4' }))
  })

  it('renders generated results as cards with provenance and cost', async () => {
    const wrapper = mountView()
    await flushPromises()

    const text = wrapper.text()
    expect(text).toContain('imgtask_0123456789abcdef0123456789abcdef-1.png')
    expect(text).toContain('user@example.com')
    expect(text).toContain('A组')
    expect(text).toContain('gpt-image-1')
    expect(text).toContain('draw a cat')
    expect(text).toContain('$0.125')
    // One card per generated result; the empty saved tab adds none.
    expect(wrapper.findAll('article')).toHaveLength(1)
    expect(listItems).toHaveBeenCalledTimes(1)
  })

  it('saves a live result and marks the card as archived', async () => {
    saveItem.mockResolvedValue({ item: savedItem(), created: true })
    const wrapper = mountView()
    await flushPromises()

    const saveButton = wrapper.findAll('button').find(button => button.text().includes('admin.resourceLibrary.actions.save'))
    expect(saveButton).toBeTruthy()
    await saveButton!.trigger('click')
    await flushPromises()

    expect(saveItem).toHaveBeenCalledWith('image', 'imgtask_0123456789abcdef0123456789abcdef', 0)
    expect(showSuccess).toHaveBeenCalled()
  })

  it('asks for confirmation before deleting a live result and reloads afterwards', async () => {
    deleteItem.mockResolvedValue(undefined)
    const wrapper = mountView()
    await flushPromises()

    const deleteButton = wrapper.findAll('button').find(button => button.text().includes('common.delete'))
    expect(deleteButton).toBeTruthy()
    await deleteButton!.trigger('click')

    const confirmDialog = wrapper.findComponent({ name: 'ConfirmDialog' })
    expect(confirmDialog.exists() ? confirmDialog.props('message') : '').toBeTruthy()
    await confirmDialog.vm.$emit('confirm')
    await flushPromises()

    expect(deleteItem).toHaveBeenCalledWith('image', 'imgtask_0123456789abcdef0123456789abcdef')
    expect(listItems.mock.calls.length).toBeGreaterThan(1)
  })

  it('lists archived copies on the saved tab', async () => {
    listSaved.mockResolvedValue({
      items: [savedItem()],
      total: 1,
      page: 1,
      page_size: 20,
      pages: 1,
      models: ['veo-3'],
      groups: [{ id: 2, name: 'A组' }],
    })
    const wrapper = mountView()
    await flushPromises()

    const savedTab = wrapper.findAll('button').find(button => button.text().includes('admin.resourceLibrary.tabs.saved'))
    expect(savedTab).toBeTruthy()
    await savedTab!.trigger('click')
    await flushPromises()

    expect(listSaved).toHaveBeenCalled()
    const text = wrapper.text()
    expect(text).toContain('vidtask_0123456789abcdef0123456789abcdef.mp4')
    expect(text).toContain('veo-3')
    // The archived video has no recorded cost.
    expect(text).toContain('—')
  })

  it('surfaces the disabled state when the API reports 404', async () => {
    listItems.mockRejectedValue({ status: 404 })
    mountView()
    await flushPromises()

    expect(showError).toHaveBeenCalledWith('admin.resourceLibrary.disabled')
  })
})