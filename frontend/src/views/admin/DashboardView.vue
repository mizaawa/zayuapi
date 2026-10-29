<template>
  <AppLayout class="dashboard-layout">
    <div class="space-y-6">
      <!-- Loading State -->
      <div v-if="loading" class="flex items-center justify-center py-12">
        <LoadingSpinner />
      </div>

      <template v-else-if="stats">
        <!-- Today: traffic leads; spend, cost and speed sit beside it. -->
        <section class="card adm-hero">
          <div class="adm-traffic">
            <p class="adm-traffic-label">{{ t('admin.dashboard.todayRequests') }}</p>
            <p class="adm-traffic-figure">{{ formatNumber(stats.today_requests) }}</p>
            <p class="adm-traffic-caption">{{ t('common.total') }} {{ formatNumber(stats.total_requests) }}</p>
            <p class="adm-live">
              <span class="adm-live-dot" :class="{ 'is-active': (stats.rpm || 0) > 0 }" aria-hidden="true"></span>
              {{ t('admin.dashboard.liveThroughput', { rpm: formatTokens(stats.rpm), tpm: formatTokens(stats.tpm) }) }}
            </p>
          </div>

          <div class="adm-today">
            <header class="adm-today-head">
              <h2 class="adm-section-title">{{ t('admin.dashboard.today') }}</h2>
              <span
                v-if="statsUpdatedTime"
                class="adm-updated"
                :class="{ 'is-stale': stats.stats_stale }"
                data-testid="dashboard-stats-updated"
              >
                {{ stats.stats_stale
                  ? t('admin.dashboard.statsStale', { time: statsUpdatedTime })
                  : t('admin.dashboard.statsUpdatedAt', { time: statsUpdatedTime }) }}
              </span>
            </header>

            <dl class="adm-kpis">
              <div class="adm-kpi">
                <dt>{{ t('admin.dashboard.todayTokens') }}</dt>
                <dd class="adm-kpi-value">{{ formatTokens(stats.today_tokens) }}</dd>
                <dd class="adm-kpi-sub">{{ t('common.total') }} {{ formatTokens(stats.total_tokens) }}</dd>
              </div>
              <div class="adm-kpi">
                <dt>{{ t('admin.dashboard.todayActualCost') }}</dt>
                <dd class="adm-kpi-value" :title="t('admin.dashboard.actual')">${{ formatCost(stats.today_actual_cost) }}</dd>
                <dd class="adm-kpi-sub">
                  <span
                    v-if="(stats.today_cost || 0) > (stats.today_actual_cost || 0)"
                    class="adm-list-price"
                    :title="t('admin.dashboard.standard')"
                  >${{ formatCost(stats.today_cost) }}</span>
                  <span>{{ t('common.total') }} ${{ formatCost(stats.total_actual_cost) }}</span>
                </dd>
              </div>
              <div class="adm-kpi">
                <dt>{{ t('admin.dashboard.todayAccountCost') }}</dt>
                <dd class="adm-kpi-value">${{ formatCost(stats.today_account_cost) }}</dd>
                <dd class="adm-kpi-sub">{{ t('common.total') }} ${{ formatCost(stats.total_account_cost) }}</dd>
              </div>
              <div class="adm-kpi">
                <dt>{{ t('admin.dashboard.avgResponse') }}</dt>
                <dd class="adm-kpi-value">{{ formatDuration(stats.average_duration_ms) }}</dd>
                <dd class="adm-kpi-sub">{{ stats.active_users }} {{ t('admin.dashboard.activeUsers') }}</dd>
              </div>
            </dl>

            <!-- Token mix: same entity colours as the token trend chart below. -->
            <div class="adm-mix">
              <div class="adm-mix-head">
                <span>{{ t('admin.dashboard.tokenMix') }}</span>
                <span v-if="tokenMix.total === 0" class="font-normal">{{ t('admin.dashboard.noTokensToday') }}</span>
              </div>
              <div class="adm-mix-bar" role="img" :aria-label="tokenMixLabel">
                <span
                  v-for="part in visibleTokenParts"
                  :key="part.key"
                  class="adm-mix-segment"
                  :style="{ width: part.percent + '%', background: part.color }"
                  :title="part.label + ' ' + formatTokens(part.value)"
                ></span>
              </div>
              <ul class="adm-mix-legend">
                <li v-for="part in tokenMix.parts" :key="part.key" :class="{ 'is-zero': part.value === 0 }">
                  <span class="adm-mix-swatch" :style="{ background: part.color }" aria-hidden="true"></span>
                  <span>{{ part.label }}</span>
                  <span class="adm-mix-value">{{ formatTokens(part.value) }}</span>
                </li>
              </ul>
            </div>
          </div>
        </section>

        <!-- Who and what is behind the traffic. -->
        <div class="adm-resources">
          <button type="button" class="card adm-resource" @click="router.push('/admin/users')">
            <span class="adm-resource-head">
              <span class="adm-resource-icon is-users" aria-hidden="true"><Icon name="users" size="md" /></span>
              <span class="adm-resource-label">{{ t('admin.dashboard.users') }}</span>
              <Icon name="chevronRight" size="sm" class="adm-resource-chevron" />
            </span>
            <span class="adm-resource-value">{{ formatNumber(stats.total_users) }}</span>
            <span class="adm-resource-facts">
              <span><strong class="text-accent-700">+{{ formatNumber(stats.today_new_users) }}</strong> {{ t('admin.dashboard.newToday') }}</span>
              <span><strong>{{ formatNumber(stats.active_users) }}</strong> {{ t('admin.dashboard.activeToday') }}</span>
              <span><strong>{{ formatNumber(stats.hourly_active_users) }}</strong> {{ t('admin.dashboard.activeThisHour') }}</span>
            </span>
          </button>

          <button type="button" class="card adm-resource" @click="router.push('/admin/accounts')">
            <span class="adm-resource-head">
              <span class="adm-resource-icon is-accounts" aria-hidden="true"><Icon name="server" size="md" /></span>
              <span class="adm-resource-label">{{ t('admin.dashboard.accounts') }}</span>
              <Icon name="chevronRight" size="sm" class="adm-resource-chevron" />
            </span>
            <span class="adm-resource-value">{{ formatNumber(stats.total_accounts) }}</span>
            <span class="adm-meter" aria-hidden="true">
              <span class="adm-meter-fill" :style="{ width: sharePercent(stats.normal_accounts, stats.total_accounts) + '%' }"></span>
            </span>
            <span class="adm-resource-facts">
              <span><strong>{{ formatNumber(stats.normal_accounts) }}</strong> {{ t('admin.dashboard.schedulableAccounts') }}</span>
              <span v-for="flag in accountFlags" :key="flag.key" class="adm-flag" :class="'is-' + flag.tone">
                <Icon :name="flag.icon" size="xs" />
                {{ flag.label }} {{ flag.count }}
              </span>
              <span v-if="accountFlags.length === 0" class="adm-flag is-good">
                <Icon name="checkCircle" size="xs" />
                {{ t('admin.dashboard.accountsHealthy') }}
              </span>
            </span>
          </button>

          <div class="card adm-resource">
            <span class="adm-resource-head">
              <span class="adm-resource-icon is-keys" aria-hidden="true"><Icon name="key" size="md" /></span>
              <span class="adm-resource-label">{{ t('admin.dashboard.apiKeys') }}</span>
            </span>
            <span class="adm-resource-value">{{ formatNumber(stats.total_api_keys) }}</span>
            <span class="adm-meter" aria-hidden="true">
              <span class="adm-meter-fill" :style="{ width: sharePercent(stats.active_api_keys, stats.total_api_keys) + '%' }"></span>
            </span>
            <span class="adm-resource-facts">
              <span><strong>{{ formatNumber(stats.active_api_keys) }}</strong> {{ t('admin.dashboard.activeKeys') }}</span>
            </span>
          </div>

          <div class="card adm-shortcuts">
            <p class="adm-shortcuts-title">{{ t('admin.dashboard.quickActions') }}</p>
            <button
              v-if="canUseBatchImage"
              type="button"
              class="adm-shortcut"
              @click="router.push('/batch-image')"
            >
              <span class="adm-shortcut-icon is-image" aria-hidden="true"><Icon name="sparkles" size="sm" /></span>
              <span class="min-w-0 flex-1">
                <span class="adm-shortcut-label">{{ t('admin.dashboard.batchImage') }}</span>
                <span class="adm-shortcut-desc">{{ t('admin.dashboard.batchImageDesc') }}</span>
              </span>
            </button>
            <button type="button" class="adm-shortcut" @click="router.push('/admin/groups')">
              <span class="adm-shortcut-icon" aria-hidden="true"><Icon name="grid" size="sm" /></span>
              <span class="min-w-0 flex-1">
                <span class="adm-shortcut-label">{{ t('admin.dashboard.groupPricing') }}</span>
                <span class="adm-shortcut-desc">{{ t('admin.dashboard.groupPricingDesc') }}</span>
              </span>
            </button>
          </div>
        </div>

        <!-- One usage panel: controls in its header, charts below. -->
        <section class="card adm-usage">
          <header class="adm-usage-head">
            <h2 class="adm-section-title">{{ t('admin.dashboard.usageTrends') }}</h2>
            <div class="adm-usage-controls">
              <DateRangePicker
                v-model:start-date="startDate"
                v-model:end-date="endDate"
                @change="onDateRangeChange"
              />
              <div class="segment-group" role="radiogroup" :aria-label="t('admin.dashboard.granularity')">
                <button
                  v-for="option in granularityOptions"
                  :key="option.value"
                  type="button"
                  role="radio"
                  class="adm-segment"
                  :class="granularity === option.value ? 'segment-active' : 'segment-idle'"
                  :aria-checked="granularity === option.value"
                  @click="selectGranularity(option.value)"
                >
                  {{ option.label }}
                </button>
              </div>
              <button
                type="button"
                class="btn btn-secondary adm-refresh"
                :disabled="chartsLoading"
                :title="t('common.refresh')"
                :aria-label="t('common.refresh')"
                @click="loadDashboardStats"
              >
                <Icon name="refresh" size="sm" :class="chartsLoading ? 'animate-spin' : ''" />
              </button>
            </div>
          </header>

          <div class="adm-charts">
            <ModelDistributionChart
              :model-stats="modelStats"
              :enable-ranking-view="true"
              :ranking-items="rankingItems"
              :ranking-total-actual-cost="rankingTotalActualCost"
              :ranking-total-requests="rankingTotalRequests"
              :ranking-total-tokens="rankingTotalTokens"
              :loading="chartsLoading"
              :ranking-loading="rankingLoading"
              :ranking-error="rankingError"
              :start-date="startDate"
              :end-date="endDate"
              @ranking-click="goToUserUsage"
            />
            <div class="adm-trend">
              <TokenUsageTrend :trend-data="trendData" :loading="chartsLoading" />
            </div>
          </div>

          <div class="adm-user-trend">
            <h3 class="adm-chart-title">{{ t('admin.dashboard.userUsageTrend') }}</h3>
            <div class="h-64">
              <div v-if="userTrendLoading" class="flex h-full items-center justify-center">
                <LoadingSpinner size="md" />
              </div>
              <Line v-else-if="userTrendChartData" :data="userTrendChartData" :options="lineOptions" />
              <div
                v-else
                class="flex h-full items-center justify-center text-sm text-gray-500 dark:text-gray-400"
              >
                {{ t('admin.dashboard.noDataAvailable') }}
              </div>
            </div>
          </div>
        </section>
      </template>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { useAppStore } from '@/stores/app'

