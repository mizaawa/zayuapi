const MAX_RETRIES = 2
const INITIAL_RETRY_DELAY_MS = 500
const RATE_LIMIT_RETRY_DELAY_MS = 60_000
const TRANSIENT_STATUSES = new Set([0, 429, 500, 502, 503, 504])
const TRANSIENT_CODES = new Set(['ERR_NETWORK', 'ECONNABORTED', 'ETIMEDOUT'])

interface UsageRequestError {
  status?: number
  code?: string
  name?: string
  retryAfter?: number
  __CANCEL__?: boolean
}

function requestAborted(): DOMException {
  return new DOMException('Usage request canceled', 'AbortError')
}

function getRetryDelay(error: unknown, retries: number): number | null {
  if (typeof error !== 'object' || error === null) return null
  const failure = error as UsageRequestError
  if (failure.name === 'AbortError' || failure.code === 'ERR_CANCELED' || failure.__CANCEL__) {
    return null
  }

  const transient = typeof failure.status === 'number'
    ? TRANSIENT_STATUSES.has(failure.status)
    : typeof failure.code === 'string' && TRANSIENT_CODES.has(failure.code)
  if (!transient) return null

  if (typeof failure.retryAfter === 'number' && Number.isFinite(failure.retryAfter) && failure.retryAfter >= 0) {
    return failure.retryAfter * 1000
  }
  return failure.status === 429
    ? RATE_LIMIT_RETRY_DELAY_MS
    : INITIAL_RETRY_DELAY_MS * 2 ** retries
}

function waitForRetry(milliseconds: number, signal: AbortSignal): Promise<void> {
  return new Promise((resolve, reject) => {
    if (signal.aborted) {
      reject(requestAborted())
      return
    }

    const onAbort = () => {
      clearTimeout(timer)
      signal.removeEventListener('abort', onAbort)
      reject(requestAborted())
    }
    const timer = setTimeout(() => {
      signal.removeEventListener('abort', onAbort)
      resolve()
    }, milliseconds)
    signal.addEventListener('abort', onAbort, { once: true })
  })
}

/** Retry transient usage-page read failures without restarting successful sections. */
export async function fetchUsageWithRetry<T>(
  request: () => Promise<T>,
  signal: AbortSignal
): Promise<T> {
  for (let retries = 0; ; retries++) {
    if (signal.aborted) throw requestAborted()
    try {
      const result = await request()
      if (signal.aborted) throw requestAborted()
      return result
    } catch (error) {
      if (signal.aborted) throw requestAborted()
      const delay = getRetryDelay(error, retries)
      if (delay === null || retries >= MAX_RETRIES) throw error
      await waitForRetry(delay, signal)
    }
  }
}
