import { defineComponent } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import type { AdminGroup } from '@/types'
import GroupsView from '@/views/admin/GroupsView.vue'

const {
  listGroups,
  createGroup,
  deleteGroup,
  getModelsListCandidates,
  getUsageSummary,
  getCapacitySummary,
  getLiveCapability,
  showSuccess,
  showError
} = vi.hoisted(() => ({
  listGroups: vi.fn(),
  createGroup: vi.fn(),
  deleteGroup: vi.fn(),
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
      create: createGroup,
      delete: deleteGroup,
      duplicate: vi.fn(),
      getModelsListCandidates,
      getUsageSummary,
      getCapacitySummary,
      getLiveCapability,
      getAll: vi.fn().mockResolvedValue([]),
      update: vi.fn(),
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
  name: 'Primary',
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

const deferred = <T>() => {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((res) => { resolve = res })
  return { promise, resolve }
}

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

const ConfirmDialogStub = defineComponent({
  props: ['show', 'loading', 'title'],
  emits: ['confirm', 'cancel'],
  template: `
    <div v-if="show" data-test="confirm-dialog" :data-loading="String(!!loading)">
      <button data-test="confirm-dialog-confirm" @click="$emit('confirm')">confirm</button>
    </div>
  `
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
        ConfirmDialog: ConfirmDialogStub,
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

describe('GroupsView pending actions', () => {
  beforeEach(() => {
    localStorage.clear()
    vi.spyOn(console, 'error').mockImplementation(() => {})
    for (const fn of [
      listGroups, createGroup, deleteGroup, getModelsListCandidates, getUsageSummary,
      getCapacitySummary, getLiveCapability, showSuccess, showError
    ]) {
      fn.mockReset()
    }
    listGroups.mockResolvedValue({ items: [group], total: 1, page: 1, page_size: 20, pages: 1 })
    getModelsListCandidates.mockResolvedValue([])
    getUsageSummary.mockResolvedValue([])
    getCapacitySummary.mockResolvedValue([])
    getLiveCapability.mockResolvedValue({ supported: false })
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('sends one delete and shows it as running until the server answers', async () => {
    const pending = deferred<{ message: string }>()
    deleteGroup.mockReturnValueOnce(pending.promise)
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="group-delete"]').trigger('click')
    const confirm = wrapper.get('[data-test="confirm-dialog-confirm"]')
    await confirm.trigger('click')
    await confirm.trigger('click')

    expect(deleteGroup).toHaveBeenCalledTimes(1)
    expect(deleteGroup).toHaveBeenCalledWith(42)
    expect(wrapper.get('[data-test="confirm-dialog"]').attributes('data-loading')).toBe('true')
    expect(wrapper.get('[data-testid="group-delete"]').attributes('disabled')).toBeDefined()

    pending.resolve({ message: 'deleted' })
    await flushPromises()

    expect(showSuccess).toHaveBeenCalledWith('admin.groups.groupDeleted')
    expect(wrapper.find('[data-test="confirm-dialog"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="group-delete"]').attributes('disabled')).toBeUndefined()
    wrapper.unmount()
  })

  it('creates a group once and keeps the dialog open while the request runs', async () => {
    const pending = deferred<AdminGroup>()
    createGroup.mockReturnValueOnce(pending.promise)
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-tour="groups-create-btn"]').trigger('click')
    const form = wrapper.get('form#create-group-form')
    await form.get('input[type="text"]').setValue('New group')
    await form.trigger('submit.prevent')
    await form.trigger('submit.prevent')

    expect(createGroup).toHaveBeenCalledTimes(1)
    const dialog = wrapper.get('[data-dialog="admin.groups.createGroup"]')
    const cancel = dialog.findAll('button').find((button) => button.text() === 'common.cancel')
    expect(cancel?.attributes('disabled')).toBeDefined()
    expect(wrapper.get('[data-tour="group-form-submit"]').attributes('disabled')).toBeDefined()

    wrapper.findAllComponents(BaseDialogStub)
      .find((dialogStub) => dialogStub.props('title') === 'admin.groups.createGroup')!
      .vm.$emit('close')
    await flushPromises()
    expect(wrapper.find('form#create-group-form').exists()).toBe(true)

    pending.resolve({ ...group, id: 43, name: 'New group' })
    await flushPromises()

    expect(showSuccess).toHaveBeenCalledWith('admin.groups.groupCreated')
    expect(wrapper.find('form#create-group-form').exists()).toBe(false)
    wrapper.unmount()
  })
})
