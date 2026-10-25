import { beforeEach, describe, expect, it, vi } from 'vitest'

const { get, post } = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn() }))
vi.mock('@/api/client', () => ({ apiClient: { get, post } }))
import grokAPI, { queryQuota, resetQuota } from '@/api/admin/grok'

describe('admin Grok existing-account maintenance API', () => {
  beforeEach(() => { get.mockReset(); post.mockReset() })

  it('queries quota for an existing account', async () => {
    const quota = { source: 'active_probe', headers_observed: true, reset_supported: true, fetched_at: 1 }
    get.mockResolvedValue({ data: quota })
    expect(await queryQuota(42)).toEqual(quota)
    expect(get).toHaveBeenCalledWith('/admin/grok/accounts/42/quota')
  })

  it('resets quota for an existing account', async () => {
    const result = { supported: true, code: 'ok', message: 'reset' }
    post.mockResolvedValue({ data: result })
    expect(await resetQuota(42)).toEqual(result)
    expect(post).toHaveBeenCalledWith('/admin/grok/accounts/42/reset-quota')
  })

  it('exports no OAuth or SSO onboarding methods', () => {
    expect(Object.keys(grokAPI).sort()).toEqual(['queryQuota', 'resetQuota'])
  })
})
