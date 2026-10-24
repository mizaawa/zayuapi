export function safeUIAccessPath(path: string): string | null {
  return path.startsWith('/') && !path.startsWith('//') && !/[\\\r\n]/.test(path) ? path : null
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
    const response = await request(path.split('#')[0], {
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
