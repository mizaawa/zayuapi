import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createI18n } from 'vue-i18n'
import { createPinia, setActivePinia } from 'pinia'
import { defineComponent, h } from 'vue'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import type { Driver } from 'driver.js'
import { useOnboardingStore } from '@/stores/onboarding'
import { useOnboardingTour } from '../useOnboardingTour'

vi.mock('driver.js', () => ({ driver: vi.fn() }))
vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({
    user: { id: 7, role: 'admin' },
    isSimpleMode: false,
  }),
}))

function createTourRoute(targetId: string) {
  return defineComponent({
    name: `TourRoute-${targetId}`,
    setup() {
      useOnboardingTour({ storageKey: 'admin_guide', autoStart: false })
      return () => h('button', { id: targetId }, targetId)
    },
  })
}

describe('useOnboardingTour route navigation cleanup', () => {
  let wrapper: VueWrapper | undefined

  beforeEach(() => {
    vi.useFakeTimers()
    setActivePinia(createPinia())
    localStorage.clear()
    document.body.classList.remove('driver-active')
  })

  afterEach(() => {
    wrapper?.unmount()
    wrapper = undefined
    document.body.classList.remove('driver-active')
    localStorage.clear()
    vi.clearAllTimers()
    vi.useRealTimers()
  })

  async function mountAcrossRoutes(activeStepSelector = '#old-tour-target') {
    const i18n = createI18n({ legacy: false, locale: 'en', messages: { en: {} } })
    const pinia = createPinia()
    setActivePinia(pinia)
    wrapper = mount(createTourRoute('old-tour-target'), {
      global: { plugins: [pinia, i18n] },
      attachTo: document.body,
    })
    await flushPromises()

    let active = true
    let activeElement: HTMLElement | null = document.getElementById('old-tour-target')
    const fakeDriver = {
      isActive: vi.fn(() => active),
      getActiveElement: vi.fn(() => activeElement),
      getActiveStep: vi.fn(() => ({ element: activeStepSelector })),
      refresh: vi.fn(() => {
        activeElement = document.querySelector(activeStepSelector) as HTMLElement | null
      }),
      destroy: vi.fn(() => {
        active = false
        document.body.classList.remove('driver-active')
      }),
      moveNext: vi.fn(),
      movePrevious: vi.fn(),
      drive: vi.fn(),
    } as unknown as Driver

    document.body.classList.add('driver-active')
    useOnboardingStore().setDriverInstance(fakeDriver)

    // AppLayout is nested in each route view, so navigation unmounts the old
    // view and mounts a fresh composable instance around the new route target.
    wrapper.unmount()
    wrapper = mount(createTourRoute('new-tour-target'), {
      global: { plugins: [pinia, i18n] },
      attachTo: document.body,
    })
    await flushPromises()

    return { fakeDriver }
  }

  it('destroys a tour left pointing at a detached route target', async () => {
    const { fakeDriver } = await mountAcrossRoutes()

    expect(document.getElementById('old-tour-target')).toBeNull()
    expect(fakeDriver.destroy).not.toHaveBeenCalled()

    await vi.advanceTimersByTimeAsync(1000)
    await flushPromises()

    expect(fakeDriver.destroy).toHaveBeenCalledOnce()
    expect(document.body.classList.contains('driver-active')).toBe(false)
    expect(useOnboardingStore().getDriverInstance()).toBeNull()
  })

  it('refreshes the tour when its guided target is mounted on the new route', async () => {
    const { fakeDriver } = await mountAcrossRoutes('#new-tour-target')

    expect(document.getElementById('new-tour-target')).not.toBeNull()
    expect(fakeDriver.refresh).toHaveBeenCalledOnce()

    await vi.advanceTimersByTimeAsync(1000)
    await flushPromises()

    expect(fakeDriver.destroy).not.toHaveBeenCalled()
    expect(useOnboardingStore().getDriverInstance()).toBe(fakeDriver)
    expect(document.body.classList.contains('driver-active')).toBe(true)
  })
})
