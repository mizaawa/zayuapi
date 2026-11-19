<template>
  <span v-if="speed" class="usage-chip mt-1 inline-flex items-center gap-1 whitespace-nowrap bg-sky-50 text-sky-800 ring-1 ring-inset ring-sky-200 dark:bg-sky-950 dark:text-sky-200 dark:ring-sky-800" data-testid="usage-speed-badge">
    <span>{{ speed }}</span><span class="tabular-nums">×{{ multiplier }}</span>
  </span>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { normalizeUsageServiceTier } from '@/utils/usageServiceTier'

const props = defineProps<{ tier?: string | null; multiplier?: number | null }>()
const speed = computed(() => {
  if (typeof props.multiplier !== 'number' || !(props.multiplier >= 0.1 && props.multiplier <= 100)) return null
  const tier = normalizeUsageServiceTier(props.tier)
  return tier === 'priority' ? 'Fast' : tier === 'ultrafast' ? 'UltraFast' : null
})
</script>
