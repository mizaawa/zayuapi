import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { defineComponent, h, ref, type PropType } from 'vue'
import ImageWorkbenchView from '../ImageWorkbenchView.vue'
import type { ImageModel, ImageTask, WorkbenchTasksResponse } from '@/api/imageWorkbench'

const {
  listKeys, listModels, listTasks, submitTask, deleteTask, downloadImage, saveBlob, showError, showSuccess,
} = vi.hoisted(() => ({
  listKeys: vi.fn(), listModels: vi.fn(), listTasks: vi.fn(), submitTask: vi.fn(),
  deleteTask: vi.fn(), downloadImage: vi.fn(), saveBlob: vi.fn(), showError: vi.fn(), showSuccess: vi.fn(),
}))

vi.mock('@/api/keys', () => ({ keysAPI: { list: listKeys } }))
vi.mock('@/api/imageWorkbench', () => ({
  listImageModels: listModels,
  listWorkbenchTasks: listTasks,
  submitImageTask: submitTask,
  deleteWorkbenchTask: deleteTask,
  downloadWorkbenchImage: downloadImage,
}))
vi.mock('@/api/batchImage', () => ({ saveBlob }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => ({ user: { email: 'owner@example.com' } }) }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showError, showSuccess }) }))
vi.mock('@/composables/useClipboard', () => ({ useClipboard: () => ({ copyToClipboard: vi.fn() }) }))
vi.mock('vue-i18n', async () => ({
  ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'),
  useI18n: () => ({
    locale: ref('en'),
    t: (key: string, params?: Record<string, unknown>) => params?.count !== undefined ? `${key}:${params.count}`
      : params?.max !== undefined ? `${key}:${params.max}` : key,
  }),
}))

const now = Math.floor(new Date('2026-10-10T12:00:00Z').getTime() / 1000)
const keys = [
  { id: 1, name: 'Studio key', key: 'sk-studio', status: 'active', group: { platform: 'openai', allow_image_generation: true, image_workbench_enabled: true } },
  { id: 2, name: 'Secondary key', key: 'sk-secondary', status: 'active', group: { platform: 'openai', allow_image_generation: true, image_workbench_enabled: true } },
]

function task(id: string, status: ImageTask['status'] = 'processing'): ImageTask {
  return {
    id, task_id: id, status, created_at: now - 10, expires_at: now + 900,
    workbench: { api_key_id: 1, prompt: 'A coastal lighthouse', model: 'gpt-image-2', quality: 'auto', size: '1024x1024', count: 1 },
    ...(status === 'completed' ? { completed_at: now, result: { data: [{ url: 'https://cdn.example.com/result.png', size_bytes: 1024 }] } } : {}),
  }
}

function tasksResponse(tasks: ImageTask[] = []): WorkbenchTasksResponse {
  return { enabled: true, max_concurrent: 5, max_image_size_mb: 50, retention_seconds: 900, tasks }
}

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>(res => { resolve = res })
  return { promise, resolve }
}

const selectStub = defineComponent({
  props: {
    modelValue: { type: [String, Number], default: null },
    options: { type: Array as PropType<Array<{ label: string; value: string | number }>>, default: () => [] },
    disabled: Boolean,
  },
  emits: ['update:modelValue'],
  setup(props, { emit }) {
    return () => h('select', {
      value: props.modelValue ?? '',
      disabled: props.disabled,
      onChange: (event: Event) => {
        const option = props.options.find(item => String(item.value) === (event.target as HTMLSelectElement).value)
        emit('update:modelValue', option?.value ?? null)
      },
    }, [h('option', { value: '' }, ''), ...props.options.map(option => h('option', { value: option.value }, option.label))])
  },
})

const wrappers: VueWrapper[] = []
function mountWorkbench() {
  const wrapper = mount(ImageWorkbenchView, {
    global: {
      stubs: {
        AppLayout: { template: '<main><slot /></main>' },
        Icon: true,
        Select: selectStub,
        BaseDialog: { props: ['show'], template: '<div v-if="show" role="dialog"><slot /><slot name="footer" /></div>' },
        RouterLink: { template: '<a><slot /></a>' },
      },
    },
  })
  wrappers.push(wrapper)
  return wrapper
}

async function clickButton(wrapper: VueWrapper, label: string) {
  const button = wrapper.findAll('button').find(item => item.text() === label)
  expect(button, `Expected a button labeled ${label}`).toBeDefined()
  await button!.trigger('click')
}

