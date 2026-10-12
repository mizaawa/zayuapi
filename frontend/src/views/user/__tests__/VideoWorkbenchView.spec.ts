import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { defineComponent, h, ref } from 'vue'
import VideoWorkbenchView from '../VideoWorkbenchView.vue'
import type { VideoTask, VideoWorkbenchSettings } from '@/api/videoWorkbench'
import { ImageWorkbenchError } from '@/api/imageWorkbench'

const mocks = vi.hoisted(() => ({ keys: vi.fn(), models: vi.fn(), list: vi.fn(), submit: vi.fn(), download: vi.fn(), delete: vi.fn(), error: vi.fn(), success: vi.fn(), save: vi.fn() }))
vi.mock('@/api/keys', () => ({ keysAPI: { list: mocks.keys } }))
vi.mock('@/api/videoWorkbench', async () => ({ ...await vi.importActual<typeof import('@/api/videoWorkbench')>('@/api/videoWorkbench'), listVideoTasks: mocks.list, listVideoModels: mocks.models, submitVideoTask: mocks.submit, downloadVideoTask: mocks.download, deleteVideoTask: mocks.delete }))
vi.mock('@/api/batchImage', () => ({ saveBlob: mocks.save }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => ({ user: { email: 'owner@example.test' } }) }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showError: mocks.error, showSuccess: mocks.success, sidebarCollapsed: false }) }))
vi.mock('@/composables/useClipboard', () => ({ useClipboard: () => ({ copyToClipboard: vi.fn() }) }))
vi.mock('vue-i18n', async () => ({ ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'), useI18n: () => ({ locale: ref('en'), t: (key: string, params?: Record<string, unknown>) => params?.minutes !== undefined ? `${key}:${params.minutes}` : key }) }))

const settings: VideoWorkbenchSettings = { enabled: true, max_concurrent: 3, max_reference_images: 6, max_reference_videos: 6, max_reference_audios: 6, retention_minutes: 30, tutorial_url: '', documentation_url: '', announcement: '' }
const key = { id: 1, name: 'Video key', key: 'sk-video', status: 'active', group: { platform: 'composite', video_workbench_enabled: true } }
const now = Math.floor(Date.now() / 1000)
const task = (id: string, status: VideoTask['status']): VideoTask => ({ id, status, created_at: now, expires_at: now + 1800, workbench: { api_key_id: 1, prompt: 'waves', model: 'custom-video', ratio: '16:9', duration: 60, reference_images: 0, reference_videos: 0, reference_audios: 0 } })
const wrappers: VueWrapper[] = []
const SelectStub = defineComponent({
  props: ['modelValue', 'options', 'disabled'], emits: ['update:modelValue'],
  setup(props, { emit }) { return () => h('select', { value: props.modelValue ?? '', disabled: props.disabled, onChange: (event: Event) => { const value = (event.target as HTMLSelectElement).value; emit('update:modelValue', props.options.find((option: { value: unknown }) => String(option.value) === value)?.value) } }, props.options.map((option: { value: string | number; label: string }) => h('option', { value: option.value }, option.label))) }
})
function create() {
  const wrapper = mount(VideoWorkbenchView, { global: { stubs: { AppLayout: { template: '<main><slot /></main>' }, Icon: true, Select: SelectStub, RouterLink: { template: '<a><slot /></a>' }, BaseDialog: { props: ['show'], template: '<div v-if="show"><slot /><slot name="footer" /></div>' } } } })
  wrappers.push(wrapper); return wrapper
}
beforeEach(() => {
  vi.resetAllMocks(); vi.useFakeTimers()
  mocks.keys.mockResolvedValue({ items: [key], pages: 1 }); mocks.models.mockResolvedValue([{ id: 'custom-video' }]); mocks.list.mockResolvedValue({ settings: { ...settings }, tasks: [] }); mocks.submit.mockResolvedValue(task('new-task', 'queued'))
  vi.stubGlobal('URL', class extends URL { static createObjectURL = vi.fn(() => 'blob:reference'); static revokeObjectURL = vi.fn() })
  vi.stubGlobal('FileReader', class { result = 'data:image/png;base64,aW1hZ2U='; onload?: () => void; readAsDataURL() { this.onload?.() } })
})
afterEach(() => { wrappers.splice(0).forEach(wrapper => wrapper.unmount()); vi.clearAllTimers(); vi.useRealTimers(); vi.unstubAllGlobals() })

describe('VideoWorkbenchView', () => {
  it('defaults to square auto resolution and forwards each selected tier', async () => {
    const wrapper = create(); await flushPromises()
    expect(wrapper.get('.ratio-options .selected').text()).toBe('1:1')
    const options = wrapper.get('#video-resolution').findAll('option')
    expect(options.map(option => option.attributes('value'))).toEqual(['auto', '480p', '720p', '1080p', '2k', '4k'])
    for (const resolution of ['480p', '720p', '1080p', '2k', '4k']) {
      await wrapper.get('#video-resolution').setValue(resolution)
      await wrapper.get('#video-prompt').setValue('Resolution test')
      await wrapper.get('form').trigger('submit'); await flushPromises()
      expect(mocks.submit).toHaveBeenLastCalledWith('sk-video', 'owner@example.test', expect.objectContaining({ ratio: '1:1', resolution }))
      expect(wrapper.get<HTMLSelectElement>('#video-resolution').element.value).toBe('auto')
    }
  })
  it('filters keys to declared Custom groups and paginates', async () => {
    mocks.keys.mockResolvedValueOnce({ items: [
      { ...key, id: 2, name: 'Undeclared', group: { platform: 'custom', video_workbench_enabled: false } },
      { ...key, id: 3, name: 'Wrong platform', group: { platform: 'openai', video_workbench_enabled: true } },
      { ...key, id: 4, name: 'Expired', expires_at: new Date(0).toISOString() }
    ], pages: 2 }).mockResolvedValueOnce({ items: [key], pages: 2 })
    const wrapper = create(); await flushPromises()
    expect(wrapper.get('#video-key').findAll('option').map(option => option.text())).toEqual(['Video key'])
    expect(mocks.models).toHaveBeenCalledWith('sk-video', 'owner@example.test', expect.any(AbortSignal))
  })
  it('submits sixty seconds and a custom duration without image-only parameters', async () => {
    const wrapper = create(); await flushPromises(); await wrapper.get('#video-prompt').setValue('waves')
    await wrapper.findAll('.duration-options button')[3]!.trigger('click'); await wrapper.get('form').trigger('submit'); await flushPromises()
    expect(mocks.submit).toHaveBeenCalledWith('sk-video', 'owner@example.test', { model: 'custom-video', prompt: 'waves', duration: 60, ratio: '1:1', resolution: 'auto' })
    await wrapper.get('#video-workbench-tab').trigger('click'); await wrapper.findAll('.duration-options button')[4]!.trigger('click')
    expect(wrapper.get('#video-custom-duration').attributes('placeholder')).toBe('videoWorkbench.customDurationPlaceholder')
    await wrapper.get('#video-prompt').setValue('Next video'); await wrapper.get('#video-custom-duration').setValue(123); await wrapper.get('form').trigger('submit'); await flushPromises()
    expect(mocks.submit).toHaveBeenLastCalledWith('sk-video', 'owner@example.test', expect.objectContaining({ duration: 123 }))
    expect(wrapper.find('.quality').exists()).toBe(false)
  })
  it('only pastes images and requires file selection for video and audio', async () => {
    const wrapper = create(); await flushPromises()
    const image = new File(['image'], 'ref.png', { type: 'image/png' }), video = new File(['video'], 'ref.mp4', { type: 'video/mp4' })
    const paste = new Event('paste', { bubbles: true, cancelable: true })
    Object.defineProperty(paste, 'clipboardData', { value: { items: [image, video].map(file => ({ kind: 'file', type: file.type, getAsFile: () => file })) } })
    wrapper.get('.video-workbench').element.dispatchEvent(paste); await flushPromises()
    expect(wrapper.findAll('[data-reference-kind="images"] .reference-item')).toHaveLength(1)
    expect(wrapper.findAll('[data-reference-kind="videos"] .reference-item')).toHaveLength(0)
    const input = wrapper.get('[data-reference-kind="videos"] input')
    Object.defineProperty(input.element, 'files', { value: [video], configurable: true }); await input.trigger('change')
    expect(wrapper.findAll('[data-reference-kind="videos"] .reference-item')).toHaveLength(1)
    expect(wrapper.findAll('[data-reference-kind="audios"] .reference-item')).toHaveLength(0)
  })
  it('keeps new work queued when three tasks are running and blocks reduced reference limits', async () => {
    mocks.list.mockResolvedValue({ settings: { ...settings, max_reference_videos: 1 }, tasks: [1,2,3].map(id => task(String(id),'processing')) })
    const wrapper = create(); await flushPromises(); await wrapper.get('#video-prompt').setValue('waves')
    expect(wrapper.get<HTMLButtonElement>('.create-task').element.disabled).toBe(false)
    const input = wrapper.get('[data-reference-kind="videos"] input')
    Object.defineProperty(input.element, 'files', { value: [new File(['1'], 'one.mp4', { type: 'video/mp4' }),new File(['2'], 'two.mp4', { type: 'video/mp4' })] }); await input.trigger('change')
    expect(wrapper.findAll('[data-reference-kind="videos"] .reference-item')).toHaveLength(1)
    expect(mocks.error).toHaveBeenCalledWith('videoWorkbench.uploadLimit')
  })
  it('submits reference URLs without loading untrusted media in the browser', async () => {
    const wrapper = create(); await flushPromises()
    await wrapper.get('#video-prompt').setValue('waves')
    for (const kind of ['images', 'videos', 'audios']) {
      await wrapper.get(`[data-reference-kind="${kind}"] button[aria-label="videoWorkbench.addReferenceURL"]`).trigger('click')
      await wrapper.get(`#video-reference-url-${kind}`).setValue(`https://cdn.example/${kind}?signature=abc`)
      await wrapper.get(`#video-reference-url-${kind}`).trigger('keydown', { key: 'Enter' })
    }
    expect(wrapper.findAll('.reference-item')).toHaveLength(3)
    expect(wrapper.find('.reference-item img').exists()).toBe(false)
    expect(wrapper.find('.reference-item a').exists()).toBe(false)
    await wrapper.get('form').trigger('submit'); await flushPromises()
    expect(mocks.submit).toHaveBeenCalledWith('sk-video', 'owner@example.test', expect.objectContaining({
      referenceImages: ['https://cdn.example/images?signature=abc'],
      referenceVideos: ['https://cdn.example/videos?signature=abc'],
      referenceAudios: ['https://cdn.example/audios?signature=abc'],
    }))
    expect(wrapper.findAll('.reference-item')).toHaveLength(0)
    expect(URL.createObjectURL).not.toHaveBeenCalled()
  })
  it('rejects unsafe reference URL schemes and embedded credentials', async () => {
    const wrapper = create(); await flushPromises()
    await wrapper.get('[data-reference-kind="images"] button[aria-label="videoWorkbench.addReferenceURL"]').trigger('click')
    for (const value of ['javascript:alert(1)', 'data:image/png;base64,AAAA', 'https://user:secret@example.test/a.png', '/relative.png']) {
      await wrapper.get('#video-reference-url-images').setValue(value)
      await wrapper.get('#video-reference-url-images').trigger('keydown', { key: 'Enter' })
      expect(mocks.error).toHaveBeenLastCalledWith('videoWorkbench.invalidReferenceURL')
    }
    expect(wrapper.find('.reference-item').exists()).toBe(false)
  })
  it('does not dispatch a paid job after navigation during local reference reading', async () => {
    let completeRead!: () => void
    vi.stubGlobal('FileReader', class {
      result = 'data:image/png;base64,aW1hZ2U='
      onload?: () => void
      readAsDataURL() { completeRead = () => this.onload?.() }
    })
    const wrapper = create(); await flushPromises()
    await wrapper.get('#video-prompt').setValue('waves')
    const input = wrapper.get('[data-reference-kind="images"] input')
    Object.defineProperty(input.element, 'files', { value: [new File(['image'], 'image.png', { type: 'image/png' })] })
    await input.trigger('change')
    await wrapper.get('form').trigger('submit')
    wrapper.unmount()
    completeRead(); await flushPromises()
    expect(mocks.submit).not.toHaveBeenCalled()
  })
  it('hides empty or unsafe documentation links and displays safe external links', async () => {
    const wrapper = create(); await flushPromises(); expect(wrapper.findAll('.workbench-tabs a')).toHaveLength(0)
    mocks.list.mockResolvedValue({ settings: { ...settings, tutorial_url: 'https://docs.example/tutorial', documentation_url: 'javascript:alert(1)', announcement: '<script>alert(1)</script>' }, tasks: [] })
    await vi.advanceTimersByTimeAsync(5000); await flushPromises()
    expect(wrapper.get('.workbench-tabs a').attributes()).toMatchObject({ href: 'https://docs.example/tutorial', target: '_blank', rel: 'noopener noreferrer' })
    expect(wrapper.findAll('.workbench-tabs a')).toHaveLength(1); expect(wrapper.find('.announcement script').exists()).toBe(false)
    expect(wrapper.get('.announcement').text()).toContain('<script>alert(1)</script>')
  })
  it('does not expose a clickable preview or request content before download', async () => {
    mocks.list.mockResolvedValue({ settings, tasks: [task('done','completed')] }); mocks.download.mockResolvedValue(new Blob(['video'],{type:'video/mp4'}))
    const wrapper = create(); await flushPromises(); await wrapper.get('#video-tasks-tab').trigger('click')
    expect(wrapper.find('.task-preview a').exists()).toBe(false); expect(wrapper.find('.task-preview video').exists()).toBe(false); expect(mocks.download).not.toHaveBeenCalled()
    await wrapper.get('.download-action').trigger('click'); await flushPromises(); expect(mocks.download).toHaveBeenCalledWith('done',expect.any(AbortSignal)); expect(mocks.save).toHaveBeenCalled()
  })
  it('rejects invalid custom durations before submission', async () => {
    const wrapper = create(); await flushPromises(); await wrapper.get('#video-prompt').setValue('waves'); await wrapper.findAll('.duration-options button')[4]!.trigger('click')
    for (const value of ['',0,-1,1.5,86401]) { await wrapper.get('#video-custom-duration').setValue(value); expect(wrapper.get<HTMLButtonElement>('.create-task').element.disabled).toBe(true); await wrapper.get('form').trigger('submit') }
    expect(mocks.submit).not.toHaveBeenCalled()
  })

  it('uses valid Hailuo durations and retains the model default after clearing a submission', async () => {
    mocks.models.mockResolvedValue([{ id: 'MiniMax-Hailuo-2.3' }])
    const wrapper = create(); await flushPromises()
    expect(wrapper.findAll('.duration-options button').map(button => button.text())).toEqual(['6s', '10s', 'videoWorkbench.custom'])
    expect(wrapper.get('.duration-options button.selected').text()).toBe('6s')
    await wrapper.get('#video-prompt').setValue('waves')
    await wrapper.findAll('.duration-options button')[2]!.trigger('click')
    await wrapper.get('#video-custom-duration').setValue(7)
    expect(wrapper.get<HTMLButtonElement>('.create-task').element.disabled).toBe(true)
    await wrapper.get('form').trigger('submit'); expect(mocks.submit).not.toHaveBeenCalled()
    await wrapper.get('#video-custom-duration').setValue(10)
    await wrapper.get('form').trigger('submit'); await flushPromises()
    expect(mocks.submit).toHaveBeenCalledWith('sk-video', 'owner@example.test', { model: 'MiniMax-Hailuo-2.3', prompt: 'waves', duration: 10, ratio: '1:1', resolution: 'auto' })
    expect(wrapper.get('.duration-options button.selected').text()).toBe('6s')
    expect(wrapper.get('#video-workbench-tab').attributes('aria-selected')).toBe('true')
    expect(wrapper.get<HTMLTextAreaElement>('#video-prompt').element.value).toBe('')
  })

  it('requires an image for Hailuo Fast and prevents unsupported reference media', async () => {
    mocks.models.mockResolvedValue([{ id: 'MiniMax-Hailuo-2.3-Fast' }])
    const wrapper = create(); await flushPromises(); await wrapper.get('#video-prompt').setValue('waves')
    expect(wrapper.get<HTMLButtonElement>('.create-task').element.disabled).toBe(true)
    expect(wrapper.text()).toContain('videoWorkbench.referenceImageRequired')
    expect(wrapper.find('[data-reference-kind="videos"] .reference-add').exists()).toBe(false)
    expect(wrapper.find('[data-reference-kind="audios"] .reference-add').exists()).toBe(false)
    const input = wrapper.get('[data-reference-kind="images"] input')
    Object.defineProperty(input.element, 'files', { value: [new File(['ref'], 'ref.png', { type: 'image/png' })] })
    await input.trigger('change')
    expect(wrapper.get<HTMLButtonElement>('.create-task').element.disabled).toBe(false)
    await wrapper.get('form').trigger('submit'); await flushPromises()
    expect(mocks.submit).toHaveBeenCalledWith('sk-video', 'owner@example.test', expect.objectContaining({ duration: 6, referenceImages: ['data:image/png;base64,aW1hZ2U='] }))
    expect(wrapper.get<HTMLButtonElement>('.create-task').element.disabled).toBe(true)
  })

  it('keeps a valid non-preset duration visible when switching models', async () => {
    mocks.models.mockResolvedValue([{ id: 'MiniMax-Hailuo-2.3' }, { id: 'MiniMax-H3' }, { id: 'custom-video' }])
    const wrapper = create(); await flushPromises(); await wrapper.get('#video-prompt').setValue('waves')
    expect(wrapper.get('.duration-options button.selected').text()).toBe('6s')
    await wrapper.get('#video-model').setValue('MiniMax-H3')
    expect(wrapper.get('.duration-options button.selected').text()).toBe('videoWorkbench.custom')
    expect(wrapper.get<HTMLInputElement>('#video-custom-duration').element.value).toBe('6')
    await wrapper.get('#video-model').setValue('custom-video')
    expect(wrapper.get<HTMLInputElement>('#video-custom-duration').element.value).toBe('6')
    await wrapper.get('form').trigger('submit'); await flushPromises()
    expect(mocks.submit).toHaveBeenCalledWith('sk-video', 'owner@example.test', expect.objectContaining({ model: 'custom-video', duration: 6 }))
  })

  it('validates H3 custom duration and ratio while combining model and administrator media limits', async () => {
    mocks.models.mockResolvedValue([{ id: 'minimax-h3-f' }])
    const wrapper = create(); await flushPromises(); await wrapper.get('#video-prompt').setValue('waves')
    await wrapper.findAll('.duration-options button')[4]!.trigger('click')
    expect(wrapper.get('#video-custom-duration').attributes()).toMatchObject({ min: '4', max: '15' })
    await wrapper.get('#video-custom-duration').setValue(16)
    expect(wrapper.get<HTMLButtonElement>('.create-task').element.disabled).toBe(true)
    await wrapper.get('#video-custom-duration').setValue(14)
    await wrapper.get('.custom-ratio button').trigger('click')
    await wrapper.get('#video-width').setValue(2); await wrapper.get('#video-height').setValue(1)
    expect(wrapper.get<HTMLButtonElement>('.create-task').element.disabled).toBe(true)
    await wrapper.get('#video-width').setValue(21); await wrapper.get('#video-height').setValue(9)
    const input = wrapper.get('[data-reference-kind="videos"] input')
    Object.defineProperty(input.element, 'files', { value: Array.from({ length: 4 }, (_, index) => new File(['ref'], `${index}.mp4`, { type: 'video/mp4' })) })
    await input.trigger('change')
    expect(wrapper.findAll('[data-reference-kind="videos"] .reference-item')).toHaveLength(3)
    expect(mocks.error).toHaveBeenCalledWith('videoWorkbench.uploadLimit')
    await wrapper.get('form').trigger('submit'); await flushPromises()
    expect(mocks.submit).toHaveBeenCalledWith('sk-video', 'owner@example.test', expect.objectContaining({ model: 'minimax-h3-f', duration: 14, ratio: '21:9', referenceVideos: expect.any(Array) }))
  })

  it('shows a safe model unavailable message in the task list', async () => {
    mocks.list.mockResolvedValue({ settings, tasks: [{ ...task('unavailable', 'failed'), error_code: 'model_unavailable' }] })
    const wrapper = create(); await flushPromises(); await wrapper.get('#video-tasks-tab').trigger('click')
    expect(wrapper.get('.task-error').text()).toBe('videoWorkbench.errors.model_unavailable')
  })

  it('keeps all resolution options for sd-2.0 and displays parameter guidance', async () => {
    mocks.models.mockResolvedValue([{ id: 'sd-2.0' }])
    const wrapper = create(); await flushPromises(); await wrapper.get('#video-prompt').setValue('waves')
    expect(wrapper.get('#video-resolution').findAll('option').map(option => option.attributes('value'))).toEqual(['auto', '480p', '720p', '1080p', '2k', '4k'])
    expect(wrapper.findAll('.parameter-hint').map(hint => hint.text())).toEqual(['videoWorkbench.resolutionHint', 'videoWorkbench.parameterHint'])
    expect(wrapper.find('[data-reference-kind="videos"] .reference-add').exists()).toBe(true)
    await wrapper.get('form').trigger('submit'); await flushPromises()
    expect(mocks.submit).toHaveBeenCalledWith('sk-video', 'owner@example.test', expect.objectContaining({ model: 'sd-2.0', duration: 5, resolution: 'auto' }))
    expect(wrapper.get('#video-workbench-tab').attributes('aria-selected')).toBe('true')
  })

  it('shows a safe error on the workbench when the newly created task later fails', async () => {
    const wrapper = create(); await flushPromises(); await wrapper.get('#video-prompt').setValue('waves')
    await wrapper.get('form').trigger('submit'); await flushPromises()
    mocks.list.mockResolvedValue({ settings, tasks: [{ ...task('new-task', 'failed'), error_code: 'upstream_pricing_unavailable' }] })
    await vi.advanceTimersByTimeAsync(3000); await flushPromises()
    expect(wrapper.get('#video-workbench-tab').attributes('aria-selected')).toBe('true')
    expect(wrapper.get('.workbench-message[role="alert"]').text()).toContain('videoWorkbench.errors.upstream_pricing_unavailable')
    await wrapper.get('.workbench-message button').trigger('click')
    expect(wrapper.get('#video-tasks-tab').attributes('aria-selected')).toBe('true')
  })

  it('explains rejected submissions using safe status messages and preserves an uncertain request warning', async () => {
    const wrapper = create(); await flushPromises(); await wrapper.get('#video-prompt').setValue('waves')
    for (const [status, code] of [[400, 'invalid_parameters'], [402, 'insufficient_balance'], [413, 'request_too_large'], [429, 'rate_limited'], [503, 'service_unavailable']] as const) {
      mocks.submit.mockRejectedValueOnce(new ImageWorkbenchError('https://internal.example/sk-secret', status, 'private'))
      await wrapper.get('form').trigger('submit'); await flushPromises()
      expect(mocks.error).toHaveBeenLastCalledWith(`videoWorkbench.errors.${code}`)
      expect(wrapper.get('.workbench-message[role="alert"]').text()).toContain(`videoWorkbench.errors.${code}`)
      expect(wrapper.text()).not.toContain('sk-secret')
    }
    mocks.submit.mockRejectedValueOnce(new ImageWorkbenchError('private', 0, 'ETIMEDOUT'))
    await wrapper.get('form').trigger('submit'); await flushPromises()
    expect(mocks.error).toHaveBeenLastCalledWith('videoWorkbench.submitError')
    expect(wrapper.find('.workbench-message[role="alert"]').exists()).toBe(false)
    expect(wrapper.get<HTMLTextAreaElement>('#video-prompt').element.value).toBe('waves')
  })

  it('uses safe messages for upstream pricing errors and unknown private error codes', async () => {
    mocks.list.mockResolvedValue({ settings, tasks: [
      { ...task('pricing', 'failed'), error_code: 'upstream_pricing_unavailable' },
      { ...task('unknown', 'failed'), error_code: 'https://internal.example/sk-secret' }
    ] })
    const wrapper = create(); await flushPromises(); await wrapper.get('#video-tasks-tab').trigger('click')
    expect(wrapper.get('[data-task-id="pricing"] .task-error').text()).toBe('videoWorkbench.errors.upstream_pricing_unavailable')
    expect(wrapper.get('[data-task-id="unknown"] .task-error').text()).toBe('videoWorkbench.errors.generation_failed')
    expect(wrapper.text()).not.toContain('sk-secret')
  })

  it('clears all submitted media and restores defaults while staying on the workbench', async () => {
    const wrapper = create(); await flushPromises()
    await wrapper.get('#video-prompt').setValue('waves')
    await wrapper.findAll('.duration-options button')[3]!.trigger('click')
    for (const [kind, type] of [['images', 'image/png'], ['videos', 'video/mp4'], ['audios', 'audio/mpeg']]) {
      const input = wrapper.get(`[data-reference-kind="${kind}"] input`)
      Object.defineProperty(input.element, 'files', { value: [new File(['ref'], 'reference', { type })], configurable: true })
      await input.trigger('change')
    }
    expect(wrapper.findAll('.reference-item')).toHaveLength(3)
    await wrapper.get('form').trigger('submit'); await flushPromises()
    expect(wrapper.get('#video-workbench-tab').attributes('aria-selected')).toBe('true')
    expect(wrapper.get<HTMLTextAreaElement>('#video-prompt').element.value).toBe('')
    expect(wrapper.findAll('.reference-item')).toHaveLength(0)
    expect(URL.revokeObjectURL).toHaveBeenCalledWith('blob:reference')
    expect(wrapper.get('.duration-options button.selected').text()).toBe('5s')
    expect(wrapper.get<HTMLSelectElement>('#video-model').element.value).toBe('custom-video')
    expect(mocks.submit).toHaveBeenCalledWith('sk-video', 'owner@example.test', expect.objectContaining({ duration: 60, referenceImages: ['data:image/png;base64,aW1hZ2U='], referenceVideos: ['data:image/png;base64,aW1hZ2U='], referenceAudios: ['data:image/png;base64,aW1hZ2U='] }))
    await wrapper.get('#video-tasks-tab').trigger('click')
    expect(wrapper.find('[data-task-id="new-task"]').exists()).toBe(true)
  })

  it('keeps the draft after failure and preserves edits made during an accepted request', async () => {
    mocks.submit.mockRejectedValueOnce(new Error('Unavailable'))
    const wrapper = create(); await flushPromises()
    await wrapper.get('#video-prompt').setValue('Original draft')
    await wrapper.get('form').trigger('submit'); await flushPromises()
    expect(wrapper.get<HTMLTextAreaElement>('#video-prompt').element.value).toBe('Original draft')
    expect(wrapper.get('#video-workbench-tab').attributes('aria-selected')).toBe('true')
    let finish!: (value: VideoTask) => void
    mocks.submit.mockImplementationOnce(() => new Promise<VideoTask>(resolve => { finish = resolve }))
    await wrapper.get('form').trigger('submit')
    await wrapper.get('#video-prompt').setValue('Newer draft')
    finish(task('accepted', 'queued')); await flushPromises()
    expect(wrapper.get<HTMLTextAreaElement>('#video-prompt').element.value).toBe('Newer draft')
    expect(mocks.submit).toHaveBeenLastCalledWith('sk-video', 'owner@example.test', expect.objectContaining({ prompt: 'Original draft' }))
  })

  it('paginates by twenty and resets or clamps the page when the filtered list changes', async () => {
    const items = Array.from({ length: 41 }, (_, index) => task(`page-${index}`, index < 21 ? 'completed' : 'failed'))
    mocks.list.mockResolvedValue({ settings, tasks: items })
    const wrapper = create(); await flushPromises(); await wrapper.get('#video-tasks-tab').trigger('click')
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
    mocks.list.mockResolvedValue({ settings, tasks: items.filter(item => item.id !== 'page-20') })
    await wrapper.get('button[aria-label="videoWorkbench.delete"]').trigger('click')
    await wrapper.get('.btn-danger').trigger('click'); await flushPromises()
    expect(wrapper.findAll('[data-task-id]')).toHaveLength(20)
    expect(wrapper.get<HTMLButtonElement>('button[aria-label="pagination.previous"]').element.disabled).toBe(true)
    await wrapper.findAll('.status-filter button')[0]!.trigger('click')
    await wrapper.get('button[aria-label="pagination.next"]').trigger('click')
    vi.setSystemTime((now + 1801) * 1000); await vi.advanceTimersByTimeAsync(1000)
    expect(wrapper.findAll('[data-task-id]')).toHaveLength(0)
    expect(wrapper.find('.task-pagination').exists()).toBe(false)
  })

  it('animates a fast manual refresh and updates the notice from the server retention setting', async () => {
    const wrapper = create(); await flushPromises(); await wrapper.get('#video-tasks-tab').trigger('click')
    expect(wrapper.get('.task-notice').text()).toBe('videoWorkbench.retention:30')
    mocks.list.mockResolvedValue({ settings: { ...settings, retention_minutes: 45 }, tasks: [] })
    const refresh = wrapper.get('.task-toolbar .icon-button')
    await refresh.trigger('click'); await flushPromises()
    expect(refresh.find('.animate-spin').exists()).toBe(true)
    expect(wrapper.get('.task-notice').text()).toBe('videoWorkbench.retention:45')
    await refresh.trigger('click')
    expect(mocks.list).toHaveBeenCalledTimes(2)
    await vi.advanceTimersByTimeAsync(1000)
    expect(refresh.find('.animate-spin').exists()).toBe(false)
    expect((refresh.element as HTMLButtonElement).disabled).toBe(false)
  })
})
