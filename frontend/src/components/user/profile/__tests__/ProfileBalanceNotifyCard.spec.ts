import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import ProfileBalanceNotifyCard from '../ProfileBalanceNotifyCard.vue'

const { sendNotifyEmailCode, verifyNotifyEmail, getProfile } = vi.hoisted(() => ({
  sendNotifyEmailCode: vi.fn(),
  verifyNotifyEmail: vi.fn(),
  getProfile: vi.fn()
}))

vi.mock('@/api', () => ({
  userAPI: { sendNotifyEmailCode, verifyNotifyEmail, getProfile }
}))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => ({ user: null }) }))
vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showSuccess: vi.fn(), showError: vi.fn() })
}))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

enableAutoUnmount(afterEach)

const deferred = () => {
  let resolve!: () => void
  const promise = new Promise<void>((done) => { resolve = done })
  return { promise, resolve }
}

const button = (wrapper: VueWrapper, text: string) =>
  wrapper.findAll('button').find(item => item.text() === text)!

describe('ProfileBalanceNotifyCard', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.resetAllMocks()
    sendNotifyEmailCode.mockResolvedValue({})
    getProfile.mockResolvedValue({ balance_notify_extra_emails: [] })
  })

  afterEach(() => {
    vi.clearAllTimers()
    vi.useRealTimers()
  })

  it.each(['remove', 'unmount'])('does not start a pending email timer after %s', async (action) => {
    const request = deferred()
    sendNotifyEmailCode.mockReturnValueOnce(request.promise)
    const wrapper = mount(ProfileBalanceNotifyCard, {
      props: { enabled: true, threshold: null, extraEmails: [], systemDefaultThreshold: 5, userEmail: '' }
    })
    await wrapper.get('input[type="email"]').setValue('new@example.com')
    await button(wrapper, 'common.add').trigger('click')
    await button(wrapper, 'profile.balanceNotify.sendCode').trigger('click')
    if (action === 'remove') await button(wrapper, 'profile.balanceNotify.removeEmail').trigger('click')
    else wrapper.unmount()

    request.resolve()
    await flushPromises()
    expect(vi.getTimerCount()).toBe(0)
  })

  it('does not start a saved email timer after unmount', async () => {
    const request = deferred()
    sendNotifyEmailCode.mockReturnValueOnce(request.promise)
    const wrapper = mount(ProfileBalanceNotifyCard, {
      props: {
        enabled: true, threshold: null, systemDefaultThreshold: 5, userEmail: '',
        extraEmails: [{ email: 'saved@example.com', disabled: false, verified: false }]
      }
    })
    await button(wrapper, 'profile.balanceNotify.verify').trigger('click')
    wrapper.unmount()
    request.resolve()
    await flushPromises()
    expect(vi.getTimerCount()).toBe(0)
  })

})
