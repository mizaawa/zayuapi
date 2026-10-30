import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

import AccountsView from '../AccountsView.vue'

const {
  listAccounts,
  listWithEtag,
  getBatchTodayStats,
  getAllProxies,
  getAllGroups
} = vi.hoisted(() => ({
  listAccounts: vi.fn(),
  listWithEtag: vi.fn(),
  getBatchTodayStats: vi.fn(),
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

// Render each row through the phone card slot, the layout these accounts get below 768px.
const DataTableStub = {
  props: ['columns', 'data'],
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
    extra: {},
    created_at: '2026-10-30T00:00:00Z',
    updated_at: '2026-10-30T00:00:00Z',
    ...overrides
  }
}

function mountView() {
  return mount(AccountsView, {
    global: {
      stubs: {
        AppLayout: { template: '<div><slot /></div>' },
        TablePageLayout: {
          template: '<div><slot name="filters" /><slot name="table" /><slot name="pagination" /></div>'
        },
        DataTable: DataTableStub,
        HelpTooltip: true,
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
}

describe('admin AccountsView phone card billing multiplier', () => {
  beforeEach(() => {
    localStorage.clear()
    listAccounts.mockReset()
    listWithEtag.mockReset()
    getBatchTodayStats.mockReset()
    getAllProxies.mockReset()
    getAllGroups.mockReset()

    listWithEtag.mockResolvedValue({ notModified: true, etag: null, data: null })
    getBatchTodayStats.mockResolvedValue({ stats: {} })
    getAllProxies.mockResolvedValue([])
    getAllGroups.mockResolvedValue([])
  })

  it("shows each account's own billing multiplier on its card", async () => {
    listAccounts.mockResolvedValueOnce({
      items: [
        account(1, { rate_multiplier: 0.6 }),
        account(2, { rate_multiplier: 0.065, extra: { upstream_billing_rate_sync_enabled: true } }),
        account(3),
        account(4, { rate_multiplier: 0 })
      ],
      total: 4,
      page: 1,
      page_size: 20,
      pages: 1
    })

    const wrapper = mountView()
    await flushPromises()

    const rate = (id: number) => wrapper.get(`[data-test="card-${id}"] [data-testid="account-card-rate"]`)
    expect(rate(1).text()).toBe('admin.accounts.card.rateMultiplier0.60x')
    expect(rate(1).get('strong').text()).toBe('0.60x')
    expect(rate(1).attributes('title')).toBe('admin.accounts.card.rateMultiplierHint')
    expect(rate(1).find('[data-testid="account-card-rate-sync"]').exists()).toBe(false)

    expect(rate(2).get('strong').text()).toBe('0.065x')
    expect(rate(2).get('[data-testid="account-card-rate-sync"]').attributes('title'))
      .toBe('admin.accounts.upstreamBilling.syncedRateTooltip')

    expect(rate(3).get('strong').text()).toBe('1.00x')
    expect(rate(4).get('strong').text()).toBe('0.0x')
  })
})
