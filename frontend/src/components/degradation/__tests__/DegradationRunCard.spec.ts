import { defineComponent } from 'vue'
import { mount, type VueWrapper } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { afterEach, describe, expect, it, vi } from 'vitest'

import type { DegradationRun } from '@/api/degradation'
import enDegradation from '@/i18n/locales/en/degradation'
import DegradationRunCard from '@/components/degradation/DegradationRunCard.vue'

// The test config aliases i18n to its runtime-only build; real locale strings need the compiler.
vi.mock('vue-i18n', async () => {
  const { createRequire } = await import('node:module')
  const fullBuild = createRequire(import.meta.url).resolve('vue-i18n/dist/vue-i18n.esm-bundler.js')
  return vi.importActual<typeof import('vue-i18n')>(fullBuild)
})

const PosterStub = defineComponent({
  props: { run: { type: Object, required: true }, title: { type: String, required: true } },
  template: '<div class="poster-stub" :data-run="run.id">{{ title }}</div>',
})

function makeRun(overrides: Partial<DegradationRun> = {}): DegradationRun {
  return {
    id: 4,
    monitor_id: 1,
    status: 'success',
    model: 'gpt-6-astra',
    response_model: '',
    reasoning_effort: 'high',
    protocol: 'openai_responses',
    prompt_name: 'Pelican',
    duration_ms: 1000,
    started_at: '2026-11-03T14:11:34Z',
    finished_at: '2026-11-03T14:15:22Z',
    ...overrides,
  }
}

const wrappers: VueWrapper[] = []

function mountCard(run: DegradationRun): VueWrapper {
  const i18n = createI18n({ legacy: false, locale: 'en', messages: { en: enDegradation } })
  const wrapper = mount(DegradationRunCard, {
    props: { run, title: 'Astra' },
    global: { plugins: [i18n], stubs: { DegradationRunPoster: PosterStub, Icon: true } },
  })
  wrappers.push(wrapper)
  return wrapper
}

describe('DegradationRunCard', () => {
  afterEach(() => {
    for (const wrapper of wrappers.splice(0)) wrapper.unmount()
  })

  it('shows a still poster and opens the detail on click', async () => {
    const wrapper = mountCard(makeRun())
    expect(wrapper.get('.poster-stub').attributes('data-run')).toBe('4')
    expect(wrapper.find('iframe').exists()).toBe(false)
    expect(wrapper.text()).toContain('gpt-6-astra')

    await wrapper.get('button').trigger('click')
    expect(wrapper.emitted('open')).toEqual([[4]])
  })

  it('marks a failure without saying why', () => {
    const run = { ...makeRun({ status: 'failed' }), error_message: 'upstream HTTP 502: sk-live-secret' } as DegradationRun
    const wrapper = mountCard(run)
    expect(wrapper.find('.poster-stub').exists()).toBe(false)
    expect(wrapper.text()).toContain('Failed')
    expect(wrapper.text()).not.toContain('sk-live-secret')
    expect(wrapper.text()).not.toContain('502')
  })
})
