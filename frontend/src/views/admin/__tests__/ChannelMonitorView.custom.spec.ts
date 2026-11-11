import { defineComponent } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import MonitorFiltersBar from '@/components/admin/monitor/MonitorFiltersBar.vue'
import MonitorFormDialog from '@/components/admin/monitor/MonitorFormDialog.vue'
import Select from '@/components/common/Select.vue'
import type { AdminGroup, GroupPlatform } from '@/types'
import {
  PROVIDERS,
  PROVIDER_CUSTOM,
} from '@/constants/channelMonitor'

const { listTemplates, getAllGroups, createMonitor, showError } = vi.hoisted(() => ({
  listTemplates: vi.fn(),
  getAllGroups: vi.fn(),
  createMonitor: vi.fn(),
  showError: vi.fn(),
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    channelMonitor: {
      create: createMonitor,
      update: vi.fn(),
    },
    groups: {
      getAll: getAllGroups,
    },
    channelMonitorTemplate: {
      list: listTemplates,
    },
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
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key }),
  }
})

const BaseDialogStub = defineComponent({
  props: { show: { type: Boolean, default: false } },
  template: '<div v-if="show"><slot /><slot name="footer" /></div>',
})

function mountDialog() {
  return mount(MonitorFormDialog, {
    props: { show: true, monitor: null },
    global: {
      stubs: {
        BaseDialog: BaseDialogStub,
        Toggle: true,
        ModelTagInput: true,
        MonitorAdvancedRequestConfig: true,
      },
    },
  })
}

function group(id: number, platform: GroupPlatform, rateMultiplier = 1): AdminGroup {
  return {
    id,
    name: `${platform} group`,
    platform,
    rate_multiplier: rateMultiplier,
    status: 'active',
  } as AdminGroup
}

function groupSelect(wrapper: ReturnType<typeof mountDialog>) {
  const select = wrapper.findAllComponents(Select).find(item => (
    item.props('id') === 'channel-monitor-group'
  ))
  if (!select) throw new Error('group select not found')
  return select
}

