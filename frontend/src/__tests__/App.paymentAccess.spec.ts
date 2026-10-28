import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { h, nextTick, reactive } from 'vue'
import { createI18n } from 'vue-i18n'
import { createMemoryHistory, createRouter, type Router } from 'vue-router'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import App from '../App.vue'
import NotFoundView from '@/views/NotFoundView.vue'

const stores = vi.hoisted(() => ({ app: {} as any, auth: {} as any, payment: {} as any }))
vi.mock('@/stores', () => ({
  useAppStore: () => stores.app,
  useAuthStore: () => stores.auth,
  useSubscriptionStore: () => ({ fetchActiveSubscriptions: vi.fn().mockResolvedValue([]), startPolling: vi.fn(), clear: vi.fn() }),
  useAnnouncementStore: () => ({ fetchAnnouncements: vi.fn(), reset: vi.fn() }),
  useAdminComplianceStore: () => ({ reset: vi.fn(), requireAcknowledgement: vi.fn() }),
  useAdminSettingsStore: () => ({ customMenuItems: [] }),
}))
vi.mock('@/stores/paymentAccess', async importOriginal => ({
  ...await importOriginal<typeof import('@/stores/paymentAccess')>(),
  usePaymentAccessStore: () => stores.payment,
}))
vi.mock('@/api/setup', () => ({ getSetupStatus: vi.fn().mockResolvedValue({ needs_setup: false }) }))
vi.mock('@/utils/branding', () => ({ updateFavicon: vi.fn() }))
vi.mock('@/components/admin/AdminComplianceDialog.vue', () => ({ default: { template: '<div />' } }))

let wrapper: VueWrapper | undefined
let router: Router
const unmounted = vi.fn()

beforeEach(() => {
  unmounted.mockReset()
  Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'hidden' })
  stores.app = reactive({
    siteName: 'Test', siteLogo: '', cachedPublicSettings: { payment_exclusive_enabled: false },
    fetchPublicSettings: vi.fn().mockResolvedValue({ payment_exclusive_enabled: false }),
  })
  stores.auth = reactive({ isAuthenticated: true, isAdmin: false, user: { id: 10 } })
  stores.payment = reactive({
    allowed: true, access: { enabled: true, allowed: true, exclusive_enabled: true },
    refresh: vi.fn().mockResolvedValue(null), waitForChange: vi.fn(() => new Promise(() => {})),
    stopWatching: vi.fn(), reset: vi.fn(),
    deny: vi.fn(() => {
      stores.payment.allowed = false
      stores.payment.access = { enabled: true, allowed: false, exclusive_enabled: true }
    }),
  })
})

afterEach(() => {
  wrapper?.unmount()
  wrapper = undefined
  vi.restoreAllMocks()
})

async function mountPayment(path = '/purchase?from=test#resume', requiresAuth = true) {
  router = createRouter({
    history: createMemoryHistory(),
    routes: [
      {
        path: path.split('?')[0], name: 'Payment',
        component: { render: () => h('div', { 'data-testid': 'protected-payment' }, 'Private payment'), unmounted },
        meta: { requiresAuth, title: 'Payment details' },
      },
      { path: '/:pathMatch(.*)*', name: 'NotFound', component: NotFoundView, meta: { requiresAuth: false, title: '404 Not Found' } },
    ],
  })
  await router.push(path)
  wrapper = mount(App, {
    global: {
      plugins: [router, createI18n({ legacy: false, locale: 'en', messages: { en: { errors: { pageNotFound: () => 'Page not found' } } } })],
      stubs: { Toast: true, NavigationProgress: true, AnnouncementPopup: true, AdminComplianceDialog: true },
    },
  })
  await flushPromises()
}

