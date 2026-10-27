<template>
  <section class="card p-4 sm:p-5" data-testid="operations-trend">
    <div class="mb-5 flex flex-wrap items-center justify-between gap-3">
      <div>
        <h2 class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('admin.operations.trend') }}</h2>
        <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ range.start }} — {{ range.end }} · USD</p>
      </div>
      <div class="segment-group">
        <button v-for="mode in modes" :key="String(mode.value)" type="button"
          class="rounded-full px-3 py-1.5 text-xs font-medium transition-colors"
          :class="profitOnly === mode.value ? 'segment-active' : 'segment-idle'"
          :aria-pressed="profitOnly === mode.value" @click="profitOnly = mode.value">{{ t(mode.label) }}</button>
      </div>
    </div>
    <div class="mb-4 flex flex-wrap items-center gap-x-5 gap-y-2 text-xs">
      <button v-for="(series, index) in seriesDefinitions" :key="series.key" type="button"
        class="inline-flex items-center gap-2 transition-opacity"
        :class="profitOnly && index !== 2 ? 'opacity-35' : 'opacity-100'"
        :data-series="series.key" :aria-pressed="!profitOnly || index === 2"
        @click="profitOnly = index === 2">
        <span class="h-1 w-5 rounded-full" :style="{ backgroundColor: series.color }"></span>
        <span class="text-gray-600 dark:text-gray-300">{{ t(series.label) }}</span>
      </button>
    </div>
    <div class="relative h-64 sm:h-80">
      <Line :data="chartData" :options="chartOptions" />
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useMutationObserver } from '@vueuse/core'
import { Chart as ChartJS, CategoryScale, LinearScale, PointElement, LineElement, Tooltip, Legend, type ChartOptions } from 'chart.js'
import { Line } from 'vue-chartjs'
import type { OperationsProfitTrendPoint } from '@/api/admin/dashboard'
import { dateRange, formatMoney, localDate } from '@/views/admin/operationsUtils'

ChartJS.register(CategoryScale, LinearScale, PointElement, LineElement, Tooltip, Legend)
const props = defineProps<{ points: OperationsProfitTrendPoint[]; days: number }>()
const { t } = useI18n()
const profitOnly = ref(false)
const dark = ref(document.documentElement.classList.contains('dark'))
useMutationObserver(document.documentElement, () => { dark.value = document.documentElement.classList.contains('dark') }, { attributes: true, attributeFilter: ['class'] })
const modes = [{ value: false, label: 'admin.operations.defaultView' }, { value: true, label: 'admin.operations.profitOnly' }]
const seriesDefinitions = [
  { key: 'upstream_cost', label: 'admin.operations.upstreamCost', color: '#5b8fb9' },
  { key: 'user_cost', label: 'admin.operations.userCost', color: '#5c9865' },
  { key: 'profit', label: 'admin.operations.profit', color: '#d99a00' }
] as const
const range = computed(() => dateRange(props.days))
const points = computed(() => {
  const byDate = new Map(props.points.map(point => [point.date, point]))
  const today = new Date()
  return Array.from({ length: props.days }, (_, index) => {
    const date = new Date(today)
    date.setDate(date.getDate() - props.days + index + 1)
    const key = localDate(date)
    return byDate.get(key) ?? { date: key, upstream_cost: 0, user_cost: 0, profit: 0 }
  })
})
const chartData = computed(() => ({
  labels: points.value.map(point => point.date.slice(5).replace('-', '/')),
  datasets: seriesDefinitions.map((series, index) => ({
    label: t(series.label),
    data: points.value.map(point => point[series.key]),
    borderColor: series.color,
    backgroundColor: series.color,
    hidden: profitOnly.value && index !== 2,
    pointRadius: 2,
    pointHoverRadius: 5,
    pointHitRadius: 14,
    borderWidth: 2.5,
    tension: 0.25
  }))
}))
const chartOptions = computed<ChartOptions<'line'>>(() => ({
  responsive: true, maintainAspectRatio: false,
  interaction: { intersect: false, mode: 'nearest' },
  onClick: (_event, elements) => {
    if (elements[0]?.datasetIndex === 2) profitOnly.value = true
  },
  plugins: {
    legend: { display: false },
    tooltip: {
      padding: 12,
      backgroundColor: 'rgba(63, 52, 32, 0.92)',
      cornerRadius: 12,
      boxPadding: 4,
      callbacks: {
        title: items => points.value[items[0]?.dataIndex ?? 0]?.date ?? '',
        label: item => `${item.dataset.label}: ${formatMoney(Number(item.parsed.y ?? 0))}`,
        footer: items => {
          if (items[0]?.datasetIndex !== 2) return ''
          const point = points.value[items[0].dataIndex]
          const rate = point && point.user_cost > 0 ? `${(point.profit / point.user_cost * 100).toFixed(2)}%` : '—'
          return `${t('admin.operations.profitRate')}: ${rate}`
        }
      }
    }
  },
  scales: {
    x: { grid: { display: false }, ticks: { color: dark.value ? '#9ca3af' : '#716744', maxRotation: 0, autoSkip: false, font: { size: 11 } }, border: { display: false } },
    y: { grid: { color: dark.value ? '#1f2937' : 'rgba(215, 202, 162, 0.35)' }, ticks: { color: dark.value ? '#9ca3af' : '#716744', font: { size: 11 }, callback: value => formatMoney(Number(value)) }, border: { display: false } }
  }
}))
</script>
