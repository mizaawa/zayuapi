export const USAGE_EXPORT_PAGE_SIZE = 1000
export const USAGE_EXPORT_TIMEOUT = 120000

const MAX_RATE_LIMIT_RETRIES = 3
const DEFAULT_RETRY_AFTER_SECONDS = 60

function exportAborted(): DOMException {
  return new DOMException('Usage export canceled', 'AbortError')
}

function waitForRetry(milliseconds: number, signal?: AbortSignal): Promise<void> {
  return new Promise((resolve, reject) => {
    if (signal?.aborted) {
      reject(exportAborted())
      return
    }

    const onAbort = () => {
      clearTimeout(timer)
      signal?.removeEventListener('abort', onAbort)
      reject(exportAborted())
    }
    const timer = setTimeout(() => {
      signal?.removeEventListener('abort', onAbort)
      resolve()
    }, milliseconds)

    signal?.addEventListener('abort', onAbort, { once: true })
  })
}

export async function fetchUsageExportPage<T>(
  request: () => Promise<T>,
  signal?: AbortSignal
): Promise<T> {
  for (let retries = 0; ; retries++) {
    if (signal?.aborted) throw exportAborted()

    try {
      const result = await request()
      if (signal?.aborted) throw exportAborted()
      return result
    } catch (error) {
      if (signal?.aborted) throw exportAborted()
      if (
        typeof error !== 'object' || error === null ||
        !('status' in error) || error.status !== 429 ||
        retries >= MAX_RATE_LIMIT_RETRIES
      ) {
        throw error
      }

      const retryAfter = 'retryAfter' in error ? error.retryAfter : undefined
      const seconds = typeof retryAfter === 'number' && Number.isFinite(retryAfter) && retryAfter >= 0
        ? retryAfter
        : DEFAULT_RETRY_AFTER_SECONDS
      await waitForRetry(seconds * 1000, signal)
    }
  }
}
