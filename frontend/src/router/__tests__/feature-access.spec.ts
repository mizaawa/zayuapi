import { beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'

type NavigationGuard = (
  to: Record<string, any>,
  from: Record<string, any>,
  next: ReturnType<typeof vi.fn>
) => Promise<void>

const routerHarness = vi.hoisted(() => ({
  guard: null as NavigationGuard | null,
  routes: [] as Array<Record<string, any>>,
}))

const authStore = vi.hoisted(() => ({
  checkAuth: vi.fn(),
  isAuthenticated: true,
  isAdmin: false,
  isSimpleMode: false,
  hasPendingAuthSession: false,
}))

const paymentAccess = vi.hoisted(() => ({ refresh: vi.fn() }))
vi.mock('@/stores/paymentAccess', async (importOriginal) => ({
  ...await importOriginal<typeof import('@/stores/paymentAccess')>(),
  usePaymentAccessStore: () => paymentAccess,
}))

const appStore = vi.hoisted(() => ({
  siteName: 'Sub2API',
  backendModeEnabled: false,
  publicSettingsLoaded: false,
  cachedPublicSettings: null as null | {
    payment_enabled?: boolean
    risk_control_enabled?: boolean
    custom_menu_items?: []
  },
  fetchPublicSettings: vi.fn(),
}))

vi.mock('vue-router', () => ({
  createWebHistory: vi.fn(() => ({})),
  createRouter: vi.fn((options: { routes?: Array<Record<string, any>> }) => {
    routerHarness.routes = options.routes ?? []
    return {
      beforeEach: vi.fn((guard: NavigationGuard) => {
        routerHarness.guard = guard
      }),
      afterEach: vi.fn(),
      onError: vi.fn(),
    }
  }),
}))

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => authStore,
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => appStore,
}))

vi.mock('@/stores/adminSettings', () => ({
  useAdminSettingsStore: () => ({ customMenuItems: [] }),
}))

vi.mock('@/stores/adminCompliance', () => ({
  useAdminComplianceStore: () => ({
    initialized: true,
    fetchStatus: vi.fn(),
    requireAcknowledgement: vi.fn(),
  }),
}))

vi.mock('@/composables/useNavigationLoading', () => ({
  useNavigationLoadingState: () => ({
    startNavigation: vi.fn(),
    endNavigation: vi.fn(),
    isLoading: { value: false },
  }),
}))

vi.mock('@/composables/useRoutePrefetch', () => ({
  useRoutePrefetch: () => ({
    triggerPrefetch: vi.fn(),
    cancelPendingPrefetch: vi.fn(),
    resetPrefetchState: vi.fn(),
  }),
}))

function createDeferred<T>() {
  let resolve!: (value: T | PromiseLike<T>) => void
  const promise = new Promise<T>((resolvePromise) => {
    resolve = resolvePromise
  })
  return { promise, resolve }
}

function runGuard(meta: Record<string, unknown>, path: string) {
  if (!routerHarness.guard) {
    throw new Error('router guard was not registered')
  }

  const next = vi.fn()
  const navigation = routerHarness.guard(
    {
      path,
      fullPath: path,
      name: 'FeatureRoute',
      params: {},
      meta: { requiresAuth: true, ...meta },
    },
    {},
    next
  )
  return { navigation, next }
}

