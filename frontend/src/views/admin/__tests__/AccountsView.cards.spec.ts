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
      t: (key: string, params?: Record<string, unknown>) =>
        params ? `${key}:${Object.values(params).join(',')}` : key
    })
  }
})

// Renders each row through the card slot, the layout below 768px, and keeps
// the row class the page passes so selection outlines can be checked.
const DataTableStub = {
  props: {
    columns: Array,
    data: Array,
    rowClass: [String, Array, Function],
    mobileCardGrid: Boolean
  },
  template: `
    <div data-test="data-table" :data-grid="mobileCardGrid ? 'on' : 'off'">
      <div
        v-for="(row, index) in data"
        :key="row.id"
        :data-test="'card-' + row.id"
        :class="typeof rowClass === 'function' ? rowClass(row, index) : rowClass"
      >
        <slot name="mobile-card" :row="row" />
      </div>
    </div>
  `
}

const group = (id: number, name: string) => ({ id, name, platform: 'anthropic', rate_multiplier: 1, subscription_type: 'standard' })

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
        AccountSourceBadge: true,
        AccountCapacityCell: true,
        AccountStatusIndicator: true,
        AccountTodayStatsCell: true,
        AccountGroupsCell: true,
        AccountUsageCell: {
          props: { account: Object, hideKeyStats: Boolean },
          template: '<div data-testid="card-usage" :data-hide-key-stats="String(hideKeyStats)"></div>'
        },
        UpstreamBillingRateCell: {
          props: ['account', 'variant'],
          template: '<span data-testid="card-upstream" :data-variant="variant"></span>'
        },
        GroupBadge: { props: ['name'], template: '<span data-testid="group-badge">{{ name }}</span>' },
        Icon: true
      }
    }
  })
}

async function mountWith(items: Array<Record<string, unknown>>, stats: Record<string, unknown> = {}) {
  listAccounts.mockResolvedValueOnce({ items, total: items.length, page: 1, page_size: 20, pages: 1 })
  getBatchTodayStats.mockResolvedValue({ stats })
  const wrapper = mountView()
  await flushPromises()
  return wrapper
}

