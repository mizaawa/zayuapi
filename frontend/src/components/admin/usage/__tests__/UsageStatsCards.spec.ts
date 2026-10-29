import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'

import UsageStatsCards from '../UsageStatsCards.vue'

const messages: Record<string, string> = {
  'usage.totalRequests': 'Total Requests',
  'usage.inSelectedRange': 'in selected range',
  'usage.totalTokens': 'Total Tokens',
  'usage.in': 'In',
  'usage.out': 'Out',
  'usage.cacheTotal': 'Cache',
  'usage.cacheBreakdown': 'Cache Token Breakdown',
  'usage.cacheCreationTokensLabel': 'Cache Creation',
  'usage.cacheReadTokensLabel': 'Cache Read',
  'usage.totalCost': 'Total Cost',
  'usage.accountCost': 'Cost',
  'usage.standardCost': 'Standard',
  'usage.avgDuration': 'Avg Duration',
}

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => messages[key] ?? key,
    }),
  }
})

const stats = {
  total_requests: 1,
  total_input_tokens: 100,
  total_output_tokens: 50,
  total_cache_tokens: 34,
  total_cache_creation_tokens: 12,
  total_cache_read_tokens: 22,
  total_tokens: 184,
  total_cost: 0.001,
  total_actual_cost: 0.001,
  total_account_cost: 0.001,
  average_duration_ms: 250,
}

describe('UsageStatsCards', () => {
  const mountCards = (props: Record<string, unknown> = {}) => mount(UsageStatsCards, {
    props: { stats, ...props },
    global: { stubs: { Icon: true } },
  })

  it('spells out the cache token breakdown without a hover-only tooltip', () => {
    const wrapper = mountCards()

    const text = wrapper.text()
    expect(text).toContain('Cache 34')
    expect(wrapper.get('[aria-label="Cache Token Breakdown"]').text()).toContain('Cache Creation12')
    expect(wrapper.get('[aria-label="Cache Token Breakdown"]').text()).toContain('Cache Read22')
    // Hidden panels with a fixed width used to widen phone pages; nothing is hover-gated now.
    expect(wrapper.findAll('span').filter((el) => el.classes().includes('group-hover:block'))).toHaveLength(0)
  })

  it('shows account cost only when asked and strikes the standard price for users', () => {
    const admin = mountCards()
    expect(admin.text()).toContain('Cost $0.0010')
    expect(admin.find('.line-through').exists()).toBe(false)

    const user = mountCards({ showAccountCost: false, strikeStandardCost: true, cumulative: true })
    expect(user.text()).not.toContain('Cost $0.0010')
    expect(user.get('.line-through').text()).toBe('$0.0010')
  })

  it('draws one bar segment per non-empty token kind', () => {
    const wrapper = mountCards({ stats: { ...stats, total_cache_creation_tokens: 0, total_cache_tokens: 22 } })
    expect(wrapper.findAll('.usage-summary-bar > span')).toHaveLength(3)
  })
})
