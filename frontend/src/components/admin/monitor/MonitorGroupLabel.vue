<template>
  <span class="inline-flex min-w-0 max-w-full items-center gap-1.5">
    <span class="truncate font-medium text-gray-900 dark:text-gray-100">{{ name }}</span>
    <span
      v-if="rateMultiplier != null"
      data-testid="monitor-group-rate"
      class="flex-none rounded-md bg-gray-100 px-1.5 py-0.5 text-[11px] font-medium text-gray-600 dark:bg-dark-600 dark:text-gray-300"
    >
      {{ rateMultiplier }}x
    </span>
    <span
      v-if="platformLabel"
      class="flex-none text-[11px] text-gray-400 dark:text-gray-500"
    >
      {{ platformLabel }}
    </span>
  </span>
</template>

<script setup lang="ts">
defineProps<{
  name: string
  rateMultiplier?: number | null
  platformLabel?: string
}>()
</script>
