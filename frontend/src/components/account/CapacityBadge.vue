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
  <!-- Current usage leads; the limit stays secondary, the same reading as the group live pill. -->
  <span
    :class="['capacity-pill', colorClass]"
    :title="tooltip"
  >
    <slot />
    <span class="capacity-current">{{ current }}</span>
    <span
      class="capacity-max"
      :title="maxTitle"
      :aria-label="maxTitle"
    >/{{ max }}</span>
    <span v-if="suffix" class="capacity-suffix">{{ suffix }}</span>
  </span>
</template>

<style scoped>
.capacity-pill {
  display: inline-flex;
  align-items: center;
  gap: 0.3125rem;
  padding: 0.125rem 0.5rem 0.125rem 0.4375rem;
  border-radius: 999px;
  font-size: 0.6875rem;
  line-height: 1.125rem;
  white-space: nowrap;
  font-variant-numeric: tabular-nums;
}

.capacity-pill :deep(svg) {
  width: 0.6875rem;
  height: 0.6875rem;
  flex-shrink: 0;
  opacity: 0.8;
}

.capacity-current {
  font-size: 0.75rem;
  font-weight: 700;
}

.capacity-max {
  margin-left: -0.1875rem;
  font-weight: 500;
  opacity: 0.6;
}

.capacity-suffix {
  font-size: 0.625rem;
  opacity: 0.6;
}
</style>
