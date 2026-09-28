<template>
  <div class="grid grid-cols-2 gap-4 lg:grid-cols-4" data-testid="dashboard-financial-stats">
    <div v-for="metric in metrics" :key="metric.key" class="card min-w-0 p-4" :data-metric="metric.key">
      <div class="flex h-full flex-col gap-2 sm:flex-row sm:items-center sm:gap-3">
        <div class="w-fit shrink-0 rounded-lg p-2" :class="metric.color">
          <svg
            class="h-5 w-5"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            stroke-width="2"
            stroke-linecap="round"
            stroke-linejoin="round"
            aria-hidden="true"
          >
            <path :d="metric.icon" />
          </svg>
        </div>
        <div class="min-w-0">
          <p class="text-xs font-medium text-gray-500 dark:text-gray-400">
            {{ t(`admin.dashboard.${metric.label}`) }}
          </p>
          <p
            class="break-all text-xl font-bold tabular-nums text-gray-900 dark:text-white"
            :title="formatAmount(stats[metric.key], false)"
            :aria-label="formatAmount(stats[metric.key], false)"
          >
            {{ formatAmount(stats[metric.key], true) }}
          </p>
          <p class="text-xs text-gray-500 dark:text-gray-400">
            {{ t(`admin.dashboard.${metric.label}Desc`) }}
          </p>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type { DashboardStats } from '@/types'

defineProps<{
  stats: Pick<DashboardStats, 'today_actual_cost' | 'total_consumption' | 'total_balance' | 'total_recharged'>
}>()

const { t } = useI18n()

const metrics = [
  {
    key: 'today_actual_cost',
    label: 'todayCost',
    color: 'bg-amber-100 text-amber-600 dark:bg-amber-900/30 dark:text-amber-400',
    icon: 'M8 3v4m8-4v4M4 10h16M6 5h12a2 2 0 0 1 2 2v12a2 2 0 0 1-2 2H6a2 2 0 0 1-2-2V7a2 2 0 0 1 2-2m8 9h-3a1 1 0 0 0 0 2h2a1 1 0 0 1 0 2h-3m2-5v6'
  },
  {
    key: 'total_consumption',
    label: 'totalCost',
    color: 'bg-rose-100 text-rose-600 dark:bg-rose-900/30 dark:text-rose-400',
    icon: 'M5 3h14v18l-3-2-4 2-4-2-3 2V3m4 5h6m-6 4h6m-6 4h3'
  },
  {
    key: 'total_balance',
    label: 'totalBalance',
    color: 'bg-sky-100 text-sky-600 dark:bg-sky-900/30 dark:text-sky-400',
    icon: 'M19 8V5a1 1 0 0 0-1.2-1L5 6a2 2 0 0 0-2 2v10a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2V10a2 2 0 0 0-2-2H5m16 4h-5a2 2 0 0 0 0 4h5m-4-2h.01'
  },
  {
    key: 'total_recharged',
    label: 'totalRecharged',
    color: 'bg-emerald-100 text-emerald-600 dark:bg-emerald-900/30 dark:text-emerald-400',
    icon: 'M12 3v10m-4-4 4 4 4-4M5 11H4a1 1 0 0 0-1 1v7a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2v-7a1 1 0 0 0-1-1h-1M3 16h5l2 2h4l2-2h5'
  }
] as const

const fullAmount = new Intl.NumberFormat('en-US', {
  style: 'currency',
  currency: 'USD',
  minimumFractionDigits: 2,
  maximumFractionDigits: 2
})
const compactAmount = new Intl.NumberFormat('en-US', {
  style: 'currency',
  currency: 'USD',
  notation: 'compact',
  minimumFractionDigits: 2,
  maximumFractionDigits: 2
})

const formatAmount = (value: number | undefined | null, compact: boolean): string => {
  if (value == null || !Number.isFinite(value)) return '--'
  return (compact ? compactAmount : fullAmount).format(value)
}
</script>
