import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'

import type { ApiKey, Group } from '@/types'
import type { BatchApiKeyUsageStats } from '@/api/usage'
import GroupBadge from '@/components/common/GroupBadge.vue'
import type { KeyCardField } from '../keyCards'

vi.mock('vue-i18n', async () => ({
  ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'),
  useI18n: () => ({
    t: (key: string, params?: Record<string, unknown>) => (params ? `${key}:${JSON.stringify(params)}` : key)
  })
}))

import KeyCard from '../KeyCard.vue'

const NOW = Date.parse('2026-11-04T08:30:00.000Z')

const group = (overrides: Partial<Group> = {}): Group => ({
  id: 7,
  name: 'Claude 官方直连',
  description: null,
  platform: 'anthropic',
  status: 'active',
  subscription_type: 'standard',
  rate_multiplier: 1,
  peak_rate_enabled: false,
  peak_start: '',
  peak_end: '',
  peak_rate_multiplier: 1,
  ...overrides
} as Group)

const apiKey = (overrides: Partial<ApiKey> = {}): ApiKey => ({
  id: 42,
  user_id: 1,
  key: 'sk-zy-7f3a9c2e4b1d8f6a0c5e3b9d7a1f4c8e2b6d',
  name: 'Claude Code 主力',
  group_id: 7,
  group: group(),
  status: 'active',
  ip_whitelist: [],
  ip_blacklist: [],
  last_used_at: null,
  last_used_ip: '203.0.113.10',
  quota: 0,
  quota_used: 0,
  expires_at: null,
  created_at: '2026-10-01T00:00:00Z',
  updated_at: '2026-10-01T00:00:00Z',
  current_concurrency: 2,
  rate_limit_5h: 0,
  rate_limit_1d: 0,
  rate_limit_7d: 0,
  usage_5h: 0,
  usage_1d: 0,
  usage_7d: 0,
  window_5h_start: null,
  window_1d_start: null,
  window_7d_start: null,
  reset_5h_at: null,
  reset_1d_at: null,
  reset_7d_at: null,
  ...overrides
})

const usage: BatchApiKeyUsageStats = {
  api_key_id: 42,
  today_actual_cost: 1234.5678,
  total_actual_cost: 3.05,
  input_tokens: 200,
  output_tokens: 50,
  cache_creation_tokens: 300,
  cache_read_tokens: 500
}

const mountCard = (props: Partial<InstanceType<typeof KeyCard>['$props']> = {}) =>
  mount(KeyCard, {
    props: {
      apiKey: apiKey(),
      usage,
      hiddenFields: new Set<KeyCardField>(),
      now: NOW,
      ...props
    },
    global: { stubs: { GroupBadge: true } }
  })

