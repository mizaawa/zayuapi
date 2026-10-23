// Keep navigation and QR contents limited to web URLs and supported payment apps.
// URL() needs no base: relative URLs must never inherit the site's trusted origin.
const PAYMENT_PROTOCOLS = new Set(['http:', 'https:', 'weixin:', 'alipays:', 'alipay:', 'wxp:'])

export function sanitizePaymentUrl(value: unknown): string {
  if (typeof value !== 'string') return ''
  const candidate = value.trim()
  if (!candidate || Array.from(candidate).some(character => {
    const code = character.charCodeAt(0)
    return code < 0x20 || code === 0x7f
  })) return ''

  try {
    const parsed = new URL(candidate)
    if (!PAYMENT_PROTOCOLS.has(parsed.protocol)) return ''
    if ((parsed.protocol === 'http:' || parsed.protocol === 'https:') && !parsed.hostname) return ''
    return candidate
  } catch {
    return ''
  }
}
