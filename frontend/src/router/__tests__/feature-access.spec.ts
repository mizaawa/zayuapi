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

const paymentAccess = vi.hoisted(() => ({ refresh: vi.fn(), reset: vi.fn() }))
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
      query: {},
      hash: '',
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
    paymentAccess.reset.mockReset()
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
      expect(next).toHaveBeenCalledWith(expect.objectContaining({ name: 'NotFound', replace: true }))
    }
  )

  it('fails closed when payment permission cannot be refreshed', async () => {
    paymentAccess.refresh.mockResolvedValue(null)
    const { navigation, next } = runGuard({ requiresPayment: true }, '/purchase')
    await navigation
    expect(next).toHaveBeenCalledWith(expect.objectContaining({ name: 'NotFound', replace: true }))
  })

  it.each(['/purchase', '/orders', '/payment/result', '/auth/wechat/payment/callback'])('hides an exclusive anonymous payment path %s', async path => {
    authStore.isAuthenticated = false
    appStore.fetchPublicSettings.mockResolvedValue({ payment_exclusive_enabled: true })
    const { navigation, next } = runGuard({ requiresAuth: !path.startsWith('/payment/') && !path.startsWith('/auth/') }, path)
    await navigation
    expect(next).toHaveBeenCalledWith(expect.objectContaining({ name: 'NotFound', replace: true }))
  })

  it.each([null, { payment_exclusive_enabled: undefined }, { payment_exclusive_enabled: false, payment_enabled: false }])(
    'hides anonymous payment continuations when the feature state is unknown or disabled: %j', async settings => {
      authStore.isAuthenticated = false
      appStore.fetchPublicSettings.mockResolvedValue(settings)
      const { navigation, next } = runGuard({ requiresAuth: false }, '/payment/result')
      await navigation
      expect(next).toHaveBeenCalledWith(expect.objectContaining({ name: 'NotFound' }))
    },
  )

  it('fails closed when the anonymous settings request rejects', async () => {
    authStore.isAuthenticated = false
    appStore.fetchPublicSettings.mockRejectedValue(new Error('offline'))
    const { navigation, next } = runGuard({ requiresAuth: false }, '/payment/result')
    await navigation
    expect(next).toHaveBeenCalledWith(expect.objectContaining({ name: 'NotFound' }))
  })

  it.each([
    { enabled: false, allowed: true, exclusive_enabled: false },
    { enabled: true, allowed: 'true', exclusive_enabled: false },
  ])('requires an explicit enabled grant: %j', async access => {
    paymentAccess.refresh.mockResolvedValue(access)
    const { navigation, next } = runGuard({ requiresPayment: true }, '/purchase')
    await navigation
    expect(next).toHaveBeenCalledWith(expect.objectContaining({ name: 'NotFound' }))
  })

  it('does not publish a protected title while checking authorization', async () => {
    document.title = '404 Not Found - Sub2API'
    const deferred = createDeferred<null>()
    paymentAccess.refresh.mockReturnValue(deferred.promise)
    const { navigation } = runGuard({ title: 'Exclusive payment' }, '/purchase')
    expect(document.title).toBe('404 Not Found - Sub2API')
    deferred.resolve(null)
    await navigation
    expect(document.title).toBe('404 Not Found - Sub2API')
  })

  it('rechecks a session established during an anonymous payment continuation check', async () => {
    authStore.isAuthenticated = false
    const deferred = createDeferred<{ payment_exclusive_enabled: boolean }>()
    appStore.fetchPublicSettings.mockReturnValue(deferred.promise)
    paymentAccess.refresh.mockResolvedValue({ enabled: false, allowed: false, exclusive_enabled: false })
    const { navigation, next } = runGuard({ requiresAuth: false }, '/payment/result')
    authStore.isAuthenticated = true
    deferred.resolve({ payment_exclusive_enabled: false })
    await navigation
    expect(paymentAccess.refresh).toHaveBeenCalledOnce()
    expect(next).toHaveBeenCalledWith(expect.objectContaining({ name: 'NotFound' }))
  })

  it('lets the public unknown-page record handle a denied URL without rechecking payment', async () => {
    authStore.isAuthenticated = false
    const next = vi.fn()
    await routerHarness.guard!({ name: 'NotFound', path: '/purchase', meta: { requiresAuth: false } }, {}, next)
    expect(next).toHaveBeenCalledWith()
    expect(paymentAccess.refresh).not.toHaveBeenCalled()
    expect(appStore.fetchPublicSettings).not.toHaveBeenCalled()
  })

  it('preserves anonymous payment continuations when exclusive mode is off', async () => {
    authStore.isAuthenticated = false
    appStore.fetchPublicSettings.mockResolvedValue({ payment_exclusive_enabled: false })
    const { navigation, next } = runGuard({ requiresAuth: false }, '/payment/result')
    await navigation
    expect(next).toHaveBeenCalledWith()
    expect(paymentAccess.reset).toHaveBeenCalledOnce()
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
    ['payment', { requiresPayment: true }, { payment_enabled: false }, { name: 'NotFound', params: { pathMatch: ['feature'] }, query: {}, hash: '', replace: true }],
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