const { t } = useI18n()
import { adminAPI } from '@/api/admin'
import type {
  DashboardStats,
  TrendDataPoint,
  ModelStat,
  UserUsageTrendPoint,
  UserSpendingRankingItem
} from '@/types'
import AppLayout from '@/components/layout/AppLayout.vue'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import Icon from '@/components/icons/Icon.vue'
import DateRangePicker from '@/components/common/DateRangePicker.vue'
import ModelDistributionChart from '@/components/charts/ModelDistributionChart.vue'
import TokenUsageTrend from '@/components/charts/TokenUsageTrend.vue'
import { useBatchImageAccess } from '@/composables/useBatchImageAccess'

import {
  Chart as ChartJS,
  CategoryScale,
  LinearScale,
  PointElement,
  LineElement,
  Tooltip,
  Legend,
  Filler
} from 'chart.js'
import { Line } from 'vue-chartjs'

// Register Chart.js components
ChartJS.register(
  CategoryScale,
  LinearScale,
  PointElement,
  LineElement,
  Tooltip,
  Legend,
  Filler
)

const appStore = useAppStore()
const router = useRouter()
const { canUseBatchImage, refreshBatchImageAccess } = useBatchImageAccess()
const stats = ref<DashboardStats | null>(null)
const loading = ref(false)
const chartsLoading = ref(false)
const userTrendLoading = ref(false)
const rankingLoading = ref(false)
const rankingError = ref(false)

