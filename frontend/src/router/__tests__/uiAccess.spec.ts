import { describe, expect, it, vi } from 'vitest'
import {
  checkUIAccessNavigation,
  removeUIAccessCacheBuster,
  safeUIAccessPath,
  withoutUIAccessCacheBuster,
} from '../uiAccess'

describe('UI access cache buster', () => {
  it.each([
    ['?_ui_access=abc', ''],
    ['?tab=security&_ui_access=abc', '?tab=security'],
    ['?_ui_access=abc&redirect=%2Fkeys%3Fa%3D1&q=a+b', '?redirect=%2Fkeys%3Fa%3D1&q=a+b'],
    ['?_ui_access&x=1', '?x=1'],
    ['?ui_access=keep&x_ui_access=keep', '?ui_access=keep&x_ui_access=keep'],
    ['', ''],
  ])('drops only the marker from %s', (search, expected) => {
    expect(withoutUIAccessCacheBuster(search)).toBe(expected)
  })

  it('cleans the address bar in place and leaves clean URLs alone', () => {
    const history = { state: { back: '/home' }, replaceState: vi.fn() } as unknown as History
    removeUIAccessCacheBuster({ pathname: '/admin/accounts', search: '?page=2&_ui_access=k3', hash: '#top' } as Location, history)
    expect(history.replaceState).toHaveBeenCalledWith({ back: '/home' }, '', '/admin/accounts?page=2#top')

    const untouched = { state: null, replaceState: vi.fn() } as unknown as History
    removeUIAccessCacheBuster({ pathname: '/keys', search: '?page=2', hash: '' } as Location, untouched)
    expect(untouched.replaceState).not.toHaveBeenCalled()
  })
})

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
    expect(request).toHaveBeenCalledWith(
      expect.stringMatching(/^\/admin\/settings\?tab=security&_ui_access=[a-z0-9]+$/),
      expect.objectContaining({ method: 'HEAD', credentials: 'same-origin', cache: 'no-store', redirect: 'manual' }),
    )
  })

  it('gives every probe a URL that no CDN has cached', async () => {
    const request = vi.fn().mockResolvedValue({ ok: true })
    await checkUIAccessNavigation('/keys', true, request)
    await checkUIAccessNavigation('/keys', true, request)
    const [first, second] = request.mock.calls.map(([url]) => url as string)
    expect(first).toMatch(/^\/keys\?_ui_access=[a-z0-9]+$/)
    expect(second).not.toBe(first)
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
