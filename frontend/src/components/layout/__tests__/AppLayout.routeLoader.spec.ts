import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick, ref } from 'vue'

const isLoading = ref(false)

vi.mock('@/composables/useNavigationLoading', () => ({
  useNavigationLoadingState: () => ({ isLoading }),
}))
vi.mock('@/stores', () => ({
  useAppStore: () => ({ sidebarCollapsed: false }),
}))
vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({ user: { role: 'user' } }),
}))
vi.mock('@/stores/onboarding', () => ({
  useOnboardingStore: () => ({ setReplayCallback: vi.fn() }),
}))
vi.mock('@/composables/useOnboardingTour', () => ({
  useOnboardingTour: () => ({ replayTour: vi.fn() }),
}))
vi.mock('vue-i18n', async () => ({
  ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'),
  useI18n: () => ({ t: (key: string) => key }),
}))

import AppLayout from '../AppLayout.vue'

function mountLayout() {
  return mount(AppLayout, {
    slots: { default: '<p data-testid="page">page</p>' },
    global: { stubs: { AppSidebar: true, AppHeader: true, Transition: false } },
  })
}

describe('AppLayout route loader', () => {
  beforeEach(() => {
    isLoading.value = false
  })

  it('keeps the current page and shows a loader while the next page loads', async () => {
    const wrapper = mountLayout()
    expect(wrapper.find('[data-testid="route-loader"]').exists()).toBe(false)
    expect(wrapper.get('main').attributes('aria-busy')).toBeUndefined()

    isLoading.value = true
    await nextTick()
    expect(wrapper.find('[data-testid="route-loader"]').exists()).toBe(true)
    expect(wrapper.get('main').attributes('aria-busy')).toBe('true')
    expect(wrapper.get('main').classes()).toContain('is-navigating')
    expect(wrapper.find('[data-testid="page"]').exists()).toBe(true)

    isLoading.value = false
    await nextTick()
    expect(wrapper.get('main').classes()).not.toContain('is-navigating')
  })
})
