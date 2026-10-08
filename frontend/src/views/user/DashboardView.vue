<template>
  <AppLayout class="dashboard-layout">
    <div class="space-y-6">
      <div v-if="loading && !stats" class="flex items-center justify-center py-12"><LoadingSpinner /></div>
      <div v-if="statsError || totalsError" role="alert" class="flex items-center gap-3 text-sm text-red-600">
        <span>{{ t(statsError ? 'dashboard.statsLoadFailed' : 'dashboard.totalsLoadFailed') }}</span>
        <button type="button" class="btn btn-secondary h-9 w-9 p-2" :title="t('common.refresh')" :aria-label="t('common.refresh')" @click="statsError ? loadStats() : loadTotals()">
          <Icon name="refresh" size="sm" />
        </button>
      </div>
      <UserDashboardStats v-if="stats" :stats="stats" :balance="user?.balance || 0" :is-simple="authStore.isSimpleMode" :platform-quotas="platformQuotas" />
      <div v-if="chartsError" role="alert" class="flex items-center gap-3 text-sm text-red-600">
        <span>{{ t('dashboard.chartsLoadFailed') }}</span>
        <button type="button" class="btn btn-secondary h-9 w-9 p-2" :title="t('common.refresh')" :aria-label="t('common.refresh')" @click="loadCharts"><Icon name="refresh" size="sm" /></button>
      </div>
      <UserDashboardCharts v-model:startDate="startDate" v-model:endDate="endDate" v-model:granularity="granularity" :loading="loadingCharts" :trend="trendData" :models="modelStats" @dateRangeChange="loadCharts" @granularityChange="loadCharts" @refresh="refreshAll" />
      <div class="grid grid-cols-1 gap-6 lg:grid-cols-3">
        <div class="lg:col-span-2">
          <div v-if="recentError" role="alert" class="mb-3 flex items-center gap-3 text-sm text-red-600">
            <span>{{ t('dashboard.recentLoadFailed') }}</span>
            <button type="button" class="btn btn-secondary h-9 w-9 p-2" :title="t('common.refresh')" :aria-label="t('common.refresh')" @click="loadRecent"><Icon name="refresh" size="sm" /></button>
          </div>
          <UserDashboardRecentUsage :data="recentUsage" :loading="loadingUsage" />
        </div>
        <div class="lg:col-span-1"><UserDashboardQuickActions /></div>
      </div>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, onBeforeUnmount, watch, defineAsyncComponent } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAuthStore } from '@/stores/auth'
import { usageAPI, type UserDashboardStats as UserStatsType } from '@/api/usage'
import AppLayout from '@/components/layout/AppLayout.vue'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import Icon from '@/components/icons/Icon.vue'
import UserDashboardStats from '@/components/user/dashboard/UserDashboardStats.vue'
import UserDashboardRecentUsage from '@/components/user/dashboard/UserDashboardRecentUsage.vue'
import UserDashboardQuickActions from '@/components/user/dashboard/UserDashboardQuickActions.vue'
import type { UsageLog, TrendDataPoint, ModelStat, PlatformQuotaItem } from '@/types'
import { getMyPlatformQuotas } from '@/api/user'
import { formatDateLocalInput } from '@/utils/format'

const UserDashboardCharts = defineAsyncComponent(() => import('@/components/user/dashboard/UserDashboardCharts.vue'))
const { t } = useI18n()
const authStore = useAuthStore()
const user = computed(() => authStore.user)
const stats = ref<UserStatsType | null>(null)
const loading = ref(true)
const loadingUsage = ref(true)
const loadingCharts = ref(true)
const statsError = ref(false)
const totalsError = ref(false)
const chartsError = ref(false)
const recentError = ref(false)
const trendData = ref<TrendDataPoint[]>([])
const modelStats = ref<ModelStat[]>([])
const recentUsage = ref<UsageLog[]>([])
const platformQuotas = ref<PlatformQuotaItem[] | null>(null)
const startDate = ref(formatDateLocalInput(new Date(Date.now() - 6 * 86400000)))
const endDate = ref(formatDateLocalInput(new Date()))
const granularity = ref('day')

