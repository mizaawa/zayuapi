import type { DegradationRun } from '@/api/degradation'
import {
  DEGRADATION_PREVIEW_HEIGHT,
  DEGRADATION_PREVIEW_WIDTH,
  buildDegradationPreviewDocument,
} from './degradationPreview'

/**
 * Gallery cards show still posters, not live animations. A generated document
 * renders once inside the sandboxed preview frame, runs for a moment, freezes
 * every animation and paints its current frame into an image that it posts
 * back. This page decodes and re-encodes those pixels itself and caches the
 * result, so a gallery never keeps dozens of animations running and never
 * renders a finished result twice. Only the detail dialog plays the animation.
 */

export const DEGRADATION_POSTER_MESSAGE = 'sub2api-degradation-poster'
export const DEGRADATION_POSTER_WIDTH = 800
export const DEGRADATION_POSTER_HEIGHT = 600
/** How long a document animates before its frame is captured. */
export const DEGRADATION_POSTER_SETTLE_MS = 1200
/** A frame that has not reported back by then is removed. */
export const DEGRADATION_POSTER_TIMEOUT_MS = 9000

const MAX_POSTER_BYTES = 4 * 1024 * 1024
const MAX_POSTER_EDGE = 2048
const POSTER_QUALITY = 0.85
const CACHE_VERSION = 'v1'
const MEMORY_LIMIT = 400
const STORE_LIMIT = 300
const DB_NAME = 'sub2api-degradation'
const STORE_NAME = 'posters'

/**
 * Runs inside the sandboxed frame next to the generated code, so it is plain
 * script kept as text and depends on nothing bundled with this page. It never
 * contains a closing script tag.
 */
