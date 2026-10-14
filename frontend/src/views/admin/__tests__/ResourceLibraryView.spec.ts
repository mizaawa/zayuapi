import { defineComponent } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import ResourceLibraryView from '@/views/admin/ResourceLibraryView.vue'

const {
  t,
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
  t: vi.fn((key: string) => key),
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
  useI18n: () => ({ t: (key: string) => t(key) }),
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
    details: {
      quality: 'auto',
      size: '1024x1024',
      count: 1,
      ratio: '16:9',
      resolution: '1080p',
      duration: 8,
      reference_images: 2,
      reference_videos: 0,
      reference_audios: 1,
      completed_at: 1700000100,
      billing_state: 'settled'
    },
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

  it('never renders a raw i18n key for detail labels', async () => {
    const wrapper = mountView()
    await flushPromises()
    // Open the preview so the detail block is rendered.
    const previewButton = wrapper.findAll('button').find(button =>
      button.text().includes('admin.resourceLibrary.actions.preview')
    )
    expect(previewButton).toBeTruthy()
    await previewButton!.trigger('click')
    await flushPromises()

    // In this suite `t` is identity, so a translated label would equal its i18n
    // key and `detailLabel` would fall back to the raw key. The real proof that
    // the view never prints backend machine keys verbatim is that it performs an
    // i18n lookup for every documented detail key.
    const documentedDetailKeys = [
      'quality', 'size', 'count', 'ratio', 'resolution', 'duration',
      'reference_images', 'reference_videos', 'reference_audios',
      'completed_at', 'billing_state'
    ]
    for (const key of documentedDetailKeys) {
      expect(t).toHaveBeenCalledWith(`admin.resourceLibrary.details.${key}`)
    }

    // And it must not have asked for a translation of the raw key itself.
    expect(t).not.toHaveBeenCalledWith('quality')
    expect(t).not.toHaveBeenCalledWith('reference_images')
  })

  it('keeps the live and saved tab counters independent', async () => {
    listItems.mockResolvedValue({
      items: [liveItem(), liveItem({ task_id: 'imgtask_ffffffffffffffffffffffffffffffff', index: 1, name: 'second.png' })],
      total: 2, page: 1, page_size: 20, pages: 1,
      models: ['gpt-image-1'], groups: [{ id: 2, name: 'A组' }], truncated: false
    })
    listSaved.mockResolvedValue({
      items: [savedItem()],
      total: 1, page: 1, page_size: 20, pages: 1,
      models: ['veo-3'], groups: [{ id: 2, name: 'A组' }]
    })
    const wrapper = mountView()
    await flushPromises()

    const badges = wrapper.findAll('button').filter(b => b.text().includes('admin.resourceLibrary.tabs.'))
    expect(badges.length).toBeGreaterThanOrEqual(2)
    // Live tab shows its own total (2), not the archived count.
    expect(badges[0]!.text()).toContain('2')
    expect(badges[1]!.text()).toContain('1')
  })

  it('surfaces the disabled state when the API reports 404', async () => {
    listItems.mockRejectedValue({ status: 404 })
    mountView()
    await flushPromises()

    expect(showError).toHaveBeenCalledWith('admin.resourceLibrary.disabled')
  })
})

// 与真实语言包联动：让上面的 t spy 改走真实的 zh 语言包，断言页面渲染的是译文，
// 而不是 admin.resourceLibrary.* 这类原始 key。这是最能直接复现线上问题的断言：
// 只要语言包结构写错（例如多包一层 admin 覆盖了同级模块），页面立刻退化成原始 key。
describe('ResourceLibraryView with real zh messages', () => {
  it('renders translated Chinese labels instead of raw i18n keys', async () => {
    const zh = (await import('@/i18n/locales/zh')).default as Record<string, unknown>
    const resolve = (obj: unknown, path: string): unknown =>
      path.split('.').reduce<unknown>((acc, part) => {
        if (acc && typeof acc === 'object') return (acc as Record<string, unknown>)[part]
        return undefined
      }, obj)

    t.mockImplementation(((key: string) => {
      const value = resolve(zh, key)
      return typeof value === 'string' ? value : key
    }) as never)

    const wrapper = mountView()
    await flushPromises()

    const text = wrapper.text()
    expect(text).toContain('资源库')
    expect(text).toContain('生成资源')
    expect(text).toContain('搜索')
    expect(text).not.toContain('admin.resourceLibrary.title')
    expect(text).not.toContain('admin.resourceLibrary.actions.preview')
    expect(text).not.toContain('admin.resourceLibrary.filters.q')

    // Restore the identity implementation for any later assertions.
    t.mockImplementation(((key: string) => key) as never)
  })
})
