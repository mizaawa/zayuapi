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
    t: (key: string, params?: Record<string, unknown>) => key === 'imageWorkbench.referenceCount' ? `${key}:${params?.count}/${params?.max}`
      : key === 'imageWorkbench.submitPartial' ? `${key}:${params?.created}/${params?.failed}`
      : params?.count !== undefined ? `${key}:${params.count}`
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
  return { enabled: true, max_concurrent: 5, max_image_size_mb: 50, max_reference_images: 6, retention_seconds: 900, tasks }
}

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (error: unknown) => void
  const promise = new Promise<T>((res, rej) => { resolve = res; reject = rej })
  return { promise, resolve, reject }
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

  it.each([undefined, 0, -1, 1.5])('defaults an absent or invalid reference limit to six: %s', async (limit) => {
    stubImageReading()
    listTasks.mockResolvedValue({ ...tasksResponse(), max_reference_images: limit })
    submitTask.mockResolvedValue(task('six-references'))
    const wrapper = mountWorkbench()
    await flushPromises()
    for (let index = 0; index < 7; index++) await addReference(wrapper, 5)
    expect(wrapper.findAll('.reference-image')).toHaveLength(6)
    expect(wrapper.get('.reference-counter').text()).toBe('imageWorkbench.referenceCount:6/6')
    expect(wrapper.find('.reference-add').exists()).toBe(false)
    expect(showError).toHaveBeenCalledWith('imageWorkbench.referencesLimit:6')
    await wrapper.get('#image-prompt').setValue('Six reference images')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(submitTask).toHaveBeenCalledTimes(1)
    expect(submitTask.mock.calls[0][2].images).toHaveLength(6)
  })

  it.each(['upload', 'paste', 'drop'])('enforces the configured reference count for %s', async (method) => {
    stubImageReading()
    listTasks.mockResolvedValue({ ...tasksResponse(), max_reference_images: 2 })
    const wrapper = mountWorkbench()
    await flushPromises()
    const files = Array.from({ length: 3 }, (_, index) => new File(['image'], `reference-${index}.png`, { type: 'image/png' }))
    if (method === 'upload') {
      const input = wrapper.get('input[type="file"]')
      Object.defineProperty(input.element, 'files', { value: files, configurable: true })
      await input.trigger('change')
    } else if (method === 'drop') {
      await wrapper.get('.reference-strip').trigger('drop', { dataTransfer: { files } })
    } else {
      await wrapper.get('.image-workbench').trigger('paste', {
        clipboardData: { items: files.map(file => ({ kind: 'file', type: file.type, getAsFile: () => file })) },
      })
    }
    await flushPromises()
    expect(wrapper.findAll('.reference-image')).toHaveLength(2)
    expect(showError).toHaveBeenCalledWith('imageWorkbench.referencesLimit:2')
  })

  it('updates reference controls live and blocks excess selected references on submit', async () => {
    stubImageReading()
    const wrapper = mountWorkbench()
    await flushPromises()
    for (let index = 0; index < 3; index++) await addReference(wrapper, 5)
    await wrapper.get('#image-prompt').setValue('A lighthouse')
    listTasks.mockResolvedValue({ ...tasksResponse(), max_reference_images: 2 })
    await vi.advanceTimersByTimeAsync(5000)
    await flushPromises()
    expect(wrapper.findAll('.reference-image')).toHaveLength(3)
    expect(wrapper.get('.reference-counter').text()).toBe('imageWorkbench.referenceCount:3/2')
    expect(wrapper.get('.validation-error').text()).toBe('imageWorkbench.referencesLimit:2')
    expect(wrapper.get<HTMLButtonElement>('.create-task').element.disabled).toBe(true)
    await wrapper.get('form').trigger('submit')
    expect(submitTask).not.toHaveBeenCalled()
    expect(wrapper.find('#guide-tab').exists()).toBe(false)
    await wrapper.get('#workbench-tab').trigger('click')
    await wrapper.get('.reference-image button').trigger('click')
    expect(wrapper.get<HTMLButtonElement>('.create-task').element.disabled).toBe(false)
    expect(wrapper.find('.validation-error').exists()).toBe(false)

    listTasks.mockResolvedValue({ ...tasksResponse(), max_reference_images: 8 })
    await vi.advanceTimersByTimeAsync(5000)
    await flushPromises()
    for (let index = 0; index < 6; index++) await addReference(wrapper, 5)
    expect(wrapper.findAll('.reference-image')).toHaveLength(8)
    expect(wrapper.get('.reference-counter').text()).toBe('imageWorkbench.referenceCount:8/8')
    expect(wrapper.find('.reference-add').exists()).toBe(false)
  })

  it('rechecks the reference limit after an in-progress image read completes', async () => {
    stubImageReading()
    const wrapper = mountWorkbench()
    await flushPromises()
    await addReference(wrapper, 5)
    let finishRead: (() => void) | undefined
    vi.stubGlobal('FileReader', class {
      result = 'data:image/png;base64,aW1hZ2U='
      onload?: () => void
      readAsDataURL() { finishRead = () => this.onload?.() }
    })
    await addReference(wrapper, 5)
    listTasks.mockResolvedValue({ ...tasksResponse(), max_reference_images: 1 })
    await vi.advanceTimersByTimeAsync(5000)
    await flushPromises()
    finishRead?.()
    await flushPromises()
    expect(wrapper.findAll('.reference-image')).toHaveLength(1)
    expect(showError).toHaveBeenCalledWith('imageWorkbench.referencesLimit:1')
  })

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
    expect(wrapper.find('#guide-tab').exists()).toBe(false)

    listTasks.mockResolvedValue({ ...tasksResponse(), max_image_size_mb: 20 })
    await vi.advanceTimersByTimeAsync(5000)
    await flushPromises()
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
        { ...keys[0], id: 11, name: 'Custom key', group: { ...keys[0].group, platform: 'custom' } },
      ], pages: 2,
    })
    const wrapper = mountWorkbench()
    await flushPromises()
    expect(wrapper.get('#image-key').findAll('option').map(option => option.text()))
      .toEqual(['', 'Secondary key', 'Grok key', 'Composite key', 'Custom key'])
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

  it('dispatches three independent requests concurrently from one snapshot and retains every accepted task', async () => {
    stubImageReading()
    const pending = Array.from({ length: 3 }, () => deferred<ImageTask>())
    pending.forEach(request => submitTask.mockReturnValueOnce(request.promise))
    const wrapper = mountWorkbench()
    await flushPromises()
    await addReference(wrapper, 5)
    await wrapper.get('#image-prompt').setValue('  A coastal lighthouse  ')
    await clickButton(wrapper, 'imageWorkbench.high')
    await clickButton(wrapper, '16:9')
    await clickButton(wrapper, '2K')
    await clickButton(wrapper, 'imageWorkbench.imageCount:3')
    await wrapper.get('form').trigger('submit')
    await wrapper.get('form').trigger('submit')

    expect(submitTask).toHaveBeenCalledTimes(3)
    for (const request of submitTask.mock.calls) {
      expect(request).toEqual(['sk-studio', 'owner@example.com', {
        prompt: 'A coastal lighthouse', model: 'gpt-image-2', quality: 'high', size: '2048x1152', n: 1,
        images: [{ image_url: 'data:image/png;base64,aW1hZ2U=' }],
      }])
    }
    expect(wrapper.get<HTMLButtonElement>('.create-task').element.disabled).toBe(true)
    await wrapper.get('#image-prompt').setValue('Changed prompt')
    await clickButton(wrapper, 'imageWorkbench.low')
    await clickButton(wrapper, 'imageWorkbench.imageCount:1')
    await wrapper.get('.reference-image button').trigger('click')
    pending[1]!.resolve(task('created-2', 'queued'))
    await flushPromises()
    expect(wrapper.get('#workbench-tab').attributes('aria-selected')).toBe('true')
    await wrapper.get('#tasks-tab').trigger('click')
    expect(wrapper.find('[data-task-id="created-2"]').exists()).toBe(true)
    await vi.advanceTimersByTimeAsync(5000)
    expect(wrapper.find('[data-task-id="created-2"]').exists()).toBe(true)
    pending[0]!.resolve(task('created-1', 'queued'))
    pending[2]!.resolve(task('created-3', 'queued'))
    await flushPromises()
    expect(wrapper.findAll('[data-task-id]').map(row => row.attributes('data-task-id')).sort())
      .toEqual(['created-1', 'created-2', 'created-3'])
    expect(submitTask.mock.calls[2]![2].prompt).toBe('A coastal lighthouse')
    expect(submitTask.mock.calls[2]![2].images).toHaveLength(1)
    expect(showSuccess).toHaveBeenCalledWith('imageWorkbench.createdMany:3')
    expect(showError).not.toHaveBeenCalled()
    expect(wrapper.get<HTMLTextAreaElement>('#image-prompt').element.value).toBe('Changed prompt')
  })

  it('accepts new queued tasks while all execution slots are occupied', async () => {
    const active = Array.from({ length: 5 }, (_, index) => task(`active-${index}`))
    listTasks.mockResolvedValue(tasksResponse(active))
    const wrapper = mountWorkbench()
    await flushPromises()
    await wrapper.get('#image-prompt').setValue('A coastal lighthouse')
    submitTask.mockResolvedValue(task('waiting', 'queued'))
    expect(wrapper.get<HTMLButtonElement>('.create-task').element.disabled).toBe(false)
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(submitTask).toHaveBeenCalledTimes(1)
    expect(wrapper.get('#workbench-tab').attributes('aria-selected')).toBe('true')
    expect(wrapper.get<HTMLButtonElement>('.create-task').element.disabled).toBe(true)
    await wrapper.get('#tasks-tab').trigger('click')
    expect(wrapper.find('[data-task-id="waiting"] .status-queued').text()).toBe('imageWorkbench.queued')
    expect(wrapper.findAll('[data-task-id="waiting"] .task-details dd').map(item => item.text())).toContain('-')
    expect(wrapper.find('[data-task-id="waiting"] button[aria-label="imageWorkbench.delete"]').exists()).toBe(true)
    await wrapper.get('#workbench-tab').trigger('click')
    await wrapper.get('#image-prompt').setValue('Another lighthouse')
    expect(wrapper.get<HTMLButtonElement>('.create-task').element.disabled).toBe(false)
  })

  it('keeps accepted tasks after partial failure and reconciles an ambiguous response without retrying POSTs', async () => {
    const created = [task('accepted-1', 'queued'), task('response-lost', 'queued'), task('accepted-3', 'queued')]
    submitTask.mockResolvedValueOnce(created[0]).mockRejectedValueOnce(new Error('Network failure')).mockResolvedValueOnce(created[2])
    listTasks.mockResolvedValueOnce(tasksResponse()).mockResolvedValue(tasksResponse(created))
    const wrapper = mountWorkbench()
    await flushPromises()
    await wrapper.get('#image-prompt').setValue('A lighthouse')
    await clickButton(wrapper, 'imageWorkbench.imageCount:3')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(wrapper.get('#workbench-tab').attributes('aria-selected')).toBe('true')
    expect(wrapper.get<HTMLTextAreaElement>('#image-prompt').element.value).toBe('')
    await wrapper.get('#tasks-tab').trigger('click')
    expect(wrapper.findAll('[data-task-id]')).toHaveLength(3)
    expect(showError).toHaveBeenCalledWith('imageWorkbench.submitPartial:2/1')
    expect(showSuccess).not.toHaveBeenCalled()
    await vi.advanceTimersByTimeAsync(6000)
    expect(submitTask).toHaveBeenCalledTimes(3)
  })

  it('clears a successfully submitted draft and its references without switching tabs', async () => {
    stubImageReading()
    submitTask.mockResolvedValue(task('accepted', 'queued'))
    const wrapper = mountWorkbench()
    await flushPromises()
    await addReference(wrapper, 5)
    await wrapper.get('#image-prompt').setValue('A lighthouse')
    await clickButton(wrapper, 'imageWorkbench.high')
    await clickButton(wrapper, '16:9')
    await clickButton(wrapper, '2K')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(wrapper.get('#workbench-tab').attributes('aria-selected')).toBe('true')
    expect(wrapper.get<HTMLTextAreaElement>('#image-prompt').element.value).toBe('')
    expect(wrapper.findAll('.reference-image')).toHaveLength(0)
    expect(URL.revokeObjectURL).toHaveBeenCalledWith('blob:reference')
    expect(wrapper.get<HTMLSelectElement>('#image-key').element.value).toBe('1')
    expect(wrapper.get<HTMLSelectElement>('#image-model').element.value).toBe('gpt-image-2')
    await wrapper.get('#image-prompt').setValue('Next image')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(submitTask).toHaveBeenLastCalledWith('sk-studio', 'owner@example.com', {
      prompt: 'Next image', model: 'gpt-image-2', quality: 'auto', size: '1024x1024', n: 1,
    })
  })

  it('preserves the full draft when every submission fails', async () => {
    stubImageReading()
    submitTask.mockRejectedValue(new Error('Unavailable'))
    const wrapper = mountWorkbench()
    await flushPromises()
    await addReference(wrapper, 5)
    await wrapper.get('#image-prompt').setValue('Keep this prompt')
    await clickButton(wrapper, 'imageWorkbench.imageCount:3')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(submitTask).toHaveBeenCalledTimes(3)
    expect(wrapper.get('#workbench-tab').attributes('aria-selected')).toBe('true')
    expect(wrapper.get<HTMLTextAreaElement>('#image-prompt').element.value).toBe('Keep this prompt')
    expect(wrapper.findAll('.reference-image')).toHaveLength(1)
    expect(URL.revokeObjectURL).not.toHaveBeenCalled()
    expect(showError).toHaveBeenCalledWith('imageWorkbench.submitPartial:0/3')
  })

  it('paginates by twenty, resets filters, and clamps the page after deletion or expiry', async () => {
    const items = Array.from({ length: 41 }, (_, index) => task(`page-${index}`, index < 21 ? 'completed' : 'failed'))
    listTasks.mockResolvedValue(tasksResponse(items))
    const wrapper = mountWorkbench()
    await flushPromises()
    await wrapper.get('#tasks-tab').trigger('click')
    expect(wrapper.findAll('[data-task-id]')).toHaveLength(20)
    expect(wrapper.find('.task-pagination select').exists()).toBe(false)
    await wrapper.get('button[aria-label="pagination.next"]').trigger('click')
    expect(wrapper.findAll('[data-task-id]')).toHaveLength(20)
    expect(wrapper.find('[data-task-id="page-20"]').exists()).toBe(true)
    await wrapper.get('button[aria-label="pagination.next"]').trigger('click')
    expect(wrapper.findAll('[data-task-id]')).toHaveLength(1)
    expect(wrapper.get<HTMLButtonElement>('button[aria-label="pagination.next"]').element.disabled).toBe(true)
    await wrapper.get('button[aria-label="pagination.previous"]').trigger('click')
    expect(wrapper.findAll('[data-task-id]')).toHaveLength(20)
    await wrapper.findAll('.status-filter button')[3]!.trigger('click')
    expect(wrapper.findAll('[data-task-id]')).toHaveLength(20)
    expect(wrapper.get<HTMLButtonElement>('button[aria-label="pagination.previous"]').element.disabled).toBe(true)
    await wrapper.get('button[aria-label="pagination.next"]').trigger('click')
    expect(wrapper.findAll('[data-task-id]')).toHaveLength(1)
    await wrapper.get('button[aria-label="imageWorkbench.delete"]').trigger('click')
    await wrapper.get('[role="dialog"] .btn-danger').trigger('click')
    await flushPromises()
    expect(wrapper.findAll('[data-task-id]')).toHaveLength(20)
    expect(wrapper.get<HTMLButtonElement>('button[aria-label="pagination.previous"]').element.disabled).toBe(true)
    listTasks.mockResolvedValue(tasksResponse(items.filter(item => item.id !== 'page-20')))
    await wrapper.findAll('.status-filter button')[0]!.trigger('click')
    await wrapper.get('button[aria-label="pagination.next"]').trigger('click')
    vi.setSystemTime((now + 901) * 1000)
    await vi.advanceTimersByTimeAsync(1000)
    expect(wrapper.findAll('[data-task-id]')).toHaveLength(0)
    expect(wrapper.find('.task-pagination').exists()).toBe(false)
  })

  it('shows a full refresh animation for fast requests and avoids overlapping manual refreshes', async () => {
    const wrapper = mountWorkbench()
    await flushPromises()
    await wrapper.get('#tasks-tab').trigger('click')
    const refresh = wrapper.get('.toolbar-refresh')
    await refresh.trigger('click')
    await flushPromises()
    expect(refresh.find('.animate-spin').exists()).toBe(true)
    await refresh.trigger('click')
    expect(listTasks).toHaveBeenCalledTimes(2)
    await vi.advanceTimersByTimeAsync(1000)
    expect(refresh.find('.animate-spin').exists()).toBe(false)
    expect((refresh.element as HTMLButtonElement).disabled).toBe(false)
  })

  it('does not abort dispatched submissions on navigation and recovers accepted tasks on return', async () => {
    const pending = Array.from({ length: 3 }, () => deferred<ImageTask>())
    pending.forEach(request => submitTask.mockReturnValueOnce(request.promise))
    const wrapper = mountWorkbench()
    await flushPromises()
    await wrapper.get('#image-prompt').setValue('A lighthouse')
    await clickButton(wrapper, 'imageWorkbench.imageCount:3')
    await wrapper.get('form').trigger('submit')
    wrapper.unmount()
    const created = pending.map((request, index) => {
      const item = task(`accepted-${index}`, 'queued')
      request.resolve(item)
      return item
    })
    await flushPromises()
    expect(submitTask.mock.calls.every(request => request[3] === undefined)).toBe(true)
    expect(showSuccess).not.toHaveBeenCalled()
    expect(showError).not.toHaveBeenCalled()
    listTasks.mockResolvedValue(tasksResponse(created))
    const recovered = mountWorkbench()
    await flushPromises()
    await recovered.get('#tasks-tab').trigger('click')
    expect(recovered.findAll('[data-task-id]')).toHaveLength(3)
    expect(submitTask).toHaveBeenCalledTimes(3)
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