// Chart data
const trendData = ref<TrendDataPoint[]>([])
const modelStats = ref<ModelStat[]>([])
const userTrend = ref<UserUsageTrendPoint[]>([])
const rankingItems = ref<UserSpendingRankingItem[]>([])
const rankingTotalActualCost = ref(0)
const rankingTotalRequests = ref(0)
const rankingTotalTokens = ref(0)
let chartLoadSeq = 0
let usersTrendLoadSeq = 0
let rankingLoadSeq = 0
const rankingLimit = 12

// Helper function to format date in local timezone
const formatLocalDate = (date: Date): string => {
  return `${date.getFullYear()}-${String(date.getMonth() + 1).padStart(2, '0')}-${String(date.getDate()).padStart(2, '0')}`
}

const getLast24HoursRangeDates = (): { start: string; end: string } => {
  const end = new Date()
  const start = new Date(end.getTime() - 24 * 60 * 60 * 1000)
  return {
    start: formatLocalDate(start),
    end: formatLocalDate(end)
  }
}

// Date range
const granularity = ref<'day' | 'hour'>('hour')
const defaultRange = getLast24HoursRangeDates()
const startDate = ref(defaultRange.start)
const endDate = ref(defaultRange.end)

// Granularity options for the segmented switch
const granularityOptions = computed(() => [
  { value: 'day' as const, label: t('admin.dashboard.day') },
  { value: 'hour' as const, label: t('admin.dashboard.hour') }
])