describe('open payment page authorization', () => {
  it('unmounts a revoked page immediately and renders the ordinary 404 at the same URL', async () => {
    await mountPayment()
    expect(wrapper!.find('[data-testid="protected-payment"]').exists()).toBe(true)
    let finishNavigation!: () => void
    router.beforeEach(to => to.name === 'NotFound' ? new Promise<void>(done => { finishNavigation = done }) : true)
    stores.payment.allowed = false
    await nextTick()
    expect(wrapper!.find('[data-testid="protected-payment"]').exists()).toBe(false)
    expect(wrapper!.text()).toContain('Page not found')
    expect(unmounted).toHaveBeenCalledOnce()
    expect(document.title).toBe('404 Not Found - Test')
    await vi.waitFor(() => expect(finishNavigation).toBeTypeOf('function'))
    finishNavigation()
    await flushPromises()
    expect(router.currentRoute.value.name).toBe('NotFound')
    expect(router.currentRoute.value.fullPath).toBe('/purchase?from=test#resume')
  })

  it('hides an anonymous continuation immediately when its payment request is denied', async () => {
    stores.auth.isAuthenticated = false
    stores.auth.user = null
    stores.payment.allowed = false
    stores.payment.access = null
    await mountPayment('/payment/result?resume_token=opaque', false)
    expect(wrapper!.find('[data-testid="protected-payment"]').exists()).toBe(true)
    window.dispatchEvent(new CustomEvent('payment-access-denied', { detail: { status: 404 } }))
    await flushPromises()
    expect(wrapper!.find('[data-testid="protected-payment"]').exists()).toBe(false)
    expect(router.currentRoute.value.name).toBe('NotFound')
    expect(router.currentRoute.value.fullPath).toBe('/payment/result?resume_token=opaque')
  })

  it('keeps an authorized payment page visible when a missing order returns the common 404', async () => {
    await mountPayment()
    stores.payment.refresh.mockResolvedValue({ enabled: true, allowed: true, exclusive_enabled: true })
    window.dispatchEvent(new CustomEvent('payment-access-denied', { detail: { status: 404 } }))
    await flushPromises()
    expect(stores.payment.refresh).toHaveBeenCalledWith(true)
    expect(stores.payment.deny).not.toHaveBeenCalled()
    expect(wrapper!.find('[data-testid="protected-payment"]').exists()).toBe(true)
    expect(router.currentRoute.value.name).toBe('Payment')
  })

  it('hides a payment page when the common 404 confirms that its grant was revoked', async () => {
    await mountPayment()
    stores.payment.refresh.mockImplementation(async () => {
      stores.payment.allowed = false
      stores.payment.access = { enabled: false, allowed: false, exclusive_enabled: false }
      return stores.payment.access
    })
    window.dispatchEvent(new CustomEvent('payment-access-denied', { detail: { status: 404 } }))
    await flushPromises()
    expect(stores.payment.refresh).toHaveBeenCalledWith(true)
    expect(wrapper!.find('[data-testid="protected-payment"]').exists()).toBe(false)
    expect(router.currentRoute.value.name).toBe('NotFound')
    expect(router.currentRoute.value.fullPath).toBe('/purchase?from=test#resume')
  })
})

describe('payment access watch loop', () => {
  afterEach(() => {
    vi.useRealTimers()
    delete (document as { hidden?: boolean }).hidden
  })

  it('backs off failed watches, keeps the open page, and resumes immediately after an answer', async () => {
    Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'visible' })
    Object.defineProperty(document, 'hidden', { configurable: true, value: false })
    const outcomes = [false, false, false, true]
    stores.payment.waitForChange.mockImplementation(() => outcomes.length > 0
      ? Promise.resolve(outcomes.shift())
      : new Promise(() => {}))
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
    await mountPayment()
    const watches = () => stores.payment.waitForChange.mock.calls.length

    await vi.advanceTimersByTimeAsync(0)
    expect(watches()).toBe(1)
    await vi.advanceTimersByTimeAsync(2999)
    expect(watches()).toBe(1)
    await vi.advanceTimersByTimeAsync(1)
    expect(watches()).toBe(2)
    await vi.advanceTimersByTimeAsync(5999)
    expect(watches()).toBe(2)
    await vi.advanceTimersByTimeAsync(1)
    expect(watches()).toBe(3)
    await vi.advanceTimersByTimeAsync(12000)
    expect(watches()).toBe(4)
    // A delivered answer starts the next watch right away (fake timers turn 0ms into 1ms mid-tick).
    await vi.advanceTimersByTimeAsync(1)
    expect(watches()).toBe(5)
    expect(wrapper!.find('[data-testid="protected-payment"]').exists()).toBe(true)
  })
})
