/**
 * Administrator resource library API.
 *
 * Lists every retained image/video workbench result (all users), exposes their
 * provenance — creation time, owner email, file name, destruction time, size,
 * group, model, prompt and cost — and preserves selected results in a
 * server-side archive that survives workbench retention. No field is masked:
 * the page exists precisely for administrator review and evidence keeping.
 */

import { apiClient } from '../client'

export type ResourceKind = 'image' | 'video'

export interface ResourceItem {
  kind: ResourceKind
  task_id: string
  index: number
  name: string
  user_id: number
  user_email: string
  api_key_id: number
  group_id: number
  group_name: string
  model: string
  prompt: string
  size_bytes: number
  content_type: string
  created_at: number
  completed_at?: number
  expires_at: number
  billing_id?: string
  cost: number | null
  billing_state?: string
  details?: Record<string, unknown>
  saved_id?: string
}

export interface SavedResource {
  id: string
  /** Absent for archived copies; present only on live results. */
  task_id?: undefined
  kind: ResourceKind
  source_task_id: string
  source_index: number
  name: string
  content_type: string
  size_bytes: number
  sha256: string
  user_id: number
  user_email: string
  api_key_id: number
  group_id: number
  group_name: string
  model: string
  prompt: string
  billing_id?: string
  cost: number | null
  details?: Record<string, unknown>
  generated_at: string
  source_expires_at?: string
  saved_by: number
  saved_by_email: string
  created_at: string
}

export interface ResourceGroupOption {
  id: number
  name: string
}

export interface ResourceListQuery {
  kind?: ResourceKind | ''
  model?: string
  group_id?: number
  q?: string
  page?: number
  page_size?: number
}

export interface ResourceListResponse {
  items: ResourceItem[]
  total: number
  page: number
  page_size: number
  pages: number
  models: string[]
  groups: ResourceGroupOption[]
  truncated: boolean
}

export interface SavedResourceListResponse {
  items: SavedResource[]
  total: number
  page: number
  page_size: number
  pages: number
  models: string[]
  groups: ResourceGroupOption[]
}

function resourceQueryParams(query: ResourceListQuery): Record<string, string | number> {
  const params: Record<string, string | number> = {}
  if (query.kind) params.kind = query.kind
  if (query.model) params.model = query.model
  if (query.group_id) params.group_id = query.group_id
  if (query.q) params.q = query.q
  if (query.page) params.page = query.page
  if (query.page_size) params.page_size = query.page_size
  return params
}

/** List retained results of every user (newest first). */
export async function listItems(query: ResourceListQuery = {}): Promise<ResourceListResponse> {
  const { data } = await apiClient.get<ResourceListResponse>('/admin/resource-library/items', {
    params: resourceQueryParams(query)
  })
  return data
}

/** Fetch one retained result including its full prompt. */
export async function getItem(
  kind: ResourceKind,
  taskId: string,
  index = 0
): Promise<ResourceItem> {
  const { data } = await apiClient.get<ResourceItem>(
    `/admin/resource-library/items/${kind}/${encodeURIComponent(taskId)}`,
    { params: { index } }
  )
  return data
}

/** Fetch a retained result's bytes for inline preview. */
export async function fetchItemContent(
  kind: ResourceKind,
  taskId: string,
  index = 0,
  signal?: AbortSignal
): Promise<Blob> {
  const { data } = await apiClient.get<Blob>(
    `/admin/resource-library/items/${kind}/${encodeURIComponent(taskId)}/content`,
    { params: { index }, responseType: 'blob', timeout: 0, signal }
  )
  return data
}

/** Download a retained result (server sets the attachment disposition). */
export async function downloadItem(kind: ResourceKind, taskId: string, index = 0): Promise<Blob> {
  const { data } = await apiClient.get<Blob>(
    `/admin/resource-library/items/${kind}/${encodeURIComponent(taskId)}/content`,
    { params: { index, download: 1 }, responseType: 'blob', timeout: 0 }
  )
  return data
}

/** Delete a retained result of any user from the workbench and its storage. */
export async function deleteItem(kind: ResourceKind, taskId: string): Promise<void> {
  await apiClient.delete(`/admin/resource-library/items/${kind}/${encodeURIComponent(taskId)}`)
}

/** Preserve a retained result in the archive. */
export async function saveItem(
  kind: ResourceKind,
  taskId: string,
  index = 0
): Promise<{ item: SavedResource; created: boolean }> {
  const { data } = await apiClient.post<{ item: SavedResource; created: boolean }>(
    `/admin/resource-library/items/${kind}/${encodeURIComponent(taskId)}/save`,
    { index }
  )
  return data
}

/** List preserved copies (newest first). */
export async function listSaved(query: ResourceListQuery = {}): Promise<SavedResourceListResponse> {
  const { data } = await apiClient.get<SavedResourceListResponse>('/admin/resource-library/saved', {
    params: resourceQueryParams(query)
  })
  return data
}

/** Fetch one preserved copy's metadata. */
export async function getSaved(id: string): Promise<SavedResource> {
  const { data } = await apiClient.get<SavedResource>(
    `/admin/resource-library/saved/${encodeURIComponent(id)}`
  )
  return data
}

/** Fetch a preserved copy's bytes for preview. */
export async function fetchSavedContent(id: string, signal?: AbortSignal): Promise<Blob> {
  const { data } = await apiClient.get<Blob>(
    `/admin/resource-library/saved/${encodeURIComponent(id)}/content`,
    { responseType: 'blob', timeout: 0, signal }
  )
  return data
}

/** Download a preserved copy (server sets the attachment disposition). */
export async function downloadSaved(id: string): Promise<Blob> {
  const { data } = await apiClient.get<Blob>(
    `/admin/resource-library/saved/${encodeURIComponent(id)}/content`,
    { params: { download: 1 }, responseType: 'blob', timeout: 0 }
  )
  return data
}

/** Permanently remove a preserved copy and its file. */
export async function deleteSaved(id: string): Promise<void> {
  await apiClient.delete(`/admin/resource-library/saved/${encodeURIComponent(id)}`)
}

export const resourceLibraryAPI = {
  listItems,
  getItem,
  fetchItemContent,
  downloadItem,
  deleteItem,
  saveItem,
  listSaved,
  getSaved,
  fetchSavedContent,
  downloadSaved,
  deleteSaved
}

export default resourceLibraryAPI