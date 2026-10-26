<template>
  <AppLayout>
    <div class="space-y-6">
      <div class="flex items-center justify-between gap-3">
        <p class="text-sm text-gray-500 dark:text-gray-400">{{ t('admin.operations.description') }}</p>
        <button type="button" class="btn btn-secondary shrink-0" :disabled="loading" @click="loadData">
          <Icon name="refresh" size="sm" :class="{ 'animate-spin': loading }" />
          <span class="hidden sm:inline">{{ t('admin.operations.refresh') }}</span>
        </button>
      </div>
      <div v-if="error" role="alert" class="flex items-center justify-between gap-3 rounded-xl bg-red-50 p-4 text-sm text-red-600 dark:bg-red-900/20 dark:text-red-400">
        {{ t('admin.operations.failedToLoad') }}
        <button type="button" class="font-medium underline" @click="loadData">{{ t('admin.operations.retry') }}</button>
      </div>
      <div v-if="loading && !data" class="flex justify-center py-20"><LoadingSpinner /></div>
      <template v-if="data">
        <section class="grid grid-cols-2 gap-3 lg:grid-cols-4 lg:gap-4" :aria-label="t('admin.operations.metrics')" data-testid="operations-metrics">
          <article v-for="metric in metrics" :key="metric.key" class="card min-w-0 p-4" :data-metric="metric.key">
            <div class="mb-3 flex items-center justify-between gap-2">
              <p class="text-xs font-medium text-gray-500 dark:text-gray-400">{{ t(metric.label) }}</p>
              <span class="hidden shrink-0 rounded-lg p-1.5 sm:flex" :class="metric.color"><Icon :name="metric.icon" size="sm" /></span>
            </div>
            <div v-if="metric.key === 'today_revenue'" class="flex flex-wrap items-baseline gap-x-3 gap-y-1">
              <p v-for="revenue in revenues" :key="revenue.currency" class="break-all text-lg font-bold tabular-nums text-gray-900 dark:text-white sm:text-xl" :title="revenue.currency">{{ formatMoney(revenue.amount, revenue.currency) }}</p>
            </div>
            <p v-else class="break-all text-lg font-bold tabular-nums text-gray-900 dark:text-white sm:text-xl">{{ formatMoney(data.metrics[metric.key]) }}</p>
          </article>
        </section>

        <section class="space-y-4" :aria-busy="loading">
          <div class="flex flex-wrap items-center justify-between gap-3">
            <div>
              <h2 class="text-base font-semibold text-gray-900 dark:text-white">{{ t('admin.operations.consumptionPanel') }}</h2>
              <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ consumptionRange.start }} — {{ consumptionRange.end }} · USD</p>
            </div>
            <div class="inline-flex rounded-lg bg-gray-100 p-1 dark:bg-dark-800" :aria-label="t('admin.dashboard.timeRange')">
              <button v-for="period in periods" :key="period.days" type="button" class="rounded-md px-3 py-1.5 text-xs font-medium transition-colors"
                :class="selectedDays === period.days ? 'bg-white text-primary-600 shadow-sm dark:bg-dark-700 dark:text-primary-400' : 'text-gray-500 hover:text-gray-800 dark:text-gray-400 dark:hover:text-gray-200'"
                :aria-pressed="selectedDays === period.days" :data-period="period.days" @click="selectedDays = period.days">{{ t(period.label) }}</button>
            </div>
          </div>
          <div class="grid grid-cols-1 items-start gap-4 xl:grid-cols-2">
            <div class="card min-w-0 overflow-hidden">
              <div class="border-b border-gray-100 px-4 py-4 dark:border-dark-700"><h3 class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('admin.operations.groups') }} <span class="ml-1 text-xs font-normal text-gray-400">{{ groups.length }}</span></h3></div>
              <div class="operations-grid border-b border-gray-100 bg-gray-50/80 px-4 py-2.5 text-[11px] font-medium text-gray-500 dark:border-dark-700 dark:bg-dark-800/40 dark:text-gray-400">
                <span>{{ t('admin.operations.groups') }}</span><span class="text-right">{{ t('admin.operations.profit') }}</span><span class="text-right">{{ t('admin.operations.userCost') }}</span><span class="text-right">{{ t('admin.operations.upstreamCost') }}</span>
              </div>
              <button type="button" data-group="all" class="operations-grid w-full border-b border-gray-100 px-4 py-4 text-left transition-colors dark:border-dark-700"
                :class="selectedGroup === null ? 'bg-primary-50/70 dark:bg-primary-900/15' : 'hover:bg-gray-50 dark:hover:bg-dark-800/50'" :aria-pressed="selectedGroup === null" @click="selectedGroup = null">
                <span class="flex min-w-0 items-center gap-2 text-sm font-semibold text-gray-900 dark:text-white"><Icon name="grid" size="sm" class="shrink-0 text-primary-500" /><span>{{ t('admin.operations.allGroups') }}</span></span>
                <span class="money-cell font-semibold" :class="profitClass(profitOf(totals))">{{ formatMoney(profitOf(totals)) }}</span>
                <span class="money-cell text-gray-700 dark:text-gray-300">{{ formatMoney(totals.user_cost) }}</span>
                <span class="money-cell text-gray-500 dark:text-gray-400">{{ formatMoney(totals.upstream_cost) }}</span>
              </button>
              <div class="max-h-[450px] overflow-y-auto">
                <button v-for="group in groups" :key="group.group_id" type="button" class="operations-grid w-full border-b border-gray-100 px-4 py-4 text-left transition-colors last:border-b-0 dark:border-dark-700/70"
                  :data-group="group.group_id" :aria-pressed="selectedGroup === group.group_id" :class="selectedGroup === group.group_id ? 'bg-primary-50/70 dark:bg-primary-900/15' : 'hover:bg-gray-50 dark:hover:bg-dark-800/50'" @click="selectedGroup = group.group_id">
                  <span class="min-w-0"><span class="block break-words text-sm font-medium text-gray-900 dark:text-white">{{ groupName(group) }}</span><span class="mt-1 inline-flex rounded bg-gray-100 px-1.5 py-0.5 text-[10px] font-medium tabular-nums text-gray-500 dark:bg-dark-700 dark:text-gray-400">{{ group.rate_multiplier }}×</span></span>
                  <span class="money-cell font-semibold" :class="profitClass(profitOf(group))">{{ formatMoney(profitOf(group)) }}</span>
                  <span class="money-cell text-gray-700 dark:text-gray-300">{{ formatMoney(group.user_cost) }}</span>
                  <span class="money-cell text-gray-500 dark:text-gray-400">{{ formatMoney(group.upstream_cost) }}</span>
                </button>
                <p v-if="!groups.length" class="px-4 py-10 text-center text-sm text-gray-400">{{ t('admin.operations.noGroups') }}</p>
              </div>
            </div>
            <div class="card min-w-0 overflow-hidden">
              <div class="flex items-center justify-between gap-3 border-b border-gray-100 px-4 py-4 dark:border-dark-700"><h3 class="shrink-0 text-sm font-semibold text-gray-900 dark:text-white">{{ t('admin.operations.models') }} <span class="ml-1 text-xs font-normal text-gray-400">{{ models.length }}</span></h3><span class="truncate rounded-full bg-primary-50 px-2.5 py-0.5 text-xs text-primary-600 dark:bg-primary-900/20 dark:text-primary-400">{{ selectedGroupName }}</span></div>
              <div class="operations-grid border-b border-gray-100 bg-gray-50/80 px-4 py-2.5 text-[11px] font-medium text-gray-500 dark:border-dark-700 dark:bg-dark-800/40 dark:text-gray-400">
                <span>Model ID</span><span class="text-right">{{ t('admin.operations.profit') }}</span><span class="text-right">{{ t('admin.operations.userCost') }}</span><span class="text-right">{{ t('admin.operations.upstreamCost') }}</span>
              </div>
              <div v-if="loading" class="flex min-h-40 justify-center py-16"><LoadingSpinner size="md" /></div>
              <div v-else class="max-h-[518px] overflow-y-auto">
                <div v-for="model in models" :key="model.model" :data-model="model.model" class="operations-grid border-b border-gray-100 px-4 py-4 last:border-b-0 dark:border-dark-700/70">
                  <span class="break-all font-mono text-xs font-medium leading-5 text-gray-800 dark:text-gray-200">{{ model.model }}</span>
                  <span class="money-cell font-semibold" :class="profitClass(profitOf(model))">{{ formatMoney(profitOf(model)) }}</span>
                  <span class="money-cell text-gray-700 dark:text-gray-300">{{ formatMoney(model.user_cost) }}</span>
                  <span class="money-cell text-gray-500 dark:text-gray-400">{{ formatMoney(model.upstream_cost) }}</span>
                </div>
                <p v-if="!models.length" class="px-4 py-16 text-center text-sm text-gray-400">{{ t('admin.operations.noModels') }}</p>
              </div>
            </div>
          </div>
        </section>
        <OperationsTrendChart :points="data.trend" :days="trendDays" />
      </template>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import type { OperationsDashboardResponse, OperationsGroupProfitStat } from '@/api/admin/dashboard'
