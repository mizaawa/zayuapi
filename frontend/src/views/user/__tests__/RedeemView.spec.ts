import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import RedeemView from '../RedeemView.vue'

const mocks = vi.hoisted(() => ({
  redeem: vi.fn(),
  getHistory: vi.fn(),
  getPublicSettings: vi.fn(),
  refreshUser: vi.fn(),
  fetchActiveSubscriptions: vi.fn(),
  showError: vi.fn(),
  showSuccess: vi.fn(),
  showWarning: vi.fn(),
}))

vi.mock('@/api', () => ({
  redeemAPI: { redeem: mocks.redeem, getHistory: mocks.getHistory },
  authAPI: { getPublicSettings: mocks.getPublicSettings },
}))

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({ user: { balance: 10, concurrency: 1 }, refreshUser: mocks.refreshUser }),
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError: mocks.showError,
    showSuccess: mocks.showSuccess,
    showWarning: mocks.showWarning,
  }),
}))

vi.mock('@/stores/subscriptions', () => ({
  useSubscriptionStore: () => ({ fetchActiveSubscriptions: mocks.fetchActiveSubscriptions }),
}))

vi.mock('vue-i18n', async () => ({
  ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'),
  useI18n: () => ({ t: (key: string) => key }),
}))

function mountRedeemView() {
  return mount(RedeemView, {
    global: {
      stubs: {
        AppLayout: { template: '<main><slot /></main>' },
        Icon: true,
      },
    },
  })
}

beforeEach(() => {
  vi.clearAllMocks()
  mocks.redeem.mockResolvedValue({
    message: 'Redeemed',
    type: 'balance',
    value: 20,
    new_balance: 30,
  })
  mocks.getHistory.mockResolvedValue([])
  mocks.getPublicSettings.mockResolvedValue({ contact_info: '' })
  mocks.refreshUser.mockResolvedValue({ balance: 30, concurrency: 1 })
  mocks.fetchActiveSubscriptions.mockResolvedValue([])
})

describe('RedeemView', () => {
  it('keeps a successful redemption successful when refreshing the user profile fails', async () => {
    mocks.refreshUser.mockRejectedValue(new Error('profile service unavailable'))
    const consoleError = vi.spyOn(console, 'error').mockImplementation(() => undefined)
    const wrapper = mountRedeemView()
    await flushPromises()

    await wrapper.get('#code').setValue('REDEEM-123')
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    expect(mocks.redeem).toHaveBeenCalledWith('REDEEM-123')
    expect(mocks.showSuccess).toHaveBeenCalledWith('redeem.codeRedeemSuccess')
    expect(mocks.showError).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('redeem.redeemSuccess')
    expect(wrapper.text()).toContain('Redeemed')
    expect(wrapper.text()).not.toContain('redeem.redeemFailed')
    expect(wrapper.get('#code').element).toHaveProperty('value', '')
    expect(consoleError).toHaveBeenCalledWith('Failed to refresh user after redeem:', expect.any(Error))

    consoleError.mockRestore()
  })

  it('reports a redemption request failure as an error', async () => {
    mocks.redeem.mockRejectedValue({ response: { data: { detail: 'Invalid code' } } })
    const wrapper = mountRedeemView()
    await flushPromises()

    await wrapper.get('#code').setValue('BAD-CODE')
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    expect(mocks.showError).toHaveBeenCalledWith('redeem.redeemFailed')
    expect(mocks.showSuccess).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('redeem.redeemFailed')
    expect(wrapper.text()).toContain('Invalid code')
  })
})