const selectGranularity = (value: 'day' | 'hour') => {
  if (granularity.value === value) return
  granularity.value = value
  void loadChartData()
}

// Console pages are light-only; chart chrome uses the warm theme inks.
const chartColors = {
  text: '#6b6045',
  grid: '#eee5c9'
}

const statsUpdatedTime = computed(() => {
  const value = stats.value?.stats_updated_at
  if (!value) return ''
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return ''
  return date.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit', hour12: false })
})

// Entity colours match TokenUsageTrend so input/output/cache read the same on the page.
const TOKEN_MIX_COLORS = {
  input: '#3b82f6',
  output: '#10b981',
  cacheWrite: '#f59e0b',
  cacheRead: '#06b6d4'
} as const

const tokenMix = computed(() => {
  const current = stats.value
  const parts = [
    { key: 'input', label: t('admin.dashboard.input'), value: toFiniteNumber(current?.today_input_tokens), color: TOKEN_MIX_COLORS.input },
    { key: 'output', label: t('admin.dashboard.output'), value: toFiniteNumber(current?.today_output_tokens), color: TOKEN_MIX_COLORS.output },
    { key: 'cacheWrite', label: t('admin.dashboard.cacheWrite'), value: toFiniteNumber(current?.today_cache_creation_tokens), color: TOKEN_MIX_COLORS.cacheWrite },
    { key: 'cacheRead', label: t('admin.dashboard.cacheRead'), value: toFiniteNumber(current?.today_cache_read_tokens), color: TOKEN_MIX_COLORS.cacheRead }
  ]
  const total = parts.reduce((sum, part) => sum + Math.max(part.value, 0), 0)
  return {
    total,
    parts: parts.map((part) => ({ ...part, percent: total > 0 ? (Math.max(part.value, 0) / total) * 100 : 0 }))
  }
})

const visibleTokenParts = computed(() => tokenMix.value.parts.filter((part) => part.value > 0))

const tokenMixLabel = computed(() =>
  tokenMix.value.parts.map((part) => `${part.label} ${formatTokens(part.value)}`).join(', ')
)

const sharePercent = (part: number | null | undefined, total: number | null | undefined): number => {
  const whole = toFiniteNumber(total)
  if (whole <= 0) return 0
  return Math.min(Math.max((toFiniteNumber(part) / whole) * 100, 0), 100)
}

// Counts that need attention; each carries an icon and a label, never colour alone.
const accountFlags = computed(() => {
  const current = stats.value
  if (!current) return []
  const flags: Array<{ key: string; label: string; count: number; tone: 'warning' | 'serious' | 'critical'; icon: 'clock' | 'bolt' | 'exclamationTriangle' }> = [
    { key: 'ratelimit', label: t('admin.dashboard.rateLimitedAccounts'), count: toFiniteNumber(current.ratelimit_accounts), tone: 'warning', icon: 'clock' },
    { key: 'overload', label: t('admin.dashboard.overloadedAccounts'), count: toFiniteNumber(current.overload_accounts), tone: 'serious', icon: 'bolt' },
    { key: 'error', label: t('admin.dashboard.errorAccounts'), count: toFiniteNumber(current.error_accounts), tone: 'critical', icon: 'exclamationTriangle' }
  ]
  return flags.filter((flag) => flag.count > 0)
})

// Line chart options (for user trend chart)
const lineOptions = computed(() => ({
  responsive: true,
  maintainAspectRatio: false,
  interaction: {
    intersect: false,
    mode: 'index' as const
  },
  plugins: {
    legend: {
      position: 'top' as const,
      labels: {
        color: chartColors.text,
        usePointStyle: true,
        pointStyle: 'circle',
        padding: 15,
        font: {
          size: 11
        }
      }
    },
    tooltip: {
      itemSort: (a: any, b: any) => {
        const aValue = typeof a?.raw === 'number' ? a.raw : Number(a?.parsed?.y ?? 0)
        const bValue = typeof b?.raw === 'number' ? b.raw : Number(b?.parsed?.y ?? 0)
        return bValue - aValue
      },
      callbacks: {
        label: (context: any) => {
          return `${context.dataset.label}: ${formatTokens(context.raw)}`
        }
      }
    }
  },
  scales: {
    x: {
      grid: {
        color: chartColors.grid
      },
      ticks: {
        color: chartColors.text,
        font: {
          size: 10
        }
      }
    },
    y: {
      grid: {
        color: chartColors.grid
      },
      ticks: {
        color: chartColors.text,
        font: {
          size: 10
        },
        callback: (value: string | number) => formatTokens(Number(value))
      }
    }
  }
}))