import AppLayout from '@/components/layout/AppLayout.vue'
import Icon from '@/components/icons/Icon.vue'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import OperationsTrendChart from '@/components/admin/dashboard/OperationsTrendChart.vue'
import { compareProfit, dateRange, formatMoney, profitOf } from './operationsUtils'

const { t } = useI18n()
const data = ref<OperationsDashboardResponse | null>(null)
const loading = ref(false)
const error = ref(false)
const selectedDays = ref(1)
const selectedGroup = ref<number | null>(null)
const trendDays = ref<7 | 15>(window.innerWidth < 768 ? 7 : 15)
let requestSequence = 0
const periods = [{ days: 1, label: 'admin.operations.today' }, { days: 7, label: 'admin.operations.last7Days' }, { days: 30, label: 'admin.operations.last30Days' }]
const metrics = [
  { key: 'today_user_consumption', label: 'admin.operations.todayUserConsumption', icon: 'users', color: 'bg-blue-100 text-blue-600 dark:bg-blue-900/30 dark:text-blue-400' },
  { key: 'today_user_recharge', label: 'admin.operations.todayUserRecharge', icon: 'creditCard', color: 'bg-emerald-100 text-emerald-600 dark:bg-emerald-900/30 dark:text-emerald-400' },
  { key: 'today_monitor_consumption', label: 'admin.operations.todayMonitorConsumption', icon: 'server', color: 'bg-violet-100 text-violet-600 dark:bg-violet-900/30 dark:text-violet-400' },
  { key: 'today_revenue', label: 'admin.operations.todayRevenue', icon: 'trendingUp', color: 'bg-amber-100 text-amber-600 dark:bg-amber-900/30 dark:text-amber-400' },
  { key: 'total_user_consumption', label: 'admin.operations.totalUserConsumption', icon: 'chart', color: 'bg-blue-100 text-blue-600 dark:bg-blue-900/30 dark:text-blue-400' },
  { key: 'total_user_balance', label: 'admin.operations.totalUserBalance', icon: 'creditCard', color: 'bg-cyan-100 text-cyan-600 dark:bg-cyan-900/30 dark:text-cyan-400' },
  { key: 'total_monitor_consumption', label: 'admin.operations.totalMonitorConsumption', icon: 'server', color: 'bg-violet-100 text-violet-600 dark:bg-violet-900/30 dark:text-violet-400' },
  { key: 'admin_balance', label: 'admin.operations.adminBalance', icon: 'shield', color: 'bg-rose-100 text-rose-600 dark:bg-rose-900/30 dark:text-rose-400' }
] as const
const revenues = computed(() => {
  const entries = Object.entries(data.value?.metrics.today_revenue ?? {}).sort(([a], [b]) => a.localeCompare(b))
  return (entries.length ? entries : [['USD', 0] as const]).map(([currency, amount]) => ({ currency, amount }))
})
const consumptionRange = computed(() => dateRange(selectedDays.value))
const groups = computed(() => [...(data.value?.groups ?? [])].sort((a, b) => compareProfit(a, b) || a.group_name.localeCompare(b.group_name)))
const models = computed(() => [...(data.value?.models ?? [])].sort((a, b) => compareProfit(a, b) || a.model.localeCompare(b.model)))
const totals = computed(() => groups.value.reduce((sum, group) => ({ user_cost: sum.user_cost + group.user_cost, upstream_cost: sum.upstream_cost + group.upstream_cost }), { user_cost: 0, upstream_cost: 0 }))
const groupName = (group: OperationsGroupProfitStat) => group.group_id === 0 ? t('admin.operations.unassigned') : group.group_name
const selectedGroupName = computed(() => {
  const group = groups.value.find(item => item.group_id === selectedGroup.value)
  return group ? groupName(group) : t('admin.operations.allGroups')
})
const profitClass = (profit: number) => profit < 0 ? 'text-red-600 dark:text-red-400' : profit > 0 ? 'text-emerald-600 dark:text-emerald-400' : 'text-gray-500 dark:text-gray-400'
async function loadData() {
  const sequence = ++requestSequence
  loading.value = true
  error.value = false
  const range = dateRange(selectedDays.value)
  try {
    const result = await adminAPI.dashboard.getOperationsDashboard({
      start_date: range.start, end_date: range.end, trend_days: trendDays.value,
      timezone: Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC',
      group_id: selectedGroup.value === null ? 0 : selectedGroup.value === 0 ? -1 : selectedGroup.value
    })
    if (sequence !== requestSequence) return
    data.value = result
  } catch (err) {
    if (sequence !== requestSequence) return
    console.error('Failed to load operations dashboard:', err)
    error.value = true
  } finally {
    if (sequence === requestSequence) loading.value = false
  }
}
function onResize() { trendDays.value = window.innerWidth < 768 ? 7 : 15 }
watch([selectedDays, selectedGroup, trendDays], loadData)
onMounted(() => { window.addEventListener('resize', onResize); void loadData() })
onUnmounted(() => { requestSequence++; window.removeEventListener('resize', onResize) })
</script>

<style scoped>
.operations-grid { display: grid; grid-template-columns: minmax(0, 1.45fr) repeat(3, minmax(0, 1fr)); gap: 0.5rem; align-items: center; }
.money-cell { text-align: right; font-size: 0.75rem; line-height: 1.4; font-variant-numeric: tabular-nums; overflow-wrap: anywhere; }
@media (min-width: 1536px) { .operations-grid { gap: 1rem; } .money-cell { font-size: 0.8125rem; } }
</style>
