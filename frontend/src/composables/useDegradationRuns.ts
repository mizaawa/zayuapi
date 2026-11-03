import { getRun, type DegradationRun } from '@/api/degradation'

const MAX_CACHED_RUNS = 120
const cache = new Map<number, Promise<DegradationRun>>()

/**
 * Loads a degradation result with its generated document. Finished results
 * never change, so they are cached (bounded, least recently used first out);
 * in-progress results are always fetched again.
 */
export function loadDegradationRun(id: number): Promise<DegradationRun> {
  const cached = cache.get(id)
  if (cached) {
    cache.delete(id)
    cache.set(id, cached)
    return cached
  }
  const pending = getRun(id).then(
    (run) => {
      if (run.status === 'running') cache.delete(id)
      return run
    },
    (error: unknown) => {
      cache.delete(id)
      throw error
    },
  )
  cache.set(id, pending)
  while (cache.size > MAX_CACHED_RUNS) {
    const oldest = cache.keys().next().value
    if (oldest === undefined) break
    cache.delete(oldest)
  }
  return pending
}

/** Test hook: forget every cached result. */
export function clearDegradationRunCache(): void {
  cache.clear()
}
