import { defineComponent, type PropType } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import type { ChannelMonitor } from '@/api/admin/channelMonitor'
import ChannelMonitorView from '@/views/admin/ChannelMonitorView.vue'

const {
  listMonitors,
  updateSortOrder,
  showSuccess,
  showError,
} = vi.hoisted(() => ({
  listMonitors: vi.fn(),
  updateSortOrder: vi.fn(),
  showSuccess: vi.fn(),
  showError: vi.fn(),
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    channelMonitor: {
      list: listMonitors,
      updateSortOrder,
      update: vi.fn(),
      duplicate: vi.fn(),
      runNow: vi.fn(),
      del: vi.fn(),
    },
  },
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showSuccess, showError }),
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key }),
  }
})

const AppLayoutStub = defineComponent({
  template: '<main><slot /></main>',
})

const TablePageLayoutStub = defineComponent({
  template: '<section><slot name="filters" /><slot name="table" /><slot name="pagination" /></section>',
})

const MonitorFiltersBarStub = defineComponent({
  emits: ['sort'],
  template: '<button data-testid="open-monitor-sort" @click="$emit(\'sort\')">sort</button>',
})

const BaseDialogStub = defineComponent({
  props: { show: { type: Boolean, default: false } },
  template: '<div v-if="show" data-testid="sort-dialog"><slot /><slot name="footer" /></div>',
})

const VueDraggableStub = defineComponent({
  props: {
    modelValue: {
      type: Array as PropType<ChannelMonitor[]>,
      required: true,
    },
    animation: {
      type: Number,
      default: undefined,
    },
    forceFallback: {
      type: Boolean,
      default: false,
    },
    fallbackOnBody: {
      type: Boolean,
      default: false,
    },
    fallbackTolerance: {
      type: Number,
      default: 0,
    },
  },
  emits: ['update:modelValue'],
  setup(props, { emit }) {
    return {
      reverse() {
        emit('update:modelValue', [...props.modelValue].reverse())
      },
    }
  },
  template: '<div><button data-testid="reverse-list" @click="reverse">reverse</button><slot /></div>',
})

function makeMonitor(overrides: Partial<ChannelMonitor> = {}): ChannelMonitor {
  return {
    id: 1,
    sort_order: 0,
    name: 'Monitor',
    provider: 'openai',
    api_mode: 'chat_completions',
    endpoint: 'https://api.example.com',
    primary_model: 'gpt-4o-mini',
    extra_models: [],
    group_id: 1,
    group_name: '',
    group_rate_multiplier: 1,
    enabled: true,
    simulate_requests: false,
    interval_seconds: 60,
    jitter_seconds: 0,
    last_checked_at: null,
    created_by: 1,
    created_at: '2026-09-19T00:00:00Z',
    updated_at: '2026-09-19T00:00:00Z',
    primary_status: '',
    primary_latency_ms: null,
    availability_7d: 0,
    extra_models_status: [],
    template_id: null,
    extra_headers: {},
    body_override_mode: 'off',
    body_override: null,
    ...overrides,
  }
}

function listResponse(items: ChannelMonitor[], page: number, pages: number) {
  return {
    items,
    total: 4,
    page,
    page_size: 100,
    pages,
  }
}

function mountView() {
  return mount(ChannelMonitorView, {
    global: {
      stubs: {
        AppLayout: AppLayoutStub,
        TablePageLayout: TablePageLayoutStub,
        MonitorFiltersBar: MonitorFiltersBarStub,
        DataTable: true,
        Pagination: true,
        BaseDialog: BaseDialogStub,
        ConfirmDialog: true,
        EmptyState: true,
        HelpTooltip: true,
        Icon: true,
        Toggle: true,
        MonitorFormDialog: true,
        MonitorTemplateManagerDialog: true,
        MonitorRunResultDialog: true,
        MonitorPrimaryModelCell: true,
        ProviderIcon: true,
        VueDraggable: VueDraggableStub,
      },
    },
  })
}

describe('ChannelMonitorView sorting', () => {
  beforeEach(() => {
    for (const fn of [listMonitors, updateSortOrder, showSuccess, showError]) fn.mockReset()
    updateSortOrder.mockResolvedValue({ message: 'ok' })

    listMonitors.mockImplementation((params: { page?: number; page_size?: number }) => {
      if (params.page_size !== 100) {
        return Promise.resolve({
          items: [],
          total: 0,
          page: 1,
          page_size: params.page_size || 20,
          pages: 0,
        })
      }
      if (params.page === 1) {
        return Promise.resolve(listResponse([
          makeMonitor({ id: 1, name: 'OpenAI later', provider: 'openai', sort_order: 20 }),
          makeMonitor({ id: 2, name: 'Anthropic', provider: 'anthropic', sort_order: 0 }),
        ], 1, 2))
      }
      return Promise.resolve(listResponse([
        makeMonitor({ id: 3, name: 'OpenAI first', provider: 'openai', sort_order: 10 }),
        makeMonitor({ id: 4, name: 'Custom', provider: 'custom', sort_order: 30 }),
      ], 2, 2))
    })
  })

  it('loads every page and persists a global deterministic order', async () => {
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="open-monitor-sort"]').trigger('click')
    await flushPromises()

    expect(listMonitors).toHaveBeenCalledWith({ page: 1, page_size: 100 })
    expect(listMonitors).toHaveBeenCalledWith({ page: 2, page_size: 100 })

    const sortList = wrapper.get('[data-testid="channel-monitor-sort-list"]')
    const draggable = wrapper.findComponent(VueDraggableStub)
    expect(draggable.props('animation')).toBe(0)
    expect(draggable.props('forceFallback')).toBe(true)
    expect(draggable.props('fallbackOnBody')).toBe(true)
    expect(draggable.props('fallbackTolerance')).toBe(4)
    expect(sortList.findAll('[data-monitor-id]').map(row => row.attributes('data-monitor-id')))
      .toEqual(['2', '3', '1', '4'])

    await sortList.get('[data-testid="reverse-list"]').trigger('click')
    await wrapper.get('[data-testid="channel-monitor-sort-save"]').trigger('click')
    await flushPromises()

    expect(updateSortOrder).toHaveBeenCalledWith([
      { id: 4, sort_order: 0 },
      { id: 1, sort_order: 10 },
      { id: 3, sort_order: 20 },
      { id: 2, sort_order: 30 },
    ])
    expect(showSuccess).toHaveBeenCalledWith('admin.channelMonitor.sortOrderUpdated')
    wrapper.unmount()
  })
})
