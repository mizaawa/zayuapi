import type { AccountPlatform } from '@/types'

/** How a new account receives its API key. */
export type AccountImportSource = 'api' | 'provider'

/**
 * A provider key chosen for a new account. It only names the key: the server
 * resolves the secret when the account is created.
 */
export interface ProviderAccountSelection {
  providerId: string
  providerName: string
  keyId: string
  keyName: string
  groupName: string
  groupMultiplier: number | null
  /** The base URL the server stores for the selected account platform. */
  endpoint: string
}

/** One row of the rich provider, key and group pickers. */
export interface ProviderPickerOption {
  id: string
  name: string
  detail?: string
  badge?: string
  badgeTone?: 'honey' | 'success' | 'neutral' | 'warning'
  /** Unavailable rows are shown but cannot be chosen. Defaults to true. */
  available?: boolean
}

/** Mirrors the backend's providerAccountBaseURL. */
export function providerAccountEndpoint(origin: string, platform: AccountPlatform | string): string {
  const base = origin.trim().replace(/\/+$/, '')
  return platform === 'grok' || platform === 'custom' ? `${base}/v1` : base
}

export function formatProviderMultiplier(value: number | null | undefined): string {
  if (typeof value !== 'number' || !Number.isFinite(value)) return '—'
  return `${Number(value.toFixed(4))}×`
}

/** Formats a sync time as MM/DD HH:mm in the viewer's time zone. */
export function formatProviderSyncedAt(value?: string): string {
  if (!value) return ''
  const time = new Date(value)
  if (Number.isNaN(time.getTime())) return ''
  const pad = (part: number) => String(part).padStart(2, '0')
  return `${pad(time.getMonth() + 1)}/${pad(time.getDate())} ${pad(time.getHours())}:${pad(time.getMinutes())}`
}

const platformLabels: Record<string, string> = {
  anthropic: 'Anthropic',
  openai: 'OpenAI',
  gemini: 'Gemini',
  antigravity: 'Antigravity',
  grok: 'Grok',
  custom: 'Custom',
  composite: 'Custom'
}

export function providerPlatformLabel(platform?: string): string {
  if (!platform) return ''
  return platformLabels[platform.toLowerCase()] ?? platform
}

export function suggestedProviderAccountName(selection: ProviderAccountSelection): string {
  return `${selection.providerName} · ${selection.keyName}`.slice(0, 100)
}

interface ProviderRequestError {
  status?: unknown
  reason?: unknown
  message?: unknown
}

export function providerErrorReason(error: unknown): string {
  const reason = (error as ProviderRequestError | null)?.reason
  return typeof reason === 'string' ? reason : ''
}

export function providerErrorStatus(error: unknown): number {
  const status = (error as ProviderRequestError | null)?.status
  return typeof status === 'number' ? status : 0
}

export function providerErrorText(error: unknown, fallback: string): string {
  const message = (error as ProviderRequestError | null)?.message
  return typeof message === 'string' && message.trim() ? message.trim().slice(0, 300) : fallback
}

const localizedReasons = new Set([
  'PROVIDER_NOT_FOUND',
  'PROVIDER_KEY_NOT_FOUND',
  'PROVIDER_KEY_UNAVAILABLE',
  'PROVIDER_KEY_SECRET_UNAVAILABLE',
  'PROVIDER_LOGIN_UNAVAILABLE'
])

/**
 * Describes a provider-import failure reported by the server. Any other error
 * yields an empty string, so callers keep their existing fallback message.
 */
export function providerImportErrorMessage(error: unknown, translate: (key: string) => string): string {
  const reason = providerErrorReason(error)
  if (!reason.startsWith('PROVIDER_')) return ''
  return localizedReasons.has(reason)
    ? translate(`admin.providerImport.errors.${reason}`)
    : providerErrorText(error, '')
}
