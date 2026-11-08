import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'

import AccountsView from '../AccountsView.vue'

const {
  listAccounts,
  listWithEtag,
  getBatchTodayStats,
  probeUpstreamBilling,
  getAllProxies,
  getAllGroups
} = vi.hoisted(() => ({
  listAccounts: vi.fn(),
  listWithEtag: vi.fn(),
  getBatchTodayStats: vi.fn(),
  probeUpstreamBilling: vi.fn(),
  getAllProxies: vi.fn(),
  getAllGroups: vi.fn()
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    accounts: {
      list: listAccounts,
      listWithEtag,
      getBatchTodayStats,
      getUpstreamBillingProbeSettings: vi.fn().mockResolvedValue({ enabled: true, interval_minutes: 30 }),
      probeUpstreamBilling,
      delete: vi.fn(),
      batchClearError: vi.fn(),
      batchRefresh: vi.fn(),
      toggleSchedulable: vi.fn()
    },
    proxies: {
      getAll: getAllProxies
    },
    groups: {
      getAll: getAllGroups
    }
  }
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError: vi.fn(),
    showSuccess: vi.fn(),
    showInfo: vi.fn()
  })
}))

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({
    token: 'test-token'
  })
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => key
    })
  }
})

// Phone cards, where the refresh was reported. Every loading flag the page
// hands the table is recorded: a true one means the list went to skeletons.
const tableLoading: boolean[] = []
const DataTableStub = {
  props: { data: Array, loading: Boolean },
  watch: {
    loading: {
      immediate: true,
      handler: (value: boolean) => { tableLoading.push(value) }
    }
  },
  template: `
    <div data-test="data-table">
      <div v-for="row in data" :key="row.id" :data-test="'card-' + row.id">
        <slot name="mobile-card" :row="row" />
      </div>
    </div>
  `
}

function account(id: number, overrides: Record<string, unknown> = {}) {
  return {
    id,
    name: `account-${id}`,
    platform: 'anthropic',
    type: 'apikey',
    status: 'active',
    schedulable: true,
    concurrency: 10,
    current_concurrency: 0,
    rate_multiplier: 1,
    credentials: { base_url: 'https://relay.example.com/v1' },
    extra: {},
    groups: [],
    created_at: '2026-10-30T00:00:00Z',
    updated_at: '2026-10-30T00:00:00Z',
    ...overrides
  }
}

// What a sub2api upstream answers with right now.
function freshSnapshot(rate: number, overrides: Record<string, unknown> = {}) {
  const now = Date.now()
  return {
    status: 'ok',
    protocol: 'sub2api',
    data: {
      billing_scope: 'token',
      group_rate_multiplier: rate,
      resolved_rate_multiplier: rate,
      effective_rate_multiplier: rate,
      peak_rate_enabled: false
    },
    received_at: new Date(now).toISOString(),
    last_attempt_at: new Date(now).toISOString(),
    next_probe_at: new Date(now + 30 * 60_000).toISOString(),
    ...overrides
  }
}

async function mountWith(items: Array<Record<string, unknown>>) {
  listAccounts.mockResolvedValueOnce({ items, total: items.length, page: 1, page_size: 20, pages: 1 })
  const wrapper = mount(AccountsView, {
    global: {
      stubs: {
        AppLayout: { template: '<div><slot /></div>' },
        TablePageLayout: {
          template: '<div><slot name="filters" /><slot name="table" /><slot name="pagination" /></div>'
        },
        DataTable: DataTableStub,
        HelpTooltip: { template: '<span><slot name="trigger" /></span>' },
        Pagination: true,
        ConfirmDialog: true,
        AccountTableActions: { template: '<div><slot name="beforeCreate" /><slot name="after" /></div>' },
        AccountTableFilters: { template: '<div></div>' },
        AccountBulkActionsBar: true,
        AccountActionMenu: true,
        ImportDataModal: true,
        ReAuthAccountModal: true,
        AccountTestModal: true,
        AccountStatsModal: true,
        ScheduledTestsPanel: true,
        SyncFromCrsModal: true,
        TempUnschedStatusModal: true,
        ErrorPassthroughRulesModal: true,
        TLSFingerprintProfilesModal: true,
        CreateAccountModal: true,
        EditAccountModal: true,
        BulkEditAccountModal: true,
        PlatformTypeBadge: true,
        AccountSourceBadge: true,
        AccountCapacityCell: true,
        AccountStatusIndicator: true,
        AccountTodayStatsCell: true,
        AccountGroupsCell: true,
        AccountUsageCell: true,
        GroupBadge: true,
        Icon: true
      }
    }
  })
  await flushPromises()
  return wrapper
}