const POSTER_RUNTIME = String.raw`(function (config) {
  'use strict';
  var win = window;
  var doc = document;
  var setTimer = win.setTimeout.bind(win);
  var clearTimer = win.clearTimeout.bind(win);
  var clearRepeat = win.clearInterval.bind(win);
  var requestFrame = win.requestAnimationFrame ? win.requestAnimationFrame.bind(win) : null;
  var cancelFrame = win.cancelAnimationFrame ? win.cancelAnimationFrame.bind(win) : null;
  var Serializer = win.XMLSerializer;
  var ImageElement = win.Image;
  var parentWindow = win.parent;
  var SMIL = 'animate, animateTransform, animateMotion, animateColor, set';
  var SKIP = { offset: 1, computedOffset: 1, easing: 1, composite: 1 };
  var BOX = ['display', 'position', 'top', 'right', 'bottom', 'left', 'width', 'height', 'margin-top', 'margin-right',
    'margin-bottom', 'margin-left', 'transform', 'transform-origin', 'border-radius', 'z-index', 'float',
    'vertical-align', 'opacity', 'filter', 'box-shadow', 'object-fit', 'image-rendering'];
  var BACKGROUND = ['background-color', 'background-image', 'background-size', 'background-position',
    'background-repeat', 'background-origin', 'background-clip'];
  var reported = false;

  function each(list, fn) { Array.prototype.forEach.call(list || [], fn); }

  function report(payload, transfer) {
    if (reported) return;
    reported = true;
    payload.type = config.message;
    payload.token = config.token;
    try { parentWindow.postMessage(payload, '*', transfer || []); } catch (error) { /* the page went away */ }
  }

  // Pauses every animation and stops script loops so the frame stays still.
  function freeze() {
    try { each(doc.getAnimations ? doc.getAnimations() : [], function (animation) { try { animation.pause(); } catch (error) {} }); } catch (error) {}
    each(doc.querySelectorAll('svg'), function (svg) { try { if (svg.pauseAnimations) svg.pauseAnimations(); } catch (error) {} });
    each(doc.querySelectorAll('video, audio'), function (media) { try { media.pause(); } catch (error) {} });
    try {
      var lastTimer = Math.min(setTimer(function () {}, 0), 100000);
      for (var timerId = 1; timerId <= lastTimer; timerId++) { clearTimer(timerId); clearRepeat(timerId); }
      if (requestFrame && cancelFrame) {
        var lastFrame = Math.min(requestFrame(function () {}), 100000);
        for (var frameId = 1; frameId <= lastFrame; frameId++) cancelFrame(frameId);
      }
      win.requestAnimationFrame = function () { return 0; };
      win.setTimeout = function () { return 0; };
      win.setInterval = function () { return 0; };
    } catch (error) {}
  }

  function kebab(name) {
    if (name === 'cssFloat') return 'float';
    if (name === 'cssOffset') return 'offset';
    return name.replace(/[A-Z]/g, function (letter) { return '-' + letter.toLowerCase(); });
  }

  function isTransparent(color) {
    return !color || color === 'transparent' || /^rgba\((?:[^,]+,){3}\s*0\s*\)$/.test(color);
  }

  // Records the current value of everything that animates, keyed by a marker attribute.
  function capture() {
    var marks = [];
    var index = new Map();
    function mark(element) {
      var position = index.get(element);
      if (position === undefined) {
        position = marks.length;
        marks.push({ styles: {}, attrs: {} });
        index.set(element, position);
        element.setAttribute('data-sub2api-poster', String(position));
      }
      return marks[position];
    }
    each(doc.getAnimations ? doc.getAnimations() : [], function (animation) {
      var effect = animation.effect;
      if (!effect || !effect.target || effect.pseudoElement || typeof effect.getKeyframes !== 'function') return;
      var computed = win.getComputedStyle(effect.target);
      var styles = mark(effect.target).styles;
      each(effect.getKeyframes(), function (keyframe) {
        Object.keys(keyframe).forEach(function (key) {
          if (SKIP[key]) return;
          var property = kebab(key);
          var value = computed.getPropertyValue(property);
          if (value) styles[property] = value;
        });
      });
    });
    each(doc.querySelectorAll(SMIL), function (node) {
      var target = node.targetElement;
      if (!target) return;
      var entry = mark(target);
      var name = node.getAttribute('attributeName') || '';
      try {
        if (node.localName === 'animateTransform' || node.localName === 'animateMotion' || name === 'transform') {
          var parentNode = target.parentNode;
          var own = target.getCTM ? target.getCTM() : null;
          var base = parentNode && parentNode.getCTM ? parentNode.getCTM() : null;
          if (own && base) {
            var matrix = base.inverse().multiply(own);
            entry.attrs.transform = 'matrix(' + [matrix.a, matrix.b, matrix.c, matrix.d, matrix.e, matrix.f].join(' ') + ')';
          }
          return;
        }
        if (!name) return;
        var animated = name === 'points' ? { animVal: target.animatedPoints } : target[name];
        var value = animated && typeof animated === 'object' ? animated.animVal : undefined;
        if (typeof value === 'number' || typeof value === 'string' || typeof value === 'boolean') {
          entry.attrs[name] = String(value);
        } else if (value && typeof value.value === 'number') {
          entry.attrs[name] = String(value.value);
        } else if (value && typeof value.numberOfItems === 'number') {
          var parts = [];
          for (var item = 0; item < value.numberOfItems; item++) {
            var part = value.getItem(item);
            parts.push(part && typeof part.x === 'number' ? part.x + ',' + part.y : String(part && part.value));
          }
          entry.attrs[name] = parts.join(' ');
        } else {
          var presentation = win.getComputedStyle(target).getPropertyValue(name);
          if (presentation) entry.styles[name] = presentation;
        }
      } catch (error) {}
    });
    return marks;
  }

  // Copies the document with every captured value applied and nothing left that moves.
  function build(marks) {
    var root = doc.documentElement;
    var copy = root.cloneNode(true);
    each(doc.querySelectorAll('[data-sub2api-poster]'), function (element) { element.removeAttribute('data-sub2api-poster'); });
    each(copy.querySelectorAll('[data-sub2api-poster]'), function (element) {
      var entry = marks[Number(element.getAttribute('data-sub2api-poster'))];
      element.removeAttribute('data-sub2api-poster');
      if (!entry) return;
      Object.keys(entry.attrs).forEach(function (name) { try { element.setAttribute(name, entry.attrs[name]); } catch (error) {} });
      if (!element.style) return;
      Object.keys(entry.styles).forEach(function (property) { element.style.setProperty(property, entry.styles[property], 'important'); });
    });
    var sources = doc.querySelectorAll('canvas');
    var copies = copy.querySelectorAll('canvas');
    for (var position = 0; position < copies.length && position < sources.length; position++) {
      try {
        var computed = win.getComputedStyle(sources[position]);
        var picture = doc.createElement('img');
        picture.setAttribute('src', sources[position].toDataURL('image/png'));
        BOX.forEach(function (property) { picture.style.setProperty(property, computed.getPropertyValue(property)); });
        copies[position].parentNode.replaceChild(picture, copies[position]);
      } catch (error) {}
    }
    each(copy.querySelectorAll('script, noscript, template, iframe, object, embed, link, meta, ' + SMIL), function (node) {
      if (node.parentNode) node.parentNode.removeChild(node);
    });
    var still = doc.createElement('style');
    still.textContent = '*,*::before,*::after{animation:none!important;transition:none!important}';
    (copy.querySelector('head') || copy).appendChild(still);

    var rootStyle = win.getComputedStyle(root);
    var bodyStyle = doc.body ? win.getComputedStyle(doc.body) : null;
    var rootPainted = rootStyle.backgroundImage !== 'none' || !isTransparent(rootStyle.backgroundColor);
    var background = rootPainted ? rootStyle.backgroundColor : (bodyStyle ? bodyStyle.backgroundColor : '');
    var copyBody = copy.querySelector('body');
    // A body background normally paints the whole viewport; keep that inside the image.
    if (!rootPainted && bodyStyle && copyBody) {
      BACKGROUND.forEach(function (property) { copy.style.setProperty(property, bodyStyle.getPropertyValue(property)); });
      copyBody.style.setProperty('background', 'transparent', 'important');
    }
    copy.style.setProperty('width', config.width + 'px');
    copy.style.setProperty('height', config.height + 'px');
    copy.style.setProperty('overflow', 'hidden');
    return { markup: new Serializer().serializeToString(copy), background: isTransparent(background) ? '#ffffff' : background };
  }

  // Uniform pixels mean the copy did not render (some engines skip foreignObject).
  function isBlank(context, width, height) {
    var data = context.getImageData(0, 0, width, height).data;
    var stepX = Math.max(1, Math.floor(width / 24));
    var stepY = Math.max(1, Math.floor(height / 18));
    var first = null;
    for (var y = Math.floor(stepY / 2); y < height; y += stepY) {
      for (var x = Math.floor(stepX / 2); x < width; x += stepX) {
        var offset = (y * width + x) * 4;
        var pixel = data[offset] + ',' + data[offset + 1] + ',' + data[offset + 2] + ',' + data[offset + 3];
        if (first === null) first = pixel;
        else if (pixel !== first) return false;
      }
    }
    return true;
  }

  function paint(result) {
    var width = config.width;
    var height = config.height;
    var markup = '<svg xmlns="http://www.w3.org/2000/svg" width="' + width + '" height="' + height +
      '" viewBox="0 0 ' + width + ' ' + height + '"><foreignObject x="0" y="0" width="' + width + '" height="' + height + '">' +
      result.markup + '<' + '/foreignObject><' + '/svg>';
    var image = new ImageElement();
    var guard = setTimer(function () { report({ ok: false, reason: 'decode' }); }, 4000);
    image.onload = function () {
      clearTimer(guard);
      try {
        var canvas = doc.createElement('canvas');
        canvas.width = config.outWidth;
        canvas.height = config.outHeight;
        var context = canvas.getContext('2d');
        context.fillStyle = result.background;
        context.fillRect(0, 0, canvas.width, canvas.height);
        context.drawImage(image, 0, 0, canvas.width, canvas.height);
        if (isBlank(context, canvas.width, canvas.height)) { report({ ok: false, reason: 'blank' }); return; }
        canvas.toBlob(function (blob) {
          if (!blob) { report({ ok: false, reason: 'encode' }); return; }
          blob.arrayBuffer().then(function (buffer) {
            report({ ok: true, buffer: buffer }, [buffer]);
          }, function () { report({ ok: false, reason: 'encode' }); });
        }, 'image/webp', 0.92);
      } catch (error) {
        report({ ok: false, reason: error && error.name === 'SecurityError' ? 'security' : 'paint' });
      }
    };
    image.onerror = function () { clearTimer(guard); report({ ok: false, reason: 'decode' }); };
    image.src = 'data:image/svg+xml;charset=utf-8,' + encodeURIComponent(markup);
  }

  function run() {
    freeze();
    if (!config.snapshot) { report({ ok: false, reason: 'skipped' }); return; }
    try {
      paint(build(capture()));
    } catch (error) {
      report({ ok: false, reason: error && error.name === 'SecurityError' ? 'security' : 'capture' });
    }
  }

  setTimer(run, config.settle);
})`

