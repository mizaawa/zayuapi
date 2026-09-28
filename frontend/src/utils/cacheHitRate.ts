interface CacheUsage {
  input_tokens?: number | null
  cache_creation_tokens?: number | null
  cache_read_tokens?: number | null
}

export function getCacheHitRate(usage: CacheUsage): number | null {
  const cacheRead = usage.cache_read_tokens ?? 0
  if (cacheRead <= 0) return null

  // Stored input tokens exclude both cache reads and cache creation.
  const totalInput = (usage.input_tokens ?? 0) + (usage.cache_creation_tokens ?? 0) + cacheRead
  if (totalInput <= 0) return null

  return (cacheRead / totalInput) * 100
}

export function formatCacheHitRate(usage: CacheUsage): string {
  const rate = getCacheHitRate(usage)
  return rate === null ? '-' : `${rate.toFixed(1)}%`
}

export function getCacheHitRateClass(usage: CacheUsage): string {
  const rate = getCacheHitRate(usage)
  if (rate === null) return 'text-gray-400 dark:text-gray-500'
  if (rate >= 70) return 'text-green-600 dark:text-green-400'
  if (rate >= 20) return 'text-yellow-600 dark:text-yellow-400'
  return 'text-red-600 dark:text-red-400'
}
