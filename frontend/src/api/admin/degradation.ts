/**
 * Admin degradation monitor (降智监控) API.
 * Detection groups, prompt presets and the global configuration.
 */

import { apiClient } from '../client'
import type { GroupPlatform } from '@/types'
import type { DegradationRun } from '../degradation'

export type DegradationProtocol = 'openai_chat' | 'openai_responses' | 'anthropic'
export type DegradationEffort = 'none' | 'minimal' | 'low' | 'medium' | 'high' | 'xhigh' | 'max'
export type DegradationRunStatus = 'running' | 'success' | 'failed'

export interface DegradationMonitor {
  id: number
  name: string
  group_id: number
  group_name: string
  group_platform: GroupPlatform | ''
  group_status: string
  group_deleted: boolean
  model: string
  /** Empty string follows the global default effort. */
  reasoning_effort: DegradationEffort | ''
  effective_reasoning_effort: DegradationEffort
  protocol: DegradationProtocol
  /** 0 follows the global default interval. */
  interval_minutes: number
  effective_interval_minutes: number
  prompt_id: number | null
  prompt_name: string
  random_prompt: boolean
  enabled: boolean
  sort_order: number
  running?: boolean
  last_run_at: string | null
  last_result_status?: DegradationRunStatus | ''
  last_result_at?: string | null
  last_result_error?: string
  stored_runs?: number
  stored_bytes?: number
  created_at: string
  updated_at: string
}

export interface DegradationConfig {
  default_interval_minutes: number
  timeout_minutes: number
  max_retries: number
  max_display_per_group: number
  default_reasoning_effort: DegradationEffort
  default_prompt_id: number
}

export interface DegradationPrompt {
  id: number
  name: string
  prompt: string
  builtin_key: string
  sort_order: number
  created_at: string
  updated_at: string
}

export interface DegradationMonitorPayload {
  name: string
  group_id: number
  model: string
  reasoning_effort: DegradationEffort | ''
  protocol: DegradationProtocol
  interval_minutes: number
  prompt_id: number | null
  random_prompt: boolean
  enabled: boolean
}

export interface DegradationMonitorList {
  items: DegradationMonitor[]
  config: DegradationConfig
  enabled: boolean
}

export interface DegradationStorageStats {
  count: number
  bytes: number
}

export async function listMonitors(options?: { signal?: AbortSignal }): Promise<DegradationMonitorList> {
  const { data } = await apiClient.get<DegradationMonitorList>('/admin/degradation/monitors', { signal: options?.signal })
  return data
}

export async function createMonitor(payload: DegradationMonitorPayload): Promise<DegradationMonitor> {
  const { data } = await apiClient.post<DegradationMonitor>('/admin/degradation/monitors', payload)
  return data
}

export async function updateMonitor(id: number, payload: DegradationMonitorPayload): Promise<DegradationMonitor> {
  const { data } = await apiClient.put<DegradationMonitor>(`/admin/degradation/monitors/${id}`, payload)
  return data
}

export async function deleteMonitor(id: number): Promise<void> {
  await apiClient.delete(`/admin/degradation/monitors/${id}`)
}

export async function updateSortOrder(updates: { id: number; sort_order: number }[]): Promise<void> {
  await apiClient.put('/admin/degradation/monitors/sort-order', { updates })
}

export async function runMonitor(id: number): Promise<void> {
  await apiClient.post(`/admin/degradation/monitors/${id}/run`)
}

export async function getStorage(id: number): Promise<DegradationStorageStats> {
  const { data } = await apiClient.get<DegradationStorageStats>(`/admin/degradation/monitors/${id}/storage`)
  return data
}

export async function releaseStorage(id: number): Promise<{ deleted: number }> {
  const { data } = await apiClient.delete<{ deleted: number }>(`/admin/degradation/monitors/${id}/runs`)
  return data
}

export async function getRun(id: number): Promise<DegradationRun> {
  const { data } = await apiClient.get<DegradationRun>(`/admin/degradation/runs/${id}`)
  return data
}

export async function listPrompts(): Promise<DegradationPrompt[]> {
  const { data } = await apiClient.get<{ items: DegradationPrompt[] }>('/admin/degradation/prompts')
  return data.items || []
}

export async function createPrompt(payload: { name: string; prompt: string }): Promise<DegradationPrompt> {
  const { data } = await apiClient.post<DegradationPrompt>('/admin/degradation/prompts', payload)
  return data
}

export async function updatePrompt(id: number, payload: { name: string; prompt: string }): Promise<DegradationPrompt> {
  const { data } = await apiClient.put<DegradationPrompt>(`/admin/degradation/prompts/${id}`, payload)
  return data
}

export async function deletePrompt(id: number): Promise<void> {
  await apiClient.delete(`/admin/degradation/prompts/${id}`)
}

export async function getConfig(): Promise<DegradationConfig> {
  const { data } = await apiClient.get<DegradationConfig>('/admin/degradation/config')
  return data
}

export async function updateConfig(config: DegradationConfig): Promise<DegradationConfig> {
  const { data } = await apiClient.put<DegradationConfig>('/admin/degradation/config', config)
  return data
}

export const degradationAdminAPI = {
  listMonitors,
  createMonitor,
  updateMonitor,
  deleteMonitor,
  updateSortOrder,
  runMonitor,
  getStorage,
  releaseStorage,
  getRun,
  listPrompts,
  createPrompt,
  updatePrompt,
  deletePrompt,
  getConfig,
  updateConfig,
}

export default degradationAdminAPI
