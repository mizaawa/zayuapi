<template>
  <span
    class="group-concurrency"
    :class="toneClass"
    :title="tooltip"
    data-testid="group-concurrency"
  >
    <svg class="group-concurrency-icon" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2" aria-hidden="true">
      <path stroke-linecap="round" stroke-linejoin="round" d="M3.75 6A2.25 2.25 0 016 3.75h2.25A2.25 2.25 0 0110.5 6v2.25a2.25 2.25 0 01-2.25 2.25H6a2.25 2.25 0 01-2.25-2.25V6zM3.75 15.75A2.25 2.25 0 016 13.5h2.25a2.25 2.25 0 012.25 2.25V18a2.25 2.25 0 01-2.25 2.25H6A2.25 2.25 0 013.75 18v-2.25zM13.5 6a2.25 2.25 0 012.25-2.25H18A2.25 2.25 0 0120.25 6v2.25A2.25 2.25 0 0118 10.5h-2.25a2.25 2.25 0 01-2.25-2.25V6zM13.5 15.75a2.25 2.25 0 012.25-2.25H18a2.25 2.25 0 012.25 2.25V18A2.25 2.25 0 0118 20.25h-2.25A2.25 2.25 0 0113.5 18v-2.25z" />
    </svg>
    <span class="group-concurrency-label">{{ t('admin.groups.concurrencyLabel') }}</span>
    <span class="group-concurrency-value" :class="{ 'group-concurrency-infinite': concurrencyUnlimited }">{{ display }}</span>
  </span>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

// Shows a group's concurrency ceiling: the sum of its accounts' limits, or ∞
// when any account in the group has no limit.
const props = withDefaults(defineProps<{
  concurrencyUsed?: number
  concurrencyMax?: number
  concurrencyUnlimited?: boolean
}>(), {
  concurrencyUsed: 0,
  concurrencyMax: 0,
  concurrencyUnlimited: false
})

const { t } = useI18n()

const display = computed(() => (props.concurrencyUnlimited ? '∞' : String(props.concurrencyMax)))

const toneClass = computed(() => {
  if (!props.concurrencyUnlimited && props.concurrencyMax > 0 && props.concurrencyUsed >= props.concurrencyMax) {
    return 'group-concurrency-full'
  }
  if (props.concurrencyUsed > 0) return 'group-concurrency-busy'
  return 'group-concurrency-idle'
})

const tooltip = computed(() => t('admin.groups.concurrencyTooltip', { used: props.concurrencyUsed, max: display.value }))
</script>

<style scoped>
.group-concurrency { display: inline-flex; align-items: center; gap: 0.3rem; padding: 0.25rem 0.6rem 0.25rem 0.5rem; border-radius: 999px; font-size: 0.75rem; line-height: 1rem; white-space: nowrap; box-shadow: inset 0 0 0 1px rgb(63 52 32 / 0.06); }
.group-concurrency-icon { width: 0.75rem; height: 0.75rem; flex-shrink: 0; opacity: 0.75; }
.group-concurrency-label { font-weight: 500; opacity: 0.85; }
.group-concurrency-value { font-weight: 700; font-variant-numeric: tabular-nums; }
.group-concurrency-infinite { font-size: 0.95rem; line-height: 0.75rem; }
.group-concurrency-idle { background: #fff7d6; color: #65460b; }
.group-concurrency-busy { background: #e4f0df; color: #2e5038; }
.group-concurrency-full { background: #fee2e2; color: #991b1b; }
</style>
