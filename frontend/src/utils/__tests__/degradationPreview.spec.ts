import { describe, expect, it } from 'vitest'
import {
  buildDegradationPreviewDocument,
  degradationColumnsForWidth,
  degradationPlatformKey,
  extractDegradationHTML,
  formatDegradationDuration,
} from '@/utils/degradationPreview'

describe('extractDegradationHTML', () => {
  it('prefers a complete document inside Markdown fences', () => {
    const answer = 'Here you go:\n```html\n<!doctype html><html><body><svg></svg></body></html>\n```\nEnjoy!'
    expect(extractDegradationHTML(answer)).toBe('<!doctype html><html><body><svg></svg></body></html>')
  })

  it('accepts bare SVG fragments and drops trailing prose after </html>', () => {
    expect(extractDegradationHTML('  <svg viewBox="0 0 1 1"></svg>')).toBe('<svg viewBox="0 0 1 1"></svg>')
    expect(extractDegradationHTML('<html><body>x</body></html>\n\nThanks')).toBe('<html><body>x</body></html>')
  })

  it('returns an empty string when nothing can be rendered', () => {
    expect(extractDegradationHTML('I cannot draw that.')).toBe('')
    expect(extractDegradationHTML('')).toBe('')
    expect(extractDegradationHTML(undefined)).toBe('')
  })
})

describe('buildDegradationPreviewDocument', () => {
  it('wraps generated markup with a network-blocking policy', () => {
    const doc = buildDegradationPreviewDocument('<svg id="pelican"></svg>')
    expect(doc).toContain('<svg id="pelican"></svg>')
    expect(doc).toContain("connect-src 'none'")
    expect(doc).toContain("default-src 'none'")
    expect(doc).toContain("form-action 'none'")
    expect(doc).toContain('overflow:hidden')
    expect(buildDegradationPreviewDocument('<p></p>', { scrollable: true })).toContain('overflow:auto')
  })
})

describe('degradation layout helpers', () => {
  it('fits between one and five cards per row', () => {
    expect(degradationColumnsForWidth(0)).toBe(1)
    expect(degradationColumnsForWidth(300)).toBe(1)
    expect(degradationColumnsForWidth(400)).toBe(2)
    expect(degradationColumnsForWidth(1000)).toBe(5)
    expect(degradationColumnsForWidth(4000)).toBe(5)
  })

  it('formats durations and platform keys', () => {
    expect(formatDegradationDuration(null)).toBe('-')
    expect(formatDegradationDuration(850)).toBe('850ms')
    expect(formatDegradationDuration(1234)).toBe('1.23s')
    expect(formatDegradationDuration(42_500)).toBe('42.5s')
    expect(formatDegradationDuration(65_000)).toBe('1m 05s')
    expect(degradationPlatformKey('composite')).toBe('custom')
    expect(degradationPlatformKey('openai')).toBe('openai')
    expect(degradationPlatformKey('')).toBe('custom')
  })
})
