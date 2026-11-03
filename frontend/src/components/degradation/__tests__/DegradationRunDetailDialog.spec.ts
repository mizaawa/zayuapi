import { defineComponent } from 'vue'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import type { DegradationRun } from '@/api/degradation'
import enDegradation from '@/i18n/locales/en/degradation'
import DegradationRunDetailDialog from '@/components/degradation/DegradationRunDetailDialog.vue'

const { loadDegradationRun, copyToClipboard } = vi.hoisted(() => ({
  loadDegradationRun: vi.fn(),
  copyToClipboard: vi.fn(),
}))

vi.mock('@/composables/useDegradationRuns', () => ({ loadDegradationRun }))
vi.mock('@/composables/useClipboard', () => ({ useClipboard: () => ({ copyToClipboard }) }))
// The test config aliases i18n to its runtime-only build; real locale strings need the compiler.
vi.mock('vue-i18n', async () => {
  const { createRequire } = await import('node:module')
  const fullBuild = createRequire(import.meta.url).resolve('vue-i18n/dist/vue-i18n.esm-bundler.js')
  return vi.importActual<typeof import('vue-i18n')>(fullBuild)
})

const DialogStub = defineComponent({
  props: { show: Boolean, title: { type: String, default: '' } },
  template: '<div v-if="show" class="dialog-stub"><h3>{{ title }}</h3><slot /><slot name="footer" /></div>',
})
const FrameStub = defineComponent({
  props: { html: { type: String, default: '' }, title: { type: String, default: '' }, interactive: Boolean },
  methods: { replay() {} },
  template: '<div class="frame-stub" :data-html="html" :data-interactive="String(interactive)"></div>',
})

// Fields an older server could still send; none of them may surface.
const adminOnly = {
  error_message: 'upstream HTTP 502: account sk-live-secret',
  attempts: 3,
  first_token_ms: 40923,
  input_tokens: 81,
  output_tokens: 7407,
  total_tokens: 7488,
  cache_rate: 0.5,
  cost: 0.37116,
}

function detail(overrides: Partial<DegradationRun> = {}): DegradationRun {
  return {
    id: 9,
    monitor_id: 1,
    status: 'success',
    model: 'gpt-6-astra',
    response_model: 'gpt-6-astra',
    reasoning_effort: 'high',
    protocol: 'openai_responses',
    prompt_name: 'Pelican',
    prompt: 'Draw a pelican riding a bicycle',
    output: '```html\n<svg id="pelican"></svg>\n```',
    duration_ms: 227440,
    started_at: '2026-11-03T14:11:34Z',
    finished_at: '2026-11-03T14:15:22Z',
    ...adminOnly,
    ...overrides,
  } as DegradationRun
}

const wrappers: VueWrapper[] = []

function mountDialog(): VueWrapper {
  const i18n = createI18n({ legacy: false, locale: 'en', messages: { en: enDegradation } })
  const wrapper = mount(DegradationRunDetailDialog, {
    props: { runId: 9, title: 'ZayuAPI GPT-6 Astra' },
    global: { plugins: [i18n], stubs: { BaseDialog: DialogStub, DegradationPreviewFrame: FrameStub, Icon: true } },
  })
  wrappers.push(wrapper)
  return wrapper
}

const hiddenText = ['First token', 'Tokens', 'Cache rate', 'Cost', 'Requests', 'Failure reason', 'Show code', 'Animation code', 'sk-live-secret', '7,488', '0.37']

describe('DegradationRunDetailDialog', () => {
  beforeEach(() => {
    loadDegradationRun.mockReset()
    copyToClipboard.mockReset()
  })

  afterEach(() => {
    for (const wrapper of wrappers.splice(0)) wrapper.unmount()
  })

  it('plays the animation and lists only what the check looked like', async () => {
    loadDegradationRun.mockResolvedValue(detail())
    const wrapper = mountDialog()
    await flushPromises()

    const text = wrapper.get('[data-testid="degradation-run-detail"]').text()
    for (const label of ['Generated at', 'Model', 'Reasoning effort', 'Total time', 'Protocol', 'Input prompt', 'Draw a pelican riding a bicycle']) {
      expect(text).toContain(label)
    }
    expect(text).toContain('3m 47s')
    expect(text).toContain('OpenAI Responses')
    for (const hidden of hiddenText) expect(text).not.toContain(hidden)
    expect(wrapper.find('pre').exists()).toBe(false)

    const frame = wrapper.get('.frame-stub')
    expect(frame.attributes('data-html')).toBe('<svg id="pelican"></svg>')
    expect(frame.attributes('data-interactive')).toBe('true')

    await wrapper.get('[data-testid="degradation-copy-code"]').trigger('click')
    expect(copyToClipboard).toHaveBeenCalledWith('<svg id="pelican"></svg>', 'Animation code copied')
  })

  it('shows a failure without its reason or retry count', async () => {
    loadDegradationRun.mockResolvedValue(detail({ status: 'failed', output: '<svg>partial</svg>', response_model: '' }))
    const wrapper = mountDialog()
    await flushPromises()

    const text = wrapper.get('[data-testid="degradation-run-detail"]').text()
    expect(text).toContain('Failed')
    for (const hidden of hiddenText) expect(text).not.toContain(hidden)
    expect(wrapper.find('.frame-stub').exists()).toBe(false)
    expect(wrapper.get('[data-testid="degradation-copy-code"]').attributes('disabled')).toBeDefined()
    expect(wrapper.get('[data-testid="degradation-replay"]').attributes('disabled')).toBeDefined()
  })
})
