<template>
  <!-- Teleported so the provider cards' overflow clipping never cuts it off. -->
  <Teleport to="body">
    <div
      v-if="detail && anchor"
      :id="id"
      ref="tipEl"
      role="tooltip"
      class="monitor-check-tip"
      :class="[`is-${detail.tone}`, `is-${position.placement}`, { 'is-placed': placed }]"
      :style="tipStyle"
      data-testid="monitor-check-tooltip"
    >
      <div class="monitor-check-tip__head">
        <span class="monitor-check-tip__status">
          <span class="monitor-check-tip__dot" aria-hidden="true"></span>
          {{ detail.title }}
        </span>
        <span v-if="detail.hint" class="monitor-check-tip__hint">{{ detail.hint }}</span>
        <span v-if="detail.ago" class="monitor-check-tip__ago">{{ detail.ago }}</span>
      </div>
      <dl v-if="detail.rows.length" class="monitor-check-tip__rows">
        <div v-for="row in detail.rows" :key="row.label" class="monitor-check-tip__row">
          <dt>{{ row.label }}</dt>
          <dd :class="row.tone ? `is-${row.tone}` : undefined">{{ row.value }}</dd>
        </div>
      </dl>
    </div>
  </Teleport>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { getAnchoredTooltipPosition, type AnchoredTooltipPosition } from '@/utils/floatingPanel'
import type { MonitorCheckAnchor, MonitorCheckDetail } from './types'

const props = defineProps<{
  id: string
  detail: MonitorCheckDetail | null
  anchor: MonitorCheckAnchor | null
}>()

const tipEl = ref<HTMLElement | null>(null)
const placed = ref(false)
const position = ref<AnchoredTooltipPosition>({ top: 0, left: 0, placement: 'top', arrowLeft: 0 })

const tipStyle = computed(() => ({
  top: `${position.value.top}px`,
  left: `${position.value.left}px`,
  '--tip-arrow-left': `${position.value.arrowLeft}px`,
}))

// Measure once the content has rendered: the box is centred on the segment,
// kept inside the viewport and flipped below the bar when there is no room above.
watch(
  () => [props.detail, props.anchor] as const,
  ([detail, anchor]) => {
    const el = tipEl.value
    if (!detail || !anchor || !el) {
      placed.value = false
      return
    }
    position.value = getAnchoredTooltipPosition(
      anchor,
      { width: el.offsetWidth, height: el.offsetHeight },
      document.documentElement.clientWidth || window.innerWidth,
      window.innerHeight,
    )
    placed.value = true
  },
  { flush: 'post', immediate: true },
)
</script>

<style scoped>
.monitor-check-tip {
  position: fixed;
  z-index: 9999;
  width: max-content;
  min-width: 10.5rem;
  max-width: min(18rem, calc(100vw - 1rem));
  padding: 0.5625rem 0.75rem 0.625rem;
  border-radius: 0.875rem;
  background: #2b261c;
  color: #fff8e6;
  font-size: 0.75rem;
  line-height: 1.125rem;
  box-shadow: 0 12px 28px -14px rgb(43 38 28 / 0.6), 0 2px 6px rgb(43 38 28 / 0.2);
  pointer-events: none;
  opacity: 0;
  transform: translateY(3px);
  transition: opacity 120ms ease, transform 160ms var(--theme-ease-out, ease-out);
}

.monitor-check-tip.is-bottom {
  transform: translateY(-3px);
}

.monitor-check-tip.is-placed {
  opacity: 1;
  transform: none;
}

/* Arrow: a rotated square that always points at the hovered segment. */
.monitor-check-tip::after {
  position: absolute;
  left: var(--tip-arrow-left, 50%);
  width: 0.625rem;
  height: 0.625rem;
  margin-left: -0.3125rem;
  border-radius: 0.125rem;
  background: inherit;
  content: '';
  transform: rotate(45deg);
}

.monitor-check-tip.is-top::after {
  bottom: -0.25rem;
}

.monitor-check-tip.is-bottom::after {
  top: -0.25rem;
}

.monitor-check-tip__head {
  display: flex;
  align-items: baseline;
  gap: 0.5rem;
}

.monitor-check-tip__status {
  display: inline-flex;
  flex-shrink: 0;
  align-items: center;
  gap: 0.375rem;
  font-size: 0.8125rem;
  font-weight: 700;
}

.monitor-check-tip__dot {
  width: 0.5rem;
  height: 0.5rem;
  flex-shrink: 0;
  border-radius: 999px;
  background: var(--monitor-empty);
  box-shadow: 0 0 0 2px rgb(255 248 230 / 0.16);
}

.is-operational .monitor-check-tip__dot { background: var(--monitor-ok); }
.is-degraded .monitor-check-tip__dot { background: var(--monitor-warn); }
.is-down .monitor-check-tip__dot { background: var(--monitor-down); }

.monitor-check-tip__hint {
  min-width: 0;
  color: #e6d9b3;
}

.monitor-check-tip__ago {
  margin-left: auto;
  padding-left: 0.5rem;
  white-space: nowrap;
  font-variant-numeric: tabular-nums;
  color: #bfb393;
}

.monitor-check-tip__rows {
  display: grid;
  gap: 0.1875rem;
  margin-top: 0.5rem;
  padding-top: 0.4375rem;
  border-top: 1px solid rgb(255 248 230 / 0.12);
}

.monitor-check-tip__row {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: 1rem;
}

.monitor-check-tip__row dt {
  color: #bfb393;
}

.monitor-check-tip__row dd {
  white-space: nowrap;
  font-weight: 650;
  font-variant-numeric: tabular-nums;
}

.monitor-check-tip__row dd.is-warn { color: #ffc65c; }
.monitor-check-tip__row dd.is-bad { color: #ff8f7f; }

@media (prefers-reduced-motion: reduce) {
  .monitor-check-tip,
  .monitor-check-tip.is-bottom {
    transform: none;
    transition: none;
  }
}
</style>