export interface DegradationPosterOptions {
  /** Random per frame; replies carrying another token are ignored. */
  token: string
  /** False only freezes the frame, for engines that cannot paint it into an image. */
  snapshot: boolean
}

export type DegradationPosterResult = { ok: true; buffer: ArrayBuffer } | { ok: false; reason: string }

export type DegradationPosterMiss = 'empty' | 'unavailable'

export function createDegradationPosterToken(): string {
  const bytes = new Uint8Array(12)
  if (typeof crypto !== 'undefined' && typeof crypto.getRandomValues === 'function') {
    crypto.getRandomValues(bytes)
  } else {
    for (let index = 0; index < bytes.length; index += 1) bytes[index] = Math.floor(Math.random() * 256)
  }
  return Array.from(bytes, (byte) => byte.toString(16).padStart(2, '0')).join('')
}

/** The script injected into a poster document. */
export function degradationPosterRuntime(options: DegradationPosterOptions): string {
  const config = {
    message: DEGRADATION_POSTER_MESSAGE,
    token: options.token,
    width: DEGRADATION_PREVIEW_WIDTH,
    height: DEGRADATION_PREVIEW_HEIGHT,
    outWidth: DEGRADATION_POSTER_WIDTH,
    outHeight: DEGRADATION_POSTER_HEIGHT,
    settle: DEGRADATION_POSTER_SETTLE_MS,
    snapshot: options.snapshot,
  }
  return `${POSTER_RUNTIME}(${JSON.stringify(config).replace(/</g, '\\u003c')});`
}

