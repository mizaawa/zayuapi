import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { reactive } from 'vue'
import { createI18n } from 'vue-i18n'
import { createMemoryHistory, createRouter, type Router } from 'vue-router'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import AppSidebar from '../AppSidebar.vue'
import en from '@/i18n/locales/en/common'
import zh from '@/i18n/locales/zh/common'
import type { PaymentAccess } from '@/stores/paymentAccess'

const stores = vi.hoisted(() => ({
  app: {} as Record<string, unknown>,
  auth: {} as Record<string, unknown>,
  admin: {} as Record<string, unknown>,
  onboarding: { isCurrentStep: () => false },
  payment: { allowed: true, access: null as PaymentAccess | null },
}))

vi.mock('@/stores', () => ({
  useAppStore: () => stores.app,
  useAuthStore: () => stores.auth,
  useAdminSettingsStore: () => stores.admin,
  useOnboardingStore: () => stores.onboarding,
}))
vi.mock('@/stores/app', () => ({ useAppStore: () => stores.app }))
vi.mock('@/stores/paymentAccess', () => ({ usePaymentAccessStore: () => stores.payment }))
vi.mock('@/composables/useBatchImageAccess', () => ({
  useBatchImageAccess: () => ({ canUseBatchImage: { value: false }, refreshBatchImageAccess: vi.fn() }),
}))

let wrapper: VueWrapper
let router: Router
let setLocale: (locale: 'zh-CN' | 'en') => void

async function renderSidebar(admin = false) {
  stores.app = reactive({
    cachedPublicSettings: {}, sidebarCollapsed: false, mobileOpen: false, sidebarScrollTop: 0,
    siteName: 'Test', siteLogo: '', siteVersion: '', publicSettingsLoaded: true,
    backendModeEnabled: false, toggleSidebar: vi.fn(), setMobileOpen: vi.fn(),
  })
  stores.auth = reactive({
    isAdmin: admin, isSimpleMode: false,
    user: { id: 42, exclusive_recharge_enabled: true }, token: 'test-token',
  })
  stores.admin = reactive({ customMenuItems: [], fetch: vi.fn(), opsMonitoringEnabled: true })
  router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: '/:pathMatch(.*)*', component: { template: '<div />' } }],
  })
  await router.push(admin ? '/admin/settings' : '/usage')
  const i18n = createI18n({
    legacy: false, locale: 'zh-CN', messages: {
      'zh-CN': { nav: Object.fromEntries(Object.entries(zh.nav).map(([key, text]) => [key, () => text])) },
      en: { nav: Object.fromEntries(Object.entries(en.nav).map(([key, text]) => [key, () => text])) },
    },
    missingWarn: false, fallbackWarn: false,
  })
  setLocale = locale => { i18n.global.locale.value = locale }
  wrapper = mount(AppSidebar, {
    global: { plugins: [router, i18n], stubs: { VersionBadge: true } },
  })
  await flushPromises()
}

function findMenuLink(label: string) {
  return wrapper.findAll('a').find(link => link.text().startsWith(label))
}

function purchaseLink(label = '充值/订阅') {
  const link = findMenuLink(label)
  expect(link).toBeDefined()
  return link!
}

beforeEach(() => {
  stores.payment = reactive({
    allowed: true, access: { enabled: true, allowed: true, exclusive_enabled: true },
  })
  localStorage.setItem('sub2api-admin-panel-expanded', 'true')
})

afterEach(() => {
  wrapper?.unmount()
  localStorage.clear()
})

describe('AppSidebar exclusive payment indicator', () => {
  it.each([false, true])('labels only the purchase link for administrator=%s and preserves navigation', async admin => {
    await renderSidebar(admin)
    const purchase = purchaseLink()
    expect(purchase.text()).toContain('充值/订阅')
    expect(purchase.get('.sidebar-exclusive-badge').text()).toBe('专属')
    expect(wrapper.findAll('.sidebar-exclusive-badge')).toHaveLength(1)
    expect(findMenuLink('我的订单')!.find('.sidebar-exclusive-badge').exists()).toBe(false)

    await purchase.trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/purchase')
  })

  it('does not use a personal grant as the system exclusive-mode switch', async () => {
    stores.payment.access!.exclusive_enabled = false
    await renderSidebar()
    expect(purchaseLink().text()).toBe('充值/订阅')
    expect(wrapper.find('.sidebar-exclusive-badge').exists()).toBe(false)
  })

  it.each([false, true])('keeps authorized payment links visible when simple mode activates for administrator=%s', async admin => {
    await renderSidebar(admin)
    expect(purchaseLink().get('.sidebar-exclusive-badge').text()).toBe('专属')

    // /auth/me may switch the restored session into simple mode after the menu first renders.
    stores.auth.isSimpleMode = true
    await flushPromises()
    expect(purchaseLink().get('.sidebar-exclusive-badge').text()).toBe('专属')
    expect(findMenuLink('我的订单')).toBeDefined()

    stores.payment.allowed = false
    await flushPromises()
    expect(findMenuLink('充值/订阅')).toBeUndefined()
    expect(findMenuLink('我的订单')).toBeUndefined()
  })

  it('keeps the indicator absent until the payment access snapshot is available', async () => {
    stores.payment.access = null
    await renderSidebar()
    expect(wrapper.find('.sidebar-exclusive-badge').exists()).toBe(false)
  })

  it.each([false, true])('tracks saved exclusive-mode snapshots without remounting for administrator=%s', async admin => {
    stores.payment.access!.exclusive_enabled = false
    await renderSidebar(admin)
    expect(wrapper.find('.sidebar-exclusive-badge').exists()).toBe(false)

    stores.payment.access = { enabled: true, allowed: true, exclusive_enabled: true }
    await flushPromises()
    expect(purchaseLink().get('.sidebar-exclusive-badge').text()).toBe('专属')

    stores.payment.access = { enabled: true, allowed: true, exclusive_enabled: false }
    await flushPromises()
    expect(wrapper.find('.sidebar-exclusive-badge').exists()).toBe(false)
    expect(findMenuLink('充值/订阅')).toBeDefined()
  })

  it.each([false, true])('hides the indicator while collapsed for administrator=%s', async admin => {
    await renderSidebar(admin)
    expect(wrapper.find('.sidebar-exclusive-badge').exists()).toBe(true)

    stores.app.sidebarCollapsed = true
    await flushPromises()
    expect(wrapper.find('.sidebar-exclusive-badge').exists()).toBe(false)
    expect(purchaseLink().attributes('title')).toBe('充值/订阅')

    stores.app.sidebarCollapsed = false
    await flushPromises()
    expect(purchaseLink().get('.sidebar-exclusive-badge').text()).toBe('专属')
  })

  it('removes the payment links and indicator when access is revoked', async () => {
    await renderSidebar()
    expect(wrapper.find('.sidebar-exclusive-badge').exists()).toBe(true)

    stores.payment.allowed = false
    stores.payment.access!.allowed = false
    await flushPromises()
    expect(findMenuLink('充值/订阅')).toBeUndefined()
    expect(findMenuLink('我的订单')).toBeUndefined()
    expect(wrapper.find('.sidebar-exclusive-badge').exists()).toBe(false)
  })

  it('uses the active locale for the indicator', async () => {
    await renderSidebar()
    setLocale('en')
    await flushPromises()
    expect(purchaseLink('Recharge / Subscription').get('.sidebar-exclusive-badge').text()).toBe('Exclusive')
  })
})
