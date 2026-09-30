import { mount, flushPromises, type VueWrapper } from '@vue/test-utils'
import { reactive } from 'vue'
import { createI18n } from 'vue-i18n'
import { createMemoryHistory, createRouter, type Router } from 'vue-router'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import AppSidebar from '../AppSidebar.vue'
import type { CustomMenuItem } from '@/types'

const stores = vi.hoisted(() => ({
  app: {} as Record<string, unknown>,
  auth: {} as Record<string, unknown>,
  admin: {} as Record<string, unknown>,
  onboarding: { isCurrentStep: () => false },
}))

vi.mock('@/stores', () => ({
  useAppStore: () => stores.app,
  useAuthStore: () => stores.auth,
  useAdminSettingsStore: () => stores.admin,
  useOnboardingStore: () => stores.onboarding,
}))
vi.mock('@/stores/app', () => ({ useAppStore: () => stores.app }))
vi.mock('@/composables/useBatchImageAccess', () => ({
  useBatchImageAccess: () => ({ canUseBatchImage: { value: false }, refreshBatchImageAccess: vi.fn() }),
}))

const userMenu: CustomMenuItem = {
  id: 'portal', label: 'User portal', icon_svg: '',
  url: 'https://portal.example.com/page?plan=pro#pricing', visibility: 'user', sort_order: 0,
}
const adminMenu: CustomMenuItem = { ...userMenu, id: 'admin-portal', label: 'Admin portal', visibility: 'admin' }

let wrapper: VueWrapper
let router: Router
let setLocale: (locale: string) => void

async function renderSidebar(enabled?: boolean, admin = false, simple = false) {
  stores.app = reactive({
    cachedPublicSettings: { custom_menu_items: [userMenu], custom_menu_force_new_tab: enabled },
    sidebarCollapsed: false, mobileOpen: false, sidebarScrollTop: 0,
    siteName: 'Test', siteLogo: '', siteVersion: '', publicSettingsLoaded: true,
    backendModeEnabled: false, toggleSidebar: vi.fn(), setMobileOpen: vi.fn(),
  })
  stores.auth = reactive({ isAdmin: admin, isSimpleMode: simple, user: { id: 42 }, token: 'test-token' })
  stores.admin = reactive({ customMenuItems: [adminMenu], fetch: vi.fn(), opsMonitoringEnabled: true })
  router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: '/:pathMatch(.*)*', component: { template: '<div />' } }],
  })
  await router.push(admin ? '/admin/settings' : '/usage')
  const i18n = createI18n({ legacy: false, locale: 'zh-CN', messages: {}, missingWarn: false, fallbackWarn: false })
  setLocale = locale => { i18n.global.locale.value = locale }
  wrapper = mount(AppSidebar, {
    global: {
      plugins: [router, i18n],
      stubs: { VersionBadge: true },
    },
  })
  await flushPromises()
}

function menuLink(label: string) {
  const link = wrapper.findAll('a').find(link => link.text() === label)
  expect(link).toBeDefined()
  return link!
}

beforeEach(() => {
  localStorage.setItem('sub2api-admin-panel-expanded', 'true')
  document.documentElement.classList.remove('dark')
})

afterEach(() => {
  wrapper?.unmount()
  localStorage.clear()
  document.documentElement.classList.remove('dark')
})

describe('AppSidebar custom menu navigation', () => {
  it.each([undefined, false])('keeps embedded navigation when the flag is %s', async enabled => {
    await renderSidebar(enabled)
    const link = menuLink(userMenu.label)
    expect(link.attributes('target')).toBeUndefined()
    await link.trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/custom/portal')
  })

  it('opens the target URL with all existing iframe parameters and retains the active page', async () => {
    document.documentElement.classList.add('dark')
    await renderSidebar(true)
    const link = menuLink(userMenu.label)
    const url = new URL(link.attributes('href'))
    expect(url.origin + url.pathname).toBe('https://portal.example.com/page')
    expect(url.hash).toBe('#pricing')
    expect(Object.fromEntries(url.searchParams)).toEqual({
      plan: 'pro', user_id: '42', token: 'test-token', theme: 'dark', lang: 'zh-CN',
      ui_mode: 'embedded', src_host: window.location.origin, src_url: window.location.href,
    })
    expect(link.attributes('target')).toBe('_blank')
    expect(link.attributes('rel')).toBe('noopener noreferrer')
    const activeBefore = wrapper.findAll('.sidebar-link-active').map(link => link.text())
    await link.trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/usage')
    expect(wrapper.findAll('.sidebar-link-active').map(link => link.text())).toEqual(activeBefore)
  })

  it.each([false, true])('applies to administrator custom menus in simple mode %s', async simple => {
    await renderSidebar(true, true, simple)
    const link = menuLink(adminMenu.label)
    expect(new URL(link.attributes('href')).hostname).toBe('portal.example.com')
    expect(link.attributes('target')).toBe('_blank')
    await link.trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/admin/settings')
    if (!simple) expect(menuLink(userMenu.label).attributes('target')).toBe('_blank')
  })

  it('updates the target when the token, locale, or switch changes', async () => {
    await renderSidebar(true)
    stores.auth.token = 'refreshed-token'
    setLocale('en')
    await wrapper.vm.$nextTick()
    expect(new URL(menuLink(userMenu.label).attributes('href')).searchParams.get('token')).toBe('refreshed-token')
    expect(new URL(menuLink(userMenu.label).attributes('href')).searchParams.get('lang')).toBe('en')
    stores.app.cachedPublicSettings = { custom_menu_items: [userMenu], custom_menu_force_new_tab: false }
    await wrapper.vm.$nextTick()
    expect(menuLink(userMenu.label).attributes('target')).toBeUndefined()
    await menuLink(userMenu.label).trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/custom/portal')
  })

  it.each([
    { ...userMenu, url: 'md:guide' },
    { ...userMenu, page_slug: 'guide' },
    { ...userMenu, url: 'javascript:alert(1)' },
  ])('uses the custom page route when there is no external web URL: %j', async menu => {
    await renderSidebar(true)
    stores.app.cachedPublicSettings = { custom_menu_items: [menu], custom_menu_force_new_tab: true }
    await wrapper.vm.$nextTick()
    expect(menuLink(userMenu.label).attributes('href')).toBe('/custom/portal')
    expect(menuLink(userMenu.label).attributes('target')).toBe('_blank')
  })
})