/** The generated document plus the runtime that freezes it and reports its frame. */
export function buildDegradationPosterDocument(html: string, options: DegradationPosterOptions): string {
  return buildDegradationPreviewDocument(html, { script: degradationPosterRuntime(options) })
}

/**
 * Accepts only replies from the given frame that carry its token. The frame
 * runs generated code, so everything else about the reply stays untrusted.
 */
export function readDegradationPosterMessage(
  event: MessageEvent,
  source: Window | null | undefined,
  token: string,
): DegradationPosterResult | null {
  if (!source || !token || event.source !== source) return null
  const data: unknown = event.data
  if (!data || typeof data !== 'object') return null
  const message = data as Record<string, unknown>
  if (message.type !== DEGRADATION_POSTER_MESSAGE || message.token !== token) return null
  const buffer = message.buffer
  if (message.ok === true && buffer instanceof ArrayBuffer && buffer.byteLength > 0 && buffer.byteLength <= MAX_POSTER_BYTES) {
    return { ok: true, buffer }
  }
  return { ok: false, reason: typeof message.reason === 'string' ? message.reason.slice(0, 24) : 'invalid' }
}

interface DecodedImage {
  width: number
  height: number
  source: CanvasImageSource
  close: () => void
}

async function decodeImage(blob: Blob): Promise<DecodedImage> {
  if (typeof createImageBitmap === 'function') {
    const bitmap = await createImageBitmap(blob)
    return { width: bitmap.width, height: bitmap.height, source: bitmap, close: () => bitmap.close() }
  }
  const url = URL.createObjectURL(blob)
  try {
    const image = new Image()
    image.src = url
    await image.decode()
    return { width: image.naturalWidth, height: image.naturalHeight, source: image, close: () => undefined }
  } finally {
    URL.revokeObjectURL(url)
  }
}

/**
 * Decodes the frame's pixels and re-encodes them on this page, so only an
 * image this page produced is ever shown or stored. Anything that does not
 * decode as an image of sensible size is rejected.
 */
