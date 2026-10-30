import { beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'

import AccountsView from '../AccountsView.vue'

const {
  listAccounts,
  listWithEtag,
  getBatchTodayStats,
  getUpstreamBillingProbeSettings,
  getAllProxies,
  getAllGroups,
  deleteAccount,
  duplicateAccount,
  bulkUpdate,
  showError,
  showSuccess,
  showToast,
  hideToast
} = vi.hoisted(() => ({
  listAccounts: vi.fn(),
  listWithEtag: vi.fn(),
  getBatchTodayStats: vi.fn(),
  getUpstreamBillingProbeSettings: vi.fn(),
  getAllProxies: vi.fn(),
  getAllGroups: vi.fn(),
  deleteAccount: vi.fn(),
  duplicateAccount: vi.fn(),
  bulkUpdate: vi.fn(),
  showError: vi.fn(),
  showSuccess: vi.fn(),
  showToast: vi.fn(),
  hideToast: vi.fn()
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    accounts: {
      list: listAccounts,
      listWithEtag,
      getBatchTodayStats,
      getUpstreamBillingProbeSettings,
      delete: deleteAccount,
      duplicate: duplicateAccount,
      bulkUpdate,
      batchClearError: vi.fn(),
      batchRefresh: vi.fn()
    },
    proxies: { getAll: getAllProxies },
    groups: { getAll: getAllGroups }
  }
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError,
    showSuccess,
    showInfo: vi.fn(),
    showToast,
    hideToast
  })
}))

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({ token: 'test-token' })
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key })
  }
})

const account = {
  id: 7,
  name: 'relay-7',
  platform: 'openai',
  type: 'apikey',
  status: 'active',
  schedulable: true,
  concurrency: 0,
  created_at: '2026-10-01T00:00:00Z',
  updated_at: '2026-10-01T00:00:00Z'
}

const deferred = <T>() => {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((res) => { resolve = res })
  return { promise, resolve }
}

const DataTableStub = defineComponent({
  props: ['columns', 'data'],
  template: `
    <div>
      <div v-for="row in data" :key="row.id">
        <div data-test="select-row"><slot name="cell-select" :row="row" /></div>
        <div data-test="row-actions"><slot name="cell-actions" :row="row" /></div>
      </div>
    </div>
  `
})

