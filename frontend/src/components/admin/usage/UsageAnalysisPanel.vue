<template>
  <section class="usage-analysis">
    <header class="usage-analysis-head">
      <div class="min-w-0">
        <h2 class="usage-analysis-title">{{ t('usage.analysis.title') }}</h2>
        <p class="usage-analysis-subtitle">{{ t('usage.analysis.subtitle') }}</p>
      </div>
      <div class="usage-analysis-controls">
        <DateRangePicker
          :start-date="startDate"
          :end-date="endDate"
          @update:start-date="emit('update:startDate', $event)"
          @update:end-date="emit('update:endDate', $event)"
          @change="emit('dateRangeChange', $event)"
        />
        <div class="segment-group" role="radiogroup" :aria-label="t('admin.dashboard.granularity')">
          <button
            v-for="option in granularityOptions"
            :key="option.value"
            type="button"
            role="radio"
            class="usage-analysis-segment"
            :class="granularity === option.value ? 'segment-active' : 'segment-idle'"
            :aria-checked="granularity === option.value"
            :data-testid="`usage-granularity-${option.value}`"
            @click="selectGranularity(option.value)"
          >
            {{ option.label }}
          </button>
        </div>
      </div>
    </header>

    <!-- The chart components bring their own cards; inside this panel they sit flat, split by hairlines. -->
    <div class="usage-analysis-grid">
      <slot />
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import DateRangePicker from '@/components/common/DateRangePicker.vue'

type Granularity = 'day' | 'hour'

const props = defineProps<{
  startDate: string
  endDate: string
  granularity: Granularity
}>()

const emit = defineEmits<{
  'update:startDate': [value: string]
  'update:endDate': [value: string]
  'update:granularity': [value: Granularity]
  dateRangeChange: [range: { startDate: string; endDate: string; preset: string | null }]
  granularityChange: [value: Granularity]
}>()

const { t } = useI18n()

const granularityOptions = computed(() => [
  { value: 'day' as const, label: t('admin.dashboard.day') },
  { value: 'hour' as const, label: t('admin.dashboard.hour') },
])

function selectGranularity(value: Granularity) {
  if (value === props.granularity) return
  emit('update:granularity', value)
  emit('granularityChange', value)
}
</script>

<style scoped>
.usage-analysis {
  min-width: 0;
  padding: 1rem;
  border-radius: 1.5rem;
  background: var(--md-sys-color-surface);
  box-shadow: var(--theme-highlight), var(--theme-elevation-1);
}

.usage-analysis-head {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 0.75rem 1rem;
  margin-bottom: 0.25rem;
}

.usage-analysis-title {
  font-size: 0.9375rem;
  font-weight: 700;
  color: var(--md-sys-color-on-surface);
}

.usage-analysis-subtitle {
  margin-top: 0.125rem;
  font-size: 0.75rem;
  color: var(--md-sys-color-on-surface-variant);
}

.usage-analysis-controls {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 0.5rem;
}

.usage-analysis-segment {
  padding: 0.375rem 0.875rem;
  border-radius: 999px;
  font-size: 0.8125rem;
  transition: background-color 160ms ease, color 160ms ease, box-shadow 160ms ease;
}

/* minmax(0, 1fr): a wide distribution table scrolls inside its own box
 * instead of widening the panel (and the page) on a phone. */
.usage-analysis-grid {
  --usage-hairline: rgb(215 193 106 / 0.3);
  display: grid;
  grid-template-columns: minmax(0, 1fr);
}

.usage-analysis-grid > :deep(*) {
  min-width: 0;
  padding-top: 1.25rem;
  padding-bottom: 1.25rem;
}

.usage-analysis-grid > :deep(* + *) {
  box-shadow: inset 0 1px 0 var(--usage-hairline);
}

.usage-analysis-grid > :deep(:last-child) {
  padding-bottom: 0.25rem;
}

.usage-analysis-grid :deep(.card) {
  padding-left: 0;
  padding-right: 0;
  border-radius: 0;
  background: transparent;
  box-shadow: none;
}

@media (min-width: 768px) {
  .usage-analysis {
    padding: 1.25rem 1.5rem 1.5rem;
  }
}

/* Two columns only when each distribution table keeps room for its numbers. */
@media (min-width: 1536px) {
  .usage-analysis-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }

  .usage-analysis-grid > :deep(*) {
    box-shadow: none;
  }

  .usage-analysis-grid > :deep(:nth-child(odd)) {
    padding-right: 2rem;
  }

  .usage-analysis-grid > :deep(:nth-child(n + 3)) {
    box-shadow: inset 0 1px 0 var(--usage-hairline);
  }

  .usage-analysis-grid > :deep(:nth-child(even)) {
    padding-left: 2rem;
    box-shadow: inset 1px 0 0 var(--usage-hairline);
  }

  .usage-analysis-grid > :deep(:nth-child(even):nth-child(n + 3)) {
    box-shadow: inset 1px 0 0 var(--usage-hairline), inset 0 1px 0 var(--usage-hairline);
  }

  .usage-analysis-grid > :deep(:nth-last-child(-n + 2)) {
    padding-bottom: 0.25rem;
  }
}
</style>