export async function normalizeDegradationPoster(buffer: ArrayBuffer): Promise<Blob | null> {
  if (buffer.byteLength === 0 || buffer.byteLength > MAX_POSTER_BYTES) return null
  let image: DecodedImage
  try {
    image = await decodeImage(new Blob([buffer]))
  } catch {
    return null
  }
  try {
    if (image.width < 16 || image.height < 16 || image.width > MAX_POSTER_EDGE || image.height > MAX_POSTER_EDGE) return null
    const canvas = document.createElement('canvas')
    canvas.width = DEGRADATION_POSTER_WIDTH
    canvas.height = DEGRADATION_POSTER_HEIGHT
    const context = canvas.getContext('2d')
    if (!context) return null
    context.drawImage(image.source, 0, 0, canvas.width, canvas.height)
    return await new Promise<Blob | null>((resolve) => {
      canvas.toBlob((blob) => resolve(blob && blob.size > 0 ? blob : null), 'image/webp', POSTER_QUALITY)
    })
  } catch {
    return null
  } finally {
    image.close()
  }
}

// ---------- cache ----------

const memory = new Map<string, string>()
const misses = new Map<string, DegradationPosterMiss>()

/** Finished results never change, so a poster is keyed by the result and its finish time. */
export function degradationPosterKey(run: Pick<DegradationRun, 'id' | 'finished_at'>): string {
  return `${CACHE_VERSION}:${run.id}:${run.finished_at ?? ''}`
}

function remember(key: string, url: string) {
  memory.set(key, url)
  while (memory.size > MEMORY_LIMIT) {
    const oldest = memory.keys().next().value
    if (oldest === undefined) break
    // The URL stays valid for any card still showing it.
    memory.delete(oldest)
  }
}

/** A poster already decoded in this tab, if any. */
export function peekDegradationPoster(key: string): string | null {
  const url = memory.get(key)
  if (!url) return null
  memory.delete(key)
  memory.set(key, url)
  return url
}

/** Looks in this tab first, then in the browser's poster store. */
export async function findDegradationPoster(key: string): Promise<string | null> {
  const cached = peekDegradationPoster(key)
  if (cached) return cached
  const blob = await readStoredPoster(key)
  if (!blob) return null
  const raced = memory.get(key)
  if (raced) return raced
  const url = URL.createObjectURL(blob)
  remember(key, url)
  return url
}

export function saveDegradationPoster(key: string, blob: Blob): string {
  const existing = memory.get(key)
  if (existing) return existing
  const url = URL.createObjectURL(blob)
  remember(key, url)
  void writeStoredPoster(key, blob)
  return url
}

/** Results that cannot become a poster are not tried again in this tab. */
export function degradationPosterMiss(key: string): DegradationPosterMiss | undefined {
  return misses.get(key)
}

export function rememberDegradationPosterMiss(key: string, miss: DegradationPosterMiss): void {
  misses.set(key, miss)
  while (misses.size > MEMORY_LIMIT) {
    const oldest = misses.keys().next().value
    if (oldest === undefined) break
    misses.delete(oldest)
  }
}

let securityFailures = 0

/** Engines that refuse to paint the copy only get frozen frames after two refusals. */
export function degradationPosterSnapshotsEnabled(): boolean {
  return securityFailures < 2
}

export function noteDegradationPosterFailure(reason: string): void {
  if (reason === 'security') securityFailures += 1
}

/** Test hook. */
export function resetDegradationPosterState(): void {
  memory.clear()
  misses.clear()
  securityFailures = 0
}

interface StoredPoster {
  key: string
  blob: Blob
  saved_at: number
}

let database: Promise<IDBDatabase | null> | null = null
let writesSincePrune = 0

// Storage is optional: private modes and old engines simply fall back to this tab's cache.
function openStore(): Promise<IDBDatabase | null> {
  if (!database) {
    database = new Promise((resolve) => {
      try {
        if (typeof indexedDB === 'undefined' || !indexedDB) {
          resolve(null)
          return
        }
        const request = indexedDB.open(DB_NAME, 1)
        request.onupgradeneeded = () => {
          const db = request.result
          if (!db.objectStoreNames.contains(STORE_NAME)) {
            db.createObjectStore(STORE_NAME, { keyPath: 'key' }).createIndex('saved_at', 'saved_at')
          }
        }
        request.onsuccess = () => {
          const db = request.result
          db.onversionchange = () => db.close()
          resolve(db)
        }
        request.onerror = () => resolve(null)
        request.onblocked = () => resolve(null)
      } catch {
        resolve(null)
      }
    })
  }
  return database
}

