import type { PricingFormEntry, IntervalFormEntry } from './types'
import { findModelConflict } from './types'
import type { PlatformPricingCurrency } from '@/utils/channelPricingCurrency'

const PRICE_FIELDS = ['input_price', 'output_price', 'cache_write_price', 'cache_read_price', 'image_input_price', 'image_output_price', 'per_request_price'] as const
const INTERVAL_PRICE_FIELDS = ['input_price', 'output_price', 'cache_write_price', 'cache_read_price', 'per_request_price'] as const
const ENTRY_FIELDS = ['models', 'billing_mode', ...PRICE_FIELDS, 'intervals']
const INTERVAL_FIELDS = ['min_tokens', 'max_tokens', 'tier_label', ...INTERVAL_PRICE_FIELDS, 'sort_order']
const MODES = ['token', 'per_request', 'image', 'video']

export interface PricingTransfer {
  format: 'zayuapi-channel-pricing'
  version: 1
  platform: string
  currency: PlatformPricingCurrency
  entries: PricingFormEntry[]
}

export interface PricingFieldChange { field: string; before: unknown; after: unknown }
export interface PricingModelChange { model: string; fields: PricingFieldChange[] }
export interface PricingPreview { added: string[]; removed: string[]; changed: PricingModelChange[] }

function record(value: unknown, path: string): Record<string, unknown> {
  if (!value || typeof value !== 'object' || Array.isArray(value)) throw new Error(`${path}: expected object`)
  return value as Record<string, unknown>
}

function exactKeys(value: Record<string, unknown>, keys: readonly string[], path: string) {
  const missing = keys.filter(key => !(key in value))
  const unknown = Object.keys(value).filter(key => !keys.includes(key))
  if (missing.length || unknown.length) throw new Error(`${path}: ${missing.length ? `missing ${missing.join(', ')}` : `unknown ${unknown.join(', ')}`}`)
}

function price(value: unknown, path: string): number | null {
  if (value === null) return null
  if (typeof value !== 'number' || !Number.isFinite(value) || value < 0 || value > 1e9) {
    throw new Error(`${path}: price must be a finite non-negative number (at most 1e9) or null`)
  }
  return value
}

function integer(value: unknown, path: string): number {
  if (typeof value !== 'number' || !Number.isSafeInteger(value) || value < 0) throw new Error(`${path}: expected non-negative integer`)
  return value
}

function formPrice(value: number | string | null, path: string): number | null {
  if (value === null || value === '') return null
  const amount = Number(value)
  return price(amount, path)
}

function normalizeInterval(raw: unknown, path: string): IntervalFormEntry {
  const item = record(raw, path)
  exactKeys(item, INTERVAL_FIELDS, path)
  const min = integer(item.min_tokens, `${path}.min_tokens`)
  const max = item.max_tokens === null ? null : integer(item.max_tokens, `${path}.max_tokens`)
  if (max !== null && max <= min) throw new Error(`${path}: max_tokens must exceed min_tokens`)
  if (typeof item.tier_label !== 'string' || item.tier_label.length > 100) throw new Error(`${path}.tier_label: expected string of at most 100 characters`)
  const result = { min_tokens: min, max_tokens: max, tier_label: item.tier_label.trim(), sort_order: integer(item.sort_order, `${path}.sort_order`) } as IntervalFormEntry
  for (const field of INTERVAL_PRICE_FIELDS) result[field] = price(item[field], `${path}.${field}`)
  return result
}

function normalizeEntry(raw: unknown, path: string): PricingFormEntry {
  const item = record(raw, path)
  exactKeys(item, ENTRY_FIELDS, path)
  if (!Array.isArray(item.models) || item.models.length === 0) throw new Error(`${path}.models: expected non-empty array`)
  const models = item.models.map((model, index) => {
    if (typeof model !== 'string' || !model.trim() || model !== model.trim() || model.length > 200) {
      throw new Error(`${path}.models[${index}]: expected non-empty trimmed model name (max 200)`)
    }
    return model
  })
  if (!MODES.includes(item.billing_mode as string)) throw new Error(`${path}.billing_mode: unsupported billing mode`)
  if (!Array.isArray(item.intervals) || item.intervals.length > 100) throw new Error(`${path}.intervals: expected array (max 100)`)
  const intervals = item.intervals.map((interval, index) => normalizeInterval(interval, `${path}.intervals[${index}]`))
  const result = { models, billing_mode: item.billing_mode, intervals } as PricingFormEntry
  for (const field of PRICE_FIELDS) result[field] = price(item[field], `${path}.${field}`)
  if (item.billing_mode === 'token') {
    const sorted = [...intervals].sort((a, b) => a.min_tokens - b.min_tokens)
    for (let i = 0; i < sorted.length; i++) {
      if (!INTERVAL_PRICE_FIELDS.some(field => sorted[i][field] !== null)) throw new Error(`${path}.intervals[${i}]: empty price tier`)
      if (i && (sorted[i - 1].max_tokens === null || sorted[i - 1].max_tokens! > sorted[i].min_tokens)) throw new Error(`${path}.intervals: overlapping token ranges`)
    }
  } else {
    const labels = new Set<string>()
    for (const interval of intervals) {
      if (!interval.tier_label) throw new Error(`${path}.intervals: tier_label is required`)
      const label = interval.tier_label.toLowerCase()
      if (labels.has(label)) throw new Error(`${path}.intervals: duplicate tier_label ${interval.tier_label}`)
      labels.add(label)
      if (interval.per_request_price === null) throw new Error(`${path}.intervals: per_request_price is required for a tier`)
    }
    if (result.per_request_price === null && intervals.length === 0) throw new Error(`${path}: per_request_price or a priced tier is required`)
  }
  return result
}