describe('channel monitor Custom provider', () => {
  beforeEach(() => {
    listTemplates.mockReset().mockResolvedValue({ items: [] })
    getAllGroups.mockReset().mockResolvedValue([])
    createMonitor.mockReset().mockResolvedValue({})
    showError.mockReset()
  })

  it('offers Custom as the fifth provider with the jigsaw icon', async () => {
    const wrapper = mountDialog()
    await flushPromises()

    expect(PROVIDERS).toContain(PROVIDER_CUSTOM)
    const providerButtons = wrapper.findAll('[data-testid^="monitor-provider-"]')
    expect(providerButtons).toHaveLength(5)
    expect(providerButtons[0].element.parentElement?.className).toContain('sm:grid-cols-3')
    expect(providerButtons[0].element.parentElement?.className).toContain('md:grid-cols-5')

    const customButton = wrapper.get('[data-testid="monitor-provider-custom"]')
    expect(customButton.text()).toContain('monitorCommon.providers.custom')
    expect(customButton.get('svg').attributes('viewBox')).toBe('0 0 24 24')
    expect(customButton.get('svg').attributes('fill')).toBe('currentColor')
    expect(customButton.findAll('path')).toHaveLength(1)
  })

  it('includes Custom in the admin provider filter', () => {
    const wrapper = mount(MonitorFiltersBar, {
      props: {
        loading: false,
        search: '',
        provider: '',
        enabled: '',
        'onUpdate:search': () => undefined,
        'onUpdate:provider': () => undefined,
        'onUpdate:enabled': () => undefined,
      },
    })

    const options = wrapper.findAllComponents(Select)[0].props('options') as Array<{
      value: string
      label: string
    }>
    expect(options).toContainEqual({
      value: PROVIDER_CUSTOM,
      label: 'monitorCommon.providers.custom',
    })
  })

  it('shows only Custom groups with their default rate when Custom is selected', async () => {
    const groups = [
      group(1, 'openai', 0.1),
      group(2, 'anthropic'),
      group(3, 'gemini'),
      group(4, 'grok'),
      group(5, 'antigravity'),
      group(6, 'composite', 0.1),
    ]
    getAllGroups.mockResolvedValue(groups)
    const wrapper = mountDialog()
    await flushPromises()

    await wrapper.get('[data-testid="monitor-provider-custom"]').trigger('click')
    const select = groupSelect(wrapper)
    const options = select.props('options') as Array<{
      value: number
      platform: GroupPlatform
      rate_multiplier: number
    }>

    expect(getAllGroups).toHaveBeenCalledTimes(1)
    expect(options.map(option => option.platform)).toEqual(['composite'])
    expect(options.find(option => option.value === 6)?.rate_multiplier).toBe(0.1)

    select.vm.$emit('update:modelValue', 6)
    await wrapper.vm.$nextTick()
    const rate = wrapper.get('[data-testid="monitor-group-rate"]')
    expect(rate.text()).toBe('0.1x')
    expect(rate.classes()).toContain('rounded-md')
    expect(rate.classes()).toContain('bg-gray-100')
    wrapper.unmount()
  })

  it('offers Responses mode for Custom and submits the selected API mode', async () => {
    getAllGroups.mockResolvedValue([group(6, 'composite', 0.1)])
    const wrapper = mountDialog()
    await flushPromises()

    await wrapper.get('[data-testid="monitor-provider-custom"]').trigger('click')
    groupSelect(wrapper).vm.$emit('update:modelValue', 6)
    await wrapper.get('[data-testid="monitor-primary-model"]').setValue('custom-response-model')

    const responsesButton = wrapper.findAll('button').find(button => (
      button.text().includes('admin.channelMonitor.form.apiModeResponses')
    ))
    expect(responsesButton).toBeDefined()
    await responsesButton?.trigger('click')
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    expect(showError).not.toHaveBeenCalled()
    expect(createMonitor).toHaveBeenCalledWith(expect.objectContaining({
      provider: 'custom',
      group_id: 6,
      api_mode: 'responses',
      primary_model: 'custom-response-model',
    }))
    wrapper.unmount()
  })

  it('filters fixed platforms and clears a selection when the platform changes', async () => {
    getAllGroups.mockResolvedValue([
      group(1, 'openai', 0.1),
      group(2, 'anthropic', 0.2),
      group(3, 'composite', 0.3),
    ])
    const wrapper = mountDialog()
    await flushPromises()

    const select = groupSelect(wrapper)
    expect((select.props('options') as Array<{ value: number }>).map(option => option.value))
      .toEqual([2])

    select.vm.$emit('update:modelValue', 2)
    await wrapper.vm.$nextTick()
    expect(select.props('modelValue')).toBe(2)

    await wrapper.get('[data-testid="monitor-provider-openai"]').trigger('click')
    expect(select.props('modelValue')).toBeNull()
    expect((select.props('options') as Array<{ value: number }>).map(option => option.value))
      .toEqual([1])

    await wrapper.get('[data-testid="monitor-provider-custom"]').trigger('click')
    expect((select.props('options') as Array<{ value: number }>).map(option => option.value))
      .toEqual([3])
    wrapper.unmount()
  })

  it('keeps the name optional and submits the selected group without API-key fields', async () => {
    getAllGroups.mockResolvedValue([group(2, 'anthropic', 0.1)])
    const wrapper = mountDialog()
    await flushPromises()

    const name = wrapper.get('input[aria-describedby="channel-monitor-name-hint"]')
    expect(name.attributes('required')).toBeUndefined()
    expect(wrapper.text()).toContain('admin.channelMonitor.form.nameHint')
    expect(wrapper.text()).not.toContain('admin.channelMonitor.form.apiKey')
    expect(wrapper.find('[data-testid="monitor-endpoint"]').exists()).toBe(false)

    groupSelect(wrapper).vm.$emit('update:modelValue', 2)
    await wrapper.get('[data-testid="monitor-primary-model"]').setValue('claude-sonnet-4-5')
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    expect(showError).not.toHaveBeenCalled()
    expect(createMonitor).toHaveBeenCalledTimes(1)
    const payload = createMonitor.mock.calls[0][0]
    expect(payload).toMatchObject({ name: '', group_id: 2, provider: 'anthropic' })
    expect(payload).not.toHaveProperty('api_key')
    expect(payload).not.toHaveProperty('endpoint')
    expect(payload).not.toHaveProperty('group_name')
    wrapper.unmount()
  })

  it('requires a group before creating a monitor', async () => {
    const wrapper = mountDialog()
    await flushPromises()

    await wrapper.get('[data-testid="monitor-primary-model"]').setValue('claude-sonnet-4-5')
    await wrapper.get('form').trigger('submit')

    expect(createMonitor).not.toHaveBeenCalled()
    expect(showError).toHaveBeenCalledWith('admin.channelMonitor.groupRequired')
    wrapper.unmount()
  })
})