// User trend chart data
const userTrendChartData = computed(() => {
  if (!userTrend.value?.length) return null

  const getDisplayName = (point: UserUsageTrendPoint): string => {
    const username = point.username?.trim()
    if (username) {
      return username
    }

    const email = point.email?.trim()
    if (email) {
      return email
    }

    return t('admin.redeem.userPrefix', { id: point.user_id })
  }

  // Group by user_id to avoid merging different users with the same display name
  const userGroups = new Map<number, { name: string; data: Map<string, number> }>()
  const allDates = new Set<string>()

  userTrend.value.forEach((point) => {
    allDates.add(point.date)
    const key = point.user_id
    if (!userGroups.has(key)) {
      userGroups.set(key, { name: getDisplayName(point), data: new Map() })
    }
    userGroups.get(key)!.data.set(point.date, point.tokens)
  })

  const sortedDates = Array.from(allDates).sort()
  const colors = [
    '#3b82f6',
    '#10b981',
    '#f59e0b',
    '#ef4444',
    '#8b5cf6',
    '#ec4899',
    '#14b8a6',
    '#f97316',
    '#6366f1',
    '#84cc16',
    '#06b6d4',
    '#a855f7'
  ]

  const datasets = Array.from(userGroups.values()).map((group, idx) => ({
    label: group.name,
    data: sortedDates.map((date) => group.data.get(date) || 0),
    borderColor: colors[idx % colors.length],
    backgroundColor: `${colors[idx % colors.length]}20`,
    fill: false,
    tension: 0.3
  }))

  return {
    labels: sortedDates,
    datasets
  }
})

// Format helpers
const formatTokens = (value: number | undefined): string => {
  if (value === undefined || value === null) return '0'
  if (value >= 1_000_000_000) {
    return `${(value / 1_000_000_000).toFixed(2)}B`
  } else if (value >= 1_000_000) {
    return `${(value / 1_000_000).toFixed(2)}M`
  } else if (value >= 1_000) {
    return `${(value / 1_000).toFixed(2)}K`
  }
  return value.toLocaleString()
}

const toFiniteNumber = (value: unknown): number => {
  const numberValue = Number(value)
  return Number.isFinite(numberValue) ? numberValue : 0
}

const formatNumber = (value: number | null | undefined): string => {
  return toFiniteNumber(value).toLocaleString()
}

const formatCost = (value: number | null | undefined): string => {
  const safeValue = toFiniteNumber(value)
  if (safeValue >= 1000) {
    return (safeValue / 1000).toFixed(2) + 'K'
  } else if (safeValue >= 1) {
    return safeValue.toFixed(2)
  } else if (safeValue >= 0.01) {
    return safeValue.toFixed(3)
  }
  return safeValue.toFixed(4)
}

const formatDuration = (ms: number): string => {
  if (ms >= 1000) {
    return `${(ms / 1000).toFixed(2)}s`
  }
  return `${Math.round(ms)}ms`
}

const goToUserUsage = (item: UserSpendingRankingItem) => {
  void router.push({
    path: '/admin/usage',
    query: {
      user_id: String(item.user_id),
      start_date: startDate.value,
      end_date: endDate.value
    }
  })
}

// Date range change handler
const onDateRangeChange = (range: {
  startDate: string
  endDate: string
  preset: string | null
}) => {
  // Auto-select granularity based on date range
  const start = new Date(range.startDate)
  const end = new Date(range.endDate)
  const daysDiff = Math.ceil((end.getTime() - start.getTime()) / (1000 * 60 * 60 * 24))

  // If range is 1 day, use hourly granularity
  if (daysDiff <= 1) {
    granularity.value = 'hour'
  } else {
    granularity.value = 'day'
  }

  loadChartData()
}

// Load data
const loadDashboardSnapshot = async (includeStats: boolean) => {
  const currentSeq = ++chartLoadSeq
  if (includeStats && !stats.value) {
    loading.value = true
  }
  chartsLoading.value = true
  try {
    const response = await adminAPI.dashboard.getSnapshotV2({
      start_date: startDate.value,
      end_date: endDate.value,
      granularity: granularity.value,
      include_stats: includeStats,
      include_trend: true,
      include_model_stats: true,
      include_group_stats: false,
      include_users_trend: false
    })
    if (currentSeq !== chartLoadSeq) return
    if (includeStats && response.stats) {
      stats.value = response.stats
    }
    trendData.value = response.trend || []
    modelStats.value = response.models || []
  } catch (error) {
    if (currentSeq !== chartLoadSeq) return
    appStore.showError(t('admin.dashboard.failedToLoad'))
    console.error('Error loading dashboard snapshot:', error)
  } finally {
    if (currentSeq === chartLoadSeq) {
      loading.value = false
      chartsLoading.value = false
    }
  }
}

