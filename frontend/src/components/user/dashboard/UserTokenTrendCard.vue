<template>
  <section class="dash-tt" data-testid="token-trend-card">
    <header class="dash-tt-head">
      <div class="min-w-0">
        <h2 class="dash-tt-title">{{ t('dashboard.tokenTrend.title') }}</h2>
        <p class="dash-tt-subtitle">{{ t('dashboard.tokenTrend.subtitle') }}</p>
      </div>
      <div class="dash-tt-controls">
        <div class="segment-group" role="radiogroup" :aria-label="t('dashboard.tokenTrend.range')">
          <button
            v-for="option in rangeOptions"
            :key="option.value"
            type="button"
            role="radio"
            class="dash-tt-segment"
            :class="range === option.value ? 'segment-active' : 'segment-idle'"
            :aria-checked="range === option.value"
            :data-testid="`token-trend-range-${option.value}`"
            @click="selectRange(option.value)"
          >
            {{ option.label }}
          </button>
        </div>
        <button
          type="button"
          class="dash-tt-view"
          :class="{ 'is-active': showTable }"
          :aria-pressed="showTable"
          :title="showTable ? t('dashboard.tokenTrend.showChart') : t('dashboard.tokenTrend.showTable')"
          :aria-label="showTable ? t('dashboard.tokenTrend.showChart') : t('dashboard.tokenTrend.showTable')"
          data-testid="token-trend-view-toggle"
          @click="showTable = !showTable"
        >
          <Icon :name="showTable ? 'trendingUp' : 'grid'" size="sm" />
        </button>
      </div>
    </header>

    <div v-if="error" role="alert" class="dash-tt-alert">
      <span>{{ t('dashboard.tokenTrend.loadFailed') }}</span>
      <button type="button" class="btn btn-secondary dash-tt-retry" :title="t('common.refresh')" :aria-label="t('common.refresh')" @click="emit('retry')">
        <Icon name="refresh" size="sm" />
      </button>
    </div>

    <!-- The series tiles double as the legend: each carries its line key, and tapping one hides that line. -->
    <ul class="dash-tt-kpis" :class="{ 'is-refreshing': loading && hasData }">
      <li v-for="series in seriesTiles" :key="series.key" class="dash-tt-kpi-wrap">
        <button
          type="button"
          class="dash-tt-kpi"
          :class="{ 'is-hidden': hiddenSeries.has(series.key) }"
          :aria-pressed="!hiddenSeries.has(series.key)"
          :title="t('dashboard.tokenTrend.seriesToggle', { name: series.label })"
          :data-testid="`token-trend-kpi-${series.key}`"
          @click="toggleSeries(series.key)"
        >
          <span class="dash-tt-kpi-label">
            <span class="dash-tt-key" :style="{ background: series.color }" aria-hidden="true"></span>
            {{ series.label }}
          </span>
          <span class="dash-tt-kpi-value">{{ formatTokens(series.sum) }}</span>
          <span class="dash-tt-kpi-sub">{{ series.hint }}</span>
        </button>
      </li>
      <li class="dash-tt-kpi-wrap">
        <div class="dash-tt-kpi is-static" data-testid="token-trend-kpi-rate">
          <span class="dash-tt-kpi-label">{{ t('dashboard.tokenTrend.hitRate') }}</span>
          <span class="dash-tt-kpi-value">{{ hitRate === null ? '—' : `${hitRate.toFixed(1)}%` }}</span>
          <span class="dash-tt-meter" aria-hidden="true">
            <span class="dash-tt-meter-fill" :style="{ width: (hitRate ?? 0) + '%' }"></span>
          </span>
          <span class="dash-tt-kpi-sub">{{ t('dashboard.tokenTrend.hitRateHint') }}</span>
        </div>
      </li>
    </ul>

    <div v-if="loading && !hasData" class="dash-tt-placeholder">
      <LoadingSpinner size="md" />
    </div>
    <template v-else>
      <div v-if="!showTable" class="dash-tt-plot" :class="{ 'is-refreshing': loading }">
        <Line :data="chartData" :options="chartOptions" />
        <p v-if="totals.total === 0" class="dash-tt-empty">{{ t('dashboard.tokenTrend.empty') }}</p>
      </div>
      <div v-else class="dash-tt-table-wrap" :class="{ 'is-refreshing': loading }">
        <table class="dash-tt-table">
          <thead>
            <tr>
              <th scope="col">{{ t('dashboard.tokenTrend.date') }}</th>
              <th scope="col">{{ t('dashboard.tokenTrend.total') }}</th>
              <th scope="col">{{ t('dashboard.tokenTrend.hit') }}</th>
              <th scope="col">{{ t('dashboard.tokenTrend.miss') }}</th>
              <th scope="col">{{ t('dashboard.tokenTrend.hitRate') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="day in tableRows" :key="day.date">
              <th scope="row">{{ day.date }}</th>
              <td>{{ formatTokens(day.total) }}</td>
              <td>{{ formatTokens(day.hit) }}</td>
              <td>{{ formatTokens(day.miss) }}</td>
              <td>{{ day.rate === null ? '—' : `${day.rate.toFixed(1)}%` }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </template>
  </section>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  Chart as ChartJS,
  CategoryScale,
  LinearScale,
  PointElement,
  LineElement,
  Tooltip,
  Filler
} from 'chart.js'
import { Line } from 'vue-chartjs'
import Icon from '@/components/icons/Icon.vue'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import type { TrendDataPoint } from '@/types'
import { formatCompactNumber } from '@/utils/format'
import { shortenTrendLabel } from '@/utils/chartLabels'

ChartJS.register(CategoryScale, LinearScale, PointElement, LineElement, Tooltip, Filler)

type SeriesKey = 'total' | 'hit' | 'miss'
export type TokenTrendRange = 7 | 30

const props = withDefaults(defineProps<{
  trend: TrendDataPoint[]
  range: TokenTrendRange
  startDate: string
  endDate: string
  loading?: boolean
  error?: boolean
}>(), {
  loading: false,
  error: false
})

const emit = defineEmits<{
  'update:range': [value: TokenTrendRange]
  retry: []
}>()

const { t } = useI18n()

// Validated as a set on the card surface (#fffef8): all pairs clear the CVD and
// normal-vision floors and every line holds >= 3:1 contrast. Cache hits keep the
// cyan family the page already uses for cache reads.
const SERIES_COLORS: Record<SeriesKey, string> = {
  total: '#7e5600',
  hit: '#0891b2',
  miss: '#ea580c'
}

const rangeOptions = computed(() => [
  { value: 7 as const, label: t('dashboard.tokenTrend.last7') },
  { value: 30 as const, label: t('dashboard.tokenTrend.last30') }
])

const showTable = ref(false)
const hiddenSeries = ref(new Set<SeriesKey>())

const selectRange = (value: TokenTrendRange) => {
  if (value !== props.range) emit('update:range', value)
}

const toggleSeries = (key: SeriesKey) => {
  const next = new Set(hiddenSeries.value)
  if (next.has(key)) next.delete(key)
  else next.add(key)
  hiddenSeries.value = next
}

const safe = (value: number | null | undefined) => {
  const numberValue = Number(value)
  return Number.isFinite(numberValue) && numberValue > 0 ? numberValue : 0
}

const pad = (value: number) => String(value).padStart(2, '0')
const toLocalDate = (date: Date) => `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}`

// Every calendar day in the range gets a point, so quiet days read as zero instead of being skipped.
const days = computed(() => {
  const start = new Date(`${props.startDate}T00:00:00`)
  const end = new Date(`${props.endDate}T00:00:00`)
  if (Number.isNaN(start.getTime()) || Number.isNaN(end.getTime()) || start > end) return []
  const byDate = new Map<string, TrendDataPoint>()
  for (const point of props.trend ?? []) byDate.set(String(point.date).slice(0, 10), point)
  const result: Array<{ date: string; total: number; hit: number; miss: number; rate: number | null }> = []
  for (const cursor = new Date(start); cursor <= end && result.length < 366; cursor.setDate(cursor.getDate() + 1)) {
    const date = toLocalDate(cursor)
    const point = byDate.get(date)
    const input = safe(point?.input_tokens)
    const output = safe(point?.output_tokens)
    const cacheWrite = safe(point?.cache_creation_tokens)
    const hit = safe(point?.cache_read_tokens)
    const miss = input + cacheWrite
    const total = Math.max(safe(point?.total_tokens), input + output + cacheWrite + hit)
    result.push({ date, total, hit, miss, rate: hit + miss > 0 ? (hit / (hit + miss)) * 100 : null })
  }
  return result
})

const hasData = computed(() => (props.trend?.length ?? 0) > 0)

const totals = computed(() => days.value.reduce(
  (acc, day) => ({ total: acc.total + day.total, hit: acc.hit + day.hit, miss: acc.miss + day.miss }),
  { total: 0, hit: 0, miss: 0 }
))

const hitRate = computed<number | null>(() => {
  const prompt = totals.value.hit + totals.value.miss
  return prompt > 0 ? (totals.value.hit / prompt) * 100 : null
})

const seriesTiles = computed(() => [
  {
    key: 'total' as const,
    label: t('dashboard.tokenTrend.total'),
    color: SERIES_COLORS.total,
    sum: totals.value.total,
    hint: t('dashboard.tokenTrend.dailyAverage', { value: formatTokens(days.value.length ? totals.value.total / days.value.length : 0) })
  },
  { key: 'hit' as const, label: t('dashboard.tokenTrend.hit'), color: SERIES_COLORS.hit, sum: totals.value.hit, hint: t('dashboard.tokenTrend.hitHint') },
  { key: 'miss' as const, label: t('dashboard.tokenTrend.miss'), color: SERIES_COLORS.miss, sum: totals.value.miss, hint: t('dashboard.tokenTrend.missHint') }
])

const tableRows = computed(() => [...days.value].reverse())

const chartData = computed(() => ({
  labels: days.value.map((day) => day.date),
  datasets: seriesTiles.value.map((series) => ({
    label: series.label,
    data: days.value.map((day) => day[series.key]),
    borderColor: series.color,
    backgroundColor: series.key === 'total' ? 'rgba(126, 86, 0, 0.08)' : series.color,
    fill: series.key === 'total' ? 'origin' : false,
    borderWidth: 2,
    borderCapStyle: 'round' as const,
    borderJoinStyle: 'round' as const,
    tension: 0.3,
    pointRadius: props.range === 7 ? 3 : 0,
    pointHoverRadius: 5,
    pointHitRadius: 14,
    pointBackgroundColor: series.color,
    pointBorderColor: '#fffef8',
    pointBorderWidth: 2,
    hidden: hiddenSeries.value.has(series.key)
  }))
}))

const chartOptions = computed(() => ({
  responsive: true,
  maintainAspectRatio: false,
  animation: { duration: 300 },
  // Room for the first and last date labels, which are centred on the edge points.
  layout: { padding: { left: 6, right: 14 } },
  interaction: { mode: 'index' as const, intersect: false },
  plugins: {
    legend: { display: false },
    tooltip: {
      backgroundColor: '#3f3420',
      titleColor: '#fffef8',
      bodyColor: '#fffef8',
      footerColor: '#ffe78a',
      padding: 10,
      cornerRadius: 10,
      boxPadding: 4,
      usePointStyle: true,
      callbacks: {
        label: (context: { dataset: { label?: string }; raw: unknown }) =>
          `${context.dataset.label}: ${formatTokens(Number(context.raw))}`,
        footer: (items: Array<{ dataIndex: number }>) => {
          const day = days.value[items[0]?.dataIndex ?? -1]
          return day && day.rate !== null ? `${t('dashboard.tokenTrend.hitRate')} ${day.rate.toFixed(1)}%` : ''
        }
      }
    }
  },
  scales: {
    x: {
      grid: { display: false },
      border: { color: '#d7c16a' },
      ticks: {
        color: '#6b6045',
        maxRotation: 0,
        autoSkip: true,
        maxTicksLimit: props.range === 7 ? 7 : 8,
        font: { size: 10 },
        callback(this: { getLabelForValue: (value: number) => string | number }, value: string | number) {
          return shortenTrendLabel(String(this.getLabelForValue(Number(value))))
        }
      }
    },
    y: {
      beginAtZero: true,
      border: { display: false },
      grid: { color: '#eee5c9' },
      ticks: {
        color: '#6b6045',
        maxTicksLimit: 5,
        font: { size: 10 },
        callback: (value: string | number) => formatTokens(Number(value))
      }
    }
  }
}))

function formatTokens(value: number): string {
  return formatCompactNumber(Math.round(value))
}
</script>

<style scoped>
.dash-tt {
  display: flex;
  min-width: 0;
  flex-direction: column;
  gap: 1rem;
  padding: 1.25rem;
  border-radius: 1.5rem;
  background: var(--md-sys-color-surface);
  box-shadow: var(--theme-highlight), var(--theme-elevation-1);
}

.dash-tt-head {
  display: flex;
  flex-wrap: wrap;
  align-items: flex-start;
  justify-content: space-between;
  gap: 0.75rem;
}

.dash-tt-title {
  font-size: 0.9375rem;
  font-weight: 700;
  color: var(--md-sys-color-on-surface);
}

.dash-tt-subtitle {
  margin-top: 0.125rem;
  font-size: 0.75rem;
  color: var(--md-sys-color-on-surface-variant);
}

.dash-tt-controls {
  display: flex;
  align-items: center;
  gap: 0.5rem;
}

.dash-tt-segment {
  padding: 0.375rem 0.875rem;
  border-radius: 999px;
  font-size: 0.8125rem;
  transition: background-color 160ms ease, color 160ms ease, box-shadow 160ms ease;
}

.dash-tt-view {
  display: inline-flex;
  width: 2.25rem;
  height: 2.25rem;
  align-items: center;
  justify-content: center;
  border-radius: 999px;
  background: var(--md-sys-color-surface-container);
  color: var(--md-sys-color-on-surface-variant);
  transition: background-color 160ms ease, color 160ms ease;
}

.dash-tt-view:hover,
.dash-tt-view.is-active {
  background: #ffe78a;
  color: #65460b;
}

.dash-tt-alert {
  display: flex;
  align-items: center;
  gap: 0.75rem;
  padding: 0.5rem 0.625rem 0.5rem 0.875rem;
  border-radius: 0.875rem;
  background: #fef2f2;
  font-size: 0.8125rem;
  color: #b91c1c;
}

.dash-tt-retry {
  margin-left: auto;
  padding: 0.375rem;
  border-radius: 0.625rem;
}

.dash-tt-kpis {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 0.5rem;
  transition: opacity 200ms ease;
}

.dash-tt-kpi-wrap {
  min-width: 0;
}

.dash-tt-kpi {
  display: flex;
  width: 100%;
  height: 100%;
  min-width: 0;
  flex-direction: column;
  align-items: flex-start;
  gap: 0.1875rem;
  padding: 0.75rem 0.875rem;
  border-radius: 1rem;
  background: #fffbe8;
  text-align: left;
  transition: background-color 160ms ease, opacity 160ms ease;
}

button.dash-tt-kpi:hover {
  background: #fff3bd;
}

button.dash-tt-kpi:focus-visible {
  outline: 3px solid color-mix(in srgb, var(--md-sys-color-primary) 45%, transparent);
  outline-offset: 2px;
}

.dash-tt-kpi.is-hidden {
  opacity: 0.5;
}

.dash-tt-kpi.is-hidden .dash-tt-kpi-label {
  text-decoration: line-through;
}

.dash-tt-kpi-label {
  display: inline-flex;
  max-width: 100%;
  align-items: center;
  gap: 0.375rem;
  overflow: hidden;
  white-space: nowrap;
  font-size: 0.75rem;
  font-weight: 500;
  color: var(--md-sys-color-on-surface-variant);
}

/* A short stroke mirrors the plotted line. */
.dash-tt-key {
  width: 0.875rem;
  height: 0.1875rem;
  flex-shrink: 0;
  border-radius: 999px;
}

.dash-tt-kpi-value {
  display: block;
  max-width: 100%;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 1.25rem;
  font-weight: 700;
  line-height: 1.75rem;
  color: var(--md-sys-color-on-surface);
}

.dash-tt-kpi-sub {
  display: -webkit-box;
  max-width: 100%;
  overflow: hidden;
  -webkit-box-orient: vertical;
  -webkit-line-clamp: 2;
  font-size: 0.6875rem;
  line-height: 1rem;
  color: var(--md-sys-color-on-surface-variant);
}

.dash-tt-meter {
  display: block;
  width: 100%;
  height: 0.3125rem;
  overflow: hidden;
  border-radius: 999px;
  background: #d9f0f5;
}

.dash-tt-meter-fill {
  display: block;
  height: 100%;
  border-radius: inherit;
  background: #0891b2;
  transition: width 300ms ease;
}

.dash-tt-placeholder {
  display: flex;
  height: 14rem;
  align-items: center;
  justify-content: center;
}

.dash-tt-plot {
  position: relative;
  height: 14rem;
  min-width: 0;
  transition: opacity 200ms ease;
}

.dash-tt-empty {
  position: absolute;
  inset: 0 0 1.5rem;
  display: flex;
  align-items: center;
  justify-content: center;
  pointer-events: none;
  font-size: 0.8125rem;
  color: var(--md-sys-color-on-surface-variant);
}

.is-refreshing {
  opacity: 0.55;
}

.dash-tt-table-wrap {
  max-height: 16rem;
  overflow: auto;
  border-radius: 1rem;
  background: #fffbe8;
  transition: opacity 200ms ease;
}

.dash-tt-table {
  width: 100%;
  font-size: 0.75rem;
  font-variant-numeric: tabular-nums;
}

.dash-tt-table th,
.dash-tt-table td {
  padding: 0.5rem 0.75rem;
  text-align: right;
  white-space: nowrap;
}

.dash-tt-table thead th {
  position: sticky;
  top: 0;
  background: #fff3bd;
  font-weight: 600;
  color: var(--md-sys-color-on-surface-variant);
}

.dash-tt-table th:first-child {
  text-align: left;
}

.dash-tt-table tbody th {
  font-weight: 500;
  color: var(--md-sys-color-on-surface);
}

.dash-tt-table tbody tr + tr {
  box-shadow: inset 0 1px 0 rgb(215 193 106 / 0.25);
}

.dash-tt-table td {
  color: var(--md-sys-color-on-surface);
}

@media (min-width: 768px) {
  .dash-tt {
    padding: 1.5rem;
  }

  .dash-tt-kpis {
    grid-template-columns: repeat(4, minmax(0, 1fr));
  }

  .dash-tt-plot,
  .dash-tt-placeholder {
    height: 16rem;
  }
}

@media (prefers-reduced-motion: reduce) {
  .dash-tt-meter-fill,
  .dash-tt-kpis,
  .dash-tt-plot {
    transition: none;
  }
}
</style>
