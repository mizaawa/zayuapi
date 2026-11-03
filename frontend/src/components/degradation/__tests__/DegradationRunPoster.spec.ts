import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import enDegradation from '@/i18n/locales/en/degradation'
import DegradationRunPoster from '@/components/degradation/DegradationRunPoster.vue'
import { DEGRADATION_POSTER_MESSAGE, DEGRADATION_POSTER_TIMEOUT_MS, degradationPosterQueue, resetDegradationPosterState } from '@/utils/degradationPoster'

const { loadDegradationRun, posterMocks } = vi.hoisted(() => ({
  loadDegradationRun: vi.fn(),
  posterMocks: {
    peekDegradationPoster: vi.fn(),
    findDegradationPoster: vi.fn(),
    saveDegradationPoster: vi.fn(),
    normalizeDegradationPoster: vi.fn(),
  },
}))

vi.mock('@/composables/useDegradationRuns', () => ({ loadDegradationRun }))
vi.mock('@/utils/degradationPoster', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/utils/degradationPoster')>()),
  ...posterMocks,
  createDegradationPosterToken: () => 'tok',
}))
// The test config aliases i18n to its runtime-only build; real locale strings need the compiler.
vi.mock('vue-i18n', async () => {
  const { createRequire } = await import('node:module')
  const fullBuild = createRequire(import.meta.url).resolve('vue-i18n/dist/vue-i18n.esm-bundler.js')
  return vi.importActual<typeof import('vue-i18n')>(fullBuild)
})

const observers: ControlledIntersectionObserver[] = []
class ControlledIntersectionObserver {
  constructor(public callback: IntersectionObserverCallback) {
    observers.push(this)
  }
  observe() {}
  unobserve() {}
  disconnect() {}
}

function setVisible(visible: boolean) {
  for (const observer of observers) {
    observer.callback([{ isIntersecting: visible } as IntersectionObserverEntry], observer as unknown as IntersectionObserver)
  }
}

const run = { id: 5, finished_at: '2026-11-03T08:01:00Z' }
const wrappers: VueWrapper[] = []

function mountPoster(): VueWrapper {
  const i18n = createI18n({ legacy: false, locale: 'en', messages: { en: enDegradation } })
  const wrapper = mount(DegradationRunPoster, {
    props: { run, title: 'Pelican · 11-03 16:01' },
    attachTo: document.body,
    global: { plugins: [i18n], stubs: { Icon: true } },
  })
  wrappers.push(wrapper)
  return wrapper
}

function reply(source: Window | null, data: Record<string, unknown>) {
  const event = new MessageEvent('message', { data: { type: DEGRADATION_POSTER_MESSAGE, token: 'tok', ...data } })
  // jsdom ignores `source` in the event init, so set it the way a browser would.
  Object.defineProperty(event, 'source', { value: source })
  window.dispatchEvent(event)
}

async function renderFrame(wrapper: VueWrapper) {
  setVisible(true)
  await flushPromises()
  const frame = wrapper.get('[data-testid="degradation-poster-frame"]')
  return { frame, contentWindow: (frame.element as HTMLIFrameElement).contentWindow }
}