describe('KeyCard', () => {
  it('shows the essentials and copies the key on click', async () => {
    const wrapper = mountCard()

    expect(wrapper.get('h3').text()).toBe('Claude Code 主力')
    expect(wrapper.get('[data-test="key-status"]').text()).toBe('keys.status.active')
    expect(wrapper.get('[data-test="key-copy"]').text()).toContain('sk-zy-...2b6d')
    expect(wrapper.text()).not.toContain('7f3a9c2e4b1d8f6a0c5e3b9d7a1f4c8e2b6d')

    await wrapper.get('[data-test="key-copy"]').trigger('click')
    expect(wrapper.emitted('action')).toEqual([['copy', wrapper.props('apiKey')]])
  })

  it('switches the copy button to its confirmation state', () => {
    const wrapper = mountCard({ copied: true })
    expect(wrapper.get('[data-test="key-copy"]').classes()).toContain('is-copied')
    expect(wrapper.get('[data-test="key-copy"]').text()).toContain('keys.copied')
  })

  it('keeps a long group name inside the card and opens the picker on click', async () => {
    const longName = '超长分组名称测试高速稳定专线'.repeat(7)
    const wrapper = mountCard({ apiKey: apiKey({ group: group({ name: longName }) }), groupBlocked: true, userGroupRate: 0.9 })
    const badge = wrapper.getComponent(GroupBadge)

    expect(badge.props('name')).toBe(longName)
    expect(badge.props('maxNameLength')).toBe(20)
    expect(badge.props('userRateMultiplier')).toBe(0.9)
    expect(badge.classes()).toEqual(expect.arrayContaining(['min-w-0', 'max-w-full']))
    expect(wrapper.get('[data-test="key-group-button-42"]').text()).toContain('common.groupBlocked')

    await wrapper.get('[data-test="key-group-button-42"]').trigger('click')
    expect(wrapper.emitted('action')?.[0]?.[0]).toBe('group')
  })

  it('offers to pick a group when the key has none', () => {
    const wrapper = mountCard({ apiKey: apiKey({ group_id: null, group: undefined }) })
    expect(wrapper.findComponent(GroupBadge).exists()).toBe(false)
    expect(wrapper.get('[data-test="key-group-button-42"]').text()).toContain('keys.noGroup')
  })

  it('hides the fields the user switched off', () => {
    const wrapper = mountCard({
      hiddenFields: new Set<KeyCardField>(['id', 'usage', 'tokens', 'failover', 'expires_at', 'created_at', 'last_used_ip', 'last_used_at'])
    })

    expect(wrapper.find('[data-test="key-id"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="key-today-cost"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="key-token-mix"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="failover-status-42"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="key-expiry"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="last-used-ip"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="key-last-used"]').exists()).toBe(false)
    expect(wrapper.find('.key-card-details').exists()).toBe(false)
    // Concurrency is still on, so it keeps a tile of its own.
    expect(wrapper.get('[data-test="current-concurrency"]').text()).toBe('2')
  })

  it('shows every optional field when nothing is hidden', () => {
    const wrapper = mountCard({ apiKey: apiKey({ last_used_at: '2026-11-04T08:00:00Z' }) })

    expect(wrapper.get('[data-test="key-id"]').text()).toBe('#42')
    expect(wrapper.get('[data-test="key-last-used"]').text()).toContain('keys.lastUsedRelative')
    expect(wrapper.get('[data-test="last-used-ip"]').text()).toBe('203.0.113.10')
    expect(wrapper.get('[data-test="key-expiry"]').text()).toBe('keys.noExpiration')
    expect(wrapper.find('[data-testid="key-token-mix"]').exists()).toBe(true)
  })

  it('formats spend for the tiles and keeps the exact amount on hover', () => {
    const wrapper = mountCard()
    const today = wrapper.get('[data-test="key-today-cost"]')

    expect(today.text()).toBe('$1,234.57')
    expect(today.attributes('title')).toBe('$1234.5678')
    expect(wrapper.get('[data-test="key-total-cost"]').text()).toBe('$3.0500')
  })

  it('marks spend as unknown instead of zero until usage arrives', () => {
    const pending = mountCard({ usage: undefined, usagePending: true })
    expect(pending.get('[data-test="key-today-cost"]').text()).toBe('—')
    expect(pending.get('[data-test="key-today-cost"] .key-card-pending').classes()).toContain('is-loading')
    expect(pending.find('[data-testid="key-token-mix"]').exists()).toBe(false)
    expect(pending.find('.key-card-mix-placeholder').exists()).toBe(true)

    const failed = mountCard({ usage: undefined, usagePending: false })
    expect(failed.get('[data-test="key-total-cost"]').text()).toBe('—')
    expect(failed.find('.key-card-mix-placeholder').exists()).toBe(false)
  })

  it('grades quota and rate-limit meters and counts down to each reset', async () => {
    const wrapper = mountCard({
      apiKey: apiKey({
        quota: 50,
        quota_used: 41.2,
        rate_limit_5h: 5,
        usage_5h: 5,
        reset_5h_at: new Date(NOW + 2 * 3_600_000 + 12 * 60_000).toISOString(),
        rate_limit_1d: 20,
        usage_1d: 1,
        reset_1d_at: new Date(NOW - 1000).toISOString()
      })
    })

    const limits = wrapper.get('[data-test="key-limits"]')
    expect(limits.text()).toContain('$41.20 / $50.00')
    expect(limits.get('.key-limit-value').classes()).toContain('is-warning')
    expect(wrapper.get('[data-test="key-window-5h"] .key-limit-value').classes()).toContain('is-danger')
    expect(wrapper.get('[data-test="key-window-5h"]').text()).toContain('2h 12m')
    expect(wrapper.get('[data-test="key-window-1d"]').text()).toContain('keys.resetNow')
    expect(wrapper.find('[data-test="key-window-7d"]').exists()).toBe(false)

    await wrapper.get('[data-test="key-reset-rate-limit"]').trigger('click')
    expect(wrapper.emitted('action')?.[0]?.[0]).toBe('resetRateLimit')
  })

  it('hides the rate-limit reset until a window has been used', () => {
    const wrapper = mountCard({ apiKey: apiKey({ rate_limit_7d: 100 }) })
    expect(wrapper.find('[data-test="key-window-7d"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="key-reset-rate-limit"]').exists()).toBe(false)
  })

  it('flags keys that expire soon or already expired', () => {
    const soon = mountCard({ apiKey: apiKey({ expires_at: new Date(NOW + 3 * 86_400_000).toISOString() }) })
    expect(soon.get('[data-test="key-expiry"] .key-tag-warning').text()).toBe('keys.expiresSoon:{"days":3}')

    const expired = mountCard({ apiKey: apiKey({ status: 'expired', expires_at: new Date(NOW - 1000).toISOString() }) })
    expect(expired.get('[data-test="key-expiry"] .key-tag-danger').text()).toBe('keys.status.expired')
    expect(expired.classes()).toContain('is-expired')
  })

  it('reflects the failover state and follows the shared clock', async () => {
    const cooling = apiKey({ failover_enabled: true, failover_group_id: 8, failover_cooldown_until: new Date(NOW + 2000).toISOString() })
    const wrapper = mountCard({ apiKey: cooling })
    const status = () => wrapper.get('[data-test="failover-status-42"]')

    expect(status().text()).toBe('keys.failover.active')
    expect(status().classes()).toContain('text-red-700')
    await wrapper.setProps({ now: NOW + 2000 })
    expect(status().text()).toBe('keys.failover.healthy')
    expect(status().classes()).toContain('text-emerald-700')

    await status().trigger('click')
    expect(wrapper.emitted('action')?.[0]?.[0]).toBe('failover')
  })

  it.each(['custom', 'composite'] as const)('disables failover for the %s platform', (platform) => {
    const wrapper = mountCard({ apiKey: apiKey({ group: group({ platform }) }) })
    const status = wrapper.get('[data-test="failover-status-42"]')
    expect((status.element as HTMLButtonElement).disabled).toBe(true)
    expect(status.text()).toBe('keys.failover.disabled')
    expect(status.attributes('title')).toBe('keys.failover.supportedPlatforms')
  })

  it('offers the right actions and locks the toggle while it saves', async () => {
    const active = mountCard({ toggling: true })
    expect(active.findAll('.key-action').map((button) => button.attributes('data-test'))).toEqual([
      'key-action-use', 'key-action-ccs', 'key-action-toggle', 'key-action-edit', 'key-action-delete'
    ])
    expect(active.get('[data-test="key-action-toggle"]').text()).toBe('keys.disable')
    expect((active.get('[data-test="key-action-toggle"]').element as HTMLButtonElement).disabled).toBe(true)

    const inactive = mountCard({ apiKey: apiKey({ status: 'inactive' }), showCcsImport: false })
    expect(inactive.find('[data-test="key-action-ccs"]').exists()).toBe(false)
    expect(inactive.get('[data-test="key-action-toggle"]').text()).toBe('keys.enable')
    expect(inactive.classes()).toContain('is-inactive')

    for (const name of ['use', 'toggle', 'edit', 'delete']) {
      await inactive.get(`[data-test="key-action-${name}"]`).trigger('click')
    }
    expect(inactive.emitted('action')?.map(([name]) => name)).toEqual(['use', 'toggle', 'edit', 'delete'])
  })

  it('marks keys with an IP restriction', () => {
    expect(mountCard().find('.key-card-shield').exists()).toBe(false)
    const restricted = mountCard({ apiKey: apiKey({ ip_blacklist: ['198.51.100.7'] }) })
    expect(restricted.get('.key-card-shield').attributes('title')).toBe('keys.ipRestrictionEnabled')
  })
})
