import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'

vi.mock('vue-i18n', async () => ({
  ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'),
  useI18n: () => ({ t: (key: string) => key }),
}))

vi.mock('vue-chartjs', () => ({
  Line: {
    props: ['data', 'options'],
    template: '<div class="chart-data">{{ JSON.stringify(data) }}</div>',
  },
}))

import UserTokenTrendCard from '../dashboard/UserTokenTrendCard.vue'
import type { TrendDataPoint } from '@/types'

const point = (date: string, input: number, output: number, cacheWrite: number, cacheRead: number): TrendDataPoint => ({
  date,
  requests: 1,
  input_tokens: input,
  output_tokens: output,
  cache_creation_tokens: cacheWrite,
  cache_read_tokens: cacheRead,
  total_tokens: input + output + cacheWrite + cacheRead,
  cost: 0,
  actual_cost: 0,
})

function mountCard(trend: TrendDataPoint[], props: Record<string, unknown> = {}) {
  return mount(UserTokenTrendCard, {
    props: { trend, range: 7, startDate: '2026-10-23', endDate: '2026-10-29', ...props },
    global: { stubs: { Icon: true, LoadingSpinner: true } },
  })
}

const chartData = (wrapper: ReturnType<typeof mountCard>) => JSON.parse(wrapper.get('.chart-data').text())

describe('UserTokenTrendCard', () => {
  it('plots every day of the range, filling quiet days with zero', () => {
    const wrapper = mountCard([point('2026-10-24', 100, 10, 20, 300), point('2026-10-29', 50, 5, 0, 50)])
    const data = chartData(wrapper)

    expect(data.labels).toEqual(['2026-10-23', '2026-10-24', '2026-10-25', '2026-10-26', '2026-10-27', '2026-10-28', '2026-10-29'])
    const [total, hit, miss] = data.datasets
    expect(total.data).toEqual([0, 430, 0, 0, 0, 0, 105])
    expect(hit.data).toEqual([0, 300, 0, 0, 0, 0, 50])
    // Misses are the prompt tokens that did not come from cache: input + cache writes.
    expect(miss.data).toEqual([0, 120, 0, 0, 0, 0, 50])
  })

  it('sums the range and computes the hit rate from hits and misses', () => {
    const wrapper = mountCard([point('2026-10-24', 100, 10, 20, 300), point('2026-10-29', 50, 5, 0, 50)])

    expect(wrapper.get('[data-testid="token-trend-kpi-total"]').text()).toContain('535')
    expect(wrapper.get('[data-testid="token-trend-kpi-hit"]').text()).toContain('350')
    expect(wrapper.get('[data-testid="token-trend-kpi-miss"]').text()).toContain('170')
    // 350 / (350 + 170) = 67.3%
    expect(wrapper.get('[data-testid="token-trend-kpi-rate"]').text()).toContain('67.3%')
  })

  it('shows no hit rate when nothing was sent', () => {
    const wrapper = mountCard([])
    expect(wrapper.get('[data-testid="token-trend-kpi-rate"]').text()).toContain('—')
    expect(wrapper.text()).toContain('dashboard.tokenTrend.empty')
  })

  it('switches ranges through the parent and toggles a series without repainting the others', async () => {
    const wrapper = mountCard([point('2026-10-29', 1, 1, 1, 1)])

    await wrapper.get('[data-testid="token-trend-range-7"]').trigger('click')
    expect(wrapper.emitted('update:range')).toBeUndefined()
    await wrapper.get('[data-testid="token-trend-range-30"]').trigger('click')
    expect(wrapper.emitted('update:range')).toEqual([[30]])

    const colours = chartData(wrapper).datasets.map((dataset: { borderColor: string }) => dataset.borderColor)
    await wrapper.get('[data-testid="token-trend-kpi-hit"]').trigger('click')
    const datasets = chartData(wrapper).datasets
    expect(datasets.map((dataset: { hidden: boolean }) => dataset.hidden)).toEqual([false, true, false])
    expect(datasets.map((dataset: { borderColor: string }) => dataset.borderColor)).toEqual(colours)
  })

  it('offers a daily table as the accessible twin of the chart', async () => {
    const wrapper = mountCard([point('2026-10-29', 50, 5, 0, 50)])

    await wrapper.get('[data-testid="token-trend-view-toggle"]').trigger('click')
    const rows = wrapper.findAll('tbody tr')
    expect(rows).toHaveLength(7)
    // Newest day first.
    expect(rows[0].text()).toContain('2026-10-29')
    expect(rows[0].text()).toContain('50.0%')
    expect(rows[1].text()).toContain('—')
  })
})
