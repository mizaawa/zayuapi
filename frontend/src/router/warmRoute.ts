import type { RouteRecordNormalized, Router } from 'vue-router'

type LazyComponent = () => Promise<unknown>

// Mirrors vue-router's own test: a page that has not been loaded yet is a
// plain loader function, not a component object or functional component.
function isLazyComponent(component: unknown): component is LazyComponent {
  return typeof component === 'function' &&
    !('displayName' in component) &&
    !('props' in component) &&
    !('__vccOpts' in component)
}

/**
 * Starts downloading a route's lazy page chunks without navigating. The
 * router still performs the real load (and reports chunk errors) when a
 * navigation reaches it; the browser reuses the in-flight module request,
 * so this only moves the download earlier. Page chunks are public static
 * assets, so warming one never bypasses an access check.
 */
export function warmRouteComponents(route: { matched?: RouteRecordNormalized[] } | null | undefined): void {
  for (const record of route?.matched ?? []) {
    for (const component of Object.values(record.components ?? {})) {
      if (!isLazyComponent(component)) continue
      try {
        void Promise.resolve(component()).catch(() => undefined)
      } catch {
        // A loader that throws synchronously is reported by the real navigation.
      }
    }
  }
}

// The sidebar remounts with every page, so remember what was already started.
const warmedPaths = new Set<string>()

export function warmRoutePath(router: Pick<Router, 'resolve'>, path: string): void {
  if (!path.startsWith('/') || warmedPaths.has(path)) return
  warmedPaths.add(path)
  try {
    warmRouteComponents(router.resolve(path))
  } catch {
    // Unresolvable custom paths are handled by the navigation itself.
  }
}

type IdleHandle = number | ReturnType<typeof setTimeout>

const requestIdle = (callback: () => void): IdleHandle =>
  typeof window.requestIdleCallback === 'function'
    ? window.requestIdleCallback(callback, { timeout: 3000 })
    : setTimeout(callback, 300)

const cancelIdle = (handle: IdleHandle): void => {
  if (typeof window.cancelIdleCallback === 'function' && typeof handle === 'number') {
    window.cancelIdleCallback(handle)
  } else {
    clearTimeout(handle)
  }
}

function prefersLightNetwork(): boolean {
  const connection = (navigator as Navigator & { connection?: { saveData?: boolean; effectiveType?: string } }).connection
  return connection?.saveData === true || /(^|-)2g$/.test(connection?.effectiveType ?? '')
}

/**
 * Warms the given menu paths one at a time while the browser is idle, so the
 * next sidebar click usually finds its page already downloaded. Skipped on
 * data-saver and 2G connections. Returns a cancel function.
 */
export function warmRoutePathsWhenIdle(router: Pick<Router, 'resolve'>, paths: string[], delayMs = 1500): () => void {
  if (typeof window === 'undefined' || prefersLightNetwork()) return () => undefined
  const queue = [...new Set(paths)].filter((path) => path.startsWith('/') && !warmedPaths.has(path)).slice(0, 30)
  if (queue.length === 0) return () => undefined
  let handle: IdleHandle | null = null
  let cancelled = false

  const step = () => {
    handle = null
    const path = queue.shift()
    if (cancelled || !path) return
    warmRoutePath(router, path)
    handle = requestIdle(step)
  }
  const starter = setTimeout(() => {
    if (!cancelled) handle = requestIdle(step)
  }, delayMs)

  return () => {
    cancelled = true
    clearTimeout(starter)
    if (handle !== null) cancelIdle(handle)
  }
}