const BulkBarStub = defineComponent({
  props: ['selectedIds', 'busyAction'],
  emits: ['toggle-schedulable'],
  template: `
    <div data-test="bulk-bar" :data-busy="busyAction ?? ''">
      <button data-test="bulk-enable" @click="$emit('toggle-schedulable', true)">enable</button>
    </div>
  `
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

const ActionMenuStub = defineComponent({
  props: ['show', 'account', 'duplicating'],
  emits: ['duplicate', 'close'],
  template: `
    <div v-if="show" data-test="action-menu" :data-duplicating="String(!!duplicating)">
      <button data-test="menu-duplicate" @click="$emit('duplicate', account); $emit('close')">duplicate</button>
    </div>
  `
})

function mountView() {
  return mount(AccountsView, {
    global: {
      stubs: {
        AppLayout: { template: '<div><slot /></div>' },
        TablePageLayout: { template: '<div><slot name="table" /></div>' },
        DataTable: DataTableStub,
        Pagination: true,
        ConfirmDialog: ConfirmDialogStub,
        AccountTableActions: true,
        AccountTableFilters: true,
        AccountBulkActionsBar: BulkBarStub,
        AccountActionMenu: ActionMenuStub,
        ImportDataModal: true,
        AccountTestModal: true,
        AccountStatsModal: true,
        ScheduledTestsPanel: true,
        TempUnschedStatusModal: true,
        ErrorPassthroughRulesModal: true,
        TLSFingerprintProfilesModal: true,
        CreateAccountModal: true,
        EditAccountModal: true,
        BulkEditAccountModal: true,
        PlatformTypeBadge: true,
        AccountCapacityCell: true,
        AccountStatusIndicator: true,
        AccountTodayStatsCell: true,
        AccountGroupsCell: true,
        AccountUsageCell: true,
        Icon: true
      }
    }
  })
}

function rowButton(wrapper: ReturnType<typeof mountView>, label: string) {
  const button = wrapper.get('[data-test="row-actions"]').findAll('button').find((b) => b.text().includes(label))
  expect(button).toBeDefined()
  return button!
}

describe('admin AccountsView pending actions', () => {
  beforeEach(() => {
    localStorage.clear()
    vi.spyOn(console, 'error').mockImplementation(() => {})
    for (const fn of [
      listAccounts, listWithEtag, getBatchTodayStats, getUpstreamBillingProbeSettings, getAllProxies,
      getAllGroups, deleteAccount, duplicateAccount, bulkUpdate, showError, showSuccess, showToast, hideToast
    ]) {
      fn.mockReset()
    }
    listAccounts.mockResolvedValue({ items: [account], total: 1, page: 1, page_size: 20, pages: 1 })
    listWithEtag.mockResolvedValue({ notModified: true, etag: null, data: null })
    getBatchTodayStats.mockResolvedValue({ stats: {} })
    getUpstreamBillingProbeSettings.mockResolvedValue({ enabled: false, interval_minutes: 30 })
    getAllProxies.mockResolvedValue([])
    getAllGroups.mockResolvedValue([])
    showToast.mockReturnValue('toast-pending')
  })

  it('sends one delete and keeps the confirmation busy until it settles', async () => {
    const pending = deferred<{ message: string }>()
    deleteAccount.mockReturnValueOnce(pending.promise)
    const wrapper = mountView()
    await flushPromises()

    await rowButton(wrapper, 'common.delete').trigger('click')
    const confirm = wrapper.get('[data-test="confirm-dialog-confirm"]')
    await confirm.trigger('click')
    await confirm.trigger('click')

    expect(deleteAccount).toHaveBeenCalledTimes(1)
    expect(deleteAccount).toHaveBeenCalledWith(7)
    expect(wrapper.get('[data-test="confirm-dialog"]').attributes('data-loading')).toBe('true')

    pending.resolve({ message: 'deleted' })
    await flushPromises()

    expect(wrapper.find('[data-test="confirm-dialog"]').exists()).toBe(false)
    expect(showSuccess).toHaveBeenCalledWith('admin.accounts.accountDeleted')
    wrapper.unmount()
  })

  it('reports a failed delete and lets the user retry', async () => {
    deleteAccount.mockRejectedValueOnce(new Error('account is busy'))
    const wrapper = mountView()
    await flushPromises()

    await rowButton(wrapper, 'common.delete').trigger('click')
    await wrapper.get('[data-test="confirm-dialog-confirm"]').trigger('click')
    await flushPromises()

    expect(showError).toHaveBeenCalledWith('account is busy')
    expect(wrapper.get('[data-test="confirm-dialog"]').attributes('data-loading')).toBe('false')
    wrapper.unmount()
  })

  it('shows a pending notice while duplicating and marks the menu entry busy', async () => {
    const pending = deferred<typeof account>()
    duplicateAccount.mockReturnValueOnce(pending.promise)
    const wrapper = mountView()
    await flushPromises()

    await rowButton(wrapper, 'common.more').trigger('click')
    await wrapper.get('[data-test="menu-duplicate"]').trigger('click')

    expect(duplicateAccount).toHaveBeenCalledTimes(1)
    expect(showToast).toHaveBeenCalledWith('info', 'admin.accounts.duplicatingAccount')

    await rowButton(wrapper, 'common.more').trigger('click')
    expect(wrapper.get('[data-test="action-menu"]').attributes('data-duplicating')).toBe('true')
    await wrapper.get('[data-test="menu-duplicate"]').trigger('click')
    expect(duplicateAccount).toHaveBeenCalledTimes(1)

    pending.resolve({ ...account, id: 8, name: 'relay-7 (copy)' })
    await flushPromises()

    expect(hideToast).toHaveBeenCalledWith('toast-pending')
    expect(showSuccess).toHaveBeenCalledWith('admin.accounts.duplicateSuccess')
    wrapper.unmount()
  })

  it('runs one bulk action at a time and reports it to the bar as busy', async () => {
    const pending = deferred<{ success: number; failed: number; success_ids: number[]; failed_ids: number[] }>()
    bulkUpdate.mockReturnValueOnce(pending.promise)
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-test="select-row"] input').trigger('change')
    await wrapper.get('[data-test="bulk-enable"]').trigger('click')
    await wrapper.get('[data-test="bulk-enable"]').trigger('click')

    expect(bulkUpdate).toHaveBeenCalledTimes(1)
    expect(bulkUpdate).toHaveBeenCalledWith([7], { schedulable: true })
    expect(wrapper.get('[data-test="bulk-bar"]').attributes('data-busy')).toBe('enable-scheduling')

    pending.resolve({ success: 1, failed: 0, success_ids: [7], failed_ids: [] })
    await flushPromises()

    expect(wrapper.get('[data-test="bulk-bar"]').attributes('data-busy')).toBe('')
    wrapper.unmount()
  })
})
