<template>
  <AppLayout>
    <div class="space-y-6">
      <div v-if="error" role="alert" class="flex items-center justify-between gap-3 rounded-2xl bg-red-50 p-4 text-sm text-red-700">
        {{ t('admin.operations.failedToLoad') }}
        <button type="button" class="font-medium underline" @click="loadData">{{ t('admin.operations.retry') }}</button>
      </div>
      <div v-if="loading && !data" class="flex justify-center py-20"><LoadingSpinner /></div>
      <template v-if="data">
        <section class="space-y-4" :aria-label="t('admin.operations.metrics')" data-testid="operations-metrics">
          <header class="ops-section-head">
            <div class="min-w-0">
              <h2 class="ops-section-title"><span class="ops-section-mark" aria-hidden="true" />{{ t('admin.operations.metrics') }}</h2>
              <p class="ops-section-hint" data-testid="today-window"><Icon name="clock" size="xs" class="shrink-0" />{{ t('admin.operations.todayWindow', { date: todayDate, timezone: siteTimeZoneLabel }) }}</p>
            </div>
            <div class="flex shrink-0 items-center gap-3">
              <span v-if="updatedAt" class="hidden text-xs tabular-nums text-gray-500 dark:text-gray-400 sm:inline">{{ t('admin.operations.updatedAt', { time: updatedAt }) }}</span>
              <button type="button" class="btn btn-secondary shrink-0" :disabled="loading || userLoading" @click="refreshAll">
                <Icon name="refresh" size="sm" :class="{ 'animate-spin': loading }" />
                <span class="hidden sm:inline">{{ t('admin.operations.refresh') }}</span>
              </button>
            </div>
          </header>
          <div v-for="block in metricBlocks" :key="block.key" class="space-y-2.5">
            <p class="ops-block-label">{{ t(block.label) }}</p>
            <div class="grid grid-cols-2 gap-3 sm:gap-4 lg:grid-cols-4">
              <article v-for="metric in block.metrics" :key="metric.key" class="ops-metric card min-w-0" :class="{ 'ops-metric-today': block.key === 'today' }" :data-metric="metric.key">
                <div class="ops-metric-main">
                  <div class="flex items-start justify-between gap-2">
                    <p class="min-w-0 pt-1 text-xs font-medium leading-4 text-gray-500 dark:text-gray-400">{{ t(metric.label) }}</p>
                    <span class="ops-metric-icon" :class="metric.color"><Icon :name="metric.icon" size="sm" :stroke-width="2" /></span>
                  </div>
                  <div v-if="metric.key === 'today_revenue'" class="mt-2 flex flex-wrap items-baseline gap-x-3 gap-y-0.5">
                    <p v-for="revenue in revenues" :key="revenue.currency" class="ops-metric-value" :title="revenue.currency">{{ formatMoney(revenue.amount, revenue.currency) }}</p>
                  </div>
                  <p v-else class="ops-metric-value mt-2">{{ formatMoney(data.metrics[metric.key]) }}</p>
                </div>
                <div class="ops-metric-foot">
                  <p v-if="metric.key === 'today_revenue'" class="flex flex-wrap gap-x-2 gap-y-0.5">
                    <span>{{ t('admin.operations.yesterdayRevenue') }}</span>
                    <span v-for="revenue in revenueComparisons" :key="revenue.currency" class="whitespace-nowrap font-medium tabular-nums text-gray-700 dark:text-gray-300">{{ formatMoney(revenue.yesterday, revenue.currency) }}</span>
                  </p>
                  <p v-else class="flex flex-wrap gap-x-1.5">
                    <span>{{ t(metric.subLabel) }}</span>
                    <span class="font-medium tabular-nums text-gray-700 dark:text-gray-300">{{ formatOptionalMoney(yesterdayValue(metric)) }}</span>
                  </p>
                  <p v-if="metric.compare" class="flex flex-wrap items-center gap-1.5" data-testid="metric-comparison">
                    <span>{{ t('admin.operations.comparisonGrowth') }}</span>
                    <template v-if="metric.key === 'today_revenue'">
                      <span v-for="revenue in revenueComparisons" :key="revenue.currency" class="ops-change" :class="changeClass(revenue.today - revenue.yesterday)">{{ formatChange(revenue.today - revenue.yesterday, revenue.currency) }}</span>
                    </template>
                    <span v-else class="ops-change" :class="changeClass(todayValue(metric) - (yesterdayValue(metric) ?? 0))">{{ formatChange(todayValue(metric) - (yesterdayValue(metric) ?? 0)) }}</span>
                  </p>
                </div>
              </article>
            </div>
          </div>
        </section>

        <div class="grid grid-cols-1 items-stretch gap-6 xl:h-[560px] xl:grid-cols-12" data-testid="overview-grid">
          <OperationsTrendChart class="xl:col-span-7" :points="data.trend" :days="trendDays" :time-zone="siteTimeZone" />
          <OperationsRechargeRankingPanel class="xl:col-span-5" :ranking="rechargeRanking" :loading="rankingLoading" :error="rankingError" @retry="loadRechargeRanking" />
        </div>

        <section class="space-y-6" :aria-busy="loading">
          <div class="card flex flex-wrap items-center justify-between gap-4 p-4 sm:px-5">
            <div class="flex min-w-0 items-center gap-3">
              <span class="ops-panel-icon bg-blue-100 text-blue-600 dark:bg-blue-900/30 dark:text-blue-400"><Icon name="chartBar" size="sm" :stroke-width="2" /></span>
              <div class="min-w-0">
                <h2 class="ops-panel-title">{{ t('admin.operations.consumptionPanel') }}</h2>
                <p class="ops-panel-hint tabular-nums">{{ consumptionRange.start }} — {{ consumptionRange.end }} · USD</p>
              </div>
            </div>
            <div class="segment-group" :aria-label="t('admin.dashboard.timeRange')">
              <button v-for="period in periods" :key="period.days" type="button" class="inline-flex items-center gap-1.5 rounded-full px-3 py-1.5 text-xs font-medium transition-colors"
                :class="selectedDays === period.days ? 'segment-active' : 'segment-idle'"
                :aria-pressed="selectedDays === period.days" :data-period="period.days" @click="selectedDays = period.days"><LoadingSpinner v-if="selectedDays === period.days && (loading || userLoading)" size="sm" data-testid="period-loading" />{{ t(period.label) }}</button>
            </div>
          </div>
          <div class="grid grid-cols-1 items-stretch gap-6 xl:grid-cols-2" data-testid="site-spend-grid">
            <div class="card flex min-w-0 flex-col overflow-hidden p-0">
              <div class="ops-panel-head">
                <div class="flex min-w-0 items-center gap-3">
                  <span class="ops-panel-icon bg-primary-100 text-primary-700 dark:bg-primary-900/30 dark:text-primary-400"><Icon name="grid" size="sm" :stroke-width="2" /></span>
                  <div class="min-w-0">
                    <h3 class="ops-panel-title">{{ t('admin.operations.siteGroupSpend') }}<span class="ops-count">{{ groups.length }}</span></h3>
                    <p class="ops-panel-hint">{{ t('admin.operations.siteGroupHint') }}</p>
                  </div>
                </div>
              </div>
              <div class="operations-grid ops-table-head px-4 py-2.5 sm:px-5">
                <span>{{ t('admin.operations.groups') }}</span><span class="text-right">{{ t('admin.operations.profit') }}</span><span class="text-right">{{ t('admin.operations.userCost') }}</span><span class="text-right">{{ t('admin.operations.upstreamCost') }}</span>
              </div>
              <button type="button" data-group="all" class="operations-grid ops-row w-full px-4 py-3 text-left sm:px-5"
                :class="selectedGroup === null ? 'ops-row-active' : 'ops-row-hover'" :aria-pressed="selectedGroup === null" @click="selectedGroup = null">
                <span class="flex min-w-0 items-center gap-2.5 text-sm font-bold text-gray-900 dark:text-white"><span class="ops-tile bg-primary-100 text-primary-700 dark:bg-primary-900/30 dark:text-primary-400"><Icon name="grid" size="sm" /></span><span>{{ t('admin.operations.allGroups') }}</span><LoadingSpinner v-if="loading && selectedGroup === null" size="sm" data-testid="all-groups-selection-loading" /></span>
                <span class="money-cell font-semibold" :class="profitClass(profitOf(totals))">{{ formatMoney(profitOf(totals)) }}</span>
                <span class="money-cell text-gray-700 dark:text-gray-300">{{ formatMoney(totals.user_cost) }}</span>
                <span class="money-cell text-gray-700 dark:text-gray-300">{{ formatMoney(totals.upstream_cost) }}</span>
              </button>
              <div class="relative min-h-40 max-h-[360px] flex-1 overflow-y-auto overscroll-contain" data-testid="groups-scroll-region" :aria-busy="loading">
                <div v-if="loading" class="absolute inset-0 z-10 flex min-h-40 items-center justify-center bg-white/75 dark:bg-dark-800/75" data-testid="groups-loading" role="status">
                  <LoadingSpinner size="md" /><span class="sr-only">{{ t('admin.operations.loading') }}</span>
                </div>
                <button v-for="group in groups" :key="group.group_id" type="button" class="operations-grid ops-row w-full px-4 py-3 text-left sm:px-5"
                  :data-group="group.group_id" :aria-pressed="selectedGroup === group.group_id" :title="group.group_id === DELETED_GROUPS_ID ? t('admin.operations.deletedGroupsHint') : undefined" :class="[selectedGroup === group.group_id ? 'ops-row-active' : 'ops-row-hover', loading && selectedGroup === group.group_id ? 'relative z-20' : '']" @click="selectedGroup = group.group_id">
                  <span class="flex min-w-0 items-center gap-2.5"><span class="ops-tile" :data-platform="group.platform"><Icon v-if="group.group_id === DELETED_GROUPS_ID" name="trash" size="sm" class="text-gray-400" /><PlatformIcon v-else :platform="group.platform" size="md" :class="platformTextClass(group.platform)" /></span><span class="flex min-w-0 flex-wrap items-center gap-x-1.5 gap-y-1"><span class="break-words text-sm font-semibold" :class="group.group_id === DELETED_GROUPS_ID ? 'text-gray-500 dark:text-gray-400' : 'text-gray-900 dark:text-white'">{{ groupName(group) }}</span><span v-if="group.group_id !== DELETED_GROUPS_ID" class="inline-flex shrink-0 rounded-full bg-primary-50 px-1.5 py-0.5 text-[10px] font-semibold tabular-nums text-primary-700 dark:bg-dark-700 dark:text-gray-400">{{ group.rate_multiplier }}×</span><LoadingSpinner v-if="loading && selectedGroup === group.group_id" size="sm" data-testid="group-selection-loading" /></span></span>
                  <span class="money-cell font-semibold" :class="profitClass(profitOf(group))">{{ formatMoney(profitOf(group)) }}</span>
                  <span class="money-cell text-gray-700 dark:text-gray-300">{{ formatMoney(group.user_cost) }}</span>
                  <span class="money-cell text-gray-700 dark:text-gray-300">{{ formatMoney(group.upstream_cost) }}</span>
                </button>
                <div v-if="!groups.length" class="ops-empty"><Icon name="grid" size="lg" class="text-primary-300" /><p>{{ t('admin.operations.noGroups') }}</p></div>
              </div>
            </div>
            <div class="card flex min-w-0 flex-col overflow-hidden p-0">
              <div class="ops-panel-head flex-nowrap">
                <div class="flex min-w-0 items-center gap-3">
                  <span class="ops-panel-icon bg-violet-100 text-violet-600 dark:bg-violet-900/30 dark:text-violet-400"><Icon name="cube" size="sm" :stroke-width="2" /></span>
                  <div class="min-w-0">
                    <h3 class="ops-panel-title">{{ t('admin.operations.siteModelSpend') }}<span class="ops-count">{{ models.length }}</span></h3>
                    <p class="ops-panel-hint">{{ t('admin.operations.siteModelHint') }}</p>
                  </div>
                </div>
                <button type="button" class="flex max-w-[45%] shrink-0 items-center gap-1 rounded-full bg-primary-50 px-2.5 py-1 text-xs font-medium text-primary-700 dark:bg-primary-900/20 dark:text-primary-400" :class="selectedGroup === null ? 'cursor-default' : 'hover:bg-primary-100 dark:hover:bg-primary-900/40'" :disabled="selectedGroup === null" :title="selectedGroup === null ? selectedGroupName : t('admin.operations.clearGroupFilter')" @click="selectedGroup = null"><LoadingSpinner v-if="loading" size="sm" data-testid="model-filter-loading" /><span class="truncate">{{ selectedGroupName }}</span><Icon v-if="selectedGroup !== null && !loading" name="x" size="xs" /></button>
              </div>
              <div class="operations-grid ops-table-head px-4 py-2.5 sm:px-5">
                <span>Model ID</span><span class="text-right">{{ t('admin.operations.profit') }}</span><span class="text-right">{{ t('admin.operations.userCost') }}</span><span class="text-right">{{ t('admin.operations.upstreamCost') }}</span>
              </div>
              <div class="relative min-h-40 max-h-[412px] flex-1 overflow-y-auto overscroll-contain" data-testid="models-scroll-region" :aria-busy="loading">
                <div v-if="loading" class="absolute inset-0 z-10 flex min-h-40 items-center justify-center bg-white/75 dark:bg-dark-800/75" data-testid="models-loading" role="status"><LoadingSpinner size="md" /><span class="sr-only">{{ t('admin.operations.loading') }}</span></div>
                <div v-for="model in models" :key="model.model" :data-model="model.model" class="operations-grid ops-row ops-row-hover px-4 py-3 sm:px-5">
                  <span class="flex min-w-0 items-center gap-2.5"><span v-for="platform in modelPlatforms(model)" :key="platform" class="ops-tile" :class="platformTextClass(platform)" :title="platform"><PlatformIcon :platform="platform" size="sm" /></span><span class="min-w-0 break-words text-sm font-medium leading-5 text-gray-800 dark:text-gray-200">{{ model.model }}</span></span>
                  <span class="money-cell font-semibold" :class="profitClass(profitOf(model))">{{ formatMoney(profitOf(model)) }}</span>
                  <span class="money-cell text-gray-700 dark:text-gray-300">{{ formatMoney(model.user_cost) }}</span>
                  <span class="money-cell text-gray-700 dark:text-gray-300">{{ formatMoney(model.upstream_cost) }}</span>
                </div>
                <div v-if="!models.length" class="ops-empty"><Icon name="cube" size="lg" class="text-primary-300" /><p>{{ t('admin.operations.noModels') }}</p></div>
              </div>
            </div>
          </div>
          <div class="grid grid-cols-1 items-stretch gap-6 xl:grid-cols-2" data-testid="user-spend-grid">
            <OperationsUserSpendPanel :users="users" :selected-user-id="selectedUserId" :search="userSearch" :suggestions="suggestions" :suggestions-open="suggestionsOpen" :suggestions-loading="suggestionsLoading" :loading="userLoading" :error="userError"
              @update:search="updateUserSearch" @focus-search="focusUserSearch" @blur-search="blurUserSearch" @dismiss-suggestions="suggestionsOpen = false" @select-suggestion="selectSuggestion" @select-user="selectUser" @retry="loadUserSpend" />
            <div class="card flex min-w-0 flex-col overflow-hidden p-0" data-testid="user-detail" :aria-busy="userLoading">
              <div class="ops-panel-head flex-nowrap">
                <div class="flex min-w-0 items-center gap-3">
                  <span class="ops-panel-icon bg-cyan-100 text-cyan-600 dark:bg-cyan-900/30 dark:text-cyan-400"><Icon name="userCircle" size="sm" :stroke-width="2" /></span>
                  <div class="min-w-0">
                    <h3 class="ops-panel-title">{{ t('admin.operations.userDetails') }}</h3>
                    <p class="ops-panel-hint truncate" :title="selectedDetail?.email">{{ selectedDetail?.email || t('admin.operations.userDetailsHint') }}</p>
                  </div>
                </div>
                <button v-if="manuallySelectedUser" type="button" class="shrink-0 rounded-full bg-primary-50 px-2.5 py-1 text-xs font-medium text-primary-700 hover:bg-primary-100 dark:bg-dark-700 dark:text-gray-300 dark:hover:bg-dark-600" @click="selectTopUser">{{ t('admin.operations.backToTopUser') }}</button>
              </div>
              <div class="px-4 py-3 sm:px-5">
                <div role="tablist" :aria-label="t('admin.operations.userDetails')" class="segment-group">
                  <button v-for="tab in breakdownTabs" :key="tab.key" type="button" role="tab" :data-detail-tab="tab.key" :aria-selected="activeBreakdownTab === tab.key" class="inline-flex items-center gap-1.5 rounded-full px-4 py-1.5 text-xs font-medium transition-colors" :class="activeBreakdownTab === tab.key ? 'segment-active' : 'segment-idle'" @click="activeBreakdownTab = tab.key"><LoadingSpinner v-if="userLoading && activeBreakdownTab === tab.key" size="sm" data-testid="detail-tab-loading" />{{ t(tab.label) }}</button>
                </div>
              </div>
              <div v-if="userLoading" class="flex min-h-40 flex-1 items-center justify-center" role="status" data-testid="user-detail-loading"><LoadingSpinner size="md" /><span class="sr-only">{{ t('admin.operations.loading') }}</span></div>
              <div v-else-if="userError" class="px-4 py-12 text-center text-sm text-gray-500 dark:text-gray-400">{{ t('admin.operations.failedToLoadUsers') }}</div>
              <template v-else-if="selectedDetail">
                <div class="grid grid-cols-3 gap-2 px-4 pb-3 sm:px-5">
                  <div v-for="summary in userSummaries" :key="summary.label" class="ops-stat">
                    <p class="ops-stat-label truncate">{{ t(summary.label) }}</p>
                    <p class="ops-stat-value text-sm" :class="summary.label === 'admin.operations.profit' ? profitClass(summary.value) : ''">{{ formatMoney(summary.value) }}</p>
                  </div>
                </div>
                <div class="operations-grid ops-table-head px-4 py-2.5 sm:px-5">
                  <span>{{ t(activeBreakdownTab === 'groups' ? 'admin.operations.groups' : 'admin.operations.models') }}</span><span class="text-right">{{ t('admin.operations.profit') }}</span><span class="text-right">{{ t('admin.operations.userCost') }}</span><span class="text-right">{{ t('admin.operations.upstreamCost') }}</span>
                </div>
                <div v-if="activeBreakdownTab === 'groups'" class="max-h-[340px] overflow-y-auto overscroll-contain" data-testid="user-detail-groups">
                  <div v-for="group in userDetailGroups" :key="group.group_id" class="operations-grid ops-row ops-row-hover px-4 py-3 sm:px-5" :data-user-group="group.group_id">
                    <span class="flex min-w-0 items-center gap-2.5"><span class="ops-tile" :class="platformTextClass(group.platform)"><PlatformIcon :platform="group.platform" size="sm" /></span><span class="min-w-0 truncate text-sm font-medium text-gray-900 dark:text-white" :title="groupName(group)">{{ groupName(group) }}</span></span>
                    <span class="money-cell font-semibold" :class="profitClass(profitOf(group))">{{ formatMoney(profitOf(group)) }}</span><span class="money-cell">{{ formatMoney(group.user_cost) }}</span><span class="money-cell">{{ formatMoney(group.upstream_cost) }}</span>
                  </div>
                  <div v-if="!userDetailGroups.length" class="ops-empty"><p>{{ t('admin.operations.noGroups') }}</p></div>
                </div>
                <div v-else class="max-h-[340px] overflow-y-auto overscroll-contain" data-testid="user-detail-models">
                  <div v-for="model in userDetailModels" :key="model.model" class="operations-grid ops-row ops-row-hover px-4 py-3 sm:px-5" :data-user-model="model.model">
                    <span class="flex min-w-0 items-center gap-2.5"><span v-for="platform in modelPlatforms(model)" :key="platform" class="ops-tile" :class="platformTextClass(platform)" :title="platform"><PlatformIcon :platform="platform" size="sm" /></span><span class="min-w-0 break-words text-sm font-medium text-gray-900 dark:text-white">{{ model.model }}</span></span>
                    <span class="money-cell font-semibold" :class="profitClass(profitOf(model))">{{ formatMoney(profitOf(model)) }}</span><span class="money-cell">{{ formatMoney(model.user_cost) }}</span><span class="money-cell">{{ formatMoney(model.upstream_cost) }}</span>
                  </div>
                  <div v-if="!userDetailModels.length" class="ops-empty"><p>{{ t('admin.operations.noModels') }}</p></div>
                </div>
              </template>
              <div v-else class="ops-empty"><Icon name="userCircle" size="lg" class="text-primary-300" /><p>{{ t('admin.operations.userDetailsHint') }}</p></div>
            </div>
          </div>
        </section>

        <section class="grid grid-cols-1 items-stretch gap-6 xl:grid-cols-10" :aria-label="t('admin.operations.recharge.ordersTitle')" data-testid="recharge-dashboard-grid">
          <OperationsRechargeOrdersPanel class="xl:col-span-7"
            :orders="rechargeOrders" :loading="rechargeLoading" :error="rechargeError" :page="rechargePage" :pages="rechargePages" :total="rechargeTotal"
            :search="rechargeSearch" :suggestions="rechargeSuggestions" :suggestions-open="rechargeSuggestionsOpen" :suggestions-loading="rechargeSuggestionsLoading"
            :status="rechargeStatus" :channel="rechargeChannel" :days="rechargeDays"
            :summary="rechargeSummary" :summary-loading="rechargeSummaryLoading" :summary-error="rechargeSummaryError" :time-zone="siteTimeZone"
            @update:search="updateRechargeSearch" @focus-search="focusRechargeSearch" @blur-search="blurRechargeSearch"
            @select-suggestion="selectRechargeSuggestion" @select-status="selectRechargeStatus" @select-channel="selectRechargeChannel"
            @select-days="selectRechargeDays" @select-page="selectRechargePage" @retry="loadRechargeOrders" @retry-summary="loadRechargeSummary" />
          <OperationsRechargeRiskPanel class="xl:col-span-3" :events="rechargeRisks" :loading="rechargeRiskLoading" :error="rechargeRiskError" :time-zone="siteTimeZone" @retry="loadRechargeRisks" />
        </section>
      </template>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import type { OperationsDashboardResponse, OperationsGroupProfitStat, OperationsModelProfitStat, OperationsRechargeRankingResponse, OperationsUserSpendResponse } from '@/api/admin/dashboard'
