import { mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import AuthLayout from '@/components/layout/AuthLayout.vue'

const store = vi.hoisted(() => ({
  siteName: 'Test Gateway', siteLogo: '/brand.svg', docUrl: '', backendModeEnabled: false,
  cachedPublicSettings: { site_subtitle: 'Custom subtitle', registration_enabled: true },
  publicSettingsLoaded: true, fetchPublicSettings: vi.fn()
}))
vi.mock('@/stores', () => ({ useAppStore: () => store }))
vi.mock('vue-i18n', async (importOriginal) => ({
  ...(await importOriginal<typeof import('vue-i18n')>()),
  useI18n: () => ({ t: (key: string) => key })
}))

function render(entry?: 'login' | 'register') {
  return mount(AuthLayout, {
    props: { entry },
    slots: { default: '<form>Form</form>', footer: '<span>Recovery link</span>' },
    global: { stubs: { LocaleSwitcher: true, RouterLink: { props: ['to'], template: '<a :href="to"><slot /></a>' } } }
  })
}

describe('account entry layout', () => {
  beforeEach(() => {
    store.docUrl = ''
    store.backendModeEnabled = false
    store.cachedPublicSettings.registration_enabled = true
  })

  it.each(['login', 'register'] as const)('gives %s one form heading and an account switch', (entry) => {
    store.docUrl = 'https://docs.example.test/'
    const view = render(entry)
    expect(view.get('.entry-brand').text()).toBe(store.siteName)
    expect(view.get('.entry-brand img').attributes('src')).toBe(store.siteLogo)
    expect(view.get('.entry-docs').attributes('href')).toBe(store.docUrl)
    const panel = view.get('main > section[aria-labelledby="entry-title"]')
    expect(panel.get('form').text()).toBe('Form')
    expect(view.findAll('h1')).toHaveLength(1)
    expect(panel.get('h1').text()).toBe(entry === 'login' ? 'auth.signIn' : 'auth.createAccount')
    expect(panel.get('.entry-switch').attributes('href')).toBe(entry === 'login' ? '/register' : '/login')
    expect(view.find('aside').exists()).toBe(false)
    expect(view.text()).not.toContain(store.cachedPublicSettings.site_subtitle)
  })

  it('hides registration when closed or in backend mode, but still offers a way back to login', () => {
    store.cachedPublicSettings.registration_enabled = false
    expect(render('login').find('.entry-switch').exists()).toBe(false)
    expect(render('register').get('.entry-switch').attributes('href')).toBe('/login')
    store.cachedPublicSettings.registration_enabled = true
    store.backendModeEnabled = true
    expect(render('login').find('.entry-switch').exists()).toBe(false)
  })

  it.each(['', 'not a URL'])('omits an unconfigured or unusable documentation link', (value) => {
    store.docUrl = value
    expect(render('login').find('.entry-docs').exists()).toBe(false)
  })

  it('preserves the existing layout and footer on recovery, verification and callbacks', () => {
    const view = render()
    expect(view.find('.auth-entry').exists()).toBe(false)
    expect(view.get('.auth-card form').text()).toBe('Form')
    expect(view.text()).toContain('Recovery link')
  })
})
