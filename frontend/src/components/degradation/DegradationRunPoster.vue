<template>
  <div ref="rootRef" class="dg-poster" :data-phase="phase" data-testid="degradation-poster">
    <img
      v-if="posterURL"
      :src="posterURL"
      class="dg-poster-image"
      alt=""
      decoding="async"
      draggable="false"
      data-testid="degradation-poster-image"
    />
    <template v-else>
      <iframe
        v-if="frameDocument"
        ref="frameRef"
        class="dg-poster-frame"
        :src="frameURL"
        sandbox="allow-scripts"
        referrerpolicy="no-referrer"
        tabindex="-1"
        aria-hidden="true"
        :title="title"
        :style="frameStyle"
        data-testid="degradation-poster-frame"
        @load="postDocument"
      ></iframe>
      <div v-if="phase !== 'frozen'" class="dg-poster-cover" :class="`is-${phase}`">
        <template v-if="phase === 'empty'">
          <Icon name="document" size="lg" />
          <span class="dg-poster-title">{{ t('degradationCheck.noAnimation') }}</span>
        </template>
        <template v-else-if="phase === 'error'">
          <Icon name="exclamationCircle" size="lg" />
          <span class="dg-poster-title">{{ t('degradationCheck.animationError') }}</span>
        </template>
        <template v-else-if="phase === 'unavailable'">
          <span class="dg-poster-play" aria-hidden="true"><Icon name="play" size="md" /></span>
          <span class="dg-poster-title">{{ t('degradationCheck.poster.unavailable') }}</span>
        </template>
        <template v-else>
          <span class="dg-poster-shimmer" aria-hidden="true"></span>
          <span class="dg-poster-text">{{ t('degradationCheck.poster.generating') }}</span>
        </template>
      </div>
    </template>
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import { loadDegradationRun } from '@/composables/useDegradationRuns'
import {
  DEGRADATION_POSTER_TIMEOUT_MS,
  buildDegradationPosterDocument,
  createDegradationPosterToken,
  degradationPosterKey,
  degradationPosterMiss,
  degradationPosterQueue,
  degradationPosterSnapshotsEnabled,
  findDegradationPoster,
  normalizeDegradationPoster,
  noteDegradationPosterFailure,
  peekDegradationPoster,
  readDegradationPosterMessage,
  rememberDegradationPosterMiss,
  saveDegradationPoster,
  type DegradationPosterMiss,
} from '@/utils/degradationPoster'
import {
  DEGRADATION_PREVIEW_HEIGHT,
  DEGRADATION_PREVIEW_MAX_LENGTH,
  DEGRADATION_PREVIEW_MESSAGE,
  DEGRADATION_PREVIEW_WIDTH,
  degradationPreviewFrameURL,
  extractDegradationHTML,
} from '@/utils/degradationPreview'

/**
 * idle → queued (waiting for a render slot) → loading (fetching the document)
 * → rendering (frame running until it reports) → ready (still image).
 * frozen: the frame could not paint itself, so the paused frame stays instead.
 */
type Phase = 'idle' | 'queued' | 'loading' | 'rendering' | 'ready' | 'frozen' | DegradationPosterMiss | 'error'

const props = defineProps<{
  run: { id: number; finished_at: string | null }
  title: string
}>()

const { t } = useI18n()
const rootRef = ref<HTMLElement | null>(null)
const frameRef = ref<HTMLIFrameElement | null>(null)
const phase = ref<Phase>('idle')
const posterURL = ref<string | null>(null)
const frameDocument = ref('')
const scale = ref(1)
const frameURL = degradationPreviewFrameURL()
const posterKey = computed(() => degradationPosterKey(props.run))
const frameStyle = computed(() => ({
  width: `${DEGRADATION_PREVIEW_WIDTH}px`,
  height: `${DEGRADATION_PREVIEW_HEIGHT}px`,
  transform: `scale(${scale.value})`,
}))

let job: AbortController | null = null
let release: (() => void) | null = null
let token = ''
let timeout: ReturnType<typeof setTimeout> | null = null
let visible = false
let resizeObserver: ResizeObserver | null = null
let intersectionObserver: IntersectionObserver | null = null

function restore() {
  const cached = peekDegradationPoster(posterKey.value)
  posterURL.value = cached
  frameDocument.value = ''
  phase.value = cached ? 'ready' : (degradationPosterMiss(posterKey.value) ?? 'idle')
}

// Frees the render slot; a frozen frame stays on screen.
function settle() {
  if (timeout) clearTimeout(timeout)
  timeout = null
  window.removeEventListener('message', onMessage)
  release?.()
  release = null
  job = null
}

function cancel() {
  job?.abort()
  settle()
}

function miss(key: string, kind: DegradationPosterMiss) {
  rememberDegradationPosterMiss(key, kind)
  frameDocument.value = ''
  phase.value = kind
  settle()
}

