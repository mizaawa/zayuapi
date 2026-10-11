import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { defineComponent, h, ref } from 'vue'
import VideoWorkbenchView from '../VideoWorkbenchView.vue'
import type { VideoTask, VideoWorkbenchSettings } from '@/api/videoWorkbench'

const mocks = vi.hoisted(() => ({ keys: vi.fn(), models: vi.fn(), list: vi.fn(), submit: vi.fn(), download: vi.fn(), delete: vi.fn(), error: vi.fn(), success: vi.fn(), save: vi.fn() }))
vi.mock('@/api/keys', () => ({ keysAPI: { list: mocks.keys } }))
vi.mock('@/api/videoWorkbench', async () => ({ ...await vi.importActual<typeof import('@/api/videoWorkbench')>('@/api/videoWorkbench'), listVideoTasks: mocks.list, listVideoModels: mocks.models, submitVideoTask: mocks.submit, downloadVideoTask: mocks.download, deleteVideoTask: mocks.delete }))
vi.mock('@/api/batchImage', () => ({ saveBlob: mocks.save }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => ({ user: { email: 'owner@example.test' } }) }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showError: mocks.error, showSuccess: mocks.success, sidebarCollapsed: false }) }))
vi.mock('@/composables/useClipboard', () => ({ useClipboard: () => ({ copyToClipboard: vi.fn() }) }))
vi.mock('vue-i18n', async () => ({ ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'), useI18n: () => ({ locale: ref('en'), t: (key: string) => key }) }))

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
    expect(mocks.submit).toHaveBeenCalledWith('sk-video', 'owner@example.test', { model: 'custom-video', prompt: 'waves', duration: 60, ratio: '16:9' })
    await wrapper.get('#video-workbench-tab').trigger('click'); await wrapper.findAll('.duration-options button')[4]!.trigger('click')
    expect(wrapper.get('#video-custom-duration').attributes('placeholder')).toBe('videoWorkbench.customDurationPlaceholder')
    await wrapper.get('#video-custom-duration').setValue(123); await wrapper.get('form').trigger('submit'); await flushPromises()
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
})