describe('feature route guard', () => {
  beforeAll(async () => {
    await import('@/router')
  })

  beforeEach(() => {
    authStore.isAuthenticated = true
    authStore.isAdmin = false
    authStore.isSimpleMode = false
    appStore.publicSettingsLoaded = false
    appStore.cachedPublicSettings = null
    appStore.fetchPublicSettings.mockReset()
    paymentAccess.refresh.mockReset().mockResolvedValue({ enabled: true, allowed: true, exclusive_enabled: false })
  })

  it('keeps a direct administrator route for the feature-management tab', () => {
    const featureRoute = routerHarness.routes.find((route) => route.path === '/admin/features')

    expect(featureRoute).toMatchObject({
      name: 'AdminFeatureManagement',
      meta: {
        requiresAuth: true,
        requiresAdmin: true,
        titleKey: 'admin.settings.tabs.features',
      },
    })
  })

  it('waits for the first public-settings request before deciding payment access', async () => {
    const deferred = createDeferred<{ payment_enabled: boolean }>()
    appStore.fetchPublicSettings.mockImplementation(async () => {
      const settings = await deferred.promise
      appStore.cachedPublicSettings = settings
      appStore.publicSettingsLoaded = true
      return settings
    })

    const { navigation, next } = runGuard({ requiresPayment: true }, '/purchase')

    await vi.waitFor(() => expect(appStore.fetchPublicSettings).toHaveBeenCalledTimes(1))
    expect(next).not.toHaveBeenCalled()

    deferred.resolve({ payment_enabled: true })
    await navigation
    expect(next).toHaveBeenCalledOnce()
    expect(next).toHaveBeenCalledWith()
  })

  it.each(['/purchase', '/purchase/', '/Purchase', '/orders', '/ORDERS', '/orders/', '/payment/qrcode', '/payment/stripe', '/Payment/stripe', '/payment/airwallex', '/payment/result', '/payment/stripe-popup', '/auth/wechat/payment/callback', '/AUTH/wechat/payment/callback/'])(
    'rejects a revoked user on %s, including public payment continuations', async path => {
      paymentAccess.refresh.mockResolvedValue({ enabled: true, allowed: false, exclusive_enabled: true })
      const { navigation, next } = runGuard({ requiresAuth: false }, path)
      await navigation
      expect(next).toHaveBeenCalledWith('/dashboard')
    }
  )

  it('fails closed when payment permission cannot be refreshed', async () => {
    paymentAccess.refresh.mockResolvedValue(null)
    const { navigation, next } = runGuard({ requiresPayment: true }, '/purchase')
    await navigation
    expect(next).toHaveBeenCalledWith('/dashboard')
  })

  it('requires login for an exclusive anonymous payment continuation', async () => {
    authStore.isAuthenticated = false
    appStore.fetchPublicSettings.mockResolvedValue({ payment_exclusive_enabled: true })
    const { navigation, next } = runGuard({ requiresAuth: false }, '/payment/result')
    await navigation
    expect(next).toHaveBeenCalledWith({ path: '/login', query: { redirect: '/payment/result' } })
  })

  it('preserves anonymous payment continuations when exclusive mode is off', async () => {
    authStore.isAuthenticated = false
    appStore.fetchPublicSettings.mockResolvedValue({ payment_exclusive_enabled: false })
    const { navigation, next } = runGuard({ requiresAuth: false }, '/payment/result')
    await navigation
    expect(next).toHaveBeenCalledWith()
  })

  it.each([
    ['payment', { requiresPayment: true }, '/purchase'],
    ['risk control', { requiresRiskControl: true }, '/admin/risk-control'],
  ])('does not treat a failed %s settings load as explicitly disabled', async (_name, meta, path) => {
    authStore.isAdmin = meta.requiresRiskControl === true
    appStore.fetchPublicSettings.mockResolvedValue(null)

    const { navigation, next } = runGuard(meta, path)
    await navigation

    expect(appStore.publicSettingsLoaded).toBe(false)
    expect(next).toHaveBeenCalledOnce()
    expect(next).toHaveBeenCalledWith()
  })

  it.each([
    ['payment', { requiresPayment: true }, { payment_enabled: false }, '/dashboard'],
    [
      'risk control',
      { requiresRiskControl: true },
      { risk_control_enabled: false },
      '/admin/settings',
    ],
  ])('redirects when loaded settings explicitly disable %s', async (_name, meta, settings, target) => {
    authStore.isAdmin = meta.requiresRiskControl === true
    appStore.cachedPublicSettings = settings
    appStore.publicSettingsLoaded = true

    const { navigation, next } = runGuard(meta, '/feature')
    await navigation

    expect(appStore.fetchPublicSettings).not.toHaveBeenCalled()
    expect(next).toHaveBeenCalledOnce()
    expect(next).toHaveBeenCalledWith(target)
  })
})