import type { SimpleUser } from '@/api/admin/usage'
import type { GroupPlatform } from '@/types'
import { useAppStore } from '@/stores/app'
import AppLayout from '@/components/layout/AppLayout.vue'
import Icon from '@/components/icons/Icon.vue'
import PlatformIcon from '@/components/common/PlatformIcon.vue'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import OperationsTrendChart from '@/components/admin/dashboard/OperationsTrendChart.vue'
import OperationsUserSpendPanel from '@/components/admin/dashboard/OperationsUserSpendPanel.vue'
import OperationsRechargeOrdersPanel from '@/components/admin/dashboard/OperationsRechargeOrdersPanel.vue'
import OperationsRechargeRankingPanel from '@/components/admin/dashboard/OperationsRechargeRankingPanel.vue'
import OperationsRechargeRiskPanel from '@/components/admin/dashboard/OperationsRechargeRiskPanel.vue'
import type { RechargeChannel, RechargeOrder, RechargeRiskEvent, RechargeStatus, RechargeSummary } from './operationsRechargeTypes'
import { platformTextClass } from '@/utils/platformColors'
import { compareProfit, dateRange, formatMoney, profitOf, supportedTimeZone, zonedDate } from './operationsUtils'

const { t } = useI18n()
const appStore = useAppStore()
// Every "today" on this page is a calendar day of the site timezone counted
// from 00:00, the same day the server-side metrics and ranking use.
const siteTimeZone = ref(supportedTimeZone(appStore.cachedPublicSettings?.server_timezone))
const browserTimeZone = () => Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC'
const data = ref<OperationsDashboardResponse | null>(null)
const loading = ref(false)
const error = ref(false)
const updatedAt = ref('')
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
const rechargeRanking = ref<OperationsRechargeRankingResponse | null>(null)
const rankingLoading = ref(false)
const rankingError = ref(false)
const rechargeOrders = ref<RechargeOrder[]>([])
const rechargeRisks = ref<RechargeRiskEvent[]>([])
const rechargePage = ref(1)
const rechargePages = ref(1)
const rechargeTotal = ref(0)
const rechargeDays = ref<1 | 7 | 30>(1)
const rechargeStatus = ref<'all' | RechargeStatus>('all')
const rechargeChannel = ref<'all' | RechargeChannel>('all')
const rechargeSearch = ref('')
const rechargeSuggestions = ref<SimpleUser[]>([])
const rechargeSuggestionsOpen = ref(false)
const rechargeSuggestionsLoading = ref(false)
const rechargeLoading = ref(false)
const rechargeError = ref(false)
const rechargeSummary = ref<RechargeSummary | null>(null)
const rechargeSummaryLoading = ref(false)
const rechargeSummaryError = ref(false)
const rechargeRiskLoading = ref(false)
const rechargeRiskError = ref(false)
let rechargeSequence = 0
let rechargeSummarySequence = 0
let rechargeSuggestionSequence = 0
let rankingSequence = 0
let rechargeSearchTimer: ReturnType<typeof setTimeout> | undefined
let rechargeBlurTimer: ReturnType<typeof setTimeout> | undefined
let requestSequence = 0
let userRequestSequence = 0
let suggestionSequence = 0
let searchTimer: ReturnType<typeof setTimeout> | undefined
let blurTimer: ReturnType<typeof setTimeout> | undefined
let disposed = false
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
const metricBlocks = [
  { key: 'today', label: 'admin.operations.todayBlock', metrics: metrics.slice(0, 4) },
  { key: 'total', label: 'admin.operations.totalBlock', metrics: metrics.slice(4) }
] as const
const todayDate = computed(() => data.value?.today_date || zonedDate(new Date(), siteTimeZone.value))
const siteTimeZoneLabel = computed(() => siteTimeZone.value || data.value?.site_timezone || browserTimeZone())
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
// Rises are red and falls green, as on Chinese market boards.
const changeClass = (value: number) => value > 0 ? 'bg-rose-50 text-rose-500 dark:bg-rose-900/20 dark:text-rose-400' : value < 0 ? 'bg-emerald-50 text-emerald-600 dark:bg-emerald-900/20 dark:text-emerald-400' : 'bg-gray-100 text-gray-500 dark:bg-dark-700 dark:text-gray-400'
const consumptionRange = computed(() => dateRange(selectedDays.value, new Date(), siteTimeZone.value))
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
// The backend folds spend of deleted groups into one row with this id; the
// same id filters the model list down to that spend.
const DELETED_GROUPS_ID = -2
const groupName = (group: OperationsGroupProfitStat) => {
  if (group.group_id === 0) return t('admin.operations.unassigned')
  if (group.group_id === DELETED_GROUPS_ID) return t('admin.operations.deletedGroups')
  return group.group_name
}
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
    const range = dateRange(selectedDays.value, new Date(), siteTimeZone.value)
    const result = await adminAPI.dashboard.getOperationsUserSpend({
      start_date: range.start,
      end_date: range.end,
      timezone: siteTimeZone.value || browserTimeZone(),
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
async function loadRechargeRanking() {
  const sequence = ++rankingSequence
  rankingLoading.value = true
  rankingError.value = false
  try {
    const result = await adminAPI.dashboard.getOperationsRechargeRanking()
    if (sequence === rankingSequence) rechargeRanking.value = result
  } catch (reason) {
    if (sequence !== rankingSequence) return
    rankingError.value = true
    console.error('Failed to load recharge ranking:', reason)
  } finally {
    if (sequence === rankingSequence) rankingLoading.value = false
  }
}
function rechargeFilters() {
  const range = dateRange(rechargeDays.value, new Date(), siteTimeZone.value)
  return {
    start_date: range.start, end_date: range.end, timezone: siteTimeZone.value,
    ...(rechargeSearch.value.trim() ? { search: rechargeSearch.value.trim() } : {}),
    ...(rechargeChannel.value !== 'all' ? { channel: rechargeChannel.value } : {})
  }
}
async function loadRechargeOrders() {
  const sequence = ++rechargeSequence
  rechargeLoading.value = true
  rechargeError.value = false
  try {
    const result = await adminAPI.dashboard.getOperationsRechargeOrders({
      page: rechargePage.value, page_size: 25, ...rechargeFilters(),
      ...(rechargeStatus.value !== 'all' ? { status: rechargeStatus.value } : {})
    })
    if (sequence !== rechargeSequence) return
    rechargeOrders.value = result.items ?? []
    rechargeTotal.value = result.total
    rechargePages.value = Math.max(1, result.pages)
  } catch (reason) {
    if (sequence !== rechargeSequence) return
    rechargeError.value = true
    console.error('Failed to load recharge orders:', reason)
  } finally {
    if (sequence === rechargeSequence) rechargeLoading.value = false
  }
}
// The totals follow the time, channel and search filters but not the status
// filter or the page, so only those reload them.
async function loadRechargeSummary() {
  const sequence = ++rechargeSummarySequence
  rechargeSummaryLoading.value = true
  rechargeSummaryError.value = false
  try {
    const result = await adminAPI.dashboard.getOperationsRechargeSummary(rechargeFilters())
    if (sequence === rechargeSummarySequence) rechargeSummary.value = result
  } catch (reason) {
    if (sequence !== rechargeSummarySequence) return
    rechargeSummaryError.value = true
    console.error('Failed to load recharge summary:', reason)
  } finally {
    if (sequence === rechargeSummarySequence) rechargeSummaryLoading.value = false
  }
}
async function loadRechargeRisks() {
  rechargeRiskLoading.value = true
  rechargeRiskError.value = false
  try { rechargeRisks.value = (await adminAPI.dashboard.getOperationsRechargeRisks()).slice(0, 25) }
  catch (reason) { rechargeRiskError.value = true; console.error('Failed to load recharge risks:', reason) }
  finally { rechargeRiskLoading.value = false }
}
function resetRechargeOrders() { rechargePage.value = 1; void loadRechargeOrders() }
function reloadRechargeTotals() { resetRechargeOrders(); void loadRechargeSummary() }
function selectRechargeStatus(value: 'all' | RechargeStatus) { if (rechargeStatus.value !== value) { rechargeStatus.value = value; resetRechargeOrders() } }
function selectRechargeChannel(value: 'all' | RechargeChannel) { if (rechargeChannel.value !== value) { rechargeChannel.value = value; reloadRechargeTotals() } }
function selectRechargeDays(value: 1 | 7 | 30) { if (rechargeDays.value !== value) { rechargeDays.value = value; reloadRechargeTotals() } }
function selectRechargePage(value: number) { if (value >= 1 && value <= rechargePages.value && value !== rechargePage.value) { rechargePage.value = value; void loadRechargeOrders() } }
function updateRechargeSearch(value: string) {
  rechargeSearch.value = value
  rechargeSuggestions.value = []
  rechargeSuggestionsOpen.value = Boolean(value.trim())
  rechargeSuggestionSequence++
  rechargeSequence++
  rechargeSummarySequence++
  if (rechargeSearchTimer) clearTimeout(rechargeSearchTimer)
  if (!value.trim()) { rechargeSuggestionsLoading.value = false; reloadRechargeTotals(); return }
  rechargeSearchTimer = setTimeout(() => { void loadRechargeSuggestions(value.trim()); reloadRechargeTotals() }, 250)
}
async function loadRechargeSuggestions(keyword: string) {
  const sequence = ++rechargeSuggestionSequence
  rechargeSuggestionsLoading.value = true
  try {
    const result = await adminAPI.dashboard.getOperationsUserSuggestions(keyword)
    if (sequence === rechargeSuggestionSequence && rechargeSearch.value.trim() === keyword)
      rechargeSuggestions.value = result.map(user => ({ id: user.user_id, email: user.email, deleted: false }))
  } catch (reason) {
    if (sequence === rechargeSuggestionSequence) { rechargeSuggestions.value = []; console.error('Failed to search recharge user emails:', reason) }
  } finally { if (sequence === rechargeSuggestionSequence) rechargeSuggestionsLoading.value = false }
}
function focusRechargeSearch() { if (rechargeSearch.value.trim()) { rechargeSuggestionsOpen.value = true; if (!rechargeSuggestions.value.length) void loadRechargeSuggestions(rechargeSearch.value.trim()) } }
function blurRechargeSearch() { rechargeBlurTimer = setTimeout(() => { rechargeSuggestionsOpen.value = false }, 150) }
function selectRechargeSuggestion(user: SimpleUser) {
  if (rechargeSearchTimer) clearTimeout(rechargeSearchTimer)
  if (rechargeBlurTimer) clearTimeout(rechargeBlurTimer)
  rechargeSuggestionSequence++
  rechargeSearch.value = user.email
  rechargeSuggestionsOpen.value = false
  reloadRechargeTotals()
}
function refreshAll() {
  void loadData()
  void loadUserSpend()
  void loadRechargeRanking()
  void loadRechargeOrders()
  void loadRechargeSummary()
  void loadRechargeRisks()
}
async function loadData() {
  const sequence = ++requestSequence
  loading.value = true
  error.value = false
  const range = dateRange(selectedDays.value, new Date(), siteTimeZone.value)
  try {
    const result = await adminAPI.dashboard.getOperationsDashboard({
      start_date: range.start, end_date: range.end, trend_days: trendDays.value,
      timezone: siteTimeZone.value || browserTimeZone(),
      group_id: selectedGroup.value === null ? 0 : selectedGroup.value === 0 ? -1 : selectedGroup.value
    })
    if (sequence !== requestSequence) return
    data.value = result
    updatedAt.value = new Intl.DateTimeFormat(undefined, { hour: '2-digit', minute: '2-digit', second: '2-digit', hourCycle: 'h23', timeZone: siteTimeZone.value }).format(new Date())
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
onMounted(async () => {
  window.addEventListener('resize', onResize)
  // Public settings carry the site timezone; they are normally cached already.
  const settings = await appStore.fetchPublicSettings().catch(() => null)
  if (disposed) return
  siteTimeZone.value = supportedTimeZone(settings?.server_timezone) ?? siteTimeZone.value
  void loadData()
  void loadUserSpend()
  void loadRechargeRanking()
  void loadRechargeOrders()
  void loadRechargeSummary()
  void loadRechargeRisks()
})
onUnmounted(() => {
  disposed = true
  requestSequence++
  userRequestSequence++
  suggestionSequence++
  rankingSequence++
  rechargeSequence++
  rechargeSummarySequence++
  rechargeSuggestionSequence++
  if (rechargeSearchTimer) clearTimeout(rechargeSearchTimer)
  if (rechargeBlurTimer) clearTimeout(rechargeBlurTimer)
  if (searchTimer) clearTimeout(searchTimer)
  if (blurTimer) clearTimeout(blurTimer)
  window.removeEventListener('resize', onResize)
})
</script>

<style scoped>
.ops-metric {
  display: flex;
  flex-direction: column;
  padding: 0.875rem;
  overflow: hidden;
}

.ops-metric:hover { box-shadow: var(--theme-highlight), var(--theme-elevation-2); }

/* Today's cards sit on a warmer surface than the running totals. */
.ops-metric-today {
  background: radial-gradient(120% 90% at 100% 0%, rgb(255 243 189 / 0.55), transparent 60%), var(--md-sys-color-surface);
}

.ops-metric-icon {
  display: flex;
  flex-shrink: 0;
  align-items: center;
  justify-content: center;
  width: 1.875rem;
  height: 1.875rem;
  border-radius: 0.75rem;
}

.ops-metric-value {
  overflow-wrap: anywhere;
  font-size: 1.125rem;
  font-weight: 750;
  line-height: 1.2;
  letter-spacing: -0.01em;
  font-variant-numeric: tabular-nums;
  color: var(--md-sys-color-on-surface);
}

.ops-metric-main {
  flex: 1;
  padding-bottom: 0.75rem;
}

.ops-metric-foot {
  display: flex;
  flex-direction: column;
  gap: 0.375rem;
  padding-top: 0.625rem;
  font-size: 0.75rem;
  color: #716744;
  box-shadow: inset 0 1px 0 rgb(215 193 106 / 0.28);
}

.ops-change {
  display: inline-flex;
  align-items: center;
  padding: 0.0625rem 0.5rem;
  border-radius: 999px;
  font-weight: 650;
  font-variant-numeric: tabular-nums;
  white-space: nowrap;
}

.ops-tile {
  display: flex;
  flex-shrink: 0;
  align-items: center;
  justify-content: center;
  width: 2rem;
  height: 2rem;
  border-radius: 0.75rem;
  background: color-mix(in srgb, var(--md-sys-color-surface-container) 70%, transparent);
}

.operations-grid { display: grid; grid-template-columns: minmax(0, 1.9fr) repeat(3, minmax(0, 1fr)); gap: 0.5rem; align-items: center; }
.money-cell { text-align: right; font-size: 0.75rem; font-weight: 500; line-height: 1.4; font-variant-numeric: tabular-nums; overflow-wrap: anywhere; }

/* Values size with the card: two wide columns on tablets, four narrow ones
 * next to the sidebar on laptops. */
@media (min-width: 640px) {
  .ops-metric { padding: 1.125rem 1.25rem; }
  .ops-metric-icon { width: 2.25rem; height: 2.25rem; border-radius: 0.875rem; }
  .ops-metric-value { font-size: 1.5rem; }
}

@media (min-width: 1024px) { .ops-metric-value { font-size: 1.25rem; } }
@media (min-width: 1280px) { .ops-metric-value { font-size: 1.375rem; } }
@media (min-width: 1536px) { .ops-metric-value { font-size: 1.5rem; } }

@media (max-width: 639px) {
  .operations-grid { grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 0.375rem; }
  .operations-grid > :first-child { grid-column: 1 / -1; }
}

@media (min-width: 1536px) { .operations-grid { gap: 1rem; } }
</style>
