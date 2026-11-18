<template>
  <!-- The close button is the keyboard equivalent. Only this strip owns the drag. -->
  <div class="sheet-handle" aria-hidden="true" @pointerdown="start" @pointermove="move"
    @pointerup="finish" @pointercancel="cancel" @lostpointercapture="cancel" />
</template>

<script setup lang="ts">
const emit = defineEmits<{ dismiss: [] }>()
let drag: { id: number; x: number; y: number; panel: HTMLElement } | null = null

function start(event: PointerEvent) {
  const handle = event.currentTarget as HTMLElement
  if (drag || !event.isPrimary || event.button !== 0 || !handle.parentElement) return
  handle.setPointerCapture(event.pointerId)
  drag = { id: event.pointerId, x: event.clientX, y: event.clientY, panel: handle.parentElement }
}
function move(event: PointerEvent) {
  if (drag?.id !== event.pointerId) return
  drag.panel.style.setProperty('--sheet-pull', Math.max(0, event.clientY - drag.y) + 'px')
}
function cancel(event: PointerEvent) {
  if (drag?.id !== event.pointerId) return
  drag.panel.style.removeProperty('--sheet-pull')
  drag = null
}
function finish(event: PointerEvent) {
  if (drag?.id !== event.pointerId) return
  const dy = event.clientY - drag.y
  const dismiss = dy >= 64 && dy > Math.abs(event.clientX - drag.x)
  cancel(event)
  if (dismiss) emit('dismiss')
}
</script>

<style src="@/styles/mobile-sheet.css"></style>
