import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import {
  DEGRADATION_POSTER_HEIGHT,
  DEGRADATION_POSTER_MESSAGE,
  DEGRADATION_POSTER_SETTLE_MS,
  DEGRADATION_POSTER_WIDTH,
  buildDegradationPosterDocument,
  createDegradationPosterQueue,
  createDegradationPosterToken,
  degradationPosterKey,
  degradationPosterMiss,
  degradationPosterRuntime,
  degradationPosterSnapshotsEnabled,
  findDegradationPoster,
  normalizeDegradationPoster,
  noteDegradationPosterFailure,
  peekDegradationPoster,
  readDegradationPosterMessage,
  rememberDegradationPosterMiss,
  resetDegradationPosterState,
  saveDegradationPoster,
} from '@/utils/degradationPoster'

function message(data: unknown, source: unknown): MessageEvent {
  return { data, source } as unknown as MessageEvent
}

describe('degradation poster queue', () => {
  it('runs at most the limit at once, in request order', async () => {
    const queue = createDegradationPosterQueue(2)
    const order: string[] = []
    const first = queue.acquire().then((release) => { order.push('a'); return release })
    const second = queue.acquire().then((release) => { order.push('b'); return release })
    const third = queue.acquire().then((release) => { order.push('c'); return release })
    const releaseA = await first
    await second
    expect(order).toEqual(['a', 'b'])
    expect(queue.active).toBe(2)
    expect(queue.waiting).toBe(1)

    releaseA()
    releaseA()
    const releaseC = await third
    expect(order).toEqual(['a', 'b', 'c'])
    expect(queue.active).toBe(2)
    releaseC()
    expect(queue.active).toBe(1)
  })

  it('holds new work while paused', async () => {
    const queue = createDegradationPosterQueue(1)
    queue.pause()
    let granted = false
    const pending = queue.acquire().then((release) => { granted = true; return release })
    await Promise.resolve()
    expect(granted).toBe(false)
    queue.resume()
    const release = await pending
    expect(granted).toBe(true)
    release()
  })

  it('drops a waiter whose signal aborts before its turn', async () => {
    const queue = createDegradationPosterQueue(1)
    const release = await queue.acquire()
    const controller = new AbortController()
    const waiting = queue.acquire(controller.signal)
    expect(queue.waiting).toBe(1)
    controller.abort()
    await expect(waiting).rejects.toMatchObject({ name: 'AbortError' })
    expect(queue.waiting).toBe(0)
    release()
    expect(queue.active).toBe(0)
    await expect(queue.acquire(controller.signal)).rejects.toMatchObject({ name: 'AbortError' })
  })
})

describe('readDegradationPosterMessage', () => {
  const frame = {} as Window
  const valid = { type: DEGRADATION_POSTER_MESSAGE, token: 'tok', ok: true, buffer: new ArrayBuffer(16) }

  it('accepts only the expected frame and token', () => {
    expect(readDegradationPosterMessage(message(valid, frame), frame, 'tok')).toEqual({ ok: true, buffer: valid.buffer })
    expect(readDegradationPosterMessage(message(valid, {}), frame, 'tok')).toBeNull()
    expect(readDegradationPosterMessage(message(valid, frame), frame, 'other')).toBeNull()
    expect(readDegradationPosterMessage(message(valid, frame), null, 'tok')).toBeNull()
    expect(readDegradationPosterMessage(message({ ...valid, type: 'sub2api-degradation-preview' }, frame), frame, 'tok')).toBeNull()
    expect(readDegradationPosterMessage(message('text', frame), frame, 'tok')).toBeNull()
  })

  it('treats anything but a sensible image buffer as a failure', () => {
    expect(readDegradationPosterMessage(message({ ...valid, buffer: new ArrayBuffer(0) }, frame), frame, 'tok')).toEqual({ ok: false, reason: 'invalid' })
    expect(readDegradationPosterMessage(message({ ...valid, buffer: new ArrayBuffer(5 * 1024 * 1024) }, frame), frame, 'tok')).toEqual({ ok: false, reason: 'invalid' })
    expect(readDegradationPosterMessage(message({ ...valid, buffer: 'not-binary' }, frame), frame, 'tok')).toEqual({ ok: false, reason: 'invalid' })
    const failed = readDegradationPosterMessage(message({ type: DEGRADATION_POSTER_MESSAGE, token: 'tok', ok: false, reason: 'security'.repeat(10) }, frame), frame, 'tok')
    expect(failed).toEqual({ ok: false, reason: 'security'.repeat(10).slice(0, 24) })
  })
})

