import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { reactive } from 'vue'
import { createI18n } from 'vue-i18n'
import { createMemoryHistory, createRouter } from 'vue-router'
import { afterEach, describe, expect, it, vi } from 'vitest'
import AppHeader from '../AppHeader.vue'

const stores = vi.hoisted(() => ({
  app: {} as Record<string, unknown>,
  auth: {} as Record<string, unknown>,
}))

vi.mock('@/stores', () => ({
  useAppStore: () => stores.app,
  useAuthStore: () => stores.auth,
  useOnboardingStore: () => ({ replay: vi.fn() }),
}))
vi.mock('@/stores/adminSettings', () => ({
  useAdminSettingsStore: () => ({ customMenuItems: [] }),
}))

const AVATAR = 'data:image/webp;base64,UklGRg=='
const NEXT_AVATAR = 'data:image/png;base64,iVBORw=='

let wrapper: VueWrapper | undefined

async function renderHeader(user: Record<string, unknown>) {
  stores.app = reactive({ contactInfo: '', docUrl: '', cachedPublicSettings: null, toggleMobileSidebar: vi.fn() })
  stores.auth = reactive({ user, isAdmin: false, isSimpleMode: false, logout: vi.fn() })
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: '/:pathMatch(.*)*', component: { template: '<div />' } }],
  })
  await router.push('/dashboard')
  const i18n = createI18n({ legacy: false, locale: 'en', messages: {}, missingWarn: false, fallbackWarn: false })
  wrapper = mount(AppHeader, {
    global: {
      plugins: [router, i18n],
      stubs: { AnnouncementBell: true, LocaleSwitcher: true, SubscriptionProgressMini: true, Icon: true },
    },
  })
  await flushPromises()
  return wrapper
}

function baseUser(overrides: Record<string, unknown> = {}) {
  return { id: 7, username: 'miza', email: 'miza@example.com', role: 'admin', balance: 12, ...overrides }
}

afterEach(() => {
  wrapper?.unmount()
  wrapper = undefined
})

describe('AppHeader avatar', () => {
  it('shows the saved avatar image', async () => {
    const header = await renderHeader(baseUser({ avatar_url: AVATAR }))

    const image = header.get('[data-testid="header-avatar-image"]')
    expect(image.attributes('src')).toBe(AVATAR)
    expect(image.attributes('referrerpolicy')).toBe('no-referrer')
    expect(header.find('[data-testid="header-avatar-initials"]').exists()).toBe(false)
  })

  it('shows the initials when the user has no avatar', async () => {
    const header = await renderHeader(baseUser({ avatar_url: null }))

    expect(header.find('[data-testid="header-avatar-image"]').exists()).toBe(false)
    expect(header.get('[data-testid="header-avatar-initials"]').text()).toBe('MI')
  })

  it('picks up an avatar that arrives with a later profile refresh', async () => {
    const header = await renderHeader(baseUser())

    stores.auth.user = baseUser({ avatar_url: AVATAR })
    await flushPromises()

    expect(header.get('[data-testid="header-avatar-image"]').attributes('src')).toBe(AVATAR)
  })

  it('falls back to the initials when the image cannot load and retries a new avatar', async () => {
    const header = await renderHeader(baseUser({ avatar_url: AVATAR }))

    await header.get('[data-testid="header-avatar-image"]').trigger('error')

    expect(header.find('[data-testid="header-avatar-image"]').exists()).toBe(false)
    expect(header.get('[data-testid="header-avatar-initials"]').text()).toBe('MI')

    stores.auth.user = baseUser({ avatar_url: NEXT_AVATAR })
    await flushPromises()

    expect(header.get('[data-testid="header-avatar-image"]').attributes('src')).toBe(NEXT_AVATAR)
  })
})
