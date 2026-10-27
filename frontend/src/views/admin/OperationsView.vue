<template>
  <AppLayout>
    <div class="space-y-6">
      <div v-if="error" role="alert" class="flex items-center justify-between gap-3 rounded-xl bg-red-50 p-4 text-sm text-red-600 dark:bg-red-900/20 dark:text-red-400">
        {{ t('admin.operations.failedToLoad') }}
        <button type="button" class="font-medium underline" @click="loadData">{{ t('admin.operations.retry') }}</button>
      </div>
      <div v-if="loading && !data" class="flex justify-center py-20"><LoadingSpinner /></div>
      <template v-if="data">
        <section class="grid grid-cols-2 gap-4 lg:grid-cols-4" :aria-label="t('admin.operations.metrics')" data-testid="operations-metrics">
          <article v-for="metric in metrics" :key="metric.key" class="card min-w-0 p-4" :data-metric="metric.key">
            <div class="flex items-center gap-3">
              <span class="flex shrink-0 rounded-lg p-2" :class="metric.color"><Icon :name="metric.icon" size="md" :stroke-width="2" /></span>
              <div class="min-w-0 flex-1">
                <p class="text-xs font-medium text-gray-500 dark:text-gray-400">{{ t(metric.label) }}</p>
                <div v-if="metric.key === 'today_revenue'" class="flex flex-wrap items-baseline gap-x-2 gap-y-0.5">
                  <p v-for="revenue in revenues" :key="revenue.currency" class="break-all text-xl font-bold tabular-nums text-gray-900 dark:text-white" :title="revenue.currency">{{ formatMoney(revenue.amount, revenue.currency) }}</p>
                </div>
                <p v-else class="break-all text-xl font-bold tabular-nums text-gray-900 dark:text-white">{{ formatMoney(data.metrics[metric.key]) }}</p>
                <p v-if="metric.key === 'today_revenue'" class="mt-1 flex flex-wrap gap-x-2 gap-y-0.5 text-xs text-gray-500 dark:text-gray-400">
                  <span>{{ t('admin.operations.yesterdayRevenue') }}:</span>
                  <span v-for="revenue in revenueComparisons" :key="revenue.currency" class="whitespace-nowrap tabular-nums">
                    {{ formatMoney(revenue.yesterday, revenue.currency) }}
                    <span class="ml-0.5 font-medium" :class="changeClass(revenue.today - revenue.yesterday)">{{ formatChange(revenue.today - revenue.yesterday, revenue.currency) }}</span>
                  </span>
                </p>
                <p v-else class="mt-1 flex flex-wrap gap-x-1.5 text-xs text-gray-500 dark:text-gray-400">
                  <span>{{ t(metric.subLabel) }}: <span class="tabular-nums">{{ formatOptionalMoney(yesterdayValue(metric)) }}</span></span>
                  <span v-if="metric.compare" class="font-medium tabular-nums" :class="changeClass(todayValue(metric) - (yesterdayValue(metric) ?? 0))">
                    {{ formatChange(todayValue(metric) - (yesterdayValue(metric) ?? 0)) }}
                  </span>
                </p>
              </div>
            </div>
          </article>
        </section>

        <section class="space-y-6" :aria-busy="loading">
          <div class="card flex flex-wrap items-center justify-between gap-4 p-4">
            <div>
              <h2 class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('admin.operations.consumptionPanel') }}</h2>
              <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ consumptionRange.start }} — {{ consumptionRange.end }} · USD</p>
            </div>
            <div class="flex flex-wrap items-center gap-2">
              <div class="inline-flex rounded-lg bg-gray-100 p-1 dark:bg-dark-800" :aria-label="t('admin.dashboard.timeRange')">
                <button v-for="period in periods" :key="period.days" type="button" class="inline-flex items-center gap-1.5 rounded-md px-3 py-1.5 text-xs font-medium transition-colors"
                  :class="selectedDays === period.days ? 'bg-white text-primary-600 shadow-sm dark:bg-dark-700 dark:text-primary-400' : 'text-gray-500 hover:text-gray-800 dark:text-gray-400 dark:hover:text-gray-200'"
                  :aria-pressed="selectedDays === period.days" :data-period="period.days" @click="selectedDays = period.days"><LoadingSpinner v-if="selectedDays === period.days && (loading || userLoading)" size="sm" data-testid="period-loading" />{{ t(period.label) }}</button>
              </div>
              <button type="button" class="btn btn-secondary shrink-0" :disabled="loading || userLoading" @click="refreshAll">
                <Icon name="refresh" size="sm" :class="{ 'animate-spin': loading }" />
                <span class="hidden sm:inline">{{ t('admin.operations.refresh') }}</span>
              </button>
            </div>
          </div>
          <div class="grid grid-cols-1 items-start gap-6 xl:grid-cols-2" data-testid="site-spend-grid">
            <div class="card min-w-0 overflow-hidden p-0">
              <div class="border-b border-gray-100 px-4 py-4 dark:border-dark-700"><h3 class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('admin.operations.siteGroupSpend') }} <span class="ml-1 text-xs font-normal text-gray-400">{{ groups.length }}</span></h3><p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.operations.siteGroupHint') }}</p></div>
              <div class="operations-grid border-b border-gray-100 bg-gray-50/80 px-4 py-2.5 text-xs font-medium text-gray-500 dark:border-dark-700 dark:bg-dark-800/40 dark:text-gray-400">
                <span>{{ t('admin.operations.groups') }}</span><span class="text-right">{{ t('admin.operations.profit') }}</span><span class="text-right">{{ t('admin.operations.userCost') }}</span><span class="text-right">{{ t('admin.operations.upstreamCost') }}</span>
              </div>
              <button type="button" data-group="all" class="operations-grid w-full border-b border-gray-100 px-4 py-3 text-left transition-colors dark:border-dark-700"
                :class="selectedGroup === null ? 'bg-primary-50/70 dark:bg-primary-900/15' : 'hover:bg-gray-50 dark:hover:bg-dark-800/50'" :aria-pressed="selectedGroup === null" @click="selectedGroup = null">
                <span class="flex min-w-0 items-center gap-2 text-sm font-bold text-gray-900 dark:text-white"><span class="flex h-7 w-7 shrink-0 items-center justify-center rounded-md bg-primary-100 text-primary-600 dark:bg-primary-900/30 dark:text-primary-400"><Icon name="grid" size="sm" /></span><span>{{ t('admin.operations.allGroups') }}</span><LoadingSpinner v-if="loading && selectedGroup === null" size="sm" data-testid="all-groups-selection-loading" /></span>
                <span class="money-cell" :class="profitClass(profitOf(totals))">{{ formatMoney(profitOf(totals)) }}</span>
                <span class="money-cell text-gray-700 dark:text-gray-300">{{ formatMoney(totals.user_cost) }}</span>
                <span class="money-cell text-gray-700 dark:text-gray-300">{{ formatMoney(totals.upstream_cost) }}</span>
              </button>
              <div class="relative min-h-40 max-h-[360px] overflow-y-auto overscroll-contain" data-testid="groups-scroll-region" :aria-busy="loading">
                <div v-if="loading" class="absolute inset-0 z-10 flex min-h-40 items-center justify-center bg-white/75 dark:bg-dark-800/75" data-testid="groups-loading" role="status">
                  <LoadingSpinner size="md" /><span class="sr-only">{{ t('admin.operations.loading') }}</span>
                </div>
                <button v-for="group in groups" :key="group.group_id" type="button" class="operations-grid w-full border-b border-gray-100 px-4 py-3 text-left transition-colors last:border-b-0 dark:border-dark-700/70"
                  :data-group="group.group_id" :aria-pressed="selectedGroup === group.group_id" :class="[selectedGroup === group.group_id ? 'bg-primary-50/70 dark:bg-primary-900/15' : 'hover:bg-gray-50 dark:hover:bg-dark-800/50', loading && selectedGroup === group.group_id ? 'relative z-20' : '']" @click="selectedGroup = group.group_id">
                  <span class="flex min-w-0 items-center gap-2"><span class="flex h-7 w-7 shrink-0 items-center justify-center rounded-md bg-gray-100 dark:bg-dark-700" :data-platform="group.platform"><PlatformIcon :platform="group.platform" size="md" :class="platformTextClass(group.platform)" /></span><span class="flex min-w-0 flex-wrap items-center gap-x-1.5 gap-y-1"><span class="break-words text-sm font-bold text-gray-900 dark:text-white">{{ groupName(group) }}</span><span class="inline-flex shrink-0 rounded bg-gray-100 px-1.5 py-0.5 text-[10px] font-medium tabular-nums text-gray-500 dark:bg-dark-700 dark:text-gray-400">{{ group.rate_multiplier }}×</span><LoadingSpinner v-if="loading && selectedGroup === group.group_id" size="sm" data-testid="group-selection-loading" /></span></span>
                  <span class="money-cell" :class="profitClass(profitOf(group))">{{ formatMoney(profitOf(group)) }}</span>
                  <span class="money-cell text-gray-700 dark:text-gray-300">{{ formatMoney(group.user_cost) }}</span>
                  <span class="money-cell text-gray-700 dark:text-gray-300">{{ formatMoney(group.upstream_cost) }}</span>
                </button>
                <p v-if="!groups.length" class="px-4 py-10 text-center text-sm text-gray-400">{{ t('admin.operations.noGroups') }}</p>
              </div>
            </div>
            <div class="card min-w-0 overflow-hidden p-0">
              <div class="flex items-center justify-between gap-3 border-b border-gray-100 px-4 py-4 dark:border-dark-700"><div class="min-w-0"><h3 class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('admin.operations.siteModelSpend') }} <span class="ml-1 text-xs font-normal text-gray-400">{{ models.length }}</span></h3><p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.operations.siteModelHint') }}</p></div><button type="button" class="flex max-w-[45%] shrink-0 items-center gap-1 rounded-full bg-primary-50 px-2.5 py-0.5 text-xs text-primary-600 dark:bg-primary-900/20 dark:text-primary-400" :class="selectedGroup === null ? 'cursor-default' : 'hover:bg-primary-100 dark:hover:bg-primary-900/40'" :disabled="selectedGroup === null" :title="selectedGroup === null ? selectedGroupName : t('admin.operations.clearGroupFilter')" @click="selectedGroup = null"><LoadingSpinner v-if="loading" size="sm" data-testid="model-filter-loading" /><span class="truncate">{{ selectedGroupName }}</span><Icon v-if="selectedGroup !== null && !loading" name="x" size="xs" /></button></div>
              <div class="operations-grid border-b border-gray-100 bg-gray-50/80 px-4 py-2.5 text-xs font-medium text-gray-500 dark:border-dark-700 dark:bg-dark-800/40 dark:text-gray-400">
                <span>Model ID</span><span class="text-right">{{ t('admin.operations.profit') }}</span><span class="text-right">{{ t('admin.operations.userCost') }}</span><span class="text-right">{{ t('admin.operations.upstreamCost') }}</span>
              </div>
              <div class="relative min-h-40 max-h-[360px] overflow-y-auto overscroll-contain" data-testid="models-scroll-region" :aria-busy="loading">
                <div v-if="loading" class="absolute inset-0 z-10 flex min-h-40 items-center justify-center bg-white/75 dark:bg-dark-800/75" data-testid="models-loading" role="status"><LoadingSpinner size="md" /><span class="sr-only">{{ t('admin.operations.loading') }}</span></div>
                <div v-for="model in models" :key="model.model" :data-model="model.model" class="operations-grid border-b border-gray-100 px-4 py-3 last:border-b-0 dark:border-dark-700/70">
                  <span class="flex min-w-0 items-center gap-2"><span v-for="platform in modelPlatforms(model)" :key="platform" class="flex h-7 w-7 shrink-0 items-center justify-center rounded-lg bg-gray-100 dark:bg-dark-700" :class="platformTextClass(platform)" :title="platform"><PlatformIcon :platform="platform" size="sm" /></span><span class="min-w-0 break-all text-sm font-medium leading-5 text-gray-800 dark:text-gray-200">{{ model.model }}</span></span>
                  <span class="money-cell" :class="profitClass(profitOf(model))">{{ formatMoney(profitOf(model)) }}</span>
                  <span class="money-cell text-gray-700 dark:text-gray-300">{{ formatMoney(model.user_cost) }}</span>
                  <span class="money-cell text-gray-700 dark:text-gray-300">{{ formatMoney(model.upstream_cost) }}</span>
                </div>
                <p v-if="!models.length" class="px-4 py-16 text-center text-sm text-gray-400">{{ t('admin.operations.noModels') }}</p>
              </div>
            </div>
          </div>
          <div class="grid grid-cols-1 items-start gap-6 xl:grid-cols-2" data-testid="user-spend-grid">
            <OperationsUserSpendPanel :users="users" :selected-user-id="selectedUserId" :search="userSearch" :suggestions="suggestions" :suggestions-open="suggestionsOpen" :suggestions-loading="suggestionsLoading" :loading="userLoading" :error="userError"
              @update:search="updateUserSearch" @focus-search="focusUserSearch" @blur-search="blurUserSearch" @dismiss-suggestions="suggestionsOpen = false" @select-suggestion="selectSuggestion" @select-user="selectUser" @retry="loadUserSpend" />
              <div class="card min-w-0 overflow-hidden p-0" data-testid="user-detail" :aria-busy="userLoading">
                <div class="flex flex-wrap items-center justify-between gap-3 border-b border-gray-100 px-4 py-4 dark:border-dark-700">
                  <div class="min-w-0">
                    <h3 class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('admin.operations.userDetails') }}</h3>
                    <p class="mt-1 truncate text-xs text-gray-500 dark:text-gray-400" :title="selectedDetail?.email">{{ selectedDetail?.email || t('admin.operations.userDetailsHint') }}</p>
                  </div>
                  <button v-if="manuallySelectedUser" type="button" class="rounded-full bg-gray-100 px-2.5 py-1 text-xs font-medium text-gray-600 hover:bg-gray-200 dark:bg-dark-700 dark:text-gray-300 dark:hover:bg-dark-600" @click="selectTopUser">{{ t('admin.operations.backToTopUser') }}</button>
                </div>
                <div class="border-b border-gray-100 px-4 py-3 dark:border-dark-700">
                  <div role="tablist" :aria-label="t('admin.operations.userDetails')" class="inline-flex rounded-full bg-gray-100 p-1 dark:bg-dark-800">
                    <button v-for="tab in breakdownTabs" :key="tab.key" type="button" role="tab" :data-detail-tab="tab.key" :aria-selected="activeBreakdownTab === tab.key" class="inline-flex items-center gap-1.5 rounded-full px-4 py-1.5 text-xs font-medium transition-colors" :class="activeBreakdownTab === tab.key ? 'bg-white text-primary-600 shadow-sm dark:bg-dark-700 dark:text-primary-400' : 'text-gray-500 hover:text-gray-800 dark:text-gray-400 dark:hover:text-gray-200'" @click="activeBreakdownTab = tab.key"><LoadingSpinner v-if="userLoading && activeBreakdownTab === tab.key" size="sm" data-testid="detail-tab-loading" />{{ t(tab.label) }}</button>
                  </div>
                </div>
                <div v-if="userLoading" class="flex min-h-40 items-center justify-center" role="status" data-testid="user-detail-loading"><LoadingSpinner size="md" /><span class="sr-only">{{ t('admin.operations.loading') }}</span></div>
                <div v-else-if="userError" class="px-4 py-12 text-center text-sm text-gray-500 dark:text-gray-400">{{ t('admin.operations.failedToLoadUsers') }}</div>
                <template v-else-if="selectedDetail">
                  <div class="grid grid-cols-3 gap-2 border-b border-gray-100 px-4 py-3 dark:border-dark-700">
                    <div v-for="summary in userSummaries" :key="summary.label" class="min-w-0 rounded-lg bg-gray-50 px-2.5 py-2 dark:bg-dark-800/60">
                      <p class="truncate text-[11px] text-gray-500 dark:text-gray-400">{{ t(summary.label) }}</p>
                      <p class="mt-0.5 break-all text-sm font-semibold tabular-nums" :class="summary.label === 'admin.operations.profit' ? profitClass(summary.value) : 'text-gray-900 dark:text-white'">{{ formatMoney(summary.value) }}</p>
                    </div>
                  </div>
                  <div class="operations-grid border-b border-gray-100 bg-gray-50/80 px-4 py-2.5 text-xs font-medium text-gray-500 dark:border-dark-700 dark:bg-dark-800/40 dark:text-gray-400">
                    <span>{{ t(activeBreakdownTab === 'groups' ? 'admin.operations.groups' : 'admin.operations.models') }}</span><span class="text-right">{{ t('admin.operations.profit') }}</span><span class="text-right">{{ t('admin.operations.userCost') }}</span><span class="text-right">{{ t('admin.operations.upstreamCost') }}</span>
                  </div>
                  <div v-if="activeBreakdownTab === 'groups'" class="max-h-[340px] overflow-y-auto overscroll-contain" data-testid="user-detail-groups">
                    <div v-for="group in userDetailGroups" :key="group.group_id" class="operations-grid border-b border-gray-100 px-4 py-3 last:border-b-0 dark:border-dark-700/70" :data-user-group="group.group_id">
                      <span class="flex min-w-0 items-center gap-2"><span class="flex h-7 w-7 shrink-0 items-center justify-center rounded-lg bg-gray-100 dark:bg-dark-700" :class="platformTextClass(group.platform)"><PlatformIcon :platform="group.platform" size="sm" /></span><span class="min-w-0 truncate text-sm font-medium text-gray-900 dark:text-white" :title="groupName(group)">{{ groupName(group) }}</span></span>
                      <span class="money-cell" :class="profitClass(profitOf(group))">{{ formatMoney(profitOf(group)) }}</span><span class="money-cell">{{ formatMoney(group.user_cost) }}</span><span class="money-cell">{{ formatMoney(group.upstream_cost) }}</span>
                    </div>
                    <p v-if="!userDetailGroups.length" class="px-4 py-12 text-center text-sm text-gray-400">{{ t('admin.operations.noGroups') }}</p>
                  </div>
                  <div v-else class="max-h-[340px] overflow-y-auto overscroll-contain" data-testid="user-detail-models">
                    <div v-for="model in userDetailModels" :key="model.model" class="operations-grid border-b border-gray-100 px-4 py-3 last:border-b-0 dark:border-dark-700/70" :data-user-model="model.model">
                      <span class="flex min-w-0 items-center gap-2"><span v-for="platform in modelPlatforms(model)" :key="platform" class="flex h-7 w-7 shrink-0 items-center justify-center rounded-lg bg-gray-100 dark:bg-dark-700" :class="platformTextClass(platform)" :title="platform"><PlatformIcon :platform="platform" size="sm" /></span><span class="min-w-0 break-all text-sm font-medium text-gray-900 dark:text-white">{{ model.model }}</span></span>
                      <span class="money-cell" :class="profitClass(profitOf(model))">{{ formatMoney(profitOf(model)) }}</span><span class="money-cell">{{ formatMoney(model.user_cost) }}</span><span class="money-cell">{{ formatMoney(model.upstream_cost) }}</span>
                    </div>
                    <p v-if="!userDetailModels.length" class="px-4 py-12 text-center text-sm text-gray-400">{{ t('admin.operations.noModels') }}</p>
                  </div>
                </template>
                <p v-else class="px-4 py-12 text-center text-sm text-gray-400">{{ t('admin.operations.userDetailsHint') }}</p>
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
import type { OperationsDashboardResponse, OperationsGroupProfitStat, OperationsModelProfitStat, OperationsUserSpendResponse } from '@/api/admin/dashboard'
import type { SimpleUser } from '@/api/admin/usage'
import type { GroupPlatform } from '@/types'
import AppLayout from '@/components/layout/AppLayout.vue'
import Icon from '@/components/icons/Icon.vue'
import PlatformIcon from '@/components/common/PlatformIcon.vue'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import OperationsTrendChart from '@/components/admin/dashboard/OperationsTrendChart.vue'
import OperationsUserSpendPanel from '@/components/admin/dashboard/OperationsUserSpendPanel.vue'
import { platformTextClass } from '@/utils/platformColors'
import { compareProfit, dateRange, formatMoney, profitOf } from './operationsUtils'