describe('poster documents', () => {
  it('runs the poster script inside the sandbox policy, before the generated markup', () => {
    const doc = buildDegradationPosterDocument('<svg id="pelican"></svg>', { token: 'abc123', snapshot: true })
    const policyAt = doc.indexOf("default-src 'none'")
    const scriptAt = doc.indexOf('<script>')
    expect(policyAt).toBeGreaterThan(0)
    expect(scriptAt).toBeGreaterThan(policyAt)
    expect(scriptAt).toBeLessThan(doc.indexOf('<svg id="pelican"></svg>'))
    const script = doc.slice(scriptAt + '<script>'.length, doc.indexOf('</script>'))
    expect(script.toLowerCase()).not.toContain('</script')
    expect(script).toContain('"token":"abc123"')
    expect(script).toContain('"snapshot":true')
    expect(script).toContain(`"settle":${DEGRADATION_POSTER_SETTLE_MS}`)
    expect(() => new Function(script)).not.toThrow()
  })

  it('creates unpredictable tokens', () => {
    const token = createDegradationPosterToken()
    expect(token).toMatch(/^[0-9a-f]{24}$/)
    expect(createDegradationPosterToken()).not.toBe(token)
  })
})

/** A minimal browser for the in-frame script, so its control flow runs without a real engine. */
function fakeFrame(options: { uniform?: boolean } = {}) {
  const timers: Array<() => void> = []
  const posted: Array<{ payload: Record<string, unknown>; transfer: unknown[] }> = []
  const element = () => ({
    setAttribute: vi.fn(),
    removeAttribute: vi.fn(),
    getAttribute: vi.fn(() => null),
    querySelectorAll: vi.fn(() => []),
    querySelector: vi.fn(() => null),
    appendChild: vi.fn(),
    style: { setProperty: vi.fn() },
  })
  const copy = element()
  const root = { ...element(), cloneNode: vi.fn(() => copy) }
  const context = {
    fillStyle: '',
    fillRect: vi.fn(),
    drawImage: vi.fn(),
    getImageData: vi.fn((_x: number, _y: number, width: number, height: number) => {
      const data = new Uint8ClampedArray(width * height * 4)
      // Uniform pixels are what an engine that skipped the copy leaves behind.
      for (let index = 0; index < data.length; index += 1) data[index] = options.uniform ? 255 : (index * 7) % 256
      return { data }
    }),
  }
  const buffer = new ArrayBuffer(32)
  const canvas = {
    width: 0,
    height: 0,
    getContext: vi.fn(() => context),
    toBlob: vi.fn((callback: (blob: unknown) => void) => callback({ arrayBuffer: () => Promise.resolve(buffer) })),
  }
  const doc = {
    documentElement: root,
    body: element(),
    getAnimations: () => [],
    querySelectorAll: vi.fn(() => []),
    createElement: vi.fn((tag: string) => (tag === 'canvas' ? canvas : { ...element(), textContent: '' })),
  }
  class FakeImage {
    onload: (() => void) | null = null
    onerror: (() => void) | null = null
    set src(_value: string) {
      queueMicrotask(() => this.onload?.())
    }
  }
  let timerId = 0
  const win: Record<string, unknown> = {
    setTimeout: (callback: () => void) => { timers.push(callback); return (timerId += 1) },
    clearTimeout: vi.fn(),
    clearInterval: vi.fn(),
    requestAnimationFrame: () => 7,
    cancelAnimationFrame: vi.fn(),
    XMLSerializer: class { serializeToString() { return '<html xmlns="http://www.w3.org/1999/xhtml"></html>' } },
    Image: FakeImage,
    getComputedStyle: () => ({ backgroundImage: 'none', backgroundColor: 'rgb(255, 250, 230)', getPropertyValue: () => '' }),
    parent: { postMessage: (payload: Record<string, unknown>, _origin: string, transfer: unknown[]) => posted.push({ payload, transfer }) },
  }
  return {
    win,
    doc,
    canvas,
    context,
    buffer,
    posted,
    run(snapshot: boolean) {
      const script = degradationPosterRuntime({ token: 'tok', snapshot })
      new Function('window', 'document', script)(win, doc)
      timers.shift()?.()
    },
  }
}

describe('poster runtime', () => {
  it('freezes the frame and reports when painting is not attempted', () => {
    const frame = fakeFrame()
    frame.run(false)
    expect(frame.posted).toEqual([{ payload: { ok: false, reason: 'skipped', type: DEGRADATION_POSTER_MESSAGE, token: 'tok' }, transfer: [] }])
    const win = frame.win as { setTimeout: () => number; requestAnimationFrame: () => number }
    expect(win.setTimeout()).toBe(0)
    expect(win.requestAnimationFrame()).toBe(0)
    expect(frame.win.clearTimeout).toHaveBeenCalled()
    expect(frame.win.cancelAnimationFrame).toHaveBeenCalled()
  })

  it('paints the frozen copy and transfers the pixels', async () => {
    const frame = fakeFrame()
    frame.run(true)
    await vi.waitFor(() => expect(frame.posted).toHaveLength(1))
    expect(frame.posted[0].payload).toMatchObject({ ok: true, type: DEGRADATION_POSTER_MESSAGE, token: 'tok', buffer: frame.buffer })
    expect(frame.posted[0].transfer).toEqual([frame.buffer])
    expect(frame.canvas.width).toBe(DEGRADATION_POSTER_WIDTH)
    expect(frame.canvas.height).toBe(DEGRADATION_POSTER_HEIGHT)
    expect(frame.context.fillStyle).toBe('rgb(255, 250, 230)')
    expect(frame.context.drawImage).toHaveBeenCalledTimes(1)
  })

  it('reports a blank paint instead of posting an empty image', async () => {
    const frame = fakeFrame({ uniform: true })
    frame.run(true)
    await vi.waitFor(() => expect(frame.posted).toHaveLength(1))
    expect(frame.posted[0].payload).toMatchObject({ ok: false, reason: 'blank' })
    expect(frame.canvas.toBlob).not.toHaveBeenCalled()
  })
})