function normalizeCurrency(raw: unknown): PlatformPricingCurrency {
  const currency = record(raw, 'currency')
  exactKeys(currency, ['official_cny', 'discounted_cny', 'cny_per_usd'], 'currency')
  if (typeof currency.official_cny !== 'boolean' || typeof currency.discounted_cny !== 'boolean') throw new Error('currency: switches must be boolean')
  if (typeof currency.cny_per_usd !== 'number' || !Number.isFinite(currency.cny_per_usd) || currency.cny_per_usd <= 0 || currency.cny_per_usd > 1e6) {
    throw new Error('currency.cny_per_usd: expected a positive finite exchange rate')
  }
  return currency as unknown as PlatformPricingCurrency
}

export function parsePricingTransfer(text: string, platform: string): PricingTransfer {
  let raw: unknown
  try { raw = JSON.parse(text) } catch { throw new Error('Invalid JSON syntax') }
  const data = record(raw, 'root')
  exactKeys(data, ['format', 'version', 'platform', 'currency', 'entries'], 'root')
  if (data.format !== 'zayuapi-channel-pricing' || data.version !== 1) throw new Error('Unsupported pricing JSON format or version')
  if (data.platform !== platform) throw new Error(`Platform mismatch: file is for ${String(data.platform)}, expected ${platform}`)
  if (!Array.isArray(data.entries) || data.entries.length > 1000) throw new Error('entries: expected array (max 1000)')
  const entries = data.entries.map((entry, index) => normalizeEntry(entry, `entries[${index}]`))
  const allModels = entries.flatMap(entry => entry.models)
  if (allModels.length > 10000) throw new Error('entries: too many models (max 10000)')
  const conflict = findModelConflict(allModels)
  if (conflict) throw new Error(`Duplicate or overlapping models: ${conflict.join(' / ')}`)
  return { format: 'zayuapi-channel-pricing', version: 1, platform, currency: normalizeCurrency(data.currency), entries }
}

export function exportPricingTransfer(platform: string, currency: PlatformPricingCurrency, entries: PricingFormEntry[]): string {
  const jsonEntries = entries.map((entry, index) => {
    const output: Record<string, unknown> = { models: [...entry.models], billing_mode: entry.billing_mode, intervals: entry.intervals.map(interval => {
      const iv: Record<string, unknown> = { min_tokens: interval.min_tokens, max_tokens: interval.max_tokens, tier_label: interval.tier_label, sort_order: interval.sort_order }
      for (const field of INTERVAL_PRICE_FIELDS) iv[field] = formPrice(interval[field], `entries[${index}].intervals.${field}`)
      return iv
    }) }
    for (const field of PRICE_FIELDS) output[field] = formPrice(entry[field], `entries[${index}].${field}`)
    return output
  })
  const text = JSON.stringify({ format: 'zayuapi-channel-pricing', version: 1, platform, currency, entries: jsonEntries }, null, 2) + '\n'
  parsePricingTransfer(text, platform)
  return text
}

export function previewPricingTransfer(before: PricingFormEntry[], after: PricingFormEntry[]): PricingPreview {
  const flatten = (entries: PricingFormEntry[]) => new Map(entries.flatMap(entry => entry.models.map(model => [model.toLowerCase(), { model, entry }] as const)))
  const oldModels = flatten(before)
  const newModels = flatten(after)
  const added: string[] = [], removed: string[] = [], changed: PricingModelChange[] = []
  for (const [name, current] of newModels) {
    const previous = oldModels.get(name)
    if (!previous) { added.push(current.model); continue }
    const fields: PricingFieldChange[] = []
    for (const field of ['billing_mode', ...PRICE_FIELDS] as const) {
      if (String(previous.entry[field] ?? '') !== String(current.entry[field] ?? '')) fields.push({ field, before: previous.entry[field], after: current.entry[field] })
    }
    const count = Math.max(previous.entry.intervals.length, current.entry.intervals.length)
    for (let i = 0; i < count; i++) {
      for (const field of INTERVAL_FIELDS) {
        const oldValue = previous.entry.intervals[i]?.[field as keyof IntervalFormEntry]
        const newValue = current.entry.intervals[i]?.[field as keyof IntervalFormEntry]
        if (String(oldValue ?? '') !== String(newValue ?? '')) fields.push({ field: `intervals[${i}].${field}`, before: oldValue, after: newValue })
      }
    }
    if (fields.length) changed.push({ model: current.model, fields })
  }
  for (const [name, previous] of oldModels) if (!newModels.has(name)) removed.push(previous.model)
  return { added, removed, changed }
}
