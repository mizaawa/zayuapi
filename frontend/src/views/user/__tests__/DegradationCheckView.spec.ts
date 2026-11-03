import { defineComponent } from 'vue'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import type { DegradationOverview, DegradationRun, DegradationUserMonitor } from '@/api/degradation'
import enCommon from '@/i18n/locales/en/common'
import enDegradation from '@/i18n/locales/en/degradation'
import DegradationCheckView from '@/views/user/DegradationCheckView.vue'

const { getOverview } = vi.hoisted(() => ({ getOverview: vi.fn() }))

vi.mock('@/api/degradation', () => ({ getOverview, getRun: vi.fn() }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showError: vi.fn() }) }))

// The test config aliases i18n to its runtime-only build; real locale strings need the compiler.
vi.mock('vue-i18n', async () => {
  const { createRequire } = await import('node:module')
  const fullBuild = createRequire(import.meta.url).resolve('vue-i18n/dist/vue-i18n.esm-bundler.js')
  return vi.importActual<typeof import('vue-i18n')>(fullBuild)
})

const AppLayoutStub = defineComponent({ template: '<main><slot /></main>' })
const CardStub = defineComponent({
  props: { run: { type: Object, required: true }, title: { type: String, required: true } },
  emits: ['open'],
  template: '<button type="button" class="run-card" :data-run="run.id" @click="$emit(\'open\', run.id)">{{ title }}</button>',
})
const DetailStub = defineComponent({
  props: { runId: { type: Number, default: null }, title: { type: String, default: '' } },
  template: '<div class="detail-stub" :data-run="runId">{{ title }}</div>',
})

function makeRun(id: number, overrides: Partial<DegradationRun> = {}): DegradationRun {
  return {
    id,
    monitor_id: 1,
    status: 'success',
    trigger_source: 'schedule',
    model: 'gpt-5.5',
    response_model: '',
    reasoning_effort: 'high',
    protocol: 'openai_responses',
    prompt_name: 'Pelican',
    output_bytes: 1024,
    error_message: '',
    attempts: 1,
    first_token_ms: 1200,
    duration_ms: 30000,
    input_tokens: 10,
    output_tokens: 20,
    cache_read_tokens: 0,
    cache_creation_tokens: 0,
    reasoning_tokens: 0,
    total_tokens: 30,
    cache_rate: null,
    cost: 0.01,
    usage_synced: true,
    started_at: '2026-11-03T08:00:00Z',
    finished_at: '2026-11-03T08:01:00Z',
    ...overrides,
  }
}

function makeMonitor(id: number, platform: string, runs: DegradationRun[], overrides: Partial<DegradationUserMonitor> = {}): DegradationUserMonitor {
  return {
    id,
    name: `Monitor ${id}`,
    group_name: `Group ${id}`,
    platform,
    model: 'gpt-5.5',
    protocol: 'openai_responses',
    reasoning_effort: 'high',
    interval_minutes: 60,
    last_run_at: '2026-11-03T08:00:00Z',
    runs,
    ...overrides,
  }
}

const wrappers: VueWrapper[] = []
const originalClientWidth = Object.getOwnPropertyDescriptor(HTMLElement.prototype, 'clientWidth')

function mountView(): VueWrapper {
  const i18n = createI18n({ legacy: false, locale: 'en', messages: { en: { ...enCommon, ...enDegradation } } })
  const wrapper = mount(DegradationCheckView, {
    global: {
      plugins: [i18n],
      stubs: { AppLayout: AppLayoutStub, Icon: true, PlatformIcon: true, DegradationRunCard: CardStub, DegradationRunDetailDialog: DetailStub },
    },
  })
  wrappers.push(wrapper)
  return wrapper
}

describe('DegradationCheckView', () => {
  beforeEach(() => {
    getOverview.mockReset()
    // A 1000px gallery fits five cards per row.
    Object.defineProperty(HTMLElement.prototype, 'clientWidth', { configurable: true, get: () => 1000 })
  })

  afterEach(() => {
    for (const wrapper of wrappers.splice(0)) wrapper.unmount()
    if (originalClientWidth) Object.defineProperty(HTMLElement.prototype, 'clientWidth', originalClientWidth)
  })

  it('groups monitors by platform, hides empty platforms and shows one row until expanded', async () => {
    const runs = Array.from({ length: 7 }, (_, index) => makeRun(100 - index))
    const overview: DegradationOverview = {
      enabled: true,
      display_limit: 25,
      monitors: [
        makeMonitor(1, 'openai', runs),
        makeMonitor(2, 'anthropic', [makeRun(50)], { model: 'claude-opus', protocol: 'anthropic' }),
        makeMonitor(3, 'composite', []),
      ],
    }
    getOverview.mockResolvedValue(overview)

    const wrapper = mountView()
    await flushPromises()

    const platforms = wrapper.findAll('[data-testid="degradation-platform"]').map((section) => section.attributes('data-platform'))
    expect(platforms).toEqual(['anthropic', 'openai', 'custom'])
    expect(wrapper.text()).not.toContain('Gemini')

    const openai = wrapper.findAll('[data-testid="degradation-monitor"]').find((monitor) => monitor.text().includes('Monitor 1'))!
    expect(openai.findAll('.run-card')).toHaveLength(5)
    await openai.get('.dg-expand').trigger('click')
    expect(openai.findAll('.run-card')).toHaveLength(7)
    expect(openai.get('.dg-expand').text()).toContain('Collapse')
    await openai.get('.dg-expand').trigger('click')
    expect(openai.findAll('.run-card')).toHaveLength(5)

    const custom = wrapper.findAll('[data-testid="degradation-monitor"]').find((monitor) => monitor.text().includes('Monitor 3'))!
    expect(custom.text()).toContain('No results yet')
    expect(custom.find('.dg-expand').exists()).toBe(false)
  })

  it('opens the detail dialog for the clicked animation', async () => {
    getOverview.mockResolvedValue({ enabled: true, display_limit: 25, monitors: [makeMonitor(1, 'openai', [makeRun(9), makeRun(8)])] })
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('.run-card[data-run="8"]').trigger('click')
    expect(wrapper.get('.detail-stub').attributes('data-run')).toBe('8')
    expect(wrapper.get('.detail-stub').text()).toBe('Monitor 1')
  })

  it('explains when the feature is disabled or nothing is configured', async () => {
    getOverview.mockResolvedValueOnce({ enabled: false, display_limit: 25, monitors: [] })
    const disabled = mountView()
    await flushPromises()
    expect(disabled.text()).toContain('Degradation checks are not enabled')

    getOverview.mockResolvedValueOnce({ enabled: true, display_limit: 25, monitors: [] })
    const empty = mountView()
    await flushPromises()
    expect(empty.find('[data-testid="degradation-empty"]').exists()).toBe(true)
  })
})