describe('normalizeDegradationPoster', () => {
  const originalBitmap = globalThis.createImageBitmap
  const originalGetContext = HTMLCanvasElement.prototype.getContext
  const originalToBlob = HTMLCanvasElement.prototype.toBlob
  let drawImage: ReturnType<typeof vi.fn>

  beforeEach(() => {
    drawImage = vi.fn()
    HTMLCanvasElement.prototype.getContext = vi.fn(() => ({ drawImage })) as unknown as typeof HTMLCanvasElement.prototype.getContext
    HTMLCanvasElement.prototype.toBlob = function toBlob(callback: BlobCallback) {
      callback(new Blob(['poster'], { type: 'image/webp' }))
    }
  })

  afterEach(() => {
    globalThis.createImageBitmap = originalBitmap
    HTMLCanvasElement.prototype.getContext = originalGetContext
    HTMLCanvasElement.prototype.toBlob = originalToBlob
  })

  it('re-encodes decoded pixels at the poster size', async () => {
    const bitmap = { width: 800, height: 600, close: vi.fn() }
    globalThis.createImageBitmap = vi.fn(async () => bitmap) as unknown as typeof createImageBitmap
    const blob = await normalizeDegradationPoster(new ArrayBuffer(64))
    expect(blob).toBeInstanceOf(Blob)
    expect(drawImage).toHaveBeenCalledWith(bitmap, 0, 0, DEGRADATION_POSTER_WIDTH, DEGRADATION_POSTER_HEIGHT)
    expect(bitmap.close).toHaveBeenCalled()
  })

  it('rejects bytes that do not decode as an image of sensible size', async () => {
    globalThis.createImageBitmap = vi.fn(async () => { throw new Error('not an image') }) as unknown as typeof createImageBitmap
    expect(await normalizeDegradationPoster(new ArrayBuffer(64))).toBeNull()

    const huge = { width: 5000, height: 10, close: vi.fn() }
    globalThis.createImageBitmap = vi.fn(async () => huge) as unknown as typeof createImageBitmap
    expect(await normalizeDegradationPoster(new ArrayBuffer(64))).toBeNull()
    expect(huge.close).toHaveBeenCalled()
    expect(drawImage).not.toHaveBeenCalled()

    expect(await normalizeDegradationPoster(new ArrayBuffer(0))).toBeNull()
  })
})

describe('poster cache', () => {
  const originalCreate = URL.createObjectURL
  let counter = 0

  beforeEach(() => {
    resetDegradationPosterState()
    counter = 0
    URL.createObjectURL = vi.fn(() => `blob:poster-${(counter += 1)}`)
  })

  afterEach(() => {
    URL.createObjectURL = originalCreate
    resetDegradationPosterState()
  })

  it('keys posters by result and finish time', () => {
    expect(degradationPosterKey({ id: 7, finished_at: '2026-11-03T08:01:00Z' })).toBe('v1:7:2026-11-03T08:01:00Z')
    expect(degradationPosterKey({ id: 7, finished_at: null })).toBe('v1:7:')
  })

  it('reuses a saved poster for the rest of the session', async () => {
    const key = degradationPosterKey({ id: 1, finished_at: 'x' })
    expect(peekDegradationPoster(key)).toBeNull()
    expect(await findDegradationPoster(key)).toBeNull()
    const url = saveDegradationPoster(key, new Blob(['a']))
    expect(url).toBe('blob:poster-1')
    expect(saveDegradationPoster(key, new Blob(['b']))).toBe(url)
    expect(peekDegradationPoster(key)).toBe(url)
    expect(await findDegradationPoster(key)).toBe(url)
    expect(URL.createObjectURL).toHaveBeenCalledTimes(1)
  })

  it('remembers results that cannot become posters', () => {
    rememberDegradationPosterMiss('v1:2:x', 'empty')
    expect(degradationPosterMiss('v1:2:x')).toBe('empty')
    expect(degradationPosterMiss('v1:3:x')).toBeUndefined()
  })

  it('stops painting after an engine refuses twice', () => {
    expect(degradationPosterSnapshotsEnabled()).toBe(true)
    noteDegradationPosterFailure('blank')
    noteDegradationPosterFailure('security')
    expect(degradationPosterSnapshotsEnabled()).toBe(true)
    noteDegradationPosterFailure('security')
    expect(degradationPosterSnapshotsEnabled()).toBe(false)
  })
})
