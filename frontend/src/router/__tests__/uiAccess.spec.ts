import { describe, expect, it, vi } from 'vitest'
import { checkUIAccessNavigation, safeUIAccessPath } from '../uiAccess'

describe('UI access checks during SPA navigation', () => {
  it('does not add a request to the initial browser navigation', async () => {
    const request = vi.fn()
    expect(await checkUIAccessNavigation('/home', false, request)).toBe(true)
    expect(request).not.toHaveBeenCalled()
  })

  it('checks subsequent visits even when an old tab has no current policy settings', async () => {
    const request = vi.fn().mockResolvedValueOnce({ ok: true }).mockResolvedValueOnce({ ok: false, status: 403 })
    expect(await checkUIAccessNavigation('/home', true, request)).toBe(true)
    expect(await checkUIAccessNavigation('/home', true, request)).toBe(false)
    expect(request).toHaveBeenCalledTimes(2)
  })

  it('uses a same-origin uncached HEAD without transferring the SPA', async () => {
    const request = vi.fn().mockResolvedValue({ ok: true })
    expect(await checkUIAccessNavigation('/admin/settings?tab=security#access', true, request)).toBe(true)
    expect(request).toHaveBeenCalledWith('/admin/settings?tab=security', expect.objectContaining({ method: 'HEAD', credentials: 'same-origin', cache: 'no-store', redirect: 'manual' }))
  })

  it.each([403, 503, 302])('does not allow client navigation after status %s', async status => {
    const request = vi.fn().mockResolvedValue({ ok: false, status })
    expect(await checkUIAccessNavigation('/home', true, request)).toBe(false)
  })

  it('requires a real navigation when the probe cannot connect', async () => {
    const request = vi.fn().mockRejectedValue(new TypeError('network unavailable'))
    expect(await checkUIAccessNavigation('/home', true, request)).toBe(false)
  })

  it.each(['//evil.example', '/\\evil.example', '/home\r\n'])('rejects an unsafe destination %s before fetching', async path => {
    const request = vi.fn()
    expect(await checkUIAccessNavigation(path, true, request)).toBe(false)
    expect(safeUIAccessPath(path)).toBeNull()
    expect(request).not.toHaveBeenCalled()
  })
})
