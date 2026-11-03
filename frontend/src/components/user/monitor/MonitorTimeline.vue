<template>
  <div
    ref="barEl"
    class="monitor-status-bar"
    :class="{ 'has-active': activeIndex !== null }"
    role="group"
    tabindex="0"
    :aria-label="timelineAriaLabel"
    :aria-describedby="activeDetail ? tooltipId : undefined"
    data-testid="monitor-status-bar"
    @pointerdown="onPointerDown"
    @pointermove="onPointerMove"
    @pointerup="onPointerUp"
    @pointercancel="onPointerCancel"
    @pointerleave="onPointerLeave"
    @focus="onFocus"
    @blur="onBlur"
    @keydown="onKeydown"
  >
    <span
      v-for="(bar, index) in displayBars"
      :key="index"
      class="monitor-status-bar__segment"
      :class="[`monitor-status-bar__segment--${bar.status}`, { 'is-active': index === activeIndex }]"
      aria-hidden="true"
    ></span>
    <MonitorCheckTooltip :id="tooltipId" :detail="activeDetail" :anchor="anchor" />
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref, useId, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { MonitorTimelinePoint } from '@/api/channelMonitor'
import { useChannelMonitorFormat } from '@/composables/useChannelMonitorFormat'
import { MONITOR_TIMELINE_POINTS } from '@/constants/channelMonitor'
import {
  formatMonitorLatency,
  monitorLatencyTone,
  resolveMonitorStatus,
} from '@/utils/channelMonitorHealth'
import { formatDate } from '@/utils/format'
import MonitorCheckTooltip from './MonitorCheckTooltip.vue'
import type {
  MonitorCheckAnchor,
  MonitorCheckDetail,
  MonitorCheckDetailRow,
  MonitorCheckTone,
} from './types'

type TimelineStatus = 'operational' | 'degraded' | 'failed' | 'error' | 'empty'
type ReportedTimelineStatus = Exclude<TimelineStatus, 'empty'>

interface TimelineBar {
  status: TimelineStatus
  point: MonitorTimelinePoint | null
}

/** A finger has to travel this far sideways before a touch turns into scrubbing. */
const SCRUB_START_PX = 6
/** Focus that lands this soon after a press came from the press, not the keyboard. */
const POINTER_FOCUS_WINDOW_MS = 800

const CHECK_TIME_FORMAT: Intl.DateTimeFormatOptions = {
  month: '2-digit',
  day: '2-digit',
  hour: '2-digit',
  minute: '2-digit',
  second: '2-digit',
  hour12: false,
}

const props = withDefaults(defineProps<{
  buckets?: MonitorTimelinePoint[]
  length?: number
  countdownSeconds?: number
  maintenance?: boolean
}>(), {
  buckets: () => [],
  length: MONITOR_TIMELINE_POINTS,
  countdownSeconds: 0,
  maintenance: false,
})

const { t } = useI18n()
const { statusLabel, formatRelativeTime } = useChannelMonitorFormat()
const tooltipId = useId()

const safeLength = computed(() => Math.max(1, Math.floor(props.length)))
const realPointCount = computed(() => Math.min(props.buckets.length, safeLength.value))

const timelineAriaLabel = computed(() => {
  const label = t('channelStatus.timelineLabel', { n: realPointCount.value })
  const counts = new Map<ReportedTimelineStatus, number>()
  for (const point of props.buckets.slice(0, safeLength.value)) {
    const status = normalizeStatus(point)
    if (status !== 'empty') counts.set(status, (counts.get(status) ?? 0) + 1)
  }
  if (counts.size === 0) return label

  const summary = [...counts.entries()]
    .map(([status, count]) => `${statusLabel(status)}: ${count}`)
    .join('; ')
  return `${label}. ${summary}`
})

// Oldest on the left, newest on the right; missing history pads the left.
const displayBars = computed<TimelineBar[]>(() => {
  if (props.maintenance) {
    return Array.from({ length: safeLength.value }, () => ({ status: 'empty' as const, point: null }))
  }

  const points = props.buckets.slice(0, safeLength.value).reverse()
  const bars: TimelineBar[] = Array.from(
    { length: safeLength.value - points.length },
    () => ({ status: 'empty', point: null }),
  )
  for (const point of points) {
    bars.push({ status: normalizeStatus(point), point })
  }
  return bars
})

function normalizeStatus(point: MonitorTimelinePoint): TimelineStatus {
  const status = resolveMonitorStatus(point.status, point.latency_ms)
  if (
    status === 'operational' ||
    status === 'degraded' ||
    status === 'failed' ||
    status === 'error'
  ) {
    return status
  }
  return 'empty'
}

// ---------- The check under the pointer, finger or keyboard ----------

const barEl = ref<HTMLElement | null>(null)
const activeIndex = ref<number | null>(null)
const anchor = ref<MonitorCheckAnchor | null>(null)

