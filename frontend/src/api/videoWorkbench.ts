import { apiClient } from './client'
import { gatewayRequest, type ImageModel } from './imageWorkbench'

export type VideoTaskStatus = 'queued' | 'processing' | 'completed' | 'failed'
export interface VideoTask {
  id: string
  status: VideoTaskStatus
  created_at: number
  started_at?: number
  completed_at?: number
  expires_at: number
  size_bytes?: number
  content_type?: string
  error_code?: string
  workbench: {
    api_key_id: number
    model: string
    prompt: string
    ratio: string
    duration: number
    reference_images: number
    reference_videos: number
    reference_audios: number
  }
}
export interface VideoWorkbenchSettings {
  enabled: boolean
  max_concurrent: number
  max_reference_images: number
  max_reference_videos: number
  max_reference_audios: number
  retention_minutes: number
  tutorial_url: string
  documentation_url: string
  announcement: string
}
export interface VideoPayload {
  model: string
  prompt: string
  ratio: string
  duration: number
  referenceImages?: string[]
  referenceVideos?: string[]
  referenceAudios?: string[]
}
export function miniMaxVideoModelLimits(model: string) {
  const id = model.trim().toLowerCase()
  if (id === 'minimax-h3' || id.startsWith('minimax-h3-')) {
    return {
      durations: [5, 10, 12, 15], defaultDuration: 5, minDuration: 4, maxDuration: 15,
      fixedDurations: false, images: 9, videos: 3, audios: 3, requiresImage: false,
      ratios: ['21:9', '16:9', '4:3', '1:1', '3:4', '9:16']
    }
  }
  if (['minimax-hailuo-2.3', 'minimax-hailuo-2.3-fast', 'minimax-hailuo-02'].includes(id)) {
    return {
      durations: [6, 10], defaultDuration: 6, minDuration: 6, maxDuration: 10,
      fixedDurations: true, images: 2, videos: 0, audios: 0,
      requiresImage: id === 'minimax-hailuo-2.3-fast', ratios: null
    }
  }
  return null
}
export async function listVideoTasks(signal?: AbortSignal) {
  const { data } = await apiClient.get<{ settings: VideoWorkbenchSettings; tasks: VideoTask[] }>('/video-workbench/tasks', { signal })
  return data
}
export async function listVideoModels(key: string, email: string, signal?: AbortSignal): Promise<ImageModel[]> {
  const result = await gatewayRequest<{ data: ImageModel[] }>('/v1/models', key, email, {}, signal)
  if (!Array.isArray(result.data)) throw new Error('Invalid model response')
  // Custom groups may name video models freely; the group's model list is authoritative.
  return result.data.filter(model => typeof model?.id === 'string' && model.id.trim())
    .filter((model, index, all) => all.findIndex(item => item.id === model.id) === index)
}
export function submitVideoTask(key: string, email: string, payload: VideoPayload) {
  return gatewayRequest<VideoTask>('/v1/video-workbench/tasks', key, email, {
    method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(payload)
  })
}
export async function deleteVideoTask(id: string) {
  await apiClient.delete(`/video-workbench/tasks/${encodeURIComponent(id)}`)
}
export async function downloadVideoTask(id: string, signal?: AbortSignal): Promise<Blob> {
  const { data } = await apiClient.get<Blob>(`/video-workbench/tasks/${encodeURIComponent(id)}/content`, {
    responseType: 'blob', timeout: 0, signal
  })
  return data
}
export function safeWorkbenchLink(value: string | undefined): string {
  if (!value) return ''
  try {
    const url = new URL(value)
    return ['https:', 'http:'].includes(url.protocol) && !url.username && !url.password ? url.href : ''
  } catch { return '' }
}