const { t } = useI18n()
const data = ref<OperationsDashboardResponse | null>(null)
const loading = ref(false)
const error = ref(false)
const selectedDays = ref(1)
const selectedGroup = ref<number | null>(null)
const selectedUserId = ref<number | null>(null)
const manuallySelectedUser = ref(false)
const userSpendData = ref<OperationsUserSpendResponse | null>(null)
const userSearch = ref('')
const userLoading = ref(false)
const userError = ref(false)
const suggestions = ref<SimpleUser[]>([])
const suggestionsOpen = ref(false)
const suggestionsLoading = ref(false)
const activeBreakdownTab = ref<'groups' | 'models'>('groups')
const trendDays = ref<7 | 15>(window.innerWidth < 768 ? 7 : 15)
let requestSequence = 0
let userRequestSequence = 0
let suggestionSequence = 0
let searchTimer: ReturnType<typeof setTimeout> | undefined
let blurTimer: ReturnType<typeof setTimeout> | undefined
const periods = [{ days: 1, label: 'admin.operations.today' }, { days: 7, label: 'admin.operations.last7Days' }, { days: 30, label: 'admin.operations.last30Days' }]
const breakdownTabs = [{ key: 'groups', label: 'admin.operations.groups' }, { key: 'models', label: 'admin.operations.models' }] as const
const metrics = [
  { key: 'today_user_consumption', label: 'admin.operations.todayUserConsumption', subLabel: 'admin.operations.yesterdayUserConsumption', compare: true, icon: 'users', color: 'bg-blue-100 text-blue-600 dark:bg-blue-900/30 dark:text-blue-400' },
  { key: 'today_user_recharge', label: 'admin.operations.todayUserRecharge', subLabel: 'admin.operations.yesterdayUserRecharge', compare: true, icon: 'creditCard', color: 'bg-emerald-100 text-emerald-600 dark:bg-emerald-900/30 dark:text-emerald-400' },
  { key: 'today_monitor_consumption', label: 'admin.operations.todayMonitorConsumption', subLabel: 'admin.operations.yesterdayMonitorConsumption', compare: true, icon: 'server', color: 'bg-violet-100 text-violet-600 dark:bg-violet-900/30 dark:text-violet-400' },
  { key: 'today_revenue', label: 'admin.operations.todayRevenue', subLabel: 'admin.operations.yesterdayRevenue', compare: true, icon: 'trendingUp', color: 'bg-amber-100 text-amber-600 dark:bg-amber-900/30 dark:text-amber-400' },
  { key: 'total_user_consumption', label: 'admin.operations.totalUserConsumption', subLabel: 'admin.operations.yesterdayIncrease', compare: false, icon: 'chart', color: 'bg-blue-100 text-blue-600 dark:bg-blue-900/30 dark:text-blue-400' },
  { key: 'total_user_balance', label: 'admin.operations.totalUserBalance', subLabel: 'admin.operations.yesterdayIncrease', compare: false, icon: 'creditCard', color: 'bg-cyan-100 text-cyan-600 dark:bg-cyan-900/30 dark:text-cyan-400' },
  { key: 'total_monitor_consumption', label: 'admin.operations.totalMonitorConsumption', subLabel: 'admin.operations.yesterdayIncrease', compare: false, icon: 'server', color: 'bg-violet-100 text-violet-600 dark:bg-violet-900/30 dark:text-violet-400' },
  { key: 'admin_balance', label: 'admin.operations.adminBalance', subLabel: 'admin.operations.yesterdayConsumption', compare: false, icon: 'shield', color: 'bg-rose-100 text-rose-600 dark:bg-rose-900/30 dark:text-rose-400' }
] as const
type Metric = (typeof metrics)[number]
const revenues = computed(() => {
  const entries = Object.entries(data.value?.metrics.today_revenue ?? {}).sort(([a], [b]) => a.localeCompare(b))
  return (entries.length ? entries : [['USD', 0] as const]).map(([currency, amount]) => ({ currency, amount }))
})
const revenueComparisons = computed(() => {
  const today = data.value?.metrics.today_revenue ?? {}
  const yesterday = data.value?.metrics.yesterday_revenue ?? {}
  const currencies = [...new Set([...Object.keys(today), ...Object.keys(yesterday)])].sort()
  return (currencies.length ? currencies : ['USD']).map(currency => ({ currency, today: today[currency] ?? 0, yesterday: yesterday[currency] ?? 0 }))
})
function yesterdayValue(metric: Metric): number | null {
  const values = data.value?.metrics
  if (!values) return null
  switch (metric.key) {
    case 'today_user_consumption':
    case 'total_user_consumption': return values.yesterday_user_consumption
    case 'today_user_recharge': return values.yesterday_user_recharge
    case 'today_monitor_consumption':
    case 'total_monitor_consumption': return values.yesterday_monitor_consumption
    case 'total_user_balance': return values.yesterday_user_balance_change
    case 'admin_balance': return values.yesterday_admin_consumption
    case 'today_revenue': return null
  }
}
function todayValue(metric: Metric): number {
  const value = data.value?.metrics[metric.key]
  return typeof value === 'number' ? value : 0
}
const formatOptionalMoney = (value: number | null) => value === null ? '—' : formatMoney(value)
const formatChange = (value: number, currency = 'USD') => `${value > 0 ? '+' : ''}${formatMoney(value, currency)}`
const changeClass = (value: number) => value > 0 ? 'text-rose-500 dark:text-rose-400' : value < 0 ? 'text-emerald-600 dark:text-emerald-400' : 'text-gray-500 dark:text-gray-400'
const consumptionRange = computed(() => dateRange(selectedDays.value))
const users = computed(() => userSpendData.value?.users ?? [])
const selectedDetail = computed(() => userSpendData.value?.detail?.user_id === selectedUserId.value ? userSpendData.value.detail : null)
const selectedUser = computed(() => {
  const detail = selectedDetail.value
  if (!detail) return null
  return users.value.find(user => user.user_id === detail.user_id) ?? {
    user_id: detail.user_id,
    email: detail.email,
    user_cost: detail.groups.reduce((sum, group) => sum + group.user_cost, 0),
    upstream_cost: detail.groups.reduce((sum, group) => sum + group.upstream_cost, 0)
  }
})
const userDetailGroups = computed(() => [...(selectedDetail.value?.groups ?? [])].sort((a, b) => compareProfit(a, b) || a.group_name.localeCompare(b.group_name)))
const userDetailModels = computed(() => [...(selectedDetail.value?.models ?? [])].sort((a, b) => compareProfit(a, b) || a.model.localeCompare(b.model)))
const userSummaries = computed(() => selectedUser.value ? [
  { label: 'admin.operations.userCost', value: selectedUser.value.user_cost },
  { label: 'admin.operations.upstreamCost', value: selectedUser.value.upstream_cost },
  { label: 'admin.operations.profit', value: profitOf(selectedUser.value) }
] : [])
const groups = computed(() => [...(data.value?.groups ?? [])].sort((a, b) => compareProfit(a, b) || a.group_name.localeCompare(b.group_name)))
const models = computed(() => [...(data.value?.models ?? [])].sort((a, b) => compareProfit(a, b) || a.model.localeCompare(b.model)))
const totals = computed(() => groups.value.reduce((sum, group) => ({ user_cost: sum.user_cost + group.user_cost, upstream_cost: sum.upstream_cost + group.upstream_cost }), { user_cost: 0, upstream_cost: 0 }))
const groupName = (group: OperationsGroupProfitStat) => group.group_id === 0 ? t('admin.operations.unassigned') : group.group_name
const selectedGroupName = computed(() => {
  const group = groups.value.find(item => item.group_id === selectedGroup.value)
  return group ? groupName(group) : t('admin.operations.allGroups')
})
const profitClass = (profit: number) => profit < 0 ? 'text-red-600 dark:text-red-400' : profit > 0 ? 'text-emerald-600 dark:text-emerald-400' : 'text-gray-500 dark:text-gray-400'
function modelPlatform(model: OperationsModelProfitStat): GroupPlatform {
  if (model.platforms?.length) return model.platforms[0]
  const selectedPlatform = groups.value.find(group => group.group_id === selectedGroup.value)?.platform
  if (selectedPlatform) return selectedPlatform
  const name = model.model.toLowerCase()
  if (name.startsWith('claude')) return 'anthropic'
  if (name.startsWith('gemini')) return 'gemini'
  if (name.startsWith('grok')) return 'grok'
  if (/^(gpt|o[134]-|chatgpt)/.test(name)) return 'openai'
  return 'custom'
}
function modelPlatforms(model: OperationsModelProfitStat): GroupPlatform[] {
  return model.platforms?.length ? model.platforms : [modelPlatform(model)]
}
async function loadUserSpend() {
  const sequence = ++userRequestSequence
  userLoading.value = true
  userError.value = false
  try {
    const range = dateRange(selectedDays.value)
    const result = await adminAPI.dashboard.getOperationsUserSpend({
      start_date: range.start,
      end_date: range.end,
      timezone: Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC',
      ...(userSearch.value.trim() ? { search: userSearch.value.trim() } : {}),
      ...(selectedUserId.value !== null ? { user_id: selectedUserId.value } : {})
    })
    if (sequence !== userRequestSequence) return
    if (!manuallySelectedUser.value) {
      const firstUserId = result.users[0]?.user_id ?? null
      if (selectedUserId.value !== firstUserId) {
        selectedUserId.value = firstUserId
        userSpendData.value = result
        if (firstUserId !== null) {
          void loadUserSpend()
          return
        }
      }
    }
    userSpendData.value = result
  } catch (err) {
    if (sequence !== userRequestSequence) return
    console.error('Failed to load operations user spend:', err)
    userError.value = true
  } finally {
    if (sequence === userRequestSequence) userLoading.value = false
  }
}
async function loadSuggestions(keyword: string) {
  const sequence = ++suggestionSequence
  suggestionsLoading.value = true
  try {
    const result = await adminAPI.dashboard.getOperationsUserSuggestions(keyword)
    if (sequence !== suggestionSequence || userSearch.value.trim() !== keyword) return
    suggestions.value = result.map(user => ({ id: user.user_id, email: user.email, deleted: false }))
  } catch (err) {
    if (sequence !== suggestionSequence) return
    console.error('Failed to search user emails:', err)
    suggestions.value = []
  } finally {
    if (sequence === suggestionSequence) suggestionsLoading.value = false
  }
}
function updateUserSearch(value: string) {
  userSearch.value = value
  selectedUserId.value = null
  manuallySelectedUser.value = false
  suggestionsOpen.value = Boolean(value.trim())
  suggestions.value = []
  suggestionSequence++
  userRequestSequence++
  if (searchTimer) clearTimeout(searchTimer)
  if (!value.trim()) {
    suggestionsLoading.value = false
    void loadUserSpend()
    return
  }
  userLoading.value = true
  searchTimer = setTimeout(() => {
    void loadSuggestions(value.trim())
    void loadUserSpend()
  }, 250)
}
function selectSuggestion(user: SimpleUser) {
  if (searchTimer) clearTimeout(searchTimer)
  if (blurTimer) clearTimeout(blurTimer)
  suggestionSequence++
  userSearch.value = user.email
  selectedUserId.value = user.id
  manuallySelectedUser.value = true
  suggestionsOpen.value = false
  activeBreakdownTab.value = 'groups'
  void loadUserSpend()
}
function selectUser(userId: number) {
  selectedUserId.value = userId
  manuallySelectedUser.value = true
  suggestionsOpen.value = false
  activeBreakdownTab.value = 'groups'
  void loadUserSpend()
}
function selectTopUser() {
  manuallySelectedUser.value = false
  selectedUserId.value = users.value[0]?.user_id ?? null
  activeBreakdownTab.value = 'groups'
  void loadUserSpend()
}
function focusUserSearch() {
  if (!userSearch.value.trim()) return
  if (blurTimer) clearTimeout(blurTimer)
  suggestionsOpen.value = true
  if (!suggestions.value.length) void loadSuggestions(userSearch.value.trim())
}
function blurUserSearch() { blurTimer = setTimeout(() => { suggestionsOpen.value = false }, 150) }
function refreshAll() { void loadData(); void loadUserSpend() }
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
watch(selectedDays, () => { if (!manuallySelectedUser.value) selectedUserId.value = null; activeBreakdownTab.value = 'groups'; void loadUserSpend() })
onMounted(() => { window.addEventListener('resize', onResize); void loadData(); void loadUserSpend() })
onUnmounted(() => {
  requestSequence++
  userRequestSequence++
  suggestionSequence++
  if (searchTimer) clearTimeout(searchTimer)
  if (blurTimer) clearTimeout(blurTimer)
  window.removeEventListener('resize', onResize)
})
</script>

<style scoped>
.operations-grid { display: grid; grid-template-columns: minmax(0, 1.45fr) repeat(3, minmax(0, 1fr)); gap: 0.5rem; align-items: center; }
.money-cell { text-align: right; font-size: 0.75rem; font-weight: 500; line-height: 1.4; font-variant-numeric: tabular-nums; overflow-wrap: anywhere; }
@media (max-width: 639px) {
  .operations-grid { grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 0.375rem; }
  .operations-grid > :first-child { grid-column: 1 / -1; }
}
@media (min-width: 1536px) { .operations-grid { gap: 1rem; } }
</style>