describe('admin AccountsView account cards', () => {
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

  it('shows six stat tiles: load and both rates, then today, with last use for non-key accounts', async () => {
    const wrapper = await mountWith(
      [
        account(1, { current_concurrency: 7, concurrency: 20, rate_multiplier: 0.6 }),
        account(2, { type: 'oauth', credentials: {}, extra: { email_address: 'owner@example.com' }, last_used_at: null })
      ],
      { 1: { requests: 1284, tokens: 18_400_000, cost: 12.84, user_cost: 21.4 } }
    )

    const tiles = (id: number) => wrapper.findAll(`[data-test="card-${id}"] .account-card-stat`)
    expect(tiles(1).map((tile) => tile.get('dt').text())).toEqual([
      'admin.accounts.card.concurrency',
      'admin.accounts.card.rateMultiplier',
      'admin.accounts.card.upstreamRate',
      'admin.accounts.card.requests',
      'admin.accounts.card.tokens',
      'admin.accounts.card.cost'
    ])
    expect(wrapper.get('[data-test="card-1"] [data-testid="card-upstream"]').attributes('data-variant')).toBe('card')
    expect(tiles(1)[5].findAll('dd').map((dd) => dd.text())).toEqual(['$12.84', 'admin.accounts.card.userCost:$21.40'])
    expect(wrapper.get('[data-test="card-1"] .account-card-email').text()).toBe('relay.example.com')
    expect(wrapper.get('[data-test="card-1"] .account-card-id').text()).toBe('#1')

    expect(tiles(2)).toHaveLength(6)
    expect(tiles(2)[2].get('dt').text()).toBe('admin.accounts.card.lastUsed')
    expect(tiles(2)[2].get('dd').text()).toBe('admin.accounts.card.neverUsed')
    expect(wrapper.find('[data-test="card-2"] [data-testid="card-upstream"]').exists()).toBe(false)
    expect(wrapper.get('[data-test="card-2"] .account-card-email').text()).toBe('owner@example.com')
  })

  it('draws key quotas as meters and keeps the usage cell for provider usage', async () => {
    const wrapper = await mountWith([
      account(1, { quota_daily_limit: 20, quota_daily_used: 13.2, quota_limit: 500, quota_used: 470 }),
      account(2, { platform: 'gemini', type: 'oauth', credentials: { oauth_type: 'google_one' } }),
      account(3, { type: 'oauth', credentials: {} }),
      account(4, { platform: 'gemini', quota_daily_limit: 10, quota_daily_used: 2.5 })
    ])

    const quotas = wrapper.findAll('[data-test="card-1"] .account-card-quota')
    expect(quotas.map((quota) => quota.get('.account-card-quota-label').text())).toEqual([
      'admin.accounts.card.quotaDaily',
      'admin.accounts.card.quotaTotal'
    ])
    expect(quotas[0].get('.account-card-quota-value').text()).toBe('$13.20 / $20.00')
    expect(quotas[0].get('.account-card-meter-fill').attributes('style')).toContain('width: 66%')
    expect(quotas[0].get('.account-card-meter-fill').classes()).toContain('is-ok')
    expect(quotas[1].get('.account-card-meter-fill').classes()).toContain('is-warn')
    expect(wrapper.find('[data-test="card-1"] [data-testid="card-usage"]').exists()).toBe(false)

    expect(wrapper.get('[data-test="card-2"] [data-testid="card-usage"]').attributes('data-hide-key-stats')).toBe('true')
    expect(wrapper.find('[data-test="card-2"] [data-testid="account-card-quotas"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="card-3"] [data-testid="card-usage"]').exists()).toBe(true)

    // Gemini API keys carry no tier or simulated quota: their card shows key
    // quotas itself, exactly like an OpenAI or Anthropic API key.
    expect(wrapper.find('[data-test="card-4"] [data-testid="card-usage"]').exists()).toBe(false)
    expect(wrapper.findAll('[data-test="card-4"] .account-card-quota-label').map((label) => label.text())).toEqual([
      'admin.accounts.card.quotaDaily'
    ])
  })

  it('folds groups beyond three into a +N chip that names them', async () => {
    const wrapper = await mountWith([
      account(1, { groups: [group(1, 'A'), group(2, 'B'), group(3, 'C'), group(4, 'D'), group(5, 'E')] }),
      account(2, { groups: [group(1, 'A')] })
    ])

    expect(wrapper.findAll('[data-test="card-1"] [data-testid="group-badge"]').map((badge) => badge.text())).toEqual(['A', 'B', 'C'])
    const more = wrapper.get('[data-test="card-1"] [data-testid="account-card-groups-more"]')
    expect(more.text()).toBe('+2')
    expect(more.attributes('title')).toBe('admin.accounts.card.moreGroups:2,D, E')
    expect(wrapper.find('[data-test="card-2"] [data-testid="account-card-groups-more"]').exists()).toBe(false)
  })

  it('marks accounts that cannot serve with a status strip and outlines selected cards', async () => {
    const future = new Date(Date.now() + 60 * 60 * 1000).toISOString()
    const wrapper = await mountWith([
      account(1),
      account(2, { schedulable: false }),
      account(3, { quota_daily_limit: 10, quota_daily_used: 10 }),
      account(4, { type: 'oauth', credentials: {}, status: 'error' }),
      account(5, { type: 'oauth', credentials: {}, rate_limit_reset_at: future }),
      account(6, { type: 'oauth', credentials: {}, status: 'inactive' })
    ])

    const tone = (id: number) => wrapper.get(`[data-test="card-${id}"] .account-card`).attributes('data-tone')
    expect([1, 2, 3, 4, 5, 6].map(tone)).toEqual(['ok', 'paused', 'warn', 'error', 'warn', 'paused'])
    expect(wrapper.get('[data-test="card-2"] .account-card').classes()).toContain('is-paused')
    expect(wrapper.get('[data-test="data-table"]').attributes('data-grid')).toBe('on')

    expect(wrapper.get('[data-test="card-1"]').classes()).not.toContain('is-account-selected')
    await wrapper.get('[data-test="card-1"] .account-card-check').setValue(true)
    expect(wrapper.get('[data-test="card-1"]').classes()).toContain('is-account-selected')
  })
})
