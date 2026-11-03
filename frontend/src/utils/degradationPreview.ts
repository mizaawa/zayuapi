import { buildApiUrl } from '@/api/url'

/**
 * Generated animations run inside a sandboxed iframe that loads a static shell
 * from `/degradation/preview-frame` and receives the document via postMessage.
 * The frame never gets `allow-same-origin`, so the generated code cannot read
 * the site's cookies, storage or DOM, and its CSP blocks every network request.
 */

/** Virtual viewport the documents are laid out in before being scaled to fit. */
export const DEGRADATION_PREVIEW_WIDTH = 960
export const DEGRADATION_PREVIEW_HEIGHT = 720

export const DEGRADATION_PREVIEW_MESSAGE = 'sub2api-degradation-preview'
/** The shell ignores larger messages; keep in sync with the backend shell. */
export const DEGRADATION_PREVIEW_MAX_LENGTH = 2_200_000

const PREVIEW_POLICY = [
  "default-src 'none'",
  "script-src 'unsafe-inline'",
  "style-src 'unsafe-inline'",
  'img-src data: blob:',
  'font-src data:',
  'media-src data: blob:',
  "connect-src 'none'",
  "frame-src 'none'",
  "worker-src 'none'",
  "object-src 'none'",
  "base-uri 'none'",
  "form-action 'none'",
].join('; ')

export function degradationPreviewFrameURL(): string {
  return buildApiUrl('/degradation/preview-frame')
}

/**
 * Extracts the renderable document from a model answer. Accepts complete HTML
 * documents, Markdown-fenced HTML/SVG and bare SVG fragments. Returns an empty
 * string when the answer contains nothing that can be rendered.
 */
export function extractDegradationHTML(output: string | null | undefined): string {
  const text = output ?? ''
  if (!text.trim()) return ''
  const blocks = [...text.matchAll(/```(?:html|svg|xml)?[^\S\r\n]*\r?\n([\s\S]*?)```/gi)]
  const candidate =
    blocks.find((block) => /<!doctype\s+html|<html[\s>]/i.test(block[1]))?.[1] ??
    blocks.find((block) => /<(?:svg|div|style|main|body)[\s>]/i.test(block[1]))?.[1] ??
    text.replace(/^\s*```(?:html|svg|xml)?\s*\r?\n/i, '').replace(/\s*```\s*$/, '')
  const start = candidate.search(/<!doctype\s+html|<html[\s>]|<(?:svg|div|style|main|body|section|canvas)[\s>]/i)
  if (start < 0) return ''
  const html = candidate.slice(start).trim()
  const end = html.toLowerCase().lastIndexOf('</html>')
  return end >= 0 ? html.slice(0, end + 7) : html
}

/**
 * Wraps the generated markup with a restrictive CSP and neutral base styles.
 * The shell already applies an equivalent CSP header; the meta tag is a
 * second layer in case the shell is ever served without it.
 */
export function buildDegradationPreviewDocument(html: string, options: { scrollable?: boolean } = {}): string {
  const overflow = options.scrollable ? 'auto' : 'hidden'
  return (
    '<!doctype html><html><head><meta charset="utf-8">' +
    `<meta http-equiv="Content-Security-Policy" content="${PREVIEW_POLICY}">` +
    '<meta name="referrer" content="no-referrer">' +
    `<meta name="viewport" content="width=${DEGRADATION_PREVIEW_WIDTH}, initial-scale=1">` +
    `<style>html,body{margin:0;min-height:100%;overflow:${overflow};}svg{max-width:100%;}</style>` +
    `</head><body>${html}</body></html>`
  )
}

/** Formats a millisecond duration as "1.2s" / "2m 05s". */
export function formatDegradationDuration(ms: number | null | undefined): string {
  if (ms === null || ms === undefined || !Number.isFinite(ms) || ms < 0) return '-'
  if (ms < 1000) return `${Math.round(ms)}ms`
  const seconds = ms / 1000
  if (seconds < 60) return `${seconds.toFixed(seconds < 10 ? 2 : 1)}s`
  const minutes = Math.floor(seconds / 60)
  const rest = Math.round(seconds - minutes * 60)
  return `${minutes}m ${String(rest).padStart(2, '0')}s`
}

/** Normalizes a site group platform to the icon/label key. */
export function degradationPlatformKey(platform: string | null | undefined): string {
  return platform === 'composite' ? 'custom' : (platform || 'custom')
}

export const DEGRADATION_PLATFORM_ORDER = ['anthropic', 'openai', 'gemini', 'grok', 'custom'] as const

/** Number of cards that fit in one row, between 1 and 5. */
export function degradationColumnsForWidth(width: number, minCardWidth = 176, gap = 16): number {
  if (!Number.isFinite(width) || width <= 0) return 1
  return Math.max(1, Math.min(5, Math.floor((width + gap) / (minCardWidth + gap))))
}
