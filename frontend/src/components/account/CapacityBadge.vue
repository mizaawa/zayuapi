<script setup lang="ts">
defineProps<{
  colorClass: string
  tooltip?: string
  current: string | number
  max: string | number
  maxTitle?: string
  suffix?: string
}>()
</script>

<template>
  <span
    :class="[
      'inline-flex items-center gap-1 rounded-md px-1.5 py-px text-[10px] font-medium leading-tight',
      colorClass
    ]"
    :title="tooltip"
  >
    <slot />
    <span class="font-mono">{{ current }}</span>
    <span class="text-gray-400 dark:text-gray-500">/</span>
    <span
      class="font-mono"
      :class="max === '∞' && 'capacity-infinity'"
      :title="maxTitle"
      :aria-label="maxTitle"
    >{{ max }}</span>
    <span v-if="suffix" class="text-[9px] opacity-60">{{ suffix }}</span>
  </span>
</template>

<style scoped>
.capacity-infinity { font-size: 0.8125rem; font-weight: 700; line-height: 0.75rem; }
</style>