beforeEach(() => {
  vi.resetAllMocks()
  vi.useFakeTimers()
  vi.setSystemTime(now * 1000)
  listKeys.mockResolvedValue({ items: keys, pages: 1 })
  listModels.mockResolvedValue([{ id: 'gpt-image-2' }])
  listTasks.mockResolvedValue(tasksResponse())
})

afterEach(() => {
  wrappers.splice(0).forEach(wrapper => wrapper.unmount())
  vi.clearAllTimers()
  vi.useRealTimers()
  vi.unstubAllGlobals()
})

describe('ImageWorkbenchView', () => {
  function stubImageReading() {
    vi.stubGlobal('FileReader', class {
      result = 'data:image/png;base64,aW1hZ2U='
      onload?: () => void
      readAsDataURL() { this.onload?.() }
    })
    vi.stubGlobal('Image', class {
      naturalWidth = 1
      onload?: () => void
      set src(_value: string) { this.onload?.() }
    })
    vi.stubGlobal('URL', class extends URL {
      static createObjectURL = vi.fn(() => 'blob:reference')
      static revokeObjectURL = vi.fn()
    })
  }

  async function addReference(wrapper: VueWrapper, size: number) {
    const file = new File(['image'], 'reference.png', { type: 'image/png' })
    Object.defineProperty(file, 'size', { value: size })
    const input = wrapper.get('input[type="file"]')
    Object.defineProperty(input.element, 'files', { value: [file], configurable: true })
    await input.trigger('change')
    await flushPromises()
  }

  it('accepts the 50 MB boundary and rejects an image one byte above it', async () => {
    stubImageReading()
    const wrapper = mountWorkbench()
    await flushPromises()
    await addReference(wrapper, 50 * 1024 * 1024)
    expect(wrapper.findAll('button[aria-label="imageWorkbench.removeReference"]')).toHaveLength(1)
    expect(showError).not.toHaveBeenCalled()
    await addReference(wrapper, 50 * 1024 * 1024 + 1)
    expect(wrapper.findAll('button[aria-label="imageWorkbench.removeReference"]')).toHaveLength(1)
    expect(showError).toHaveBeenCalledWith('imageWorkbench.referenceSize:50')
  })

  it('accepts large references when unlimited and rechecks them when the setting changes', async () => {
    stubImageReading()
    listTasks.mockResolvedValue({ ...tasksResponse(), max_image_size_mb: 0 })
    const wrapper = mountWorkbench()
    await flushPromises()
    await addReference(wrapper, 51 * 1024 * 1024)
    expect(wrapper.findAll('button[aria-label="imageWorkbench.removeReference"]')).toHaveLength(1)
    expect(showError).not.toHaveBeenCalled()
    await wrapper.get('#guide-tab').trigger('click')
    expect(wrapper.text()).toContain('imageWorkbench.guideFormatsUnlimited')

    listTasks.mockResolvedValue({ ...tasksResponse(), max_image_size_mb: 20 })
    await vi.advanceTimersByTimeAsync(5000)
    await flushPromises()
    expect(wrapper.text()).toContain('imageWorkbench.guideFormats:20')
    await wrapper.get('#workbench-tab').trigger('click')
    await wrapper.get('#image-prompt').setValue('A lighthouse')
    await wrapper.get('form').trigger('submit')
    expect(submitTask).not.toHaveBeenCalled()
    expect(showError).toHaveBeenCalledWith('imageWorkbench.referenceSize:20')
  })

  it('lists only eligible keys from explicitly declared groups across pages', async () => {
    listKeys.mockResolvedValueOnce({
      items: [
        { ...keys[0], group: { ...keys[0].group, image_workbench_enabled: false } },
        { ...keys[0], id: 3, group: { platform: 'openai', allow_image_generation: true } },
        { ...keys[0], id: 4, group: undefined },
        { ...keys[0], id: 5, status: 'disabled' },
        { ...keys[0], id: 6, expires_at: new Date((now - 1) * 1000).toISOString() },
        { ...keys[0], id: 7, group: { ...keys[0].group, allow_image_generation: false } },
        { ...keys[0], id: 8, group: { ...keys[0].group, platform: 'gemini' } },
      ], pages: 2,
    }).mockResolvedValueOnce({
      items: [keys[1],
        { ...keys[0], id: 9, name: 'Grok key', group: { ...keys[0].group, platform: 'grok' } },
        { ...keys[0], id: 10, name: 'Composite key', group: { ...keys[0].group, platform: 'composite' } },
      ], pages: 2,
    })
    const wrapper = mountWorkbench()
    await flushPromises()
    expect(wrapper.get('#image-key').findAll('option').map(option => option.text()))
      .toEqual(['', 'Secondary key', 'Grok key', 'Composite key'])
    expect(listKeys).toHaveBeenCalledTimes(2)
    expect(listModels).toHaveBeenCalledWith('sk-secondary', 'owner@example.com', expect.any(AbortSignal))
  })

  it('cannot submit when no group has declared workbench support', async () => {
    listKeys.mockResolvedValue({
      items: keys.map(key => ({ ...key, group: { ...key.group, image_workbench_enabled: false } })), pages: 1,
    })
    const wrapper = mountWorkbench()
    await flushPromises()
    expect(wrapper.get('.inline-state').text()).toContain('imageWorkbench.noKeys')
    expect(listModels).not.toHaveBeenCalled()
    await wrapper.get('#image-prompt').setValue('A lighthouse')
    await wrapper.get('form').trigger('submit')
    expect(submitTask).not.toHaveBeenCalled()
    expect(wrapper.get<HTMLButtonElement>('.create-task').element.disabled).toBe(true)
  })

  it('ignores a stale model response after the selected API key changes', async () => {
    const initial = deferred<ImageModel[]>()
    const secondary = deferred<ImageModel[]>()
    listModels.mockReturnValueOnce(initial.promise).mockReturnValueOnce(secondary.promise)
    const wrapper = mountWorkbench()
    await flushPromises()
    expect(listModels).toHaveBeenCalledTimes(1)
    const oldSignal = listModels.mock.calls[0][2] as AbortSignal

    await wrapper.get('#image-key').setValue('2')
    expect(oldSignal.aborted).toBe(true)
    secondary.resolve([{ id: 'gpt-image-1.5' }])
    await flushPromises()
    initial.resolve([{ id: 'gpt-image-2' }])
    await flushPromises()

    const modelSelect = wrapper.get<HTMLSelectElement>('#image-model')
    expect(modelSelect.element.value).toBe('gpt-image-1.5')
    expect(modelSelect.text()).not.toContain('gpt-image-2')
    expect(listModels.mock.calls[1].slice(0, 2)).toEqual(['sk-secondary', 'owner@example.com'])
    expect(showError).not.toHaveBeenCalled()
  })

  it('submits the selected parameters once and switches to the task list when accepted', async () => {
    const pending = deferred<ImageTask>()
    const created = task('created-task')
    submitTask.mockReturnValue(pending.promise)
    listTasks.mockResolvedValueOnce(tasksResponse()).mockResolvedValue(tasksResponse([created]))
    const wrapper = mountWorkbench()
    await flushPromises()
    await wrapper.get('#image-prompt').setValue('  A coastal lighthouse  ')
    await clickButton(wrapper, 'imageWorkbench.high')
    await clickButton(wrapper, '16:9')
    await clickButton(wrapper, '2K')
    await clickButton(wrapper, 'imageWorkbench.imageCount:3')
    await wrapper.get('form').trigger('submit')
    await wrapper.get('form').trigger('submit')

    expect(submitTask).toHaveBeenCalledTimes(1)
    expect(submitTask).toHaveBeenCalledWith('sk-studio', 'owner@example.com', {
      prompt: 'A coastal lighthouse', model: 'gpt-image-2', quality: 'high', size: '2048x1152', n: 3,
    }, expect.any(AbortSignal))
    expect(wrapper.get<HTMLButtonElement>('.create-task').element.disabled).toBe(true)

    pending.resolve(created)
    await flushPromises()
    expect(wrapper.get('#tasks-tab').attributes('aria-selected')).toBe('true')
    expect(wrapper.find('[data-task-id="created-task"]').exists()).toBe(true)
    expect(showSuccess).toHaveBeenCalledWith('imageWorkbench.created')
  })

  it('prevents a sixth active task and re-enables submission after one completes', async () => {
    const active = Array.from({ length: 5 }, (_, index) => task(`active-${index}`))
    listTasks.mockResolvedValue(tasksResponse(active))
    const wrapper = mountWorkbench()
    await flushPromises()
    await wrapper.get('#image-prompt').setValue('A coastal lighthouse')
    expect(wrapper.get<HTMLButtonElement>('.create-task').element.disabled).toBe(true)
    await wrapper.get('form').trigger('submit')
    expect(submitTask).not.toHaveBeenCalled()

    listTasks.mockResolvedValue(tasksResponse([...active.slice(1), task('completed', 'completed')]))
    await vi.advanceTimersByTimeAsync(3000)
    await flushPromises()
    expect(wrapper.get<HTMLButtonElement>('.create-task').element.disabled).toBe(false)
  })

  it('rejects decimal custom ratios before sending a generation request', async () => {
    const wrapper = mountWorkbench()
    await flushPromises()
    await wrapper.get('#image-prompt').setValue('A coastal lighthouse')
    await clickButton(wrapper, 'imageWorkbench.custom')
    await wrapper.get('#custom-width').setValue('3.5')
    await wrapper.get('#custom-height').setValue('2')
    expect(wrapper.get('.validation-error').text()).toBe('imageWorkbench.invalidRatio')
    expect(wrapper.get<HTMLButtonElement>('.create-task').element.disabled).toBe(true)
    await wrapper.get('form').trigger('submit')
    expect(submitTask).not.toHaveBeenCalled()

    await wrapper.get('#custom-width').setValue('3')
    expect(wrapper.find('.validation-error').exists()).toBe(false)
    expect(wrapper.get<HTMLButtonElement>('.create-task').element.disabled).toBe(false)
  })

  it('recovers from a key loading failure through the visible retry action', async () => {
    listKeys.mockRejectedValueOnce(new Error('Temporary failure'))
    const wrapper = mountWorkbench()
    await flushPromises()
    expect(wrapper.get('.inline-state').text()).toContain('imageWorkbench.keysError')
    await wrapper.get('.inline-state button').trigger('click')
    await flushPromises()
    expect(listKeys).toHaveBeenCalledTimes(2)
    expect(wrapper.get<HTMLSelectElement>('#image-model').element.value).toBe('gpt-image-2')
    expect(wrapper.find('.inline-state').exists()).toBe(false)
  })

  it('disables creation after a task loading failure and recovers through retry', async () => {
    listTasks.mockRejectedValueOnce(new Error('Temporary failure'))
    const wrapper = mountWorkbench()
    await flushPromises()
    await wrapper.get('#image-prompt').setValue('A coastal lighthouse')
    expect(wrapper.get('[role="alert"]').text()).toContain('imageWorkbench.tasksError')
    expect(wrapper.get<HTMLButtonElement>('.create-task').element.disabled).toBe(true)
    await wrapper.get('[role="alert"] button').trigger('click')
    await flushPromises()
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    expect(wrapper.get<HTMLButtonElement>('.create-task').element.disabled).toBe(false)
  })

  it('retains the row after a failed deletion and removes it after confirmation succeeds', async () => {
    listTasks.mockResolvedValue(tasksResponse([task('completed-task', 'completed')]))
    deleteTask.mockRejectedValueOnce(new Error('Delete failed')).mockResolvedValueOnce(undefined)
    const wrapper = mountWorkbench()
    await flushPromises()
    await wrapper.get('#tasks-tab').trigger('click')
    await wrapper.get('button[aria-label="imageWorkbench.delete"]').trigger('click')
    expect(deleteTask).not.toHaveBeenCalled()
    await wrapper.get('[role="dialog"] .btn-danger').trigger('click')
    await flushPromises()
    expect(showError).toHaveBeenCalledWith('Delete failed')
    expect(wrapper.find('[data-task-id="completed-task"]').exists()).toBe(true)

    await wrapper.get('[role="dialog"] .btn-danger').trigger('click')
    await flushPromises()
    expect(deleteTask).toHaveBeenLastCalledWith('completed-task')
    expect(wrapper.find('[data-task-id="completed-task"]').exists()).toBe(false)
    expect(wrapper.find('[role="dialog"]').exists()).toBe(false)
  })

  it('downloads a completed image through the authenticated endpoint and saves a stable filename', async () => {
    listTasks.mockResolvedValue(tasksResponse([task('completed-task', 'completed')]))
    const image = new Blob(['png-bytes'], { type: 'image/png' })
    downloadImage.mockResolvedValue(image)
    const wrapper = mountWorkbench()
    await flushPromises()
    await wrapper.get('#tasks-tab').trigger('click')
    await wrapper.get('.download-action').trigger('click')
    await flushPromises()
    expect(downloadImage).toHaveBeenCalledWith('completed-task', 0, expect.any(AbortSignal))
    expect(saveBlob).toHaveBeenCalledWith(image, expect.stringMatching(/^20261010-[a-z0-9]{6}\.png$/))
  })
})
