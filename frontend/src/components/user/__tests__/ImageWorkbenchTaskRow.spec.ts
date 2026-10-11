import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { ref } from 'vue'
import ImageWorkbenchTaskRow from '../ImageWorkbenchTaskRow.vue'
import type { ImageTask } from '@/api/imageWorkbench'

const { downloadImage } = vi.hoisted(() => ({ downloadImage: vi.fn() }))
vi.mock('@/api/imageWorkbench', () => ({ downloadWorkbenchImage: downloadImage }))
vi.mock('@/composables/useClipboard', () => ({ useClipboard: () => ({ copyToClipboard: vi.fn() }) }))
vi.mock('vue-i18n', async () => ({
  ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'),
  useI18n: () => ({ locale: ref('en'), t: (key: string) => key }),
}))

const taskId = 'imgtask_' + 'a'.repeat(32)
const task: ImageTask = {
  id: taskId, task_id: taskId, status: 'completed', created_at: 100, completed_at: 110, expires_at: 1010,
  workbench: { api_key_id: 1, prompt: 'An image', model: 'gpt-image-2', quality: 'auto', size: '1024x1024', count: 1 },
  result: { data: [{ url: `/api/v1/image-workbench/tasks/${taskId}/images/0`, size_bytes: 64 }] },
}

let wrapper: VueWrapper | undefined
function mountRow() {
  wrapper = mount(ImageWorkbenchTaskRow, {
    props: { task, imageIndex: 0, now: 120, downloading: false },
    global: { stubs: { Icon: true } },
  })
  return wrapper
}

beforeEach(() => {
  vi.resetAllMocks()
  vi.stubGlobal('URL', class extends URL {
    static createObjectURL = vi.fn(() => 'blob:http://localhost/local-image')
    static revokeObjectURL = vi.fn()
  })
  downloadImage.mockResolvedValue(new Blob(['image'], { type: 'image/png' }))
})
afterEach(() => {
  wrapper?.unmount()
  wrapper = undefined
  vi.unstubAllGlobals()
})

describe('local workbench images', () => {
  it('loads the preview with authentication and revokes its URL on expiry', async () => {
    const row = mountRow()
    await flushPromises()
    expect(downloadImage).toHaveBeenCalledWith(taskId, 0, expect.any(AbortSignal))
    expect(row.get('img').attributes('src')).toBe('blob:http://localhost/local-image')
    expect(row.get('a.task-preview').attributes('href')).toBe('blob:http://localhost/local-image')
    await row.get('.download-action').trigger('click')
    expect(row.emitted('download')).toHaveLength(1)
    await row.setProps({ now: 1011 })
    expect(URL.revokeObjectURL).toHaveBeenCalledWith('blob:http://localhost/local-image')
    expect(row.find('img').exists()).toBe(false)
    expect(row.get('.download-action').attributes('disabled')).toBeDefined()
  })

  it('can retry a failed authenticated preview', async () => {
    downloadImage.mockRejectedValueOnce(new Error('network unavailable'))
    const row = mountRow()
    await flushPromises()
    expect(row.text()).toContain('imageWorkbench.imageLoadError')
    await row.get('.retry-image').trigger('click')
    await flushPromises()
    expect(downloadImage).toHaveBeenCalledTimes(2)
    expect(row.get('img').attributes('src')).toContain('blob:')
  })

  it('aborts the download when the row is removed and ignores a late response', async () => {
    let resolve!: (blob: Blob) => void
    downloadImage.mockReturnValue(new Promise<Blob>(done => { resolve = done }))
    const row = mountRow()
    const signal = downloadImage.mock.calls[0][2] as AbortSignal
    row.unmount()
    wrapper = undefined
    expect(signal.aborted).toBe(true)
    resolve(new Blob(['image'], { type: 'image/png' }))
    await flushPromises()
    expect(URL.createObjectURL).not.toHaveBeenCalled()
  })
})
