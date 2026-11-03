import { defineComponent } from 'vue'
import { flushPromises, mount, RouterLinkStub, type VueWrapper } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import type { DegradationConfig, DegradationMonitor, DegradationPrompt } from '@/api/admin/degradation'
import enCommon from '@/i18n/locales/en/common'
import enDegradation from '@/i18n/locales/en/degradation'
import enAdminDegradation from '@/i18n/locales/en/admin/degradation'
import DegradationMonitorView from '@/views/admin/DegradationMonitorView.vue'

const { degradationAPI, showSuccess, showError } = vi.hoisted(() => ({
  degradationAPI: {
    listMonitors: vi.fn(),
    listPrompts: vi.fn(),
    updateConfig: vi.fn(),
    updateSortOrder: vi.fn(),
    runMonitor: vi.fn(),
    deleteMonitor: vi.fn(),
  },
  showSuccess: vi.fn(),
  showError: vi.fn(),
}))

vi.mock('@/api/admin', () => ({ adminAPI: { degradation: degradationAPI } }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showSuccess, showError }) }))
vi.mock('vue-i18n', async () => {
  const { createRequire } = await import('node:module')
  const fullBuild = createRequire(import.meta.url).resolve('vue-i18n/dist/vue-i18n.esm-bundler.js')
  return vi.importActual<typeof import('vue-i18n')>(fullBuild)
})

const SlotStub = defineComponent({ template: '<div><slot /></div>' })
const DialogStub = defineComponent({
  props: { show: Boolean, title: { type: String, default: '' } },
  template: '<div v-if="show" class="dialog-stub"><h3>{{ title }}</h3><slot /><slot name="footer" /></div>',
})

const config: DegradationConfig = {
  default_interval_minutes: 60,
  timeout_minutes: 10,
  max_retries: 1,
  max_display_per_group: 25,
  default_reasoning_effort: 'high',
  default_prompt_id: 0,
}

function monitor(id: number, platform: DegradationMonitor['group_platform'], sortOrder: number, overrides: Partial<DegradationMonitor> = {}): DegradationMonitor {
  return {
    id,
    name: `Check ${id}`,
    group_id: id * 10,
    group_name: `Group ${id}`,
    group_platform: platform,
    group_status: 'active',
    group_deleted: false,
    model: 'gpt-5.5',
    reasoning_effort: '',
    effective_reasoning_effort: 'high',
    protocol: 'openai_responses',
    interval_minutes: 0,
    effective_interval_minutes: 60,
    prompt_id: null,
    prompt_name: '',
    random_prompt: false,
    enabled: true,
    sort_order: sortOrder,
    running: false,
    last_run_at: null,
    last_result_status: '',
    last_result_at: null,
    stored_runs: 0,
    created_at: '2026-11-03T00:00:00Z',
    updated_at: '2026-11-03T00:00:00Z',
    ...overrides,
  }
}

const prompts: DegradationPrompt[] = [
  { id: 1, name: 'Pelican', prompt: 'draw a pelican', builtin_key: 'pelican', sort_order: 10, created_at: '', updated_at: '' },
  { id: 2, name: 'Clock', prompt: 'draw a clock', builtin_key: 'clock', sort_order: 20, created_at: '', updated_at: '' },
]

const wrappers: VueWrapper[] = []

function mountView(): VueWrapper {
  const i18n = createI18n({
    legacy: false,
    locale: 'en',
    messages: { en: { ...enCommon, ...enDegradation, admin: { ...enAdminDegradation } } },
  })
  const wrapper = mount(DegradationMonitorView, {
    global: {
      plugins: [i18n],
      stubs: {
        AppLayout: SlotStub,
        Icon: true,
        PlatformIcon: true,
        LoadingSpinner: true,
        EmptyState: true,
        Select: true,
        BaseDialog: DialogStub,
        ConfirmDialog: true,
        SortableScrollArea: SlotStub,
        VueDraggable: SlotStub,
        DegradationMonitorFormDialog: true,
        DegradationPromptDialog: true,
        RouterLink: RouterLinkStub,
      },
    },
  })
  wrappers.push(wrapper)
  return wrapper
}

