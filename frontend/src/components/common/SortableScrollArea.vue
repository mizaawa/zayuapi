<template>
  <div class="relative min-h-0">
    <div :id="viewportId" ref="viewport" class="sort-scroll-viewport overflow-y-auto overscroll-contain pr-8" @scroll="measure">
      <div ref="content"><slot /></div>
    </div>
    <div
      v-if="maxScroll > 0"
      ref="track"
      role="scrollbar"
      tabindex="0"
      aria-orientation="vertical"
      :aria-label="label"
      :aria-controls="viewportId"
      :aria-valuemin="0"
      :aria-valuemax="100"
      :aria-valuenow="Math.round(progress * 100)"
      class="sort-scroll-track absolute inset-y-0 right-0 w-6 rounded-md outline-none"
      @pointerdown="beginDrag"
      @pointermove="drag"
      @pointerup="endDrag"
      @pointercancel="endDrag"
      @lostpointercapture="clearDrag"
      @keydown="onKeydown"
    >
      <div class="sort-scroll-thumb absolute inset-x-1.5 rounded-full" :style="{ height: `${thumbHeight}px`, transform: `translateY(${progress * travel}px)` }" />
    </div>
  </div>
</template>

<script lang="ts">
let scrollAreaId = 0
</script>

<script setup lang="ts">
import { computed, onMounted, onUpdated, ref } from 'vue'
import { useResizeObserver } from '@vueuse/core'

defineProps<{ label: string }>()
const viewportId = `sort-scroll-${++scrollAreaId}`
const viewport = ref<HTMLElement>()
const content = ref<HTMLElement>()
const track = ref<HTMLElement>()
const maxScroll = ref(0)
const height = ref(0)
const contentHeight = ref(0)
const scrollTop = ref(0)
const progress = computed(() => maxScroll.value > 0 ? Math.max(0, Math.min(1, scrollTop.value / maxScroll.value)) : 0)
const thumbHeight = computed(() => Math.min(height.value, Math.max(32, height.value * height.value / (contentHeight.value || 1))))
const travel = computed(() => height.value - thumbHeight.value)
let activePointer: number | null = null
let pointerOffset = 0

function measure() {
  if (!viewport.value) return
  height.value = viewport.value.clientHeight
  contentHeight.value = viewport.value.scrollHeight
  maxScroll.value = Math.max(0, contentHeight.value - height.value)
  scrollTop.value = Math.min(maxScroll.value, viewport.value.scrollTop)
}

function beginDrag(event: PointerEvent) {
  if (!event.isPrimary || event.button !== 0 || activePointer !== null || !track.value || !viewport.value) return
  event.preventDefault()
  measure()
  activePointer = event.pointerId
  const target = event.target as HTMLElement
  pointerOffset = target.classList.contains('sort-scroll-thumb') ? event.clientY - target.getBoundingClientRect().top : thumbHeight.value / 2
  track.value.setPointerCapture(event.pointerId)
  track.value.classList.add('is-dragging')
  drag(event)
}

function drag(event: PointerEvent) {
  if (activePointer !== event.pointerId || !track.value || !viewport.value || travel.value <= 0) return
  const top = Math.max(0, Math.min(travel.value, event.clientY - track.value.getBoundingClientRect().top - pointerOffset))
  viewport.value.scrollTop = top / travel.value * maxScroll.value
  measure()
}

function endDrag(event: PointerEvent) {
  if (activePointer !== event.pointerId) return
  clearDrag()
  if (track.value?.hasPointerCapture(event.pointerId)) track.value.releasePointerCapture(event.pointerId)
}

function clearDrag() {
  activePointer = null
  track.value?.classList.remove('is-dragging')
}

function onKeydown(event: KeyboardEvent) {
  if (!viewport.value) return
  const offsets: Record<string, number> = { ArrowDown: 48, ArrowUp: -48, PageDown: height.value, PageUp: -height.value }
  if (event.key === 'Home') viewport.value.scrollTop = 0
  else if (event.key === 'End') viewport.value.scrollTop = maxScroll.value
  else if (event.key in offsets) viewport.value.scrollTop += offsets[event.key]
  else return
  event.preventDefault()
  measure()
}

useResizeObserver([viewport, content], measure)
onMounted(measure)
onUpdated(measure)
</script>

<style scoped>
.sort-scroll-viewport {
  /* Reserve room for the dialog header, hint and footer on short screens. */
  max-height: min(55dvh, 420px, max(64px, calc(90dvh - 15rem)));
  scrollbar-width: none;
  touch-action: pan-y;
}
.sort-scroll-viewport::-webkit-scrollbar { display: none; }
.sort-scroll-track {
  touch-action: none;
  cursor: pointer;
  background: var(--md-sys-color-surface-container);
}
.sort-scroll-track:focus-visible {
  outline: 2px solid var(--md-sys-color-primary);
  outline-offset: 2px;
}
.sort-scroll-thumb {
  cursor: grab;
  background: var(--md-sys-color-primary);
}
.sort-scroll-track.is-dragging,
.sort-scroll-track.is-dragging .sort-scroll-thumb { cursor: grabbing; }

/* Fallback drag previews are attached to body, outside the scroll area. */
:global(.sort-order-item) {
  border-color: var(--md-sys-color-outline);
  background: var(--md-sys-color-surface);
  color: var(--md-sys-color-on-surface);
}
:global(.sort-order-meta) { color: var(--md-sys-color-on-surface-variant); }
:global(.sort-order-handle) { color: var(--md-sys-color-on-surface-variant); }
:global(.sort-order-handle:hover),
:global(.sort-order-handle:active) {
  background: var(--md-sys-color-surface-container);
  color: var(--md-sys-color-on-surface);
}
:global(.sort-order-ghost) {
  border-color: var(--md-sys-color-primary);
  background: var(--md-sys-color-surface-container);
  opacity: 0.45;
}
:global(.sort-order-chosen) {
  border-color: var(--md-sys-color-primary);
  box-shadow: 0 0 0 2px color-mix(in srgb, var(--md-sys-color-primary) 30%, transparent);
}
:global(.sort-order-fallback) {
  z-index: 100001 !important;
  cursor: grabbing;
}
</style>