describe('DegradationRunPoster', () => {
  beforeEach(() => {
    observers.length = 0
    vi.stubGlobal('IntersectionObserver', ControlledIntersectionObserver)
    resetDegradationPosterState()
    for (const mock of [loadDegradationRun, ...Object.values(posterMocks)]) mock.mockReset()
    posterMocks.peekDegradationPoster.mockReturnValue(null)
    posterMocks.findDegradationPoster.mockResolvedValue(null)
    posterMocks.normalizeDegradationPoster.mockResolvedValue(new Blob(['webp'], { type: 'image/webp' }))
    posterMocks.saveDegradationPoster.mockReturnValue('blob:fresh')
    loadDegradationRun.mockResolvedValue({ id: 5, status: 'success', output: '```html\n<svg id="pelican"></svg>\n```' })
  })

  afterEach(() => {
    for (const wrapper of wrappers.splice(0)) wrapper.unmount()
    degradationPosterQueue.resume()
    vi.unstubAllGlobals()
  })

  it('shows a poster from this session at once, without loading the document', async () => {
    posterMocks.peekDegradationPoster.mockReturnValue('blob:cached')
    const wrapper = mountPoster()
    expect(wrapper.get('[data-testid="degradation-poster-image"]').attributes('src')).toBe('blob:cached')

    setVisible(true)
    await flushPromises()
    expect(loadDegradationRun).not.toHaveBeenCalled()
    expect(wrapper.find('iframe').exists()).toBe(false)
  })

  it('renders once on screen and swaps the frame for the re-encoded still', async () => {
    const wrapper = mountPoster()
    expect(wrapper.attributes('data-phase')).toBe('idle')
    await flushPromises()
    expect(loadDegradationRun).not.toHaveBeenCalled()

    const { frame, contentWindow } = await renderFrame(wrapper)
    expect(wrapper.attributes('data-phase')).toBe('rendering')
    expect(frame.attributes('sandbox')).toBe('allow-scripts')
    expect(wrapper.text()).toContain('Preparing preview')
    expect(degradationPosterQueue.active).toBe(1)

    const postMessage = vi.spyOn(contentWindow!, 'postMessage')
    await frame.trigger('load')
    expect(postMessage).toHaveBeenCalledWith(expect.objectContaining({
      type: 'sub2api-degradation-preview',
      html: expect.stringContaining('"token":"tok"'),
    }), '*')

    reply(window, { ok: true, buffer: new ArrayBuffer(8) })
    reply(contentWindow, { token: 'other', ok: true, buffer: new ArrayBuffer(8) })
    await flushPromises()
    expect(posterMocks.normalizeDegradationPoster).not.toHaveBeenCalled()

    reply(contentWindow, { ok: true, buffer: new ArrayBuffer(8) })
    await flushPromises()
    expect(posterMocks.normalizeDegradationPoster).toHaveBeenCalledTimes(1)
    expect(posterMocks.saveDegradationPoster).toHaveBeenCalledWith('v1:5:2026-11-03T08:01:00Z', expect.any(Blob))
    expect(wrapper.get('[data-testid="degradation-poster-image"]').attributes('src')).toBe('blob:fresh')
    expect(wrapper.find('iframe').exists()).toBe(false)
    expect(degradationPosterQueue.active).toBe(0)
  })

  it('keeps the paused frame when it cannot paint itself', async () => {
    const wrapper = mountPoster()
    const { contentWindow } = await renderFrame(wrapper)
    reply(contentWindow, { ok: false, reason: 'security' })
    await flushPromises()

    expect(wrapper.attributes('data-phase')).toBe('frozen')
    expect(wrapper.find('[data-testid="degradation-poster-frame"]').exists()).toBe(true)
    expect(wrapper.find('.dg-poster-cover').exists()).toBe(false)
    expect(posterMocks.normalizeDegradationPoster).not.toHaveBeenCalled()
    expect(degradationPosterQueue.active).toBe(0)
  })

  it('removes a frame that never reports and does not retry it', async () => {
    // Only timers are faked; promises and the test utils' setImmediate flush stay real.
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
    try {
      const wrapper = mountPoster()
      await renderFrame(wrapper)
      vi.advanceTimersByTime(DEGRADATION_POSTER_TIMEOUT_MS - 1)
      await flushPromises()
      expect(wrapper.attributes('data-phase')).toBe('rendering')
      vi.advanceTimersByTime(1)
      await flushPromises()

      expect(wrapper.attributes('data-phase')).toBe('unavailable')
      expect(wrapper.find('iframe').exists()).toBe(false)
      expect(wrapper.text()).toContain('No preview yet, open to play')
      expect(degradationPosterQueue.active).toBe(0)

      const again = mountPoster()
      expect(again.attributes('data-phase')).toBe('unavailable')
      setVisible(true)
      await flushPromises()
      expect(loadDegradationRun).toHaveBeenCalledTimes(1)
    } finally {
      vi.useRealTimers()
    }
  })

  it('says so when the answer holds nothing renderable', async () => {
    loadDegradationRun.mockResolvedValue({ id: 5, status: 'success', output: 'I cannot draw that.' })
    const wrapper = mountPoster()
    setVisible(true)
    await flushPromises()

    expect(wrapper.attributes('data-phase')).toBe('empty')
    expect(wrapper.text()).toContain('No renderable animation')
    expect(wrapper.find('iframe').exists()).toBe(false)
    expect(degradationPosterQueue.active).toBe(0)
  })

  it('gives its place back when scrolled away before its turn', async () => {
    degradationPosterQueue.pause()
    const wrapper = mountPoster()
    setVisible(true)
    await flushPromises()
    expect(wrapper.attributes('data-phase')).toBe('queued')
    expect(degradationPosterQueue.waiting).toBe(1)

    setVisible(false)
    await flushPromises()
    expect(wrapper.attributes('data-phase')).toBe('idle')
    expect(degradationPosterQueue.waiting).toBe(0)
    degradationPosterQueue.resume()
    await flushPromises()
    expect(loadDegradationRun).not.toHaveBeenCalled()
  })

  it('frees its render slot when unmounted mid-render', async () => {
    const wrapper = mountPoster()
    await renderFrame(wrapper)
    expect(degradationPosterQueue.active).toBe(1)
    wrapper.unmount()
    wrappers.splice(wrappers.indexOf(wrapper), 1)
    expect(degradationPosterQueue.active).toBe(0)
  })
})
