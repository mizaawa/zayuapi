const DNS_LABEL = /^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$/

/** Return a canonical bare domain, empty for disabled, or null for invalid input. */
export function normalizeUiAccessDomain(raw: string): string | null {
  const input = raw.trim()
  if (!input) return ''
  if (/[\s/\\:@?#*%]/u.test(input)) return null

  let domain: string
  try {
    // A fixed suffix performs browser IDNA conversion without interpreting numeric labels as an IP.
    // URL construction is local and does not make a request. The server remains authoritative.
    const hostname = new URL(`http://${input}.invalid`).hostname
    if (!hostname.endsWith('.invalid')) return null
    domain = hostname.slice(0, -'.invalid'.length).toLowerCase()
    // Remove one root dot after IDNA conversion, including Unicode full-stop variants.
    if (domain.endsWith('.')) domain = domain.slice(0, -1)
  } catch {
    return null
  }

  const labels = domain.split('.')
  if (domain.length > 253 || labels.length < 2 || labels.some(label => !DNS_LABEL.test(label))) return null
  // Numeric TLDs can be interpreted as abbreviated/hexadecimal IP addresses by browsers.
  if (/^\d/.test(labels[labels.length - 1]!)) return null
  return domain
}
