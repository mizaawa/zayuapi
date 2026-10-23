import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import AmountInput from '@/components/payment/AmountInput.vue'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key }),
}))

describe('AmountInput', () => {
  it('hides the quick amount section when the configured list is empty', () => {
    const wrapper = mount(AmountInput, {
      props: { modelValue: null, amounts: [] },
    })

    expect(wrapper.find('button').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('payment.quickAmounts')
    expect(wrapper.find('input').exists()).toBe(true)
  })

  it('filters configured amounts by the active limits while preserving order', () => {
    const wrapper = mount(AmountInput, {
      props: { modelValue: null, amounts: [500, 10, 100], min: 50, max: 500 },
    })

    expect(wrapper.findAll('button').map((button) => button.text())).toEqual(['500', '100'])
  })
})