const loadUsersTrend = async () => {
  const currentSeq = ++usersTrendLoadSeq
  userTrendLoading.value = true
  try {
    const response = await adminAPI.dashboard.getUserUsageTrend({
      start_date: startDate.value,
      end_date: endDate.value,
      granularity: granularity.value,
      limit: 12
    })
    if (currentSeq !== usersTrendLoadSeq) return
    userTrend.value = response.trend || []
  } catch (error) {
    if (currentSeq !== usersTrendLoadSeq) return
    console.error('Error loading users trend:', error)
    userTrend.value = []
  } finally {
    if (currentSeq === usersTrendLoadSeq) {
      userTrendLoading.value = false
    }
  }
}

const loadUserSpendingRanking = async () => {
  const currentSeq = ++rankingLoadSeq
  rankingLoading.value = true
  rankingError.value = false
  try {
    const response = await adminAPI.dashboard.getUserSpendingRanking({
      start_date: startDate.value,
      end_date: endDate.value,
      limit: rankingLimit
    })
    if (currentSeq !== rankingLoadSeq) return
    rankingItems.value = response.ranking || []
    rankingTotalActualCost.value = response.total_actual_cost || 0
    rankingTotalRequests.value = response.total_requests || 0
    rankingTotalTokens.value = response.total_tokens || 0
  } catch (error) {
    if (currentSeq !== rankingLoadSeq) return
    console.error('Error loading user spending ranking:', error)
    rankingItems.value = []
    rankingTotalActualCost.value = 0
    rankingTotalRequests.value = 0
    rankingTotalTokens.value = 0
    rankingError.value = true
  } finally {
    if (currentSeq === rankingLoadSeq) {
      rankingLoading.value = false
    }
  }
}

const loadDashboardStats = async () => {
  await Promise.all([
    loadDashboardSnapshot(true),
    loadUsersTrend(),
    loadUserSpendingRanking()
  ])
}

const loadChartData = async () => {
  await Promise.all([
    loadDashboardSnapshot(false),
    loadUsersTrend(),
    loadUserSpendingRanking()
  ])
}

onMounted(() => {
  void refreshBatchImageAccess()
  loadDashboardStats()
})
</script>

<style scoped>
/* ---------- Today ---------- */
.adm-hero {
  display: grid;
  gap: 1.25rem;
  padding: 1.25rem;
}

.adm-traffic {
  display: flex;
  min-width: 0;
  flex-direction: column;
  justify-content: center;
  gap: 0.375rem;
  padding: 1.25rem 1.375rem;
  border-radius: 1.25rem;
  background: #fff7d6;
}

.adm-traffic-label {
  font-size: 0.875rem;
  font-weight: 600;
  color: #6b6045;
}

/* The one hero figure on this page; it shrinks before it would ever wrap. */
.adm-traffic-figure {
  font-size: clamp(2.25rem, 1.25rem + 2.4vw, 3.25rem);
  font-weight: 700;
  line-height: 1.05;
  letter-spacing: -0.02em;
  white-space: nowrap;
  color: #3f3420;
}

.adm-traffic-caption {
  font-size: 0.8125rem;
  color: #6b6045;
}

.adm-live {
  display: inline-flex;
  align-items: center;
  align-self: flex-start;
  gap: 0.375rem;
  margin-top: 0.375rem;
  padding: 0.1875rem 0.625rem;
  border-radius: 999px;
  background: rgb(255 254 248 / 0.75);
  font-size: 0.75rem;
  color: #5b5238;
}

.adm-live-dot {
  width: 0.4375rem;
  height: 0.4375rem;
  border-radius: 999px;
  background: #9b8c62;
}

.adm-live-dot.is-active {
  background: #5c9865;
  box-shadow: 0 0 0 3px rgb(92 152 101 / 0.22);
}

.adm-today {
  display: flex;
  min-width: 0;
  flex-direction: column;
  gap: 1rem;
}

.adm-today-head {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 0.5rem;
}

.adm-section-title {
  font-size: 0.9375rem;
  font-weight: 700;
  color: var(--md-sys-color-on-surface);
}

.adm-updated {
  padding: 0.1875rem 0.625rem;
  border-radius: 999px;
  background: #faf6e8;
  font-size: 0.75rem;
  color: var(--md-sys-color-on-surface-variant);
}

.adm-updated.is-stale {
  background: #fef3c7;
  color: #92400e;
}

