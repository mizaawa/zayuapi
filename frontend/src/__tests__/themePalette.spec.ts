import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

import { describe, expect, it } from 'vitest'

const dir = dirname(fileURLToPath(import.meta.url))
const styleSource = readFileSync(resolve(dir, '../style.css'), 'utf8')
const toastSource = readFileSync(resolve(dir, '../components/common/Toast.vue'), 'utf8')

function getBlock(selector: string): string {
  const escapedSelector = selector.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
  const match = styleSource.match(new RegExp(`${escapedSelector}\\s*{([^}]+)}`))
  if (!match) throw new Error(`Missing CSS block: ${selector}`)
  return match[1]
}

function getVariable(block: string, name: string): string {
  const match = block.match(new RegExp(`${name}:\\s*(#[0-9a-f]{6})`, 'i'))
  if (!match) throw new Error(`Missing CSS variable: ${name}`)
  return match[1]
}

function relativeLuminance(hex: string): number {
  const channels = hex.slice(1).match(/.{2}/g)
  if (!channels) throw new Error(`Invalid color: ${hex}`)

  const [red, green, blue] = channels.map((channel) => {
    const value = Number.parseInt(channel, 16) / 255
    return value <= 0.04045 ? value / 12.92 : ((value + 0.055) / 1.055) ** 2.4
  })

  return 0.2126 * red + 0.7152 * green + 0.0722 * blue
}

function contrastRatio(first: string, second: string): number {
  const firstLuminance = relativeLuminance(first)
  const secondLuminance = relativeLuminance(second)
  const lighter = Math.max(firstLuminance, secondLuminance)
  const darker = Math.min(firstLuminance, secondLuminance)
  return (lighter + 0.05) / (darker + 0.05)
}

describe('light-only Monet palette', () => {
  const root = getBlock(':root')
  const darkFallback = getBlock(':root.dark')

  it('keeps instructional and body text readable on themed surfaces', () => {
    const placeholder = getVariable(root, '--md-sys-color-placeholder')
    const surface = getVariable(root, '--md-sys-color-surface')
    const container = getVariable(root, '--md-sys-color-surface-container')
    const bodyText = getVariable(root, '--md-sys-color-on-surface-variant')
    const background = getVariable(root, '--md-sys-color-background')

    expect(contrastRatio(placeholder, surface)).toBeGreaterThanOrEqual(4.5)
    expect(contrastRatio(placeholder, container)).toBeGreaterThanOrEqual(4.5)
    expect(contrastRatio(bodyText, background)).toBeGreaterThanOrEqual(4.5)
  })

  it('uses readable dark text on the brighter primary yellow', () => {
    const primary = getVariable(root, '--md-sys-color-primary')
    const onPrimary = getVariable(root, '--md-sys-color-on-primary')

    expect(contrastRatio(onPrimary, primary)).toBeGreaterThanOrEqual(4.5)
  })

  it('keeps stale dark mode state on the same light palette', () => {
    const names = [
      '--md-sys-color-background',
      '--md-sys-color-surface',
      '--md-sys-color-surface-container',
      '--md-sys-color-primary',
      '--md-sys-color-placeholder',
    ]

    for (const name of names) {
      expect(getVariable(darkFallback, name)).toBe(getVariable(root, name))
    }
  })

  it('globally applies the placeholder token', () => {
    expect(styleSource).toMatch(/input::placeholder,\s*textarea::placeholder/)
    expect(styleSource).toContain('color: var(--md-sys-color-placeholder) !important;')
  })

  it('keeps success toasts garden green and info toasts brand gold', () => {
    const toneAccent = (tone: string) => {
      const match = toastSource.match(new RegExp(`\\.toast-tone-${tone}\\s*\\{[^}]*--toast-accent:\\s*(#[0-9a-f]{6})`, 'i'))
      if (!match) throw new Error(`Missing toast accent: ${tone}`)
      return match[1].toLowerCase()
    }

    expect(toneAccent('success')).toBe('#5c9865')
    expect(toneAccent('info')).toBe(getVariable(root, '--md-sys-color-primary').toLowerCase())
    expect(toastSource).not.toContain('#39c5bb')
  })
})