const card = (wrapper: VueWrapper, id: number) => wrapper.get(`[data-test="card-${id}"]`)
const probeButton = (wrapper: VueWrapper, id: number) => card(wrapper, id).get('[data-testid="upstream-billing-probe"]')
const upstreamRate = (wrapper: VueWrapper, id: number) => card(wrapper, id).get('[data-testid="upstream-billing-rate"]').text()
const accountRate = (wrapper: VueWrapper, id: number) => card(wrapper, id).get('[data-testid="account-card-rate"] strong').text()

describe('admin AccountsView card upstream rate refresh', () => {
  beforeEach(() => {
    localStorage.clear()
    tableLoading.length = 0
    listAccounts.mockReset()
    listWithEtag.mockReset()
    getBatchTodayStats.mockReset()
    probeUpstreamBilling.mockReset()
    getAllProxies.mockReset()
    getAllGroups.mockReset()

    listWithEtag.mockResolvedValue({ notModified: true, etag: null, data: null })
    getBatchTodayStats.mockResolvedValue({ stats: {} })
    getAllProxies.mockResolvedValue([])
    getAllGroups.mockResolvedValue([])
  })

  it('refreshes only the clicked card instead of reloading the list', async () => {
    const wrapper = await mountWith([account(1), account(2)])
    const listCalls = listAccounts.mock.calls.length
    const statsCalls = getBatchTodayStats.mock.calls.length
    const loadingSeen = tableLoading.length
    let finishProbe!: (result: unknown) => void
    probeUpstreamBilling.mockReturnValueOnce(new Promise((resolve) => { finishProbe = resolve }))

    await probeButton(wrapper, 1).trigger('click')
    expect(probeUpstreamBilling).toHaveBeenCalledWith(1)
    // Only the clicked card shows that it is working.
    expect(probeButton(wrapper, 1).attributes('disabled')).toBeDefined()
    expect(probeButton(wrapper, 2).attributes('disabled')).toBeUndefined()

    finishProbe({ account_id: 1, snapshot: freshSnapshot(0.065) })
    await flushPromises()

    expect(upstreamRate(wrapper, 1)).toBe('0.065x')
    expect(probeButton(wrapper, 1).attributes('disabled')).toBeUndefined()
    // Rate sync is off, so the account's own rate stays put.
    expect(accountRate(wrapper, 1)).toBe('1.00x')
    expect(upstreamRate(wrapper, 2)).toBe('admin.accounts.upstreamBilling.short.notProbed')
    expect(listAccounts).toHaveBeenCalledTimes(listCalls)
    expect(getBatchTodayStats).toHaveBeenCalledTimes(statsCalls)
    expect(tableLoading.slice(loadingSeen)).not.toContain(true)
  })

  it('shows the rate a probe synced on the probed card only', async () => {
    const synced = { upstream_billing_probe_enabled: true, upstream_billing_rate_sync_enabled: true }
    const wrapper = await mountWith([
      account(1, { rate_multiplier: 0.25, extra: synced }),
      account(2, { rate_multiplier: 0.25, extra: synced })
    ])
    probeUpstreamBilling.mockResolvedValueOnce({
      account_id: 1,
      snapshot: freshSnapshot(0.065, { synced_rate_multiplier: 0.065 })
    })

    await probeButton(wrapper, 1).trigger('click')
    await flushPromises()

    expect(accountRate(wrapper, 1)).toBe('0.065x')
    expect(upstreamRate(wrapper, 1)).toBe('0.065x')
    expect(accountRate(wrapper, 2)).toBe('0.25x')
    expect(upstreamRate(wrapper, 2)).toBe('admin.accounts.upstreamBilling.short.notProbed')
    expect(listAccounts).toHaveBeenCalledTimes(1)
  })
})
