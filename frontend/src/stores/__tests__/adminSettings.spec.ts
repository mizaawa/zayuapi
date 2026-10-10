import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useAdminSettingsStore } from '../adminSettings'

const { getSettings, getPaymentConfig } = vi.hoisted(() => ({
  getSettings: vi.fn(),
  getPaymentConfig: vi.fn()
}))

vi.mock('@/api', () => ({
  adminAPI: {
    settings: { getSettings },
    payment: { getConfig: getPaymentConfig }
  }
}))

describe('admin settings redeem code creation limit', () => {
  beforeEach(() => {
    localStorage.clear()
    setActivePinia(createPinia())
    getSettings.mockReset().mockResolvedValue({})
    getPaymentConfig.mockReset().mockResolvedValue({ data: { enabled: false } })
  })

  it('defaults to enforcing the limit when the setting is missing', async () => {
    const store = useAdminSettingsStore()
    expect(store.disableRedeemCodeCreationLimit).toBe(false)
    await store.fetch()
    expect(store.disableRedeemCodeCreationLimit).toBe(false)
  })

  it('loads the enabled setting and refreshes it when disabled', async () => {
    getSettings.mockResolvedValueOnce({ disable_redeem_code_creation_limit: true })
    const store = useAdminSettingsStore()
    await store.fetch()
    expect(store.disableRedeemCodeCreationLimit).toBe(true)

    getSettings.mockResolvedValueOnce({ disable_redeem_code_creation_limit: false })
    await store.fetch(true)
    expect(store.disableRedeemCodeCreationLimit).toBe(false)
    expect(getSettings).toHaveBeenCalledTimes(2)
  })

  it('loads the setting even when payment configuration fails', async () => {
    localStorage.setItem('payment_enabled_cached', 'true')
    getSettings.mockResolvedValue({ disable_redeem_code_creation_limit: true })
    const failure = new Error('payment configuration unavailable')
    getPaymentConfig.mockRejectedValue(failure)
    const errorSpy = vi.spyOn(console, 'error').mockImplementation(() => {})
    try {
      const store = useAdminSettingsStore()
      await store.fetch()
      expect(store.disableRedeemCodeCreationLimit).toBe(true)
      expect(store.paymentEnabled).toBe(true)
      expect(store.loaded).toBe(true)
      expect(errorSpy).toHaveBeenCalledWith('[adminSettings] Failed to fetch payment config:', failure)
    } finally {
      errorSpy.mockRestore()
    }
  })
})
