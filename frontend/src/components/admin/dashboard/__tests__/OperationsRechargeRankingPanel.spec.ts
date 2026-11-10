import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import OperationsRechargeRankingPanel from '../OperationsRechargeRankingPanel.vue'
import type { OperationsRechargeRankingResponse } from '@/api/admin/dashboard'

vi.mock('vue-i18n', async () => ({
  ...(await vi.importActual<typeof import('vue-i18n')>('vue-i18n')),
  useI18n: () => ({ t: (key: string, values?: Record<string, unknown>) => values ? `${key} ${JSON.stringify(values)}` : key })
}))

const ranking = (count: number): OperationsRechargeRankingResponse => ({
  users: Array.from({ length: count }, (_, index) => ({ user_id: index + 1, email: `payer${index + 1}@example.com`, amount: 500 - index * 10, recharge_count: index % 3 + 1 })),
  total_amount: 6000,
  total_users: 63,
  date: '2026-11-10',
  start_time: '2026-11-10T00:00:00+08:00',
  end_time: '2026-11-11T00:00:00+08:00',
  site_timezone: 'Asia/Shanghai'
})
const mountPanel = (props: Partial<{ ranking: OperationsRechargeRankingResponse | null; loading: boolean; error: boolean }>) =>
  mount(OperationsRechargeRankingPanel, { props: { ranking: null, loading: false, error: false, ...props }, global: { stubs: { Icon: true, LoadingSpinner: true } } })

describe('OperationsRechargeRankingPanel', () => {
  it('lists the ranked payers with email, amount and medals for the top three', () => {
    const wrapper = mountPanel({ ranking: ranking(50) })
    const rows = wrapper.findAll('[data-ranking-user]')
    expect(rows).toHaveLength(50)
    expect(rows.map(row => row.attributes('data-ranking-user')).slice(0, 3)).toEqual(['1', '2', '3'])
    expect(rows[0].text()).toContain('payer1@example.com')
    expect(rows[0].text()).toContain('$500.00')
    expect(rows[49].text()).toContain('$10.00')
    expect(rows[0].get('[data-rank]').classes()).toContain('ops-rank-gold')
    expect(rows[1].get('[data-rank]').classes()).toContain('ops-rank-silver')
    expect(rows[2].get('[data-rank]').classes()).toContain('ops-rank-bronze')
    expect(rows[3].get('[data-rank]').classes()).not.toContain('ops-rank-gold')
    expect(wrapper.get('[data-testid="recharge-ranking-total"]').text()).toBe('$6,000.00')
    expect(wrapper.get('[data-testid="recharge-ranking-users"]').text()).toBe('63')
    expect(wrapper.get('[data-testid="recharge-ranking-window"]').text()).toContain('"date":"2026-11-10"')
    expect(wrapper.get('[data-testid="recharge-ranking-window"]').text()).toContain('"timezone":"Asia/Shanghai"')
  })

  it('shows the empty state and offers a retry after a failure', async () => {
    const empty = mountPanel({ ranking: { ...ranking(0), total_amount: 0, total_users: 0 } })
    expect(empty.find('[data-ranking-user]').exists()).toBe(false)
    expect(empty.text()).toContain('admin.operations.rechargeRanking.empty')

    const failed = mountPanel({ ranking: null, error: true })
    expect(failed.get('[data-testid="recharge-ranking-total"]').text()).toBe('—')
    expect(failed.text()).not.toContain('admin.operations.rechargeRanking.empty')
    await failed.findAll('button').find(button => button.text() === 'admin.operations.retry')!.trigger('click')
    expect(failed.emitted('retry')).toHaveLength(1)

    // A failed refresh keeps the last ranking and puts the notice above it.
    const stale = mountPanel({ ranking: ranking(3), error: true })
    const notice = stale.get('[role="alert"]').element
    const firstRow = stale.get('[data-ranking-user]').element
    expect(notice.compareDocumentPosition(firstRow) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    expect(stale.findAll('[data-ranking-user]')).toHaveLength(3)
  })
})
