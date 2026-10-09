/** Serialize a page's aggregate reads, including retries, while allowing cancellation. */
export function createUsageRequestQueue() {
  let tail: Promise<void> = Promise.resolve()

  return <T>(request: () => Promise<T>, signal: AbortSignal): Promise<T> => {
    const previous = tail
    const result = new Promise<T>((resolve, reject) => {
      const onAbort = () => reject(new DOMException('Usage request canceled', 'AbortError'))
      if (signal.aborted) {
        onAbort()
        return
      }
      signal.addEventListener('abort', onAbort, { once: true })
      void previous.then(async () => {
        if (signal.aborted) return
        try {
          resolve(await request())
        } catch (error) {
          reject(error)
        } finally {
          signal.removeEventListener('abort', onAbort)
        }
      })
    })
    // Canceling a queued item must not let later work overtake the active read.
    tail = Promise.allSettled([previous, result]).then(() => undefined)
    return result
  }
}
