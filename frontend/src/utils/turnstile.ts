export type TurnstileSize = 'normal' | 'compact' | 'flexible'

interface RenderOptions {
  sitekey: string
  size: TurnstileSize
  theme: 'light' | 'dark' | 'auto'
  callback: (token: string) => void
  'expired-callback': () => void
  'error-callback': () => boolean
  'timeout-callback': () => void
}

export interface TurnstileAPI {
  render: (container: HTMLElement, options: RenderOptions) => string
  reset: (id: string) => void
  remove: (id: string) => void
}

declare global {
  interface Window { turnstile?: TurnstileAPI }
}

let pending: Promise<TurnstileAPI> | undefined

// Multiple forms (including email verification) may mount while the SDK is loading.
export function loadTurnstile(): Promise<TurnstileAPI> {
  if (pending) return pending
  if (typeof window.turnstile?.render === 'function') return Promise.resolve(window.turnstile)

  pending = new Promise<TurnstileAPI>((resolve, reject) => {
    const script = document.createElement('script')
    const cleanup = () => {
      window.clearTimeout(timer)
      script.onload = script.onerror = null
    }
    const fail = () => {
      cleanup()
      script.remove()
      reject(new Error('Turnstile SDK unavailable'))
    }
    const timer = window.setTimeout(fail, 25000)
    script.src = 'https://challenges.cloudflare.com/turnstile/v0/api.js?render=explicit'
    script.async = true
    const nonce = document.querySelector<HTMLScriptElement>('script[nonce]')?.nonce
    if (nonce) script.nonce = nonce
    script.onload = () => {
      const api = window.turnstile
      // This is an async script: the SDK rejects ready() in this mode.
      // The element's load event already follows execution of the explicit-render API.
      if (typeof api?.render !== 'function') return fail()
      cleanup()
      resolve(api)
    }
    script.onerror = fail
    document.head.appendChild(script)
  }).finally(() => { pending = undefined })
  return pending
}
