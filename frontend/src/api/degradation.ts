/**
 * User-facing degradation check (降智检测) API. Read-only.
 */

import { apiClient } from './client'
import type { DegradationEffort, DegradationProtocol, DegradationRunStatus } from './admin/degradation'

export type { DegradationEffort, DegradationProtocol, DegradationRunStatus } from './admin/degradation'

export interface DegradationRun {
  id: number
  monitor_id: number
  status: DegradationRunStatus
  trigger_source: 'schedule' | 'manual' | string
  model: string
  response_model: string
  reasoning_effort: DegradationEffort | ''
  protocol: DegradationProtocol | ''
  prompt_name: string
  /** Present on detail reads only. */
  prompt?: string
  /** The generated document. Present on detail reads only. */
  output?: string
  output_bytes: number
  error_message: string
  attempts: number
  first_token_ms: number | null
  duration_ms: number | null
  input_tokens: number
  output_tokens: number
  cache_read_tokens: number
  cache_creation_tokens: number
  reasoning_tokens: number
  total_tokens: number
  cache_rate: number | null
  cost: number | null
  usage_synced: boolean
  started_at: string
  finished_at: string | null
}

export interface DegradationUserMonitor {
  id: number
  name: string
  group_name: string
  platform: string
  model: string
  protocol: DegradationProtocol
  reasoning_effort: DegradationEffort
  interval_minutes: number
  last_run_at: string | null
  runs: DegradationRun[]
}

export interface DegradationOverview {
  enabled: boolean
  display_limit: number
  monitors: DegradationUserMonitor[]
}

export async function getOverview(options?: { signal?: AbortSignal }): Promise<DegradationOverview> {
  const { data } = await apiClient.get<DegradationOverview>('/degradation/overview', { signal: options?.signal })
  return data
}

export async function getRun(id: number, options?: { signal?: AbortSignal }): Promise<DegradationRun> {
  const { data } = await apiClient.get<DegradationRun>(`/degradation/runs/${id}`, { signal: options?.signal })
  return data
}

export const degradationAPI = { getOverview, getRun }

export default degradationAPI
