import type { RouteLocationRaw, RouteLocationNormalized } from 'vue-router'

/** Render the ordinary unknown-page record without exposing a protected page or changing its URL. */
export function notFoundLocation(
  route: Pick<RouteLocationNormalized, 'path' | 'query' | 'hash'>,
): RouteLocationRaw {
  return {
    name: 'NotFound',
    params: { pathMatch: route.path.slice(1).split('/') },
    query: route.query,
    hash: route.hash,
    replace: true,
  }
}
