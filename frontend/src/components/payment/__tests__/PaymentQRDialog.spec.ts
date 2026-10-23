import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import PaymentQRDialog from '../PaymentQRDialog.vue'

const pollOrderStatus = vi.hoisted(() => vi.fn())
const cancelOrder = vi.hoisted(() => vi.fn())
const verifyOrder = vi.hoisted(() => vi.fn())
const showError = vi.hoisted(() => vi.fn())
const toCanvas = vi.hoisted(() => vi.fn())

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => key,
    }),
  }
})

vi.mock('@/stores/payment', () => ({
  usePaymentStore: () => ({
    pollOrderStatus,
  }),
}))

vi.mock('@/stores', () => ({
  useAppStore: () => ({
    showError,
  }),
}))

vi.mock('@/api/payment', () => ({
  paymentAPI: {
    cancelOrder,
    verifyOrder,
  },
}))

vi.mock('qrcode', () => ({
  default: {
    toCanvas,
  },
}))

const paidOrder = {
  id: 42,
  user_id: 9,
  amount: 100,
  pay_amount: 108,
  currency: 'CNY',
  fee_rate: 8,
  payment_type: 'alipay',
  out_trade_no: 'sub2_202606250001',
  status: 'COMPLETED',
  order_type: 'subscription',
  created_at: '2026-06-25T10:00:00Z',
  expires_at: '2099-01-01T10:30:00Z',
  refund_amount: 0,
}

describe('PaymentQRDialog', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    pollOrderStatus.mockReset().mockResolvedValue(paidOrder)
    cancelOrder.mockReset()
    verifyOrder.mockReset()
    showError.mockReset()
    toCanvas.mockReset().mockResolvedValue(undefined)
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it.each([
    ['2026-06-25T10:01:00Z', 'payment.qr.fulfillmentFailed', 'payment.qr.fulfillmentFailedDesc'],
    [undefined, 'payment.qr.failed', 'payment.qr.failedDesc'],
  ])('shows a distinct failure result with paid_at=%s', async (paidAt, title, description) => {
    pollOrderStatus.mockResolvedValue({ ...paidOrder, status: 'FAILED', paid_at: paidAt })
    const wrapper = mount(PaymentQRDialog, {
      props: {
        show: false,
        orderId: 42,
        qrCode: '',
        payUrl: 'https://pay.example.com/session/42',
        expiresAt: '2099-01-01T10:30:00Z',
        paymentType: 'alipay',
      },
      global: {
        stubs: {
          BaseDialog: {
            props: ['show'],
            template: '<div v-if="show"><slot /><slot name="footer" /></div>',
          },
          Icon: true,
        },
      },
    })

    await wrapper.setProps({ show: true })
    await flushPromises()
    await vi.advanceTimersByTimeAsync(3000)
    expect(wrapper.text()).toContain(title)
    expect(wrapper.text()).toContain(description)
    expect(wrapper.text()).not.toContain('payment.qr.expired')
    expect(wrapper.text()).not.toContain('payment.qr.openPayWindow')
    expect(wrapper.text()).not.toContain('payment.qr.cancelOrder')
    expect(wrapper.emitted('success')).toBeUndefined()
    const callsAfterFailure = pollOrderStatus.mock.calls.length
    await vi.advanceTimersByTimeAsync(30000)
    expect(pollOrderStatus).toHaveBeenCalledTimes(callsAfterFailure)
    expect(verifyOrder).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('keeps an expired order distinct from a failed fulfillment', async () => {
    pollOrderStatus.mockResolvedValue({ ...paidOrder, status: 'EXPIRED' })
    const wrapper = mount(PaymentQRDialog, {
      props: {
        show: false,
        orderId: 42,
        qrCode: '',
        expiresAt: '2099-01-01T10:30:00Z',
        paymentType: 'alipay',
      },
      global: {
        stubs: {
          BaseDialog: {
            props: ['show'],
            template: '<div v-if="show"><slot /><slot name="footer" /></div>',
          },
          Icon: true,
        },
      },
    })
    await wrapper.setProps({ show: true })
    await flushPromises()
    await vi.advanceTimersByTimeAsync(3000)
    expect(wrapper.text()).toContain('payment.qr.expired')
    expect(wrapper.text()).not.toContain('payment.qr.fulfillmentFailed')
    wrapper.unmount()
  })

  it('uses order currency for pay_amount and USD for credited amount', async () => {
    const wrapper = mount(PaymentQRDialog, {
      props: {
        show: false,
        orderId: 42,
        qrCode: '',
        expiresAt: '2099-01-01T10:30:00Z',
        paymentType: 'alipay',
      },
      global: {
        stubs: {
          BaseDialog: {
            props: ['show'],
            template: '<div v-if="show"><slot /><slot name="footer" /></div>',
          },
          Icon: true,
        },
      },
    })

    await wrapper.setProps({ show: true })
    await flushPromises()
    await vi.advanceTimersByTimeAsync(3000)
    await flushPromises()

    expect(pollOrderStatus).toHaveBeenCalledWith(42)
    expect(wrapper.text()).toContain('$100.00')
    expect(wrapper.text()).toContain('¥108.00')
  })
})
