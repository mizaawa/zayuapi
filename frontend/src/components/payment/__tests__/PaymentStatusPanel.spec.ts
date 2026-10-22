import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

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

import PaymentStatusPanel from '../PaymentStatusPanel.vue'

const orderFactory = (status: string) => ({
  id: 42,
  user_id: 9,
  amount: 88,
  pay_amount: 88,
  fee_rate: 0,
  payment_type: 'alipay',
  out_trade_no: 'sub2_20260420abcd1234',
  status,
  order_type: 'balance',
  created_at: '2026-04-20T12:00:00Z',
  expires_at: '2099-01-01T12:30:00Z',
  refund_amount: 0,
})

describe('PaymentStatusPanel', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    pollOrderStatus.mockReset()
    cancelOrder.mockReset()
    verifyOrder.mockReset()
    showError.mockReset()
    toCanvas.mockReset().mockResolvedValue(undefined)
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it.each([
    ['2026-04-20T12:01:00Z', 'payment.qr.fulfillmentFailed', 'payment.qr.fulfillmentFailedDesc'],
    [undefined, 'payment.qr.failed', 'payment.qr.failedDesc'],
  ])('shows a distinct failure result with paid_at=%s', async (paidAt, title, description) => {
    pollOrderStatus.mockResolvedValue({ ...orderFactory('FAILED'), paid_at: paidAt })
    const wrapper = mount(PaymentStatusPanel, {
      props: {
        orderId: 42,
        qrCode: '',
        expiresAt: '2099-01-01T12:30:00Z',
        paymentType: 'alipay',
        providerKey: 'easypay',
      },
      global: { stubs: { Icon: true } },
    })

    await flushPromises()
    await vi.advanceTimersByTimeAsync(3000)
    expect(wrapper.text()).toContain(title)
    expect(wrapper.text()).toContain(description)
    expect(wrapper.text()).not.toContain('payment.qr.expired')
    expect(wrapper.text()).not.toContain('payment.qr.openPayWindow')
    expect(wrapper.emitted('settled')).toEqual([['failed']])
    expect(wrapper.emitted('success')).toBeUndefined()
    const callsAfterFailure = pollOrderStatus.mock.calls.length
    await vi.advanceTimersByTimeAsync(30000)
    expect(pollOrderStatus).toHaveBeenCalledTimes(callsAfterFailure)
    expect(verifyOrder).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('treats RECHARGING as a successful terminal state', async () => {
    pollOrderStatus.mockResolvedValue(orderFactory('RECHARGING'))

    const wrapper = mount(PaymentStatusPanel, {
      props: {
        orderId: 42,
        qrCode: 'https://pay.example.com/qr/42',
        expiresAt: '2099-01-01T12:30:00Z',
        paymentType: 'alipay',
        orderType: 'balance',
      },
      global: {
        stubs: {
          Icon: true,
        },
      },
    })

    await flushPromises()
    await vi.advanceTimersByTimeAsync(3000)
    await flushPromises()

    expect(pollOrderStatus).toHaveBeenCalledWith(42)
    expect(wrapper.text()).toContain('payment.result.success')
    expect(wrapper.emitted('success')).toHaveLength(1)
  })

  it('shows reopen button in QR mode when payUrl is also available', async () => {
    const openSpy = vi.spyOn(window, 'open').mockReturnValue({ closed: false } as Window)

    const wrapper = mount(PaymentStatusPanel, {
      props: {
        orderId: 42,
        qrCode: 'https://pay.example.com/qr/42',
        payUrl: 'https://pay.example.com/session/42',
        expiresAt: '2099-01-01T12:30:00Z',
        paymentType: 'alipay',
        orderType: 'balance',
      },
      global: {
        stubs: {
          Icon: true,
        },
      },
    })

    await flushPromises()
    expect(wrapper.text()).toContain('payment.qr.openPayWindow')

    await wrapper.get('button.btn.btn-secondary.text-sm').trigger('click')
    expect(openSpy).toHaveBeenCalledWith(
      'https://pay.example.com/session/42',
      'paymentPopup',
      expect.any(String),
    )

    openSpy.mockRestore()
  })

  it('uses generic QR copy for custom methods that contain built-in names', async () => {
    const wrapper = mount(PaymentStatusPanel, {
      props: {
        orderId: 42,
        qrCode: 'https://pay.example.com/qr/42',
        expiresAt: '2099-01-01T12:30:00Z',
        paymentType: 'card_alipay',
        orderType: 'balance',
      },
      global: {
        stubs: {
          Icon: true,
        },
      },
    })

    await flushPromises()

    expect(wrapper.text()).toContain('payment.qr.scanToPay')
    expect(wrapper.text()).not.toContain('payment.qr.scanAlipay')
  })

  it('actively verifies a stuck pending order and settles it when upstream confirms payment', async () => {
    pollOrderStatus.mockResolvedValue(orderFactory('PENDING'))
    verifyOrder.mockResolvedValue({
      data: orderFactory('COMPLETED'),
    })

    const wrapper = mount(PaymentStatusPanel, {
      props: {
        orderId: 42,
        qrCode: 'https://pay.example.com/qr/42',
        expiresAt: '2099-01-01T12:30:00Z',
        paymentType: 'wxpay',
        orderType: 'balance',
      },
      global: {
        stubs: {
          Icon: true,
        },
      },
    })

    await flushPromises()
    await vi.advanceTimersByTimeAsync(3000)
    await flushPromises()

    expect(pollOrderStatus).toHaveBeenCalledWith(42)
    expect(verifyOrder).toHaveBeenCalledWith('sub2_20260420abcd1234')
    expect(wrapper.text()).toContain('payment.result.success')
    expect(wrapper.emitted('success')).toHaveLength(1)
  })

  it.each([
    ['alipay', 'easypay'],
    ['creditcard', 'easypay'],
    ['alipay', 'easypay_alipay'],
  ])('actively verifies pending %s orders from %s', async (paymentType, providerKey) => {
    pollOrderStatus.mockResolvedValue(orderFactory('PENDING'))
    verifyOrder.mockResolvedValue({ data: orderFactory('COMPLETED') })

    const wrapper = mount(PaymentStatusPanel, {
      props: {
        orderId: 42,
        qrCode: 'https://pay.example.com/qr/42',
        expiresAt: '2099-01-01T12:30:00Z',
        paymentType,
        providerKey,
        orderType: 'balance',
      },
      global: { stubs: { Icon: true } },
    })

    await flushPromises()
    await vi.advanceTimersByTimeAsync(3000)
    await flushPromises()

    expect(verifyOrder).toHaveBeenCalledWith('sub2_20260420abcd1234')
    expect(wrapper.emitted('success')).toHaveLength(1)
    wrapper.unmount()
  })

  it('throttles EasyPay verification while keeping an unpaid order pending', async () => {
    pollOrderStatus.mockResolvedValue(orderFactory('PENDING'))
    verifyOrder.mockResolvedValue({ data: orderFactory('PENDING') })

    const wrapper = mount(PaymentStatusPanel, {
      props: {
        orderId: 42,
        qrCode: 'https://pay.example.com/qr/42',
        expiresAt: '2099-01-01T12:30:00Z',
        paymentType: 'alipay',
        providerKey: 'easypay',
        orderType: 'balance',
      },
      global: { stubs: { Icon: true } },
    })

    await flushPromises()
    await vi.advanceTimersByTimeAsync(12000)
    expect(verifyOrder).toHaveBeenCalledTimes(1)
    expect(wrapper.emitted('success')).toBeUndefined()
    await vi.advanceTimersByTimeAsync(6000)
    expect(verifyOrder).toHaveBeenCalledTimes(2)
    wrapper.unmount()
  })

  it('verifies an already expired EasyPay order before showing a terminal result', async () => {
    pollOrderStatus.mockResolvedValue(orderFactory('EXPIRED'))
    verifyOrder.mockResolvedValue({ data: orderFactory('COMPLETED') })
    const wrapper = mount(PaymentStatusPanel, {
      props: {
        orderId: 42,
        qrCode: 'https://pay.example.com/qr/42',
        expiresAt: new Date(Date.now() - 1000).toISOString(),
        paymentType: 'alipay',
        providerKey: 'easypay',
      },
      global: { stubs: { Icon: true } },
    })

    expect(wrapper.emitted('settled')).toBeUndefined()
    await flushPromises()
    expect(pollOrderStatus).toHaveBeenCalledTimes(1)
    expect(verifyOrder).toHaveBeenCalledWith('sub2_20260420abcd1234')
    expect(wrapper.emitted('settled')).toEqual([['success']])
    wrapper.unmount()
  })

  it('waits for a successful in-flight confirmation when the countdown expires', async () => {
    let resolveVerify!: (value: { data: ReturnType<typeof orderFactory> }) => void
    pollOrderStatus.mockResolvedValue(orderFactory('PENDING'))
    verifyOrder.mockImplementation(() => new Promise(resolve => { resolveVerify = resolve }))
    const wrapper = mount(PaymentStatusPanel, {
      props: {
        orderId: 42,
        qrCode: 'https://pay.example.com/qr/42',
        expiresAt: new Date(Date.now() + 4000).toISOString(),
        paymentType: 'alipay',
        providerKey: 'easypay',
      },
      global: { stubs: { Icon: true } },
    })

    await vi.advanceTimersByTimeAsync(4000)
    expect(verifyOrder).toHaveBeenCalledTimes(1)
    expect(wrapper.emitted('settled')).toBeUndefined()
    resolveVerify({ data: orderFactory('COMPLETED') })
    await flushPromises()
    expect(wrapper.emitted('settled')).toEqual([['success']])
    expect(pollOrderStatus).toHaveBeenCalledTimes(1)
    wrapper.unmount()
  })

  it('runs the final EasyPay verification after the regular retry budget is exhausted', async () => {
    pollOrderStatus.mockResolvedValue(orderFactory('PENDING'))
    verifyOrder.mockResolvedValue({ data: orderFactory('PENDING') })
    const wrapper = mount(PaymentStatusPanel, {
      props: {
        orderId: 42,
        qrCode: 'https://pay.example.com/qr/42',
        expiresAt: new Date(Date.now() + 100000).toISOString(),
        paymentType: 'alipay',
        providerKey: 'easypay',
      },
      global: { stubs: { Icon: true } },
    })

    await vi.advanceTimersByTimeAsync(99000)
    expect(verifyOrder).toHaveBeenCalledTimes(6)
    verifyOrder.mockResolvedValueOnce({ data: orderFactory('COMPLETED') })
    await vi.advanceTimersByTimeAsync(1000)
    expect(verifyOrder).toHaveBeenCalledTimes(7)
    expect(wrapper.emitted('settled')).toEqual([['success']])
    wrapper.unmount()
  })

  it('keeps an expired EasyPay order retryable when its final verification fails', async () => {
    pollOrderStatus.mockResolvedValue(orderFactory('EXPIRED'))
    verifyOrder.mockRejectedValueOnce(new Error('Gateway unavailable'))
      .mockResolvedValueOnce({ data: orderFactory('COMPLETED') })
    const wrapper = mount(PaymentStatusPanel, {
      props: {
        orderId: 42,
        qrCode: 'https://pay.example.com/qr/42',
        expiresAt: new Date(Date.now() - 1000).toISOString(),
        paymentType: 'alipay',
        providerKey: 'easypay',
      },
      global: { stubs: { Icon: true } },
    })

    await flushPromises()
    expect(wrapper.emitted('settled')).toBeUndefined()
    expect(wrapper.find('[data-test="payment-expiry-processing"]').exists()).toBe(true)
    expect(wrapper.find('canvas').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('payment.qr.openPayWindow')
    await vi.advanceTimersByTimeAsync(12000)
    expect(verifyOrder).toHaveBeenCalledTimes(1)
    await vi.advanceTimersByTimeAsync(3000)
    expect(wrapper.emitted('settled')).toEqual([['success']])
    wrapper.unmount()
  })

  it.each(['PENDING', 'EXPIRED'])('retries an HTTP 200 %s result after expiry until payment is confirmed', async (status) => {
    pollOrderStatus.mockResolvedValue(orderFactory(status))
    verifyOrder.mockResolvedValueOnce({ data: orderFactory(status) })
      .mockResolvedValueOnce({ data: orderFactory('COMPLETED') })
    const wrapper = mount(PaymentStatusPanel, {
      props: {
        orderId: 42,
        qrCode: 'https://pay.example.com/qr/42',
        expiresAt: new Date(Date.now() - 1000).toISOString(),
        paymentType: 'alipay',
        providerKey: 'easypay',
      },
      global: { stubs: { Icon: true } },
    })

    await flushPromises()
    expect(wrapper.emitted('settled')).toBeUndefined()
    await vi.advanceTimersByTimeAsync(12000)
    expect(verifyOrder).toHaveBeenCalledTimes(1)
    await vi.advanceTimersByTimeAsync(3000)
    expect(verifyOrder).toHaveBeenCalledTimes(2)
    expect(wrapper.emitted('settled')).toEqual([['success']])
    wrapper.unmount()
  })

  it('bounds post-expiry verification when the gateway never confirms payment', async () => {
    pollOrderStatus.mockResolvedValue(orderFactory('EXPIRED'))
    verifyOrder.mockResolvedValue({ data: orderFactory('EXPIRED') })
    const wrapper = mount(PaymentStatusPanel, {
      props: {
        orderId: 42,
        qrCode: 'https://pay.example.com/qr/42',
        expiresAt: new Date(Date.now() - 1000).toISOString(),
        paymentType: 'alipay',
        providerKey: 'easypay',
      },
      global: { stubs: { Icon: true } },
    })

    await flushPromises()
    await vi.advanceTimersByTimeAsync(195000)
    expect(verifyOrder).toHaveBeenCalledTimes(14)
    expect(wrapper.emitted('settled')).toBeUndefined()
    await vi.advanceTimersByTimeAsync(15000)
    expect(verifyOrder).toHaveBeenCalledTimes(15)
    expect(wrapper.emitted('settled')).toEqual([['expired']])
    expect(wrapper.text()).toContain('payment.qr.expiredDesc')
    expect(wrapper.text()).not.toContain('payment.qr.fulfillmentFailed')
    await vi.advanceTimersByTimeAsync(30000)
    expect(verifyOrder).toHaveBeenCalledTimes(15)
    wrapper.unmount()
  })

  it('actively verifies a pending mobile Alipay precreate order', async () => {
    const originalLocation = window.location
    const originalHidden = Object.getOwnPropertyDescriptor(document, 'hidden')
    Object.defineProperty(window, 'location', {
      configurable: true,
      value: { assign: vi.fn() },
    })
    Object.defineProperty(document, 'hidden', {
      configurable: true,
      get: () => false,
    })
    pollOrderStatus.mockResolvedValue(orderFactory('PENDING'))
    verifyOrder.mockResolvedValue({ data: orderFactory('COMPLETED') })

    const wrapper = mount(PaymentStatusPanel, {
      props: {
        orderId: 42,
        amount: 88,
        payAmount: 88,
        qrCode: 'https://qr.alipay.com/dynamic-order-42',
        expiresAt: '2099-01-01T12:30:00Z',
        paymentType: 'alipay',
        orderType: 'balance',
        outTradeNo: 'sub2_20260420abcd1234',
        mobileAlipayDeepLink: true,
      },
      global: { stubs: { Icon: true } },
    })

    await flushPromises()
    await vi.advanceTimersByTimeAsync(3000)
    await flushPromises()

    expect(verifyOrder).toHaveBeenCalledWith('sub2_20260420abcd1234')
    expect(wrapper.emitted('success')).toHaveLength(1)

    wrapper.unmount()
    Object.defineProperty(window, 'location', { configurable: true, value: originalLocation })
    if (originalHidden) Object.defineProperty(document, 'hidden', originalHidden)
  })

  it('keeps the QR fallback hidden until the Alipay app launch times out', async () => {
    const originalLocation = window.location
    const originalHidden = Object.getOwnPropertyDescriptor(document, 'hidden')
    const assign = vi.fn()
    Object.defineProperty(window, 'location', {
      configurable: true,
      value: { assign },
    })
    Object.defineProperty(document, 'hidden', {
      configurable: true,
      get: () => false,
    })

    const wrapper = mount(PaymentStatusPanel, {
      props: {
        orderId: 42,
        amount: 88,
        payAmount: 88,
        qrCode: 'https://qr.alipay.com/dynamic-order-42',
        expiresAt: '2099-01-01T12:30:00Z',
        paymentType: 'alipay',
        orderType: 'balance',
        outTradeNo: 'sub2_20260420abcd1234',
        mobileAlipayDeepLink: true,
      },
      global: { stubs: { Icon: true } },
    })

    await flushPromises()
    expect(assign).toHaveBeenCalledWith(expect.stringContaining('alipays://platformapi/startapp?saId=10000007&qrcode='))
    expect(wrapper.find('[data-test="alipay-qr-fallback"]').exists()).toBe(false)

    await vi.advanceTimersByTimeAsync(2200)
    await flushPromises()

    expect(wrapper.find('[data-test="alipay-qr-fallback"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('payment.qr.saveQRCode')
    expect(wrapper.text()).toContain('sub2_20260420abcd1234')
    expect(toCanvas).toHaveBeenCalledWith(expect.any(HTMLCanvasElement), 'https://qr.alipay.com/dynamic-order-42', expect.any(Object))

    wrapper.unmount()
    Object.defineProperty(window, 'location', { configurable: true, value: originalLocation })
    if (originalHidden) Object.defineProperty(document, 'hidden', originalHidden)
  })

  it('does not show the QR fallback after the page enters the background', async () => {
    const originalLocation = window.location
    const originalHidden = Object.getOwnPropertyDescriptor(document, 'hidden')
    let hidden = false
    Object.defineProperty(window, 'location', {
      configurable: true,
      value: { assign: vi.fn() },
    })
    Object.defineProperty(document, 'hidden', {
      configurable: true,
      get: () => hidden,
    })

    const wrapper = mount(PaymentStatusPanel, {
      props: {
        orderId: 42,
        amount: 88,
        payAmount: 88,
        qrCode: 'https://qr.alipay.com/dynamic-order-42',
        expiresAt: '2099-01-01T12:30:00Z',
        paymentType: 'alipay',
        orderType: 'balance',
        outTradeNo: 'sub2_20260420abcd1234',
        mobileAlipayDeepLink: true,
      },
      global: { stubs: { Icon: true } },
    })

    await flushPromises()
    hidden = true
    document.dispatchEvent(new Event('visibilitychange'))
    await vi.advanceTimersByTimeAsync(2200)
    await flushPromises()

    expect(wrapper.find('[data-test="alipay-qr-fallback"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('payment.qr.alipayContinueInApp')

    wrapper.unmount()
    Object.defineProperty(window, 'location', { configurable: true, value: originalLocation })
    if (originalHidden) Object.defineProperty(document, 'hidden', originalHidden)
  })
})