const activeDetail = computed<MonitorCheckDetail | null>(() => {
  const index = activeIndex.value
  const bar = index === null ? undefined : displayBars.value[index]
  return bar ? describeBar(bar) : null
})

function describeBar(bar: TimelineBar): MonitorCheckDetail {
  if (props.maintenance) return emptyDetail(t('monitorCommon.maintenancePaused'))
  const point = bar.point
  if (!point || bar.status === 'empty') return emptyDetail(t('channelStatus.tooltip.noRecord'))

  const tone: MonitorCheckTone =
    bar.status === 'operational' || bar.status === 'degraded' ? bar.status : 'down'
  const rows: MonitorCheckDetailRow[] = []
  const checkedAt = formatDate(point.checked_at, CHECK_TIME_FORMAT)
  if (checkedAt) rows.push({ label: t('channelStatus.tooltip.checkedAt'), value: checkedAt })

  const latency = formatMonitorLatency(point.latency_ms)
  if (latency) {
    // A failed check never produced a usable first token; its time is only
    // how long the attempt took, so it is labelled and coloured neutrally.
    const answered = tone !== 'down'
    const latencyTone = answered ? monitorLatencyTone(point.latency_ms) : 'unknown'
    rows.push({
      label: t(answered ? 'channelStatus.tooltip.firstToken' : 'channelStatus.tooltip.elapsed'),
      value: latency,
      tone: latencyTone === 'warn' || latencyTone === 'bad' ? latencyTone : undefined,
    })
  }

  return {
    tone,
    title: statusLabel(bar.status),
    hint: bar.status === 'operational' ? '' : t(`channelStatus.tooltip.hint.${bar.status}`),
    ago: formatRelativeTime(point.checked_at),
    rows,
  }
}

function emptyDetail(title: string): MonitorCheckDetail {
  return { tone: 'empty', title, hint: '', ago: '', rows: [] }
}

let hovering = false
let touchId: number | null = null
let touchStartX = 0
let scrubbing = false
let lastPointerDownAt = 0
let listening = false
let measureFrame = 0

/** Map a horizontal position to a segment; a gap counts toward its left neighbour. */
function indexAt(clientX: number): number | null {
  const el = barEl.value
  if (!el) return null
  const rect = el.getBoundingClientRect()
  if (!(rect.width > 0)) return null
  const count = displayBars.value.length
  const index = Math.floor(((clientX - rect.left) / rect.width) * count)
  return Math.min(count - 1, Math.max(0, index))
}

function measureAnchor() {
  const el = barEl.value
  const index = activeIndex.value
  const segment = index === null
    ? undefined
    : el?.querySelectorAll<HTMLElement>('.monitor-status-bar__segment')[index]
  if (!el || !segment) {
    anchor.value = null
    return
  }
  // The active segment grows a little, so the vertical anchor is the bar
  // itself; that keeps the tooltip still while the segment animates.
  const bar = el.getBoundingClientRect()
  const cell = segment.getBoundingClientRect()
  anchor.value = { top: bar.top, bottom: bar.bottom, left: cell.left, width: cell.width }
}

function scheduleMeasure() {
  if (typeof requestAnimationFrame !== 'function') {
    measureAnchor()
    return
  }
  if (measureFrame) return
  measureFrame = requestAnimationFrame(() => {
    measureFrame = 0
    if (activeIndex.value !== null) measureAnchor()
  })
}

function activate(index: number | null) {
  if (index === null) {
    deactivate()
    return
  }
  if (index === activeIndex.value && anchor.value) return
  activeIndex.value = index
  measureAnchor()
  setListening(true)
}

function deactivate() {
  activeIndex.value = null
  anchor.value = null
  setListening(false)
}

function onDocumentPointerDown(event: PointerEvent) {
  if (!barEl.value?.contains(event.target as Node | null)) deactivate()
}

// Global listeners only exist while a tooltip is open.
function setListening(on: boolean) {
  if (on === listening) return
  listening = on
  if (on) {
    document.addEventListener('pointerdown', onDocumentPointerDown, true)
    window.addEventListener('scroll', scheduleMeasure, { capture: true, passive: true })
    window.addEventListener('resize', scheduleMeasure)
  } else {
    document.removeEventListener('pointerdown', onDocumentPointerDown, true)
    window.removeEventListener('scroll', scheduleMeasure, { capture: true })
    window.removeEventListener('resize', scheduleMeasure)
  }
}

function onPointerDown(event: PointerEvent) {
  lastPointerDownAt = Date.now()
  if (event.pointerType !== 'touch') return
  touchId = event.pointerId
  touchStartX = event.clientX
  scrubbing = false
}

