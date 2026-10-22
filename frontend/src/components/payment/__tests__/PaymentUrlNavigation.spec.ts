import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import PaymentQRDialog from '../PaymentQRDialog.vue'
import PaymentStatusPanel from '../PaymentStatusPanel.vue'

const toCanvas = vi.hoisted(() => vi.fn())
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/stores/payment', () => ({ usePaymentStore: () => ({ pollOrderStatus: vi.fn() }) }))
vi.mock('@/stores', () => ({ useAppStore: () => ({ showError: vi.fn() }) }))
vi.mock('@/api/payment', () => ({ paymentAPI: { cancelOrder: vi.fn(), verifyOrder: vi.fn() } }))
vi.mock('qrcode', () => ({ default: { toCanvas } }))

describe.each([['dialog', PaymentQRDialog], ['status panel', PaymentStatusPanel]] as const)('%s payment URL navigation', (_name, component) => {
  beforeEach(() => {
    vi.useFakeTimers()
    toCanvas.mockReset().mockResolvedValue(undefined)
  })
  afterEach(() => { vi.restoreAllMocks(); vi.useRealTimers() })

  const render = async (payUrl: string, qrCode = '') => {
    const wrapper = mount(component, {
      props: { show: false, orderId: 42, payUrl, qrCode, paymentType: 'custom', expiresAt: '2099-01-01T00:30:00Z' },
      global: { stubs: { Icon: true, BaseDialog: { props: ['show'], template: '<div v-if="show"><slot /><slot name="footer" /></div>' } } },
    })
    await wrapper.setProps({ show: true })
    await flushPromises()
    return wrapper
  }

  it.each(['javascript:alert(1)', 'java\tscript:alert(1)', 'data:text/html,bad', 'file:///tmp/payment', 'intent://payment'])('does not offer an unsafe payment URL: %s', async (url) => {
    const openSpy = vi.spyOn(window, 'open').mockReturnValue({ closed: false } as Window)
    const wrapper = await render(url, url)
    expect(wrapper.text()).not.toContain('payment.qr.openPayWindow')
    expect(toCanvas).not.toHaveBeenCalled()
    expect(openSpy).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it.each(['https://pay.example/order/42', 'alipays://platformapi/startapp?saId=42'])('opens a supported payment URL: %s', async (url) => {
    const openSpy = vi.spyOn(window, 'open').mockReturnValue({ closed: false } as Window)
    const wrapper = await render(url)
    const button = wrapper.findAll('button').find((entry) => entry.text() === 'payment.qr.openPayWindow')
    expect(button).toBeDefined()
    await button!.trigger('click')
    expect(openSpy).toHaveBeenCalledWith(url, 'paymentPopup', expect.any(String))
    wrapper.unmount()
  })

  it.each(['weixin://wxpay/bizpayurl?pr=42', 'wxp://payment-token'])('renders a supported payment QR: %s', async (url) => {
    const wrapper = await render('', url)
    expect(toCanvas).toHaveBeenCalledWith(expect.any(HTMLCanvasElement), url, expect.any(Object))
    wrapper.unmount()
  })
})
