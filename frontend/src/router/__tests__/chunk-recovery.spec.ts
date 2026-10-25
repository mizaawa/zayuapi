import { beforeAll, beforeEach, afterEach, describe, expect, it, vi } from 'vitest'

type ErrorHandler = (error: Error, to: { fullPath: string }) => void
const harness = vi.hoisted(() => ({ onError: null as ErrorHandler | null, endNavigation: vi.fn() }))
vi.mock('vue-router', () => ({
  createWebHistory: vi.fn(() => ({})),
  createRouter: vi.fn(() => ({
    beforeEach: vi.fn(), afterEach: vi.fn(),
    onError: vi.fn((handler: ErrorHandler) => { harness.onError = handler }),
    resolve: vi.fn((path: string) => ({ href: `/portal${path}` })),
  })),
}))
vi.mock('@/stores/auth', () => ({ useAuthStore: vi.fn() }))
vi.mock('@/stores/app', () => ({ useAppStore: vi.fn() }))
vi.mock('@/stores/adminSettings', () => ({ useAdminSettingsStore: vi.fn() }))
vi.mock('@/stores/adminCompliance', () => ({ useAdminComplianceStore: vi.fn() }))
vi.mock('@/stores/paymentAccess', () => ({ isUserPaymentPath: vi.fn(), usePaymentAccessStore: vi.fn() }))
vi.mock('@/composables/useNavigationLoading', () => ({
  useNavigationLoadingState: () => ({ startNavigation: vi.fn(), endNavigation: harness.endNavigation }),
}))
vi.mock('@/composables/useRoutePrefetch', () => ({ useRoutePrefetch: vi.fn() }))
vi.mock('@/api/setup', () => ({ getSetupStatus: vi.fn() }))

const replace = vi.fn()
const reload = vi.fn()
let clock = 100000
const fail = (message: string, path = '/keys?sort=name#table') => {
  expect(harness.onError).toBeTypeOf('function')
  harness.onError!(new TypeError(message), { fullPath: path })
}

beforeAll(async () => { await import('@/router') })
beforeEach(() => {
  vi.clearAllMocks()
  clock += 20000
  vi.spyOn(Date, 'now').mockReturnValue(clock)
  vi.spyOn(console, 'error').mockImplementation(() => {})
  vi.spyOn(console, 'warn').mockImplementation(() => {})
  sessionStorage.clear()
  vi.stubGlobal('window', { location: { replace, reload }, sessionStorage })
})
afterEach(() => { vi.restoreAllMocks(); vi.unstubAllGlobals() })

describe('lazy-route chunk recovery', () => {
  it.each([
    'Failed to fetch dynamically imported module: /assets/Keys-old.js',
    'error loading dynamically imported module: /assets/Keys-old.js',
    'Importing a module script failed.',
    'Loading chunk 12 failed.',
    'Loading CSS chunk 12 failed.',
  ])('recovers %s by opening the failed destination', (message) => {
    fail(message)
    expect(replace).toHaveBeenCalledWith('/portal/keys?sort=name#table')
    expect(reload).not.toHaveBeenCalled()
    expect(harness.endNavigation).toHaveBeenCalled()
  })

  it('prevents repeated recovery within ten seconds', () => {
    fail('Failed to fetch dynamically imported module')
    fail('Failed to fetch dynamically imported module')
    expect(replace).toHaveBeenCalledTimes(1)
  })

  it('does not throw when session storage is blocked', () => {
    vi.stubGlobal('sessionStorage', {
      getItem: vi.fn(() => { throw new DOMException('Blocked', 'SecurityError') }),
      setItem: vi.fn(() => { throw new DOMException('Blocked', 'SecurityError') }),
    })
    expect(() => fail('Failed to fetch dynamically imported module')).not.toThrow()
    expect(harness.endNavigation).toHaveBeenCalled()
  })

  it('does not throw or enter a reload loop when writing storage fails', () => {
    vi.stubGlobal('sessionStorage', { getItem: vi.fn(() => null), setItem: vi.fn(() => { throw new DOMException('Full', 'QuotaExceededError') }) })
    expect(() => fail('Failed to fetch dynamically imported module')).not.toThrow()
    expect(replace).not.toHaveBeenCalled()
  })

  it('honors a recovery marker from the previous document', () => {
    sessionStorage.setItem('chunk_reload_attempted', String(clock - 500))
    fail('Importing a module script failed.')
    expect(replace).not.toHaveBeenCalled()
  })

  it('recovers after the throttle expires', () => {
    sessionStorage.setItem('chunk_reload_attempted', String(clock - 15000))
    fail('Importing a module script failed.')
    expect(replace).toHaveBeenCalledWith('/portal/keys?sort=name#table')
  })

  it('ignores a malformed storage marker', () => {
    sessionStorage.setItem('chunk_reload_attempted', 'not-a-timestamp')
    fail('Importing a module script failed.')
    expect(replace).toHaveBeenCalledWith('/portal/keys?sort=name#table')
  })

  it('does not reload unrelated errors', () => {
    fail('Cannot read properties of undefined')
    expect(replace).not.toHaveBeenCalled()
    expect(reload).not.toHaveBeenCalled()
    expect(harness.endNavigation).toHaveBeenCalled()
  })

  it.each(['//example.com/keys', '/\\example.com/keys', '/keys\nLocation: example.com'])('rejects unsafe target %s', (path) => {
    fail('Failed to fetch dynamically imported module', path)
    expect(replace).not.toHaveBeenCalled()
    expect(reload).not.toHaveBeenCalled()
  })
})
