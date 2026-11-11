import { defineComponent } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import type { AdminGroup } from '@/types'
import GroupsView from '@/views/admin/GroupsView.vue'

const {
  listGroups,
  updateGroup,
  getModelsListCandidates,
  getUsageSummary,
  getCapacitySummary,
  getLiveCapability,
  showSuccess,
  showError
} = vi.hoisted(() => ({
  listGroups: vi.fn(),
  updateGroup: vi.fn(),
  getModelsListCandidates: vi.fn(),
  getUsageSummary: vi.fn(),
  getCapacitySummary: vi.fn(),
  getLiveCapability: vi.fn(),
  showSuccess: vi.fn(),
  showError: vi.fn()
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    groups: {
      list: listGroups,
      create: vi.fn(),
      delete: vi.fn(),
      duplicate: vi.fn(),
      getModelsListCandidates,
      getUsageSummary,
      getCapacitySummary,
      getLiveCapability,
      getAll: vi.fn().mockResolvedValue([]),
      update: updateGroup,
      updateSortOrder: vi.fn()
    },
    accounts: {
      list: vi.fn(),
      getById: vi.fn()
    }
  }
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showSuccess, showError, showWarning: vi.fn(), showInfo: vi.fn() })
}))

vi.mock('@/stores/onboarding', () => ({
  useOnboardingStore: () => ({
    isCurrentStep: vi.fn(() => false),
    nextStep: vi.fn()
  })
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key })
  }
})

const group = {
  id: 42,
  name: 'GPT',
  description: null,
  platform: 'openai',
  rate_multiplier: 1,
  rpm_limit: 0,
  is_exclusive: false,
  status: 'active',
  subscription_type: 'standard',
  daily_limit_usd: null,
  weekly_limit_usd: null,
  monthly_limit_usd: null,
  allow_image_generation: false,
  claude_code_only: false,
  fallback_group_id: null,
  fallback_group_id_on_invalid_request: null,
  fast_rate_enabled: false,
  fast_rate_multiplier: 2,
  created_at: '2026-10-01T00:00:00Z',
  updated_at: '2026-10-01T00:00:00Z',
  model_routing: null,
  model_routing_enabled: false,
  supported_model_scopes: [],
  account_count: 1,
  active_account_count: 1,
  rate_limited_account_count: 0,
  sort_order: 10
} as unknown as AdminGroup

const AppLayoutStub = defineComponent({ template: '<main><slot /></main>' })

const TablePageLayoutStub = defineComponent({
  template: '<section><slot name="filters" /><slot name="table" /><slot name="pagination" /></section>'
})

const DataTableStub = defineComponent({
  props: {
    data: { type: Array, default: () => [] },
    columns: { type: Array, default: () => [] },
    loading: { type: Boolean, default: false }
  },
  template: '<div><div v-for="row in data" :key="row.id"><slot name="cell-actions" :row="row" /></div></div>'
})

const BaseDialogStub = defineComponent({
  name: 'BaseDialog',
  props: { show: { type: Boolean, default: false }, title: { type: String, default: '' } },
  emits: ['close'],
  template: '<div v-if="show" :data-dialog="title"><slot /><slot name="footer" /></div>'
})

function mountView() {
  return mount(GroupsView, {
    global: {
      stubs: {
        AppLayout: AppLayoutStub,
        TablePageLayout: TablePageLayoutStub,
        DataTable: DataTableStub,
        Pagination: true,
        BaseDialog: BaseDialogStub,
        ConfirmDialog: true,
        EmptyState: true,
        Select: true,
        PlatformIcon: true,
        Icon: true,
        GroupCapacityBadge: true,
        GroupRateMultipliersModal: true,
        GroupRPMOverridesModal: true,
        GroupSelector: true,
        ModelWhitelistSelector: true,
        VueDraggable: true
      }
    }
  })
}

describe('GroupsView Fast billing multiplier', () => {
  beforeEach(() => {
    localStorage.clear()
    vi.spyOn(console, 'error').mockImplementation(() => {})
    for (const fn of [
      listGroups, updateGroup, getModelsListCandidates, getUsageSummary,
      getCapacitySummary, getLiveCapability, showSuccess, showError
    ]) {
      fn.mockReset()
    }
    listGroups.mockResolvedValue({ items: [group], total: 1, page: 1, page_size: 20, pages: 1 })
    updateGroup.mockImplementation(async (_id: number, payload: Partial<AdminGroup>) => ({ ...group, ...payload }))
    getModelsListCandidates.mockResolvedValue([])
    getUsageSummary.mockResolvedValue([])
    getCapacitySummary.mockResolvedValue([])
    getLiveCapability.mockResolvedValue({ supported: false })
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('turns the Fast multiplier on with the themed switch and saves it', async () => {
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="group-edit"]').trigger('click')
    await flushPromises()

    const toggle = wrapper.get('[data-testid="edit-fast-rate-toggle"]')
    expect(toggle.attributes('role')).toBe('switch')
    expect(toggle.attributes('aria-checked')).toBe('false')
    expect(toggle.classes()).toContain('bg-gray-300')
    expect(wrapper.find('[data-testid="edit-fast-rate-multiplier"]').exists()).toBe(false)

    // The visible label is wired to the switch, so clicking the text works too.
    expect(wrapper.get('#edit-fast-rate-label').attributes('for')).toBe(toggle.attributes('id'))
    await wrapper.get('#edit-fast-rate-label').trigger('click')
    expect(toggle.attributes('aria-checked')).toBe('true')
    await toggle.trigger('click')
    expect(toggle.attributes('aria-checked')).toBe('false')
    await toggle.trigger('click')
    expect(toggle.attributes('aria-checked')).toBe('true')
    expect(toggle.classes()).toContain('bg-primary-500')
    expect(wrapper.get('[data-testid="edit-fast-rate"]').classes()).toContain('border-primary-300')
    await wrapper.get('[data-testid="edit-fast-rate-multiplier"]').setValue('3')

    await wrapper.get('form#edit-group-form').trigger('submit.prevent')
    await flushPromises()

    expect(updateGroup).toHaveBeenCalledTimes(1)
    expect(updateGroup).toHaveBeenCalledWith(42, expect.objectContaining({
      fast_rate_enabled: true,
      fast_rate_multiplier: 3
    }))
    wrapper.unmount()
  })
})
