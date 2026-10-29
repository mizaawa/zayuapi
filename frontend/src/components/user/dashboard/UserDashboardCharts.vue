<template>
  <section class="dash-charts">
    <header class="dash-charts-head">
      <h2 class="dash-charts-title">{{ t('dashboard.usageTrends') }}</h2>
      <div class="dash-charts-controls">
        <DateRangePicker
          :start-date="startDate"
          :end-date="endDate"
          @update:startDate="$emit('update:startDate', $event)"
          @update:endDate="$emit('update:endDate', $event)"
          @change="$emit('dateRangeChange', $event)"
        />
        <div class="segment-group" role="radiogroup" :aria-label="t('dashboard.granularity')">
          <button
            v-for="option in granularityOptions"
            :key="option.value"
            type="button"
            role="radio"
            class="dash-segment"
            :class="granularity === option.value ? 'segment-active' : 'segment-idle'"
            :aria-checked="granularity === option.value"
            @click="selectGranularity(option.value)"
          >
            {{ option.label }}
          </button>
        </div>
        <button
          type="button"
          class="btn btn-secondary dash-charts-refresh"
          :disabled="loading"
          :title="t('common.refresh')"
          :aria-label="t('common.refresh')"
          @click="$emit('refresh')"
        >
          <Icon name="refresh" size="sm" :class="loading ? 'animate-spin' : ''" />
        </button>
      </div>
    </header>

    <div class="dash-charts-body">
      <!-- Model ranking: one hue for magnitude; identity comes from the name and logo. -->
      <div class="dash-models">
        <div class="dash-models-head">
          <h3 class="dash-chart-title">{{ t('dashboard.modelDistribution') }}</h3>
          <span class="dash-models-unit">{{ t('dashboard.byTokens') }}</span>
        </div>
        <div v-if="loading && rankedModels.length === 0" class="dash-chart-placeholder">
          <LoadingSpinner size="md" />
        </div>
        <ol v-else-if="rankedModels.length > 0" class="dash-model-list" :class="{ 'is-refreshing': loading }">
          <li v-for="item in rankedModels" :key="item.model" class="dash-model">
            <div class="dash-model-head">
              <span class="dash-model-name">
                <ModelIcon :model="item.model" size="16px" />
                <span class="truncate" :title="item.model">{{ item.model }}</span>
              </span>
              <span class="dash-model-tokens">{{ formatTokens(item.total_tokens) }}</span>
            </div>
            <div class="dash-model-bar" aria-hidden="true">
              <span class="dash-model-bar-fill" :style="{ width: item.share + '%' }"></span>
            </div>
            <div class="dash-model-meta">
              <span>{{ t('dashboard.requestCount', { count: formatNumber(item.requests) }) }}</span>
              <span class="dash-model-cost">
                <span :title="t('dashboard.actual')">${{ formatCost(item.actual_cost) }}</span>
                <span
                  v-if="item.cost > item.actual_cost"
                  class="dash-model-list-price"
                  :title="t('dashboard.standard')"
                >${{ formatCost(item.cost) }}</span>
              </span>
            </div>
          </li>
        </ol>
        <p v-else class="dash-chart-placeholder">{{ t('dashboard.noDataAvailable') }}</p>
      </div>

      <div class="dash-trend">
        <TokenUsageTrend :trend-data="trend" :loading="loading" :title="t('dashboard.tokenBreakdownTrend')" />
      </div>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import DateRangePicker from '@/components/common/DateRangePicker.vue'
import ModelIcon from '@/components/common/ModelIcon.vue'
import TokenUsageTrend from '@/components/charts/TokenUsageTrend.vue'
import type { TrendDataPoint, ModelStat } from '@/types'
import { formatCostFixed as formatCost, formatNumberLocaleString as formatNumber, formatTokensK as formatTokens } from '@/utils/format'

const props = defineProps<{ loading: boolean, startDate: string, endDate: string, granularity: string, trend: TrendDataPoint[], models: ModelStat[] }>()
const emit = defineEmits(['update:startDate', 'update:endDate', 'update:granularity', 'dateRangeChange', 'granularityChange', 'refresh'])
const { t } = useI18n()

const granularityOptions = computed(() => [
  { value: 'day', label: t('dashboard.day') },
  { value: 'hour', label: t('dashboard.hour') }
])

function selectGranularity(value: string) {
  if (value === props.granularity) return
  emit('update:granularity', value)
  emit('granularityChange')
}

