/** One-time query parameter on a region retry's redirect and on navigation
 * probes. A cache in front of the site that stored a region notice for a page
 * cannot answer a URL it has never seen, so these requests reach the server. */
export const UI_ACCESS_CACHE_BUSTER = '_ui_access'

export function safeUIAccessPath(path: string): string | null {
  return path.startsWith('/') && !path.startsWith('//') && !/[\\\r\n]/.test(path) ? path : null
}

/** Drops the cache buster from a query string, keeping every other parameter
 * byte for byte (URLSearchParams would re-encode them). */
export function withoutUIAccessCacheBuster(search: string): string {
  const kept = search
    .replace(/^\?/, '')
    .split('&')
    .filter(part => part !== '' && part.split('=')[0] !== UI_ACCESS_CACHE_BUSTER)
  return kept.length ? `?${kept.join('&')}` : ''
}

/** Removes the retry's cache buster from the address bar. Call it before the
 * router reads the URL so no route ever sees the parameter. */
export function removeUIAccessCacheBuster(location: Location = window.location, history: History = window.history): void {
  const search = withoutUIAccessCacheBuster(location.search)
  if (search !== location.search) history.replaceState(history.state, '', `${location.pathname}${search}${location.hash}`)
}

function uncachedProbeURL(path: string): string {
  const target = path.split('#')[0]
  const token = `${Date.now().toString(36)}${Math.random().toString(36).slice(2, 10)}`
  return `${target}${target.includes('?') ? '&' : '?'}${UI_ACCESS_CACHE_BUSTER}=${token}`
}

/** Check a subsequent SPA navigation against the same server-side document
 * policy as a browser refresh. HEAD transfers no HTML or application assets.
 * Always ask the server on later navigations: cached switches may predate a
 * policy enabled by an administrator in another tab or session. */
export async function checkUIAccessNavigation(
  path: string,
  alreadyNavigated: boolean,
  request: typeof fetch = fetch,
): Promise<boolean> {
  if (!alreadyNavigated) return true
  if (safeUIAccessPath(path) === null) return false

  const controller = new AbortController()
  const timeout = setTimeout(() => controller.abort(), 8000)
  try {
    const response = await request(uncachedProbeURL(path), {
      method: 'HEAD',
      credentials: 'same-origin',
      cache: 'no-store',
      redirect: 'manual',
      signal: controller.signal,
    })
    // A rejected or unavailable document must be handled by a real browser
    // navigation so the branded server notice and retry form are displayed.
    return response.ok
  } catch {
    return false
  } finally {
    clearTimeout(timeout)
  }
}