function onPointerMove(event: PointerEvent) {
  if (event.pointerType !== 'touch') {
    hovering = true
    activate(indexAt(event.clientX))
    return
  }
  // Sliding a finger along the bar walks through the checks; vertical swipes
  // still scroll the page (touch-action: pan-y) and end in pointercancel.
  if (event.pointerId !== touchId) return
  if (!scrubbing && Math.abs(event.clientX - touchStartX) < SCRUB_START_PX) return
  scrubbing = true
  activate(indexAt(event.clientX))
}

function onPointerUp(event: PointerEvent) {
  if (event.pointerType !== 'touch' || event.pointerId !== touchId) return
  touchId = null
  if (scrubbing) {
    // Keep showing the check the finger stopped on.
    scrubbing = false
    return
  }
  // A tap shows the tapped check; tapping the same one again puts it away.
  const index = indexAt(event.clientX)
  if (index !== null && index === activeIndex.value) deactivate()
  else activate(index)
}

function onPointerCancel(event: PointerEvent) {
  if (event.pointerType !== 'touch') return
  touchId = null
  scrubbing = false
  deactivate()
}

function onPointerLeave(event: PointerEvent) {
  if (event.pointerType === 'touch') return
  hovering = false
  deactivate()
}

function onFocus() {
  // A press focuses the bar too; only keyboard focus opens the newest check.
  if (activeIndex.value !== null || Date.now() - lastPointerDownAt < POINTER_FOCUS_WINDOW_MS) return
  activate(displayBars.value.length - 1)
}

function onBlur() {
  if (!hovering) deactivate()
}

function onKeydown(event: KeyboardEvent) {
  const last = displayBars.value.length - 1
  const recorded = props.maintenance ? 0 : realPointCount.value
  const first = recorded > 0 ? last + 1 - recorded : 0
  const current = activeIndex.value
  let next: number
  switch (event.key) {
    case 'ArrowLeft':
      next = current === null ? last : Math.max(first, current - 1)
      break
    case 'ArrowRight':
      next = current === null ? last : Math.min(last, current + 1)
      break
    case 'Home':
      next = first
      break
    case 'End':
      next = last
      break
    case 'Escape':
      if (current === null) return
      event.preventDefault()
      deactivate()
      return
    default:
      return
  }
  event.preventDefault()
  activate(next)
}

// An auto refresh shifts the history under an open tooltip: keep the same
// slot and re-measure once the new segments are in place.
watch(displayBars, (bars) => {
  if (activeIndex.value === null) return
  if (activeIndex.value > bars.length - 1) activeIndex.value = bars.length - 1
  measureAnchor()
}, { flush: 'post' })

onBeforeUnmount(() => {
  setListening(false)
  if (measureFrame && typeof cancelAnimationFrame === 'function') cancelAnimationFrame(measureFrame)
})
</script>

<style scoped>
.monitor-status-bar {
  position: relative;
  display: grid;
  grid-template-columns: repeat(v-bind(safeLength), minmax(1px, 1fr));
  /* Gaps scale with the bar so narrow and wide cards keep the same rhythm. */
  column-gap: max(1px, 0.35%);
  width: 100%;
  height: 1.375rem;
  border-radius: 3px;
  outline: none;
  touch-action: pan-y pinch-zoom;
  -webkit-tap-highlight-color: transparent;
}

/* A taller invisible hit area keeps the thin bar easy to hover and tap. */
.monitor-status-bar::before {
  position: absolute;
  inset: -0.5rem 0;
  content: '';
}

.monitor-status-bar:focus-visible {
  outline: 2px solid rgb(233 184 36 / 0.75);
  outline-offset: 3px;
}

.monitor-status-bar__segment {
  min-width: 1px;
  height: 100%;
  border-radius: 2px;
  background: var(--monitor-empty);
  transition: opacity 140ms ease, transform 140ms ease;
}

.monitor-status-bar__segment--operational {
  background: var(--monitor-ok);
}

.monitor-status-bar__segment--degraded {
  background: var(--monitor-warn);
}

.monitor-status-bar__segment--failed,
.monitor-status-bar__segment--error {
  background: var(--monitor-down);
}

.monitor-status-bar__segment--empty {
  background: var(--monitor-empty);
}

/* While a check is inspected the rest step back so its segment stands out. */
.has-active .monitor-status-bar__segment {
  opacity: 0.4;
}

.has-active .monitor-status-bar__segment.is-active {
  opacity: 1;
  transform: scaleY(1.24);
}

@media (max-width: 639px) {
  .monitor-status-bar {
    height: 1.25rem;
  }

  .monitor-status-bar__segment {
    border-radius: 1.5px;
  }
}

@media (prefers-reduced-motion: reduce) {
  .monitor-status-bar__segment {
    transition: none;
  }

  .has-active .monitor-status-bar__segment.is-active {
    transform: none;
  }
}
</style>
