/**
 * User-facing degradation check (降智检测) API. Read-only.
 */

import { apiClient } from './client'
import type { DegradationEffort, DegradationProtocol, DegradationRunStatus } from './admin/degradation'

export type { DegradationEffort, DegradationProtocol, DegradationRunStatus } from './admin/degradation'

/**
 * A finished check as users see it. Checks still generating are never listed,
 * and failure reasons, retries, token usage and cost stay on the admin side.
 */
export interface DegradationRun {
  id: number
  monitor_id: number
  status: DegradationRunStatus
  model: string
  response_model: string
  reasoning_effort: DegradationEffort | ''
  protocol: DegradationProtocol | ''
  prompt_name: string
  /** Present on detail reads only. */
  prompt?: string
  /** The generated document. Present on detail reads of successful checks only. */
  output?: string
  duration_ms: number | null
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
