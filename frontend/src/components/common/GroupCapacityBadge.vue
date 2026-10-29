<template>
  <span
    class="group-live"
    :class="toneClass"
    :title="tooltip"
    data-testid="group-concurrency"
  >
    <span class="group-live-dot" aria-hidden="true"></span>
    <span v-if="showLabel" class="group-live-label">{{ t('admin.groups.concurrencyLabel') }}</span>
    <span class="group-live-value" data-testid="group-concurrency-used">{{ concurrencyUsed }}</span>
    <span class="group-live-cap" :class="{ 'group-live-cap-infinite': concurrencyUnlimited }">/{{ capText }}</span>
  </span>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

// Shows how many requests a group's accounts are serving right now. The cap
// (sum of account limits, ∞ when any account is unlimited) stays secondary.
const props = withDefaults(defineProps<{
  concurrencyUsed?: number
  concurrencyMax?: number
  concurrencyUnlimited?: boolean
  showLabel?: boolean
}>(), {
  concurrencyUsed: 0,
  concurrencyMax: 0,
  concurrencyUnlimited: false,
  showLabel: false
})

const { t } = useI18n()

const capText = computed(() => (props.concurrencyUnlimited ? '∞' : String(props.concurrencyMax)))
const hasFiniteCap = computed(() => !props.concurrencyUnlimited && props.concurrencyMax > 0)

const toneClass = computed(() => {
  if (props.concurrencyUsed <= 0) return 'group-live-idle'
  if (hasFiniteCap.value && props.concurrencyUsed >= props.concurrencyMax) return 'group-live-full'
  if (hasFiniteCap.value && props.concurrencyUsed >= props.concurrencyMax * 0.8) return 'group-live-high'
  return 'group-live-busy'
})

const tooltip = computed(() => t('admin.groups.concurrencyTooltip', {
  used: props.concurrencyUsed,
  max: props.concurrencyUnlimited ? t('admin.groups.concurrencyUnlimited') : capText.value
}))
</script>

<style scoped>
.group-live {
  display: inline-flex;
  align-items: center;
  gap: 0.375rem;
  padding: 0.25rem 0.625rem 0.25rem 0.5rem;
  border-radius: 999px;
  font-size: 0.75rem;
  line-height: 1rem;
  white-space: nowrap;
  font-variant-numeric: tabular-nums;
  transition: background-color 240ms ease, color 240ms ease;
}

.group-live-dot {
  position: relative;
  width: 0.4375rem;
  height: 0.4375rem;
  flex-shrink: 0;
  border-radius: 999px;
  background: currentColor;
}

.group-live-label {
  font-weight: 500;
  opacity: 0.8;
}

.group-live-value {
  font-size: 0.875rem;
  font-weight: 700;
}

.group-live-cap {
  margin-left: -0.125rem;
  font-weight: 500;
  opacity: 0.55;
}

.group-live-cap-infinite {
  font-size: 0.875rem;
}

.group-live-idle {
  background: #faf6e8;
  color: #716744;
}

.group-live-idle .group-live-dot {
  opacity: 0.45;
}

.group-live-busy {
  background: #e4f0df;
  color: #2e5038;
}

.group-live-high {
  background: #fef3c7;
  color: #92400e;
}

.group-live-full {
  background: #fee2e2;
  color: #991b1b;
}

/* A soft ring that breathes only while requests are in flight. */
.group-live-busy .group-live-dot::after,
.group-live-high .group-live-dot::after,
.group-live-full .group-live-dot::after {
  content: '';
  position: absolute;
  inset: 0;
  border-radius: inherit;
  background: currentColor;
  animation: group-live-pulse 1.8s ease-out infinite;
}

@keyframes group-live-pulse {
  0% { transform: scale(1); opacity: 0.55; }
  80%, 100% { transform: scale(2.6); opacity: 0; }
}

@media (prefers-reduced-motion: reduce) {
  .group-live-dot::after {
    animation: none !important;
    display: none;
  }
}
</style>