async function readStoredPoster(key: string): Promise<Blob | null> {
  try {
    const db = await openStore()
    if (!db) return null
    const record = await new Promise<StoredPoster | undefined>((resolve, reject) => {
      const request = db.transaction(STORE_NAME, 'readonly').objectStore(STORE_NAME).get(key)
      request.onsuccess = () => resolve(request.result as StoredPoster | undefined)
      request.onerror = () => reject(request.error)
    })
    return record && record.blob instanceof Blob && record.blob.size > 0 ? record.blob : null
  } catch {
    return null
  }
}

async function writeStoredPoster(key: string, blob: Blob): Promise<void> {
  try {
    const db = await openStore()
    if (!db) return
    const record: StoredPoster = { key, blob, saved_at: Date.now() }
    db.transaction(STORE_NAME, 'readwrite').objectStore(STORE_NAME).put(record)
    writesSincePrune += 1
    if (writesSincePrune >= 20) {
      writesSincePrune = 0
      pruneStoredPosters(db)
    }
  } catch {
    // Storage is optional.
  }
}

// Keeps the newest posters; callbacks keep the work inside one transaction.
function pruneStoredPosters(db: IDBDatabase): void {
  try {
    const store = db.transaction(STORE_NAME, 'readwrite').objectStore(STORE_NAME)
    const counting = store.count()
    counting.onsuccess = () => {
      let excess = counting.result - STORE_LIMIT
      if (excess <= 0) return
      const cursorRequest = store.index('saved_at').openCursor()
      cursorRequest.onsuccess = () => {
        const cursor = cursorRequest.result
        if (!cursor || excess <= 0) return
        cursor.delete()
        excess -= 1
        cursor.continue()
      }
    }
  } catch {
    // Storage is optional.
  }
}

// ---------- queue ----------

export interface DegradationPosterQueue {
  /** Resolves with a release callback once a slot is free; rejects when the signal aborts first. */
  acquire(signal?: AbortSignal): Promise<() => void>
  /** Holds new work, e.g. while the detail dialog plays an animation. */
  pause(): void
  resume(): void
  readonly active: number
  readonly waiting: number
}

function abortError(): Error {
  if (typeof DOMException === 'function') return new DOMException('Aborted', 'AbortError')
  return Object.assign(new Error('Aborted'), { name: 'AbortError' })
}

export function createDegradationPosterQueue(limit: number): DegradationPosterQueue {
  interface Waiter {
    grant: (release: () => void) => void
    signal?: AbortSignal
    onAbort?: () => void
  }
  const waiters: Waiter[] = []
  let active = 0
  let paused = false

  function pump() {
    while (!paused && active < limit && waiters.length) {
      const waiter = waiters.shift()!
      if (waiter.signal && waiter.onAbort) waiter.signal.removeEventListener('abort', waiter.onAbort)
      active += 1
      let released = false
      waiter.grant(() => {
        if (released) return
        released = true
        active -= 1
        pump()
      })
    }
  }

  return {
    acquire(signal) {
      return new Promise((resolve, reject) => {
        if (signal?.aborted) {
          reject(abortError())
          return
        }
        const waiter: Waiter = { grant: resolve, signal }
        if (signal) {
          waiter.onAbort = () => {
            const index = waiters.indexOf(waiter)
            if (index >= 0) waiters.splice(index, 1)
            reject(abortError())
          }
          signal.addEventListener('abort', waiter.onAbort, { once: true })
        }
        waiters.push(waiter)
        pump()
      })
    },
    pause() {
      paused = true
    },
    resume() {
      paused = false
      pump()
    },
    get active() {
      return active
    },
    get waiting() {
      return waiters.length
    },
  }
}

function defaultPosterConcurrency(): number {
  const cores = typeof navigator !== 'undefined' && navigator.hardwareConcurrency ? navigator.hardwareConcurrency : 4
  return cores >= 8 ? 3 : 2
}

/** Shared by every card, so only a few documents ever render at the same time. */
export const degradationPosterQueue = createDegradationPosterQueue(defaultPosterConcurrency())
