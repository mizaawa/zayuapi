import { apiClient, buildGatewayUrl } from './client'

export type ImageQuality = 'auto' | 'low' | 'medium' | 'high'
export type ImageTaskStatus = 'processing' | 'completed' | 'failed'

export interface ImageModel {
  id: string
  display_name?: string
}

export interface ImageWorkbenchMetadata {
  api_key_id: number
  prompt: string
  model: string
  size: string
  quality: ImageQuality
  count: number
}

export interface ImageTask {
  id: string
  task_id: string
  status: ImageTaskStatus
  created_at: number
  completed_at?: number
  expires_at: number
  workbench?: ImageWorkbenchMetadata
  image_url?: string
  result?: { data: Array<{ url: string; size_bytes?: number; revised_prompt?: string }> }
  error?: { message?: string; code?: string; type?: string } | string
}

export interface WorkbenchTasksResponse {
  enabled: boolean
  max_concurrent: number
  user_max_concurrent?: number
  admin_exempt?: boolean
  retention_seconds: number
  tutorial_url?: string
  tasks: ImageTask[]
}

export interface SubmitImageTaskPayload {
  prompt: string
  model: string
  quality: ImageQuality
  size: string
  n: number
  images?: Array<{ image_url: string }>
}

export class ImageWorkbenchError extends Error {
  constructor(
    message: string,
    public readonly status: number,
    public readonly code?: string | number,
    public readonly requestId?: string,
  ) {
    super(message)
    this.name = 'ImageWorkbenchError'
  }
}

function record(value: unknown): Record<string, unknown> {
  return typeof value === 'object' && value !== null ? value as Record<string, unknown> : {}
}

function redactKey(value: string, apiKey: string): string {
  return apiKey ? value.split(apiKey).join('[redacted]') : value
}

async function responseError(response: Response, apiKey: string): Promise<ImageWorkbenchError> {
  let body: Record<string, unknown> = {}
  try {
    body = record(await response.json())
  } catch {
    // Proxies can return an HTML error page instead of a JSON error.
  }
  const detail = record(body.error)
  const rawMessage = detail.message ?? body.message
  const message = typeof rawMessage === 'string' && rawMessage.trim()
    ? rawMessage
    : response.statusText || `HTTP ${response.status}`
  const rawCode = detail.code ?? detail.type ?? body.code
  const code = typeof rawCode === 'string' ? redactKey(rawCode, apiKey)
    : typeof rawCode === 'number' ? rawCode : response.status
  return new ImageWorkbenchError(
    redactKey(message, apiKey), response.status, code,
    redactKey(response.headers.get('X-Request-Id') || '', apiKey),
  )
}

async function gatewayRequest<T>(
  path: string,
  apiKey: string,
  email: string,
  init: RequestInit = {},
  signal?: AbortSignal,
): Promise<T> {
  const controller = new AbortController()
  const abort = () => controller.abort()
  if (signal?.aborted) throw new DOMException('Request canceled', 'AbortError')
  signal?.addEventListener('abort', abort, { once: true })
  let timedOut = false
  const timer = setTimeout(() => {
    timedOut = true
    controller.abort()
  }, 30_000)
  try {
    const response = await fetch(buildGatewayUrl(path), {
      ...init,
      headers: {
        Authorization: `Bearer ${apiKey}`,
        'X-Sub2API-User-Email': email,
        ...init.headers,
      },
      signal: controller.signal,
      cache: 'no-store',
    })
    if (!response.ok) throw await responseError(response, apiKey)
    const body: T = await response.json()
    if (signal?.aborted) throw new DOMException('Request canceled', 'AbortError')
    return body
  } catch (error) {
    if (signal?.aborted) throw new DOMException('Request canceled', 'AbortError')
    if (timedOut) throw new ImageWorkbenchError('Request timed out', 0, 'ETIMEDOUT')
    if (error instanceof ImageWorkbenchError) throw error
    if (error instanceof SyntaxError) throw new ImageWorkbenchError('Invalid server response', 0, 'INVALID_RESPONSE')
    const message = error instanceof Error ? error.message : 'Network request failed'
    throw new ImageWorkbenchError(redactKey(message, apiKey), 0, 'ERR_NETWORK')
  } finally {
    clearTimeout(timer)
    signal?.removeEventListener('abort', abort)
  }
}

export async function listWorkbenchTasks(signal?: AbortSignal): Promise<WorkbenchTasksResponse> {
  const { data } = await apiClient.get<WorkbenchTasksResponse>('/image-workbench/tasks', { signal })
  return data
}

export async function deleteWorkbenchTask(taskId: string): Promise<void> {
  await apiClient.delete(`/image-workbench/tasks/${encodeURIComponent(taskId)}`)
}

export async function downloadWorkbenchImage(taskId: string, imageIndex: number, signal?: AbortSignal): Promise<Blob> {
  const { data } = await apiClient.get<Blob>(`/image-workbench/tasks/${encodeURIComponent(taskId)}/images/${imageIndex}`, {
    responseType: 'blob', signal,
  })
  return data
}

export async function listImageModels(apiKey: string, email: string, signal?: AbortSignal): Promise<ImageModel[]> {
  const response = await gatewayRequest<{ data?: unknown }>('/v1/models', apiKey, email, {}, signal)
  if (!Array.isArray(response?.data)) {
    throw new ImageWorkbenchError('Invalid model list response', 0, 'INVALID_RESPONSE')
  }
  const models = new Map<string, ImageModel>()
  for (const value of response.data) {
    const model = record(value)
    if (typeof model.id !== 'string') continue
    const id = model.id.trim()
    const normalized = id.toLowerCase()
    // Match the image families accepted by the gateway's Images endpoint.
    if (!normalized.startsWith('gpt-image-') && normalized !== 'grok-imagine'
      && normalized !== 'grok-imagine-edit' && !normalized.startsWith('grok-imagine-image')) continue
    const displayName = model.display_name ?? model.displayName
    if (!models.has(id)) models.set(id, {
      id,
      ...(typeof displayName === 'string' && displayName.trim() ? { display_name: displayName.trim() } : {}),
    })
  }
  return [...models.values()]
}

export function submitImageTask(
  apiKey: string,
  email: string,
  payload: SubmitImageTaskPayload,
  signal?: AbortSignal,
): Promise<ImageTask> {
  const endpoint = payload.images?.length ? 'edits' : 'generations'
  return gatewayRequest<ImageTask>(`/v1/images/${endpoint}/async`, apiKey, email, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      'X-Sub2API-Image-Workbench': 'true',
    },
    body: JSON.stringify(payload),
  }, signal)
}
