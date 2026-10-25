/**
 * Admin Grok/xAI API endpoints
 * Handles runtime quota maintenance for existing xAI accounts.
 */

import { apiClient } from '../client'
import type { GrokBillingSummary, GrokQuotaWindow, WindowStats } from '@/types'

export type { GrokBillingSummary, GrokQuotaWindow } from '@/types'

export interface GrokQuotaSnapshot {
  requests?: GrokQuotaWindow | null
  tokens?: GrokQuotaWindow | null
  retry_after_seconds?: number | null
  subscription_tier?: string
  entitlement_status?: string
  status_code?: number
  headers?: Record<string, string>
  headers_observed: boolean
  observation_source?: string
  last_probe_at?: string
  last_headers_seen_at?: string
  updated_at: string
}

export interface GrokQuotaProbeResult {
  source: 'active_probe' | 'billing_probe' | 'hybrid_probe'
  model?: string
  billing?: GrokBillingSummary | null
  snapshot?: GrokQuotaSnapshot | null
  local_usage_24h?: WindowStats | null
  local_usage_7d?: WindowStats | null
  local_usage_monthly?: WindowStats | null
  status_code?: number
  headers_observed: boolean
  reset_supported: boolean
  fetched_at: number
  persisted?: boolean
  probe_error?: string
}

export interface GrokQuotaResetResult {
  supported: boolean
  code: string
  message: string
}

export async function queryQuota(id: number): Promise<GrokQuotaProbeResult> {
  const { data } = await apiClient.get<GrokQuotaProbeResult>(`/admin/grok/accounts/${id}/quota`)
  return data
}

export async function resetQuota(id: number): Promise<GrokQuotaResetResult> {
  const { data } = await apiClient.post<GrokQuotaResetResult>(`/admin/grok/accounts/${id}/reset-quota`)
  return data
}

export default { queryQuota, resetQuota }
