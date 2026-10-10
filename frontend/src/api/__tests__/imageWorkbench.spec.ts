import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { deleteWorkbenchTask, downloadWorkbenchImage, listImageModels, listWorkbenchTasks, submitImageTask } from '@/api/imageWorkbench'

const { get, deleteRequest } = vi.hoisted(() => ({ get: vi.fn(), deleteRequest: vi.fn() }))
vi.mock('@/api/client', () => ({
  apiClient: { get, delete: deleteRequest },
  buildGatewayUrl: (path: string) => `https://gateway.example.com${path}`,
}))

const apiKey = 'sk-test-sensitive'
const email = 'owner@example.com'
const payload = { prompt: 'A coastal lighthouse', model: 'gpt-image-2', quality: 'auto' as const, size: '1024x1024', n: 1 }
let fetchMock: ReturnType<typeof vi.fn>

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
}

beforeEach(() => {
  vi.clearAllMocks()
  fetchMock = vi.fn()
  vi.stubGlobal('fetch', fetchMock)
})

afterEach(() => {
  vi.unstubAllGlobals()
  vi.useRealTimers()
})

describe('image workbench API', () => {
  it('recovers tasks through the session client and encodes task IDs when deleting', async () => {
    const result = { enabled: true, max_concurrent: 10, retention_seconds: 900, tasks: [] }
    get.mockResolvedValue({ data: result })
    const signal = new AbortController().signal
    await expect(listWorkbenchTasks(signal)).resolves.toEqual(result)
    expect(get).toHaveBeenCalledWith('/image-workbench/tasks', { signal })
    await deleteWorkbenchTask('task/id?invalid')
    expect(deleteRequest).toHaveBeenCalledWith('/image-workbench/tasks/task%2Fid%3Finvalid')
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it('downloads image bytes through the session client with the task ID encoded and cancellation forwarded', async () => {
    const data = new Blob(['image-bytes'], { type: 'image/png' })
    const signal = new AbortController().signal
    get.mockResolvedValue({ data })
    await expect(downloadWorkbenchImage('task/id?invalid', 2, signal)).resolves.toBe(data)
    expect(get).toHaveBeenCalledWith('/image-workbench/tasks/task%2Fid%3Finvalid/images/2', {
      responseType: 'blob', signal,
    })
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it('preserves an expired or forbidden download error instead of saving its response as an image', async () => {
    const error = { status: 404, message: 'Image task not found' }
    get.mockRejectedValueOnce(error)
    await expect(downloadWorkbenchImage('expired-task', 0)).rejects.toBe(error)
  })

  it('only lists model families executable by the Images gateway, while preserving IDs and labels', async () => {
    fetchMock.mockResolvedValue(jsonResponse({ data: [
      { id: 'gpt-image-2', display_name: 'GPT Image 2' },
      { id: 'gpt-5.4' }, { id: 'dall-e-3' }, { id: 'gemini-3-pro-image-preview' },
      { id: 'grok-imagine-video' }, { id: 'grok-imagine', displayName: 'Grok Imagine' },
      { id: 'grok-imagine-edit' }, { id: 'grok-imagine-image-quality' },
      { id: 'gpt-image-2' }, null, { id: 1 },
    ] }))
    await expect(listImageModels(apiKey, email)).resolves.toEqual([
      { id: 'gpt-image-2', display_name: 'GPT Image 2' },
      { id: 'grok-imagine', display_name: 'Grok Imagine' },
      { id: 'grok-imagine-edit' }, { id: 'grok-imagine-image-quality' },
    ])
    expect(fetchMock).toHaveBeenCalledWith('https://gateway.example.com/v1/models', expect.objectContaining({
      headers: { Authorization: `Bearer ${apiKey}`, 'X-Sub2API-User-Email': email },
      cache: 'no-store',
    }))
  })

  it.each([null, {}, { data: {} }])('rejects a malformed model response %s', async body => {
    fetchMock.mockResolvedValue(jsonResponse(body))
    await expect(listImageModels(apiKey, email)).rejects.toMatchObject({ code: 'INVALID_RESPONSE' })
  })

  it('submits generation requests once with ownership and workbench headers', async () => {
    const task = { id: 'task-1', status: 'processing' }
    fetchMock.mockResolvedValue(jsonResponse(task, 202))
    await expect(submitImageTask(apiKey, email, payload)).resolves.toEqual(task)
    expect(fetchMock).toHaveBeenCalledTimes(1)
    expect(fetchMock).toHaveBeenCalledWith('https://gateway.example.com/v1/images/generations/async', expect.objectContaining({
      method: 'POST',
      headers: {
        Authorization: `Bearer ${apiKey}`,
        'X-Sub2API-User-Email': email,
        'Content-Type': 'application/json',
        'X-Sub2API-Image-Workbench': 'true',
      },
      body: JSON.stringify(payload),
    }))
  })

  it('routes reference image data URLs to the asynchronous edit endpoint', async () => {
    fetchMock.mockResolvedValue(jsonResponse({ id: 'task-2' }, 202))
    const editPayload = { ...payload, images: [{ image_url: 'data:image/png;base64,AAA=' }] }
    await submitImageTask(apiKey, email, editPayload)
    expect(fetchMock).toHaveBeenCalledWith('https://gateway.example.com/v1/images/edits/async', expect.objectContaining({
      body: JSON.stringify(editPayload),
    }))
  })

  it('reports structured upstream failures without reflecting the API key', async () => {
    fetchMock.mockResolvedValue(jsonResponse({ error: { code: 'permission_error', message: `Key ${apiKey} cannot use this model` } }, 403))
    await expect(submitImageTask(apiKey, email, payload)).rejects.toMatchObject({
      status: 403, code: 'permission_error', message: 'Key [redacted] cannot use this model',
    })
    expect(fetchMock).toHaveBeenCalledTimes(1)
  })

  it('handles a non-JSON proxy failure without exposing its body', async () => {
    fetchMock.mockResolvedValue(new Response(`<html>${apiKey}</html>`, { status: 502, statusText: 'Bad Gateway' }))
    await expect(listImageModels(apiKey, email)).rejects.toMatchObject({ status: 502, message: 'Bad Gateway' })
  })

  it('redacts API credentials from network failures', async () => {
    fetchMock.mockRejectedValue(new Error(`Network request rejected: ${apiKey}`))
    await expect(listImageModels(apiKey, email)).rejects.toMatchObject({ message: 'Network request rejected: [redacted]', code: 'ERR_NETWORK' })
  })

  it('does not send a request whose caller already canceled it', async () => {
    const controller = new AbortController()
    controller.abort()
    await expect(listImageModels(apiKey, email, controller.signal)).rejects.toMatchObject({ name: 'AbortError' })
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it('cancels model loading and removes the abort listener', async () => {
    vi.useFakeTimers()
    const controller = new AbortController()
    const removeListener = vi.spyOn(controller.signal, 'removeEventListener')
    fetchMock.mockImplementation((_url, init: RequestInit) => new Promise((_resolve, reject) => {
      init.signal?.addEventListener('abort', () => reject(new DOMException('Aborted', 'AbortError')))
    }))
    const result = listImageModels(apiKey, email, controller.signal)
    const rejected = expect(result).rejects.toMatchObject({ name: 'AbortError' })
    controller.abort()
    await rejected
    expect(removeListener).toHaveBeenCalledWith('abort', expect.any(Function))
    expect(vi.getTimerCount()).toBe(0)
  })

  it('times out a stalled request without automatically submitting a duplicate job', async () => {
    vi.useFakeTimers()
    fetchMock.mockImplementation((_url, init: RequestInit) => new Promise((_resolve, reject) => {
      init.signal?.addEventListener('abort', () => reject(new DOMException('Aborted', 'AbortError')))
    }))
    const result = submitImageTask(apiKey, email, payload)
    const rejected = expect(result).rejects.toMatchObject({ code: 'ETIMEDOUT' })
    await vi.advanceTimersByTimeAsync(30_000)
    await rejected
    expect(fetchMock).toHaveBeenCalledTimes(1)
    expect(vi.getTimerCount()).toBe(0)
  })
})