const rankedModels = computed(() => {
  const models = [...(props.models ?? [])].sort((a, b) => b.total_tokens - a.total_tokens)
  const total = models.reduce((sum, item) => sum + Math.max(item.total_tokens, 0), 0)
  return models.map((item) => ({
    ...item,
    share: total > 0 ? (Math.max(item.total_tokens, 0) / total) * 100 : 0
  }))
})
</script>

<style scoped>
.dash-charts {
  padding: 1.25rem;
  border-radius: 1.5rem;
  background: var(--md-sys-color-surface);
  box-shadow: var(--theme-highlight), var(--theme-elevation-1);
}

.dash-charts-head {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 0.75rem;
  margin-bottom: 1.25rem;
}

.dash-charts-title {
  font-size: 0.9375rem;
  font-weight: 700;
  color: var(--md-sys-color-on-surface);
}

.dash-charts-controls {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 0.5rem;
}

.dash-segment {
  padding: 0.375rem 0.875rem;
  border-radius: 999px;
  font-size: 0.8125rem;
  transition: background-color 160ms ease, color 160ms ease, box-shadow 160ms ease;
}

.dash-charts-refresh {
  padding: 0.5rem 0.625rem;
  border-radius: 0.875rem;
}

.dash-charts-body {
  display: grid;
  gap: 1.5rem;
}

.dash-chart-title {
  font-size: 0.875rem;
  font-weight: 600;
  color: var(--md-sys-color-on-surface);
}

.dash-models {
  display: flex;
  min-width: 0;
  flex-direction: column;
  gap: 0.875rem;
}

.dash-models-head {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: 0.5rem;
}

.dash-models-unit {
  font-size: 0.75rem;
  color: var(--md-sys-color-on-surface-variant);
}

.dash-chart-placeholder {
  display: flex;
  height: 12rem;
  align-items: center;
  justify-content: center;
  font-size: 0.875rem;
  color: var(--md-sys-color-on-surface-variant);
}

.dash-model-list {
  display: flex;
  max-height: 16rem;
  flex-direction: column;
  gap: 0.875rem;
  overflow-y: auto;
  padding-right: 0.25rem;
  transition: opacity 200ms ease;
}

.dash-model-list.is-refreshing {
  opacity: 0.55;
}

.dash-model {
  display: flex;
  flex-direction: column;
  gap: 0.3125rem;
}

.dash-model-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 0.75rem;
}

.dash-model-name {
  display: inline-flex;
  min-width: 0;
  align-items: center;
  gap: 0.5rem;
  font-size: 0.8125rem;
  font-weight: 600;
  color: var(--md-sys-color-on-surface);
}

.dash-model-tokens {
  flex-shrink: 0;
  font-size: 0.8125rem;
  font-weight: 650;
  font-variant-numeric: tabular-nums;
  color: var(--md-sys-color-on-surface);
}

/* Same-ramp track keeps the bar readable as "share of the whole". */
.dash-model-bar {
  height: 0.375rem;
  overflow: hidden;
  border-radius: 999px;
  background: #e4f0df;
}

.dash-model-bar-fill {
  display: block;
  min-width: 2px;
  height: 100%;
  border-radius: inherit;
  background: #5c9865;
  transition: width 300ms ease;
}

.dash-model-meta {
  display: flex;
  justify-content: space-between;
  gap: 0.75rem;
  font-size: 0.75rem;
  font-variant-numeric: tabular-nums;
  color: var(--md-sys-color-on-surface-variant);
}

.dash-model-cost {
  display: inline-flex;
  gap: 0.375rem;
}

.dash-model-list-price {
  text-decoration: line-through;
  opacity: 0.7;
}

/* The shared trend chart brings its own card; inside this panel it should sit flat. */
.dash-trend :deep(.card) {
  padding: 0;
  border-radius: 0;
  background: transparent;
  box-shadow: none;
}

@media (min-width: 768px) {
  .dash-charts {
    padding: 1.5rem;
  }
}

@media (min-width: 1024px) {
  .dash-charts-body {
    grid-template-columns: minmax(0, 0.9fr) minmax(0, 1.1fr);
    gap: 1.5rem;
  }

  .dash-trend {
    padding-left: 1.5rem;
    box-shadow: inset 1px 0 0 rgb(215 193 106 / 0.3);
  }
}
</style>