.adm-kpis {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 0.625rem;
}

.adm-kpi {
  display: flex;
  min-width: 0;
  flex-direction: column;
  gap: 0.25rem;
  padding: 0.875rem 1rem;
  border-radius: 1rem;
  background: #fffbe8;
}

.adm-kpi dt {
  font-size: 0.75rem;
  font-weight: 500;
  color: var(--md-sys-color-on-surface-variant);
}

.adm-kpi-value {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 1.375rem;
  font-weight: 700;
  line-height: 1.75rem;
  color: var(--md-sys-color-on-surface);
}

.adm-kpi-sub {
  display: flex;
  flex-wrap: wrap;
  gap: 0 0.5rem;
  font-size: 0.75rem;
  color: var(--md-sys-color-on-surface-variant);
}

.adm-list-price {
  text-decoration: line-through;
  opacity: 0.75;
}

.adm-mix {
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
}

.adm-mix-head {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: 0.5rem;
  font-size: 0.75rem;
  font-weight: 600;
  color: var(--md-sys-color-on-surface-variant);
}

/* Stacked bar: 2px surface gaps between segments instead of strokes. */
.adm-mix-bar {
  display: flex;
  gap: 2px;
  height: 0.625rem;
  overflow: hidden;
  border-radius: 999px;
  background: #eee5c9;
}

.adm-mix-segment {
  min-width: 2px;
  height: 100%;
}

.adm-mix-legend {
  display: flex;
  flex-wrap: wrap;
  gap: 0.375rem 1rem;
  font-size: 0.75rem;
}

.adm-mix-legend li {
  display: inline-flex;
  align-items: center;
  gap: 0.375rem;
  color: var(--md-sys-color-on-surface-variant);
}

.adm-mix-legend li.is-zero {
  opacity: 0.55;
}

.adm-mix-swatch {
  width: 0.5rem;
  height: 0.5rem;
  border-radius: 0.125rem;
}

.adm-mix-value {
  font-weight: 650;
  font-variant-numeric: tabular-nums;
  color: var(--md-sys-color-on-surface);
}

/* ---------- Resources ---------- */
.adm-resources {
  display: grid;
  gap: 1rem;
}

.adm-resource {
  display: flex;
  min-width: 0;
  flex-direction: column;
  align-items: stretch;
  gap: 0.625rem;
  padding: 1.125rem 1.25rem;
  text-align: left;
}

button.adm-resource {
  cursor: pointer;
}

button.adm-resource:hover {
  box-shadow: var(--theme-highlight), var(--theme-elevation-2);
}

button.adm-resource:focus-visible {
  outline: 3px solid color-mix(in srgb, var(--md-sys-color-primary) 45%, transparent);
  outline-offset: 2px;
}

.adm-resource-head {
  display: flex;
  align-items: center;
  gap: 0.625rem;
}

.adm-resource-icon {
  display: inline-flex;
  width: 2.25rem;
  height: 2.25rem;
  flex-shrink: 0;
  align-items: center;
  justify-content: center;
  border-radius: 0.75rem;
}

.adm-resource-icon.is-users {
  background: #e4f0df;
  color: #386344;
}

.adm-resource-icon.is-accounts {
  background: #fff3bd;
  color: #7e5600;
}

.adm-resource-icon.is-keys {
  background: #faf6e8;
  color: #5b5238;
}

.adm-resource-label {
  flex: 1;
  font-size: 0.875rem;
  font-weight: 600;
  color: var(--md-sys-color-on-surface-variant);
}

.adm-resource-chevron {
  color: var(--md-sys-color-on-surface-variant);
  opacity: 0.5;
  transition: opacity 160ms ease, transform 200ms var(--theme-ease-out);
}

button.adm-resource:hover .adm-resource-chevron {
  opacity: 1;
  transform: translateX(2px);
}

.adm-resource-value {
  font-size: 1.75rem;
  font-weight: 700;
  line-height: 2.125rem;
  color: var(--md-sys-color-on-surface);
}

.adm-resource-facts {
  display: flex;
  flex-wrap: wrap;
  gap: 0.375rem 0.875rem;
  font-size: 0.75rem;
  color: var(--md-sys-color-on-surface-variant);
}

.adm-resource-facts strong {
  font-weight: 700;
  font-variant-numeric: tabular-nums;
  color: var(--md-sys-color-on-surface);
}

.adm-resource-facts strong.text-accent-700 {
  color: #386344;
}

/* Same-ramp track so the bar reads as a share of the whole. */
.adm-meter {
  display: block;
  height: 0.375rem;
  overflow: hidden;
  border-radius: 999px;
  background: #e4f0df;
}

.adm-meter-fill {
  display: block;
  height: 100%;
  border-radius: inherit;
  background: #5c9865;
  transition: width 300ms ease;
}

