import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'

vi.mock('vue-i18n', async () => ({
  ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'),
  useI18n: () => ({ t: (key: string) => key }),
}))

import UsageAnalysisPanel from '../UsageAnalysisPanel.vue'

function mountPanel(granularity: 'day' | 'hour' = 'day') {
  return mount(UsageAnalysisPanel, {
    props: { startDate: '2026-10-01', endDate: '2026-10-29', granularity },
    slots: { default: '<div class="card">chart</div>' },
    global: { stubs: { DateRangePicker: { template: '<div data-testid="range" />' } } },
  })
}

describe('UsageAnalysisPanel', () => {
  it('switches granularity with a segmented control and reports the change once', async () => {
    const wrapper = mountPanel('day')

    expect(wrapper.get('[data-testid="usage-granularity-day"]').attributes('aria-checked')).toBe('true')
    await wrapper.get('[data-testid="usage-granularity-day"]').trigger('click')
    expect(wrapper.emitted('granularityChange')).toBeUndefined()

    await wrapper.get('[data-testid="usage-granularity-hour"]').trigger('click')
    expect(wrapper.emitted('update:granularity')).toEqual([['hour']])
    expect(wrapper.emitted('granularityChange')).toEqual([['hour']])
  })

  it('renders the charts it is given inside the panel', () => {
    const wrapper = mountPanel()
    expect(wrapper.get('.usage-analysis-grid').text()).toContain('chart')
    expect(wrapper.find('[data-testid="range"]').exists()).toBe(true)
  })
})
