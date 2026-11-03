<template>
  <div
    ref="containerRef"
    class="dg-frame"
    :class="{ 'is-interactive': interactive }"
    :style="{ aspectRatio: `${DEGRADATION_PREVIEW_WIDTH} / ${DEGRADATION_PREVIEW_HEIGHT}` }"
  >
    <iframe
      v-if="html && fits"
      :key="frameKey"
      ref="frameRef"
      class="dg-frame-iframe"
      :src="frameURL"
      sandbox="allow-scripts"
      referrerpolicy="no-referrer"
      :title="title"
      :tabindex="interactive ? 0 : -1"
      :style="frameStyle"
      data-testid="degradation-preview-iframe"
      @load="postDocument"
    ></iframe>
    <div v-else class="dg-frame-placeholder">
      <slot name="placeholder"></slot>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import {
  DEGRADATION_PREVIEW_HEIGHT,
  DEGRADATION_PREVIEW_MAX_LENGTH,
  DEGRADATION_PREVIEW_MESSAGE,
  DEGRADATION_PREVIEW_WIDTH,
  buildDegradationPreviewDocument,
  degradationPreviewFrameURL,
} from '@/utils/degradationPreview'

/** Plays a generated animation live (detail dialog). Gallery cards use still posters instead. */
const props = withDefaults(defineProps<{
  /** Extracted HTML document; empty renders the placeholder slot. */
  html: string
  title: string
  /** Let the pointer and keyboard reach the animation. */
  interactive?: boolean
}>(), {
  interactive: false,
})

const containerRef = ref<HTMLElement | null>(null)
const frameRef = ref<HTMLIFrameElement | null>(null)
const frameKey = ref(0)
const scale = ref(1)
const frameURL = degradationPreviewFrameURL()
const documentHTML = computed(() => buildDegradationPreviewDocument(props.html, { scrollable: props.interactive }))
const fits = computed(() => documentHTML.value.length <= DEGRADATION_PREVIEW_MAX_LENGTH)

const frameStyle = computed(() => ({
  width: `${DEGRADATION_PREVIEW_WIDTH}px`,
  height: `${DEGRADATION_PREVIEW_HEIGHT}px`,
  transform: `scale(${scale.value})`,
}))

let resizeObserver: ResizeObserver | null = null

function measure() {
  const width = containerRef.value?.clientWidth ?? 0
  if (width > 0) scale.value = width / DEGRADATION_PREVIEW_WIDTH
}

function postDocument() {
  // The shell has an opaque origin, so the target origin must be "*". Only
  // the generated document is sent; it never carries credentials.
  frameRef.value?.contentWindow?.postMessage({ type: DEGRADATION_PREVIEW_MESSAGE, html: documentHTML.value }, '*')
}

/** Restarts the animation from the beginning. */
function replay() {
  frameKey.value += 1
}

defineExpose({ replay })

watch(() => props.html, () => replay())

onMounted(() => {
  measure()
  if (typeof ResizeObserver !== 'undefined' && containerRef.value) {
    resizeObserver = new ResizeObserver(measure)
    resizeObserver.observe(containerRef.value)
  }
})

onBeforeUnmount(() => {
  resizeObserver?.disconnect()
})
</script>

<style scoped>
.dg-frame {
  position: relative;
  width: 100%;
  overflow: hidden;
  border-radius: inherit;
  background: #fff;
}

.dg-frame-iframe {
  position: absolute;
  top: 0;
  left: 0;
  border: 0;
  background: #fff;
  transform-origin: 0 0;
  pointer-events: none;
}

.dg-frame.is-interactive .dg-frame-iframe {
  pointer-events: auto;
}

.dg-frame-placeholder {
  position: absolute;
  inset: 0;
  display: flex;
  align-items: center;
  justify-content: center;
}
</style>
