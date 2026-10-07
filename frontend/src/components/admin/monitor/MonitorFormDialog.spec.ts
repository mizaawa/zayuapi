import { defineComponent } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import type { ChannelMonitor } from '@/api/admin/channelMonitor'
import MonitorFormDialog from './MonitorFormDialog.vue'
import Select from '@/components/common/Select.vue'

const { createMonitor, updateMonitor, showError } = vi.hoisted(() => ({
  createMonitor: vi.fn(),
  updateMonitor: vi.fn(),
  showError: vi.fn(),
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    channelMonitor: { create: createMonitor, update: updateMonitor },
    groups: {
      getAll: vi.fn().mockResolvedValue([
        { id: 1, name: 'Group', platform: 'anthropic', rate_multiplier: 1, status: 'active' },
      ]),
    },
    channelMonitorTemplate: { list: vi.fn().mockResolvedValue({ items: [] }) },
  },
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    cachedPublicSettings: null,
    showError,
    showSuccess: vi.fn(),
  }),
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return { ...actual, useI18n: () => ({ t: (key: string) => key }) }
})

const BaseDialogStub = defineComponent({
  props: { show: { type: Boolean, default: false } },
  template: '<div v-if="show"><slot /><slot name="footer" /></div>',
})

function monitor(simulateRequests: boolean): ChannelMonitor {
  return {
    id: 1,
    sort_order: 0,
    name: 'Monitor',
    provider: 'anthropic',
    api_mode: 'chat_completions',
    endpoint: '',
    primary_model: 'claude-sonnet-4-5',
    extra_models: [],
    group_id: 1,
    group_name: 'Group',
    group_rate_multiplier: 1,
    enabled: true,
    simulate_requests: simulateRequests,
    interval_seconds: 60,
    jitter_seconds: 0,
    last_checked_at: null,
    created_by: 1,
    created_at: '2026-10-07T00:00:00Z',
    updated_at: '2026-10-07T00:00:00Z',
    primary_status: '',
    primary_latency_ms: null,
    availability_7d: 0,
    extra_models_status: [],
    template_id: null,
    extra_headers: {},
    body_override_mode: 'off',
    body_override: null,
  }
}

function mountDialog(existing: ChannelMonitor | null = null) {
  return mount(MonitorFormDialog, {
    props: { show: true, monitor: existing },
    global: {
      stubs: {
        BaseDialog: BaseDialogStub,
        Select: true,
        ModelTagInput: true,
        MonitorAdvancedRequestConfig: true,
      },
    },
  })
}

async function fillNewMonitor(wrapper: ReturnType<typeof mountDialog>) {
  wrapper.findComponent(Select).vm.$emit('update:modelValue', 1)
  await wrapper.get('[data-testid="monitor-primary-model"]').setValue('claude-sonnet-4-5')
}

describe('channel monitor simulated requests', () => {
  beforeEach(() => {
    createMonitor.mockReset().mockResolvedValue({})
    updateMonitor.mockReset().mockResolvedValue({})
    showError.mockReset()
  })

  it.each([false, true])('creates a monitor with simulate_requests=%s', async simulateRequests => {
    const wrapper = mountDialog()
    await flushPromises()
    const toggle = wrapper.get('#channel-monitor-simulate-requests')
    expect(toggle.attributes('aria-checked')).toBe('false')
    expect(toggle.attributes('aria-label')).toBe('admin.channelMonitor.form.simulateRequests')
    expect(toggle.element.parentElement?.previousElementSibling?.textContent)
      .toContain('admin.channelMonitor.form.enabled')

    await fillNewMonitor(wrapper)
    if (simulateRequests) await toggle.trigger('click')
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    expect(showError).not.toHaveBeenCalled()
    expect(createMonitor).toHaveBeenCalledWith(expect.objectContaining({
      simulate_requests: simulateRequests,
    }))
    expect(wrapper.emitted('saved')).toHaveLength(1)
    wrapper.unmount()
  })

  it.each([false, true])('loads simulation and saves simulate_requests=%s when editing', async simulateRequests => {
    const wrapper = mountDialog(monitor(true))
    await flushPromises()
    const toggle = wrapper.get('#channel-monitor-simulate-requests')
    expect(toggle.attributes('aria-checked')).toBe('true')
    if (!simulateRequests) await toggle.trigger('click')
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    expect(showError).not.toHaveBeenCalled()
    expect(updateMonitor).toHaveBeenCalledWith(1, expect.objectContaining({
      simulate_requests: simulateRequests,
    }))
    expect(createMonitor).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('resets simulation when changing the edited monitor or reopening creation', async () => {
    const wrapper = mountDialog(monitor(true))
    await flushPromises()
    expect(wrapper.get('#channel-monitor-simulate-requests').attributes('aria-checked')).toBe('true')

    await wrapper.setProps({ monitor: { ...monitor(false), id: 2 } })
    expect(wrapper.get('#channel-monitor-simulate-requests').attributes('aria-checked')).toBe('false')
    await wrapper.get('#channel-monitor-simulate-requests').trigger('click')
    await wrapper.setProps({ show: false })
    await wrapper.setProps({ show: true, monitor: null })
    expect(wrapper.get('#channel-monitor-simulate-requests').attributes('aria-checked')).toBe('false')

    await fillNewMonitor(wrapper)
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(createMonitor).toHaveBeenCalledWith(expect.objectContaining({ simulate_requests: false }))
    expect(updateMonitor).not.toHaveBeenCalled()
    wrapper.unmount()
  })
})