type RequestKey = 'stats' | 'totals' | 'charts' | 'recent' | 'quotas'
const requests = new Map<RequestKey, AbortController>()
function startRequest(key: RequestKey): AbortController {
  requests.get(key)?.abort()
  const controller = new AbortController()
  requests.set(key, controller)
  return controller
}
function isCurrent(key: RequestKey, controller: AbortController): boolean {
  return requests.get(key) === controller && !controller.signal.aborted
}
function abortRequests(): void {
  requests.forEach((controller) => controller.abort())
  requests.clear()
}

async function loadStats(): Promise<void> {
  const controller = startRequest('stats')
  requests.get('totals')?.abort()
  loading.value = true
  statsError.value = false
  totalsError.value = false
  try {
    const data = await usageAPI.getDashboardStats({ include_totals: false }, { signal: controller.signal })
    if (!isCurrent('stats', controller)) return
    stats.value = data
    if (data.totals_pending) void loadTotals()
  } catch {
    if (isCurrent('stats', controller)) statsError.value = true
  } finally {
    if (isCurrent('stats', controller)) loading.value = false
  }
}

async function loadTotals(): Promise<void> {
  const controller = startRequest('totals')
  totalsError.value = false
  try {
    const data = await usageAPI.getDashboardStats({ include_totals: true }, { signal: controller.signal })
    if (isCurrent('totals', controller)) stats.value = data
  } catch {
    if (isCurrent('totals', controller)) totalsError.value = true
  }
}

async function loadCharts(): Promise<void> {
  const controller = startRequest('charts')
  loadingCharts.value = true
  chartsError.value = false
  const params = { start_date: startDate.value, end_date: endDate.value }
  try {
    const [trend, models] = await Promise.all([
      usageAPI.getDashboardTrend({ ...params, granularity: granularity.value === 'hour' ? 'hour' : 'day' }, { signal: controller.signal }),
      usageAPI.getDashboardModels(params, { signal: controller.signal })
    ])
    if (!isCurrent('charts', controller)) return
    trendData.value = trend.trend || []
    modelStats.value = models.models || []
  } catch {
    if (isCurrent('charts', controller)) {
      chartsError.value = true
      trendData.value = []
      modelStats.value = []
    }
  } finally {
    if (isCurrent('charts', controller)) loadingCharts.value = false
  }
}

async function loadRecent(): Promise<void> {
  const controller = startRequest('recent')
  loadingUsage.value = true
  recentError.value = false
  try {
    const data = await usageAPI.getDashboardRecent({ signal: controller.signal })
    if (isCurrent('recent', controller)) recentUsage.value = data.items
  } catch {
    if (isCurrent('recent', controller)) recentError.value = true
  } finally {
    if (isCurrent('recent', controller)) loadingUsage.value = false
  }
}

async function loadPlatformQuotas(): Promise<void> {
  const controller = startRequest('quotas')
  try {
    const data = await getMyPlatformQuotas({ signal: controller.signal })
    if (isCurrent('quotas', controller)) platformQuotas.value = data.platform_quotas ?? []
  } catch {
    if (isCurrent('quotas', controller)) platformQuotas.value = null
  }
}

function refreshAll(): void {
  void authStore.refreshUser().catch((error) => console.warn('Failed to refresh profile:', error))
  void loadStats()
  void loadCharts()
  void loadRecent()
  void loadPlatformQuotas()
}

watch(() => authStore.user?.id, () => {
  abortRequests()
  stats.value = null
  trendData.value = []
  modelStats.value = []
  recentUsage.value = []
  platformQuotas.value = null
  if (authStore.isAuthenticated) refreshAll()
})
onMounted(refreshAll)
onBeforeUnmount(abortRequests)
</script>