.adm-flag {
  display: inline-flex;
  align-items: center;
  gap: 0.25rem;
  padding: 0.0625rem 0.5rem;
  border-radius: 999px;
  font-weight: 600;
}

.adm-flag.is-warning {
  background: #fef3c7;
  color: #92400e;
}

.adm-flag.is-serious {
  background: #ffedd5;
  color: #9a3412;
}

.adm-flag.is-critical {
  background: #fee2e2;
  color: #b91c1c;
}

.adm-flag.is-good {
  background: #e4f0df;
  color: #2e5038;
}

.adm-shortcuts-title {
  margin-bottom: 0.125rem;
  font-size: 0.875rem;
  font-weight: 600;
  color: var(--md-sys-color-on-surface-variant);
}

.adm-shortcuts {
  display: flex;
  min-width: 0;
  flex-direction: column;
  gap: 0.5rem;
  padding: 1.125rem 1.25rem;
}

.adm-shortcut {
  display: flex;
  width: 100%;
  align-items: center;
  gap: 0.75rem;
  padding: 0.625rem 0.75rem;
  border-radius: 0.875rem;
  background: #fffbe8;
  text-align: left;
  transition: background-color 160ms ease;
}

.adm-shortcut:hover {
  background: #fff3bd;
}

.adm-shortcut-icon {
  display: inline-flex;
  width: 2rem;
  height: 2rem;
  flex-shrink: 0;
  align-items: center;
  justify-content: center;
  border-radius: 0.625rem;
  background: #fff3bd;
  color: #7e5600;
}

.adm-shortcut-icon.is-image {
  background: #e0f2fe;
  color: #0369a1;
}

.adm-shortcut:hover .adm-shortcut-icon {
  background: var(--md-sys-color-surface);
}

.adm-shortcut-label {
  display: block;
  font-size: 0.8125rem;
  font-weight: 600;
  color: var(--md-sys-color-on-surface);
}

.adm-shortcut-desc {
  display: block;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 0.75rem;
  color: var(--md-sys-color-on-surface-variant);
}

/* ---------- Usage ---------- */
.adm-usage {
  padding: 1.25rem;
}

.adm-usage-head {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 0.75rem;
  margin-bottom: 1.25rem;
}

.adm-usage-controls {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 0.5rem;
}

.adm-segment {
  padding: 0.375rem 0.875rem;
  border-radius: 999px;
  font-size: 0.8125rem;
  transition: background-color 160ms ease, color 160ms ease, box-shadow 160ms ease;
}

.adm-refresh {
  padding: 0.5rem 0.625rem;
  border-radius: 0.875rem;
}

.adm-charts {
  display: grid;
  gap: 1.5rem;
}

.adm-chart-title {
  margin-bottom: 1rem;
  font-size: 0.875rem;
  font-weight: 600;
  color: var(--md-sys-color-on-surface);
}

.adm-user-trend {
  margin-top: 1.5rem;
  padding-top: 1.5rem;
  box-shadow: inset 0 1px 0 rgb(215 193 106 / 0.3);
}

/* The shared chart components bring their own card; inside this panel they sit flat. */
.adm-charts :deep(.card) {
  padding: 0;
  border-radius: 0;
  background: transparent;
  box-shadow: none;
}

@media (min-width: 768px) {
  .adm-hero {
    padding: 1.5rem;
  }

  .adm-kpis {
    grid-template-columns: repeat(4, minmax(0, 1fr));
  }

  .adm-resources {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }

  .adm-usage {
    padding: 1.5rem;
  }
}

@media (min-width: 1024px) {
  .adm-hero {
    grid-template-columns: minmax(17rem, 0.42fr) minmax(0, 1fr);
    gap: 1.5rem;
  }

  .adm-kpis {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }

}

@media (min-width: 1280px) {
  .adm-kpis {
    grid-template-columns: repeat(4, minmax(0, 1fr));
  }

  .adm-resources {
    grid-template-columns: repeat(4, minmax(0, 1fr));
  }
}

/* Side by side only when the model table keeps its columns readable. */
@media (min-width: 1536px) {
  .adm-charts {
    grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
  }

  .adm-trend {
    padding-left: 1.5rem;
    box-shadow: inset 1px 0 0 rgb(215 193 106 / 0.3);
  }
}

@media (max-width: 1535px) {
  .adm-trend {
    padding-top: 1.5rem;
    box-shadow: inset 0 1px 0 rgb(215 193 106 / 0.3);
  }
}

@media (prefers-reduced-motion: reduce) {
  .adm-resource-chevron {
    transition: none;
  }

  button.adm-resource:hover .adm-resource-chevron {
    transform: none;
  }
}
</style>