describe('DegradationMonitorView', () => {
  beforeEach(() => {
    for (const fn of Object.values(degradationAPI)) fn.mockReset()
    showSuccess.mockReset()
    showError.mockReset()
    degradationAPI.listMonitors.mockResolvedValue({
      items: [
        monitor(1, 'openai', 20),
        monitor(2, 'openai', 10, { name: '' }),
        monitor(3, 'anthropic', 5, { protocol: 'anthropic', model: 'claude-opus' }),
      ],
      config,
      enabled: true,
    })
    degradationAPI.listPrompts.mockResolvedValue(prompts)
    degradationAPI.updateConfig.mockImplementation(async (value: DegradationConfig) => value)
    degradationAPI.updateSortOrder.mockResolvedValue(undefined)
    degradationAPI.runMonitor.mockResolvedValue(undefined)
  })

  afterEach(() => {
    for (const wrapper of wrappers.splice(0)) wrapper.unmount()
  })

  it('lists only platforms with check groups, collapsed until opened', async () => {
    const wrapper = mountView()
    await flushPromises()

    const sections = wrapper.findAll('[data-testid="degradation-admin-platform"]')
    expect(sections.map((section) => section.attributes('data-platform'))).toEqual(['anthropic', 'openai'])
    expect(wrapper.findAll('[data-testid="degradation-admin-row"]')).toHaveLength(0)

    await sections[1].get('.dgm-platform-toggle').trigger('click')
    const rows = wrapper.findAll('[data-testid="degradation-admin-row"]')
    expect(rows.map((row) => row.get('.dgm-row-name').text())).toEqual(['Group 2', 'Check 1'])
    expect(rows[0].text()).toContain('Every 60 min · Default')
  })

  it('reorders one platform and starts manual checks', async () => {
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="degradation-sort-openai"]').trigger('click')
    expect(wrapper.get('.dialog-stub').text()).toContain('OpenAI · Order')
    await wrapper.get('[data-testid="degradation-sort-save"]').trigger('click')
    await flushPromises()
    expect(degradationAPI.updateSortOrder).toHaveBeenCalledWith([
      { id: 2, sort_order: 10 },
      { id: 1, sort_order: 20 },
    ])

    await wrapper.findAll('.dgm-platform-toggle')[0].trigger('click')
    await wrapper.get('[data-testid="degradation-run-now"]').trigger('click')
    await flushPromises()
    expect(degradationAPI.runMonitor).toHaveBeenCalledWith(3)
    expect(showSuccess).toHaveBeenCalledWith('Check started; results refresh when it finishes')
  })

  it('selects the default preset immediately and saves the settings form', async () => {
    const wrapper = mountView()
    await flushPromises()
    await wrapper.get('[data-testid="degradation-tab-config"]').trigger('click')

    const presets = wrapper.findAll('[data-testid="degradation-preset"]')
    expect(presets).toHaveLength(2)
    expect(presets[0].classes()).toContain('is-default')
    await presets[1].get('.dgm-preset-select').trigger('click')
    await flushPromises()
    expect(degradationAPI.updateConfig).toHaveBeenLastCalledWith({ ...config, default_prompt_id: 2 })

    expect(wrapper.get('[data-testid="degradation-config-save"]').attributes('disabled')).toBeDefined()
    await wrapper.get('[data-testid="degradation-config-timeout"]').setValue('25')
    await wrapper.get('[data-testid="degradation-config-display"]').setValue('40')
    await wrapper.get('[data-testid="degradation-config-form"]').trigger('submit')
    await flushPromises()
    expect(degradationAPI.updateConfig).toHaveBeenLastCalledWith({
      ...config,
      timeout_minutes: 25,
      max_display_per_group: 40,
      default_prompt_id: 2,
    })
  })

  it('warns when the feature switch is off', async () => {
    degradationAPI.listMonitors.mockResolvedValue({ items: [], config, enabled: false })
    const wrapper = mountView()
    await flushPromises()
    expect(wrapper.text()).toContain('Degradation checks are disabled')
  })
})
