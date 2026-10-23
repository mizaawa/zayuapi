import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import PaymentQRCodeView from '../PaymentQRCodeView.vue'

const route = vi.hoisted(() => ({ query: {} as Record<string, unknown> }))
const getOrder = vi.hoisted(() => vi.fn())
const pollOrderStatus = vi.hoisted(() => vi.fn())
const cancelOrder = vi.hoisted(() => vi.fn())
const routerPush = vi.hoisted(() => vi.fn())
const routerReplace = vi.hoisted(() => vi.fn())
const toCanvas = vi.hoisted(() => vi.fn())

vi.mock('vue-router', () => ({ useRoute: () => route, useRouter: () => ({ push: routerPush, replace: routerReplace }) }))
vi.mock('vue-i18n', async () => ({
  ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'),
  useI18n: () => ({ t: (key: string) => key }),
}))
vi.mock('@/stores/payment', () => ({ usePaymentStore: () => ({ pollOrderStatus }) }))
vi.mock('@/stores', () => ({ useAppStore: () => ({ showError: vi.fn() }) }))
vi.mock('@/api/payment', () => ({ paymentAPI: { getOrder, cancelOrder } }))
vi.mock('qrcode', () => ({ default: { toCanvas } }))

const order = (overrides: Record<string, unknown> = {}) => ({
  id: 42,
  payment_type: 'custom',
  status: 'PENDING',
  expires_at: '2099-01-01T00:30:00Z',
  pay_url: 'https://trusted-pay.example/order/42',
  qr_code: '',
  ...overrides,
})

const renderView = () => mount(PaymentQRCodeView, {
  global: { stubs: { AppLayout: { template: '<main><slot /></main>' } } },
})

describe('PaymentQRCodeView server-bound payment contents', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    route.query = { order_id: '42', qr: 'https://attacker.example/qr', pay_url: 'javascript:alert(1)', expires_at: '2099-01-01', payment_type: 'wxpay' }
    getOrder.mockReset().mockResolvedValue({ data: order() })
    pollOrderStatus.mockReset().mockResolvedValue(order())
    cancelOrder.mockReset()
    routerPush.mockReset()
    routerReplace.mockReset()
    toCanvas.mockReset().mockResolvedValue(undefined)
  })
  afterEach(() => { vi.useRealTimers() })

  it.each([undefined, '', '0', '-1', '1.5', '42e0', ['42'], '9007199254740992'])('never displays query payment contents without a valid order id: %s', async (id) => {
    route.query.order_id = id
    const wrapper = renderView()
    await flushPromises()
    expect(getOrder).not.toHaveBeenCalled()
    expect(wrapper.find('a').exists()).toBe(false)
    expect(wrapper.find('canvas').exists()).toBe(false)
    expect(wrapper.find('[role="alert"]').exists()).toBe(true)
    expect(toCanvas).not.toHaveBeenCalled()
    expect(vi.getTimerCount()).toBe(0)
    wrapper.unmount()
  })

  it.each([403, 404])('hides all payment contents when owner lookup fails with HTTP %i', async (status) => {
    getOrder.mockRejectedValue({ status, message: 'order unavailable' })
    const wrapper = renderView()
    await flushPromises()
    expect(getOrder).toHaveBeenCalledWith(42)
    expect(wrapper.find('[role="alert"]').text()).toContain('order unavailable')
    expect(wrapper.find('a').exists()).toBe(false)
    expect(wrapper.find('canvas').exists()).toBe(false)
    expect(toCanvas).not.toHaveBeenCalled()
    expect(vi.getTimerCount()).toBe(0)
    wrapper.unmount()
  })

  it('waits for owner validation, then uses only the server payment URL', async () => {
    let resolveOrder!: (value: { data: ReturnType<typeof order> }) => void
    getOrder.mockReturnValue(new Promise((resolve) => { resolveOrder = resolve }))
    const wrapper = renderView()
    expect(wrapper.find('[role="status"]').exists()).toBe(true)
    expect(wrapper.find('a').exists()).toBe(false)
    expect(wrapper.find('canvas').exists()).toBe(false)
    resolveOrder({ data: order() })
    await flushPromises()
    expect(wrapper.get('a').attributes('href')).toBe('https://trusted-pay.example/order/42')
    expect(wrapper.html()).not.toContain('attacker.example')
    expect(wrapper.html()).not.toContain('javascript:')
    expect(toCanvas).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('renders only the server-bound WeChat QR and ignores query type and QR', async () => {
    getOrder.mockResolvedValue({ data: order({ qr_code: 'weixin://wxpay/bizpayurl?pr=server-42', payment_type: 'custom' }) })
    const wrapper = renderView()
    await flushPromises()
    expect(toCanvas).toHaveBeenCalledWith(expect.any(HTMLCanvasElement), 'weixin://wxpay/bizpayurl?pr=server-42', expect.any(Object))
    expect(wrapper.text()).toContain('payment.qr.scanToPay')
    expect(wrapper.text()).not.toContain('payment.qr.scanWxpay')
    expect(wrapper.html()).not.toContain('attacker.example')
    wrapper.unmount()
  })

  it('rejects unsafe stored links even when a real order exists', async () => {
    getOrder.mockResolvedValue({ data: order({ pay_url: 'javascript:alert(1)', qr_code: 'data:text/html,bad' }) })
    const wrapper = renderView()
    await flushPromises()
    expect(wrapper.find('[role="alert"]').text()).toContain('payment.errors.PAYMENT_GATEWAY_ERROR')
    expect(wrapper.find('a').exists()).toBe(false)
    expect(wrapper.find('canvas').exists()).toBe(false)
    expect(toCanvas).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('uses server expiry and refuses to revive an expired order using the query', async () => {
    getOrder.mockResolvedValue({ data: order({ expires_at: '2000-01-01T00:00:00Z', qr_code: 'wxp://server-42' }) })
    const wrapper = renderView()
    await flushPromises()
    expect(wrapper.text()).toContain('payment.qr.expired')
    expect(wrapper.find('a').exists()).toBe(false)
    expect(wrapper.find('canvas').exists()).toBe(false)
    expect(toCanvas).not.toHaveBeenCalled()
    expect(vi.getTimerCount()).toBe(0)
    wrapper.unmount()
  })

  it('routes a settled server order to the result without offering payment', async () => {
    getOrder.mockResolvedValue({ data: order({ status: 'COMPLETED' }) })
    const wrapper = renderView()
    await flushPromises()
    expect(routerReplace).toHaveBeenCalledWith({ path: '/payment/result', query: { order_id: '42', status: 'success' } })
    expect(wrapper.find('a').exists()).toBe(false)
    expect(toCanvas).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('does not start payment polling after the page is unmounted', async () => {
    let resolveOrder!: (value: { data: ReturnType<typeof order> }) => void
    getOrder.mockReturnValue(new Promise((resolve) => { resolveOrder = resolve }))
    const wrapper = renderView()
    wrapper.unmount()
    resolveOrder({ data: order({ qr_code: 'wxp://server-42' }) })
    await flushPromises()
    expect(toCanvas).not.toHaveBeenCalled()
    expect(vi.getTimerCount()).toBe(0)
  })
})