async function generate() {
  if (phase.value !== 'idle') return
  const key = posterKey.value
  const current = new AbortController()
  job = current
  phase.value = 'queued'
  try {
    const stored = await findDegradationPoster(key)
    if (current.signal.aborted) return
    if (stored) {
      posterURL.value = stored
      phase.value = 'ready'
      settle()
      return
    }
    const granted = await degradationPosterQueue.acquire(current.signal)
    if (current.signal.aborted) {
      granted()
      return
    }
    release = granted
    phase.value = 'loading'
    const detail = await loadDegradationRun(props.run.id)
    if (current.signal.aborted) return
    const html = extractDegradationHTML(detail.output)
    if (!html) {
      miss(key, 'empty')
      return
    }
    token = createDegradationPosterToken()
    const documentHTML = buildDegradationPosterDocument(html, { token, snapshot: degradationPosterSnapshotsEnabled() })
    if (documentHTML.length > DEGRADATION_PREVIEW_MAX_LENGTH) {
      miss(key, 'unavailable')
      return
    }
    phase.value = 'rendering'
    window.addEventListener('message', onMessage)
    timeout = setTimeout(() => miss(key, 'unavailable'), DEGRADATION_POSTER_TIMEOUT_MS)
    frameDocument.value = documentHTML
  } catch {
    if (current.signal.aborted) return
    // Loading can fail transiently, so this is not remembered.
    phase.value = 'error'
    settle()
  }
}

async function onMessage(event: MessageEvent) {
  const result = readDegradationPosterMessage(event, frameRef.value?.contentWindow, token)
  if (!result) return
  const current = job
  const key = posterKey.value
  window.removeEventListener('message', onMessage)
  if (timeout) clearTimeout(timeout)
  timeout = null
  if (result.ok) {
    const blob = await normalizeDegradationPoster(result.buffer)
    if (!current || current.signal.aborted || job !== current) return
    if (blob) {
      posterURL.value = saveDegradationPoster(key, blob)
      frameDocument.value = ''
      phase.value = 'ready'
      settle()
      return
    }
  } else {
    noteDegradationPosterFailure(result.reason)
  }
  // The frame froze itself before replying, so it stays as the still preview.
  phase.value = 'frozen'
  settle()
}

function postDocument() {
  if (!frameDocument.value) return
  // The shell has an opaque origin, so the target origin must be "*"; only the
  // generated document is sent.
  frameRef.value?.contentWindow?.postMessage({ type: DEGRADATION_PREVIEW_MESSAGE, html: frameDocument.value }, '*')
}

function measure() {
  const width = rootRef.value?.clientWidth ?? 0
  if (width > 0) scale.value = width / DEGRADATION_PREVIEW_WIDTH
}

function observeVisibility() {
  const root = rootRef.value
  if (!root) return
  if (typeof IntersectionObserver === 'undefined') {
    visible = true
    void generate()
    return
  }
  // Only cards on screen render; a card scrolled away before its turn gives the slot back.
  intersectionObserver = new IntersectionObserver((entries) => {
    visible = entries.some((entry) => entry.isIntersecting)
    if (visible) {
      void generate()
    } else if (phase.value === 'queued') {
      cancel()
      phase.value = 'idle'
    }
  })
  intersectionObserver.observe(root)
}

restore()

watch(posterKey, () => {
  cancel()
  restore()
  if (visible) void generate()
})

onMounted(() => {
  measure()
  if (typeof ResizeObserver !== 'undefined' && rootRef.value) {
    resizeObserver = new ResizeObserver(measure)
    resizeObserver.observe(rootRef.value)
  }
  observeVisibility()
})

onBeforeUnmount(() => {
  cancel()
  resizeObserver?.disconnect()
  intersectionObserver?.disconnect()
})
</script>

<style scoped>
.dg-poster {
  position: absolute;
  inset: 0;
  overflow: hidden;
  background: #fff;
}

.dg-poster-image {
  display: block;
  width: 100%;
  height: 100%;
  object-fit: cover;
  user-select: none;
  -webkit-user-drag: none;
}

.dg-poster-frame {
  position: absolute;
  top: 0;
  left: 0;
  border: 0;
  background: #fff;
  transform-origin: 0 0;
  pointer-events: none;
}

.dg-poster-cover {
  position: absolute;
  inset: 0;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 0.375rem;
  padding: 0.75rem;
  overflow: hidden;
  background:
    radial-gradient(18rem 9rem at 50% 0%, rgb(233 184 36 / 0.12), transparent 70%),
    var(--md-sys-color-surface);
  color: var(--md-sys-color-on-surface-variant);
  text-align: center;
}

.dg-poster-cover.is-error { color: #b4513d; }

.dg-poster-title {
  font-size: 0.8125rem;
  font-weight: 650;
}

.dg-poster-text {
  position: relative;
  font-size: 0.6875rem;
}

.dg-poster-play {
  display: inline-flex;
  width: 2.5rem;
  height: 2.5rem;
  align-items: center;
  justify-content: center;
  border-radius: 999px;
  background: var(--md-sys-color-primary);
  color: var(--md-sys-color-on-primary);
  box-shadow: 0 6px 14px -8px rgb(151 104 0 / 0.6);
}

/* A compositor-only sweep: it moves with transform, so it never repaints the card. */
.dg-poster-shimmer {
  position: absolute;
  inset: 0;
  overflow: hidden;
}

.dg-poster-shimmer::after {
  position: absolute;
  inset: 0;
  content: '';
  background: linear-gradient(100deg, transparent 20%, rgb(255 248 220 / 0.85) 50%, transparent 80%);
  transform: translateX(-100%);
  animation: dg-poster-sweep 1.6s ease-in-out infinite;
  will-change: transform;
}

@keyframes dg-poster-sweep {
  to { transform: translateX(100%); }
}

@media (prefers-reduced-motion: reduce) {
  .dg-poster-shimmer::after { animation: none; transform: none; opacity: 0.4; }
}
</style>
