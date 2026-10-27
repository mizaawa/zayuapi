<template>
  <AppLayout>
    <div class="space-y-5">
      <header class="flex flex-wrap items-end justify-between gap-3">
        <div>
          <p class="text-[11px] font-semibold uppercase tracking-[0.14em] text-primary-500">{{ t('admin.upstream.eyebrow') }}</p>
          <h1 class="mt-1 flex items-center gap-2 text-xl font-bold text-gray-900 dark:text-white">{{ t('admin.upstream.pageTitle') }} <span class="rounded-full bg-primary-100 px-2 py-0.5 text-xs font-semibold text-primary-700 dark:bg-primary-900/30 dark:text-primary-300">{{ providerCount }}</span></h1>
        </div>
        <button type="button" class="btn btn-primary inline-flex items-center gap-2" @click="editing = null; showDialog = true"><Icon name="plus" size="sm" />{{ t('admin.upstream.add') }}</button>
      </header>

      <section class="grid grid-cols-2 gap-3 lg:grid-cols-4" :aria-label="t('admin.upstream.overview')">
        <article v-for="metric in metrics" :key="metric.key" class="card min-w-0 p-4">
          <div class="flex items-start gap-3">
            <span class="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg" :class="metric.iconClass"><Icon :name="metric.icon" size="sm" /></span>
            <div class="min-w-0">
              <p class="text-xs font-medium text-gray-500 dark:text-gray-400">{{ t(metric.label) }}</p>
              <p class="mt-1 break-all text-lg font-bold tabular-nums text-gray-900 dark:text-white sm:text-xl">{{ metric.value }}</p>
            </div>
          </div>
        </article>
      </section>

      <div class="card flex flex-wrap items-center gap-3 p-3">
        <div class="relative min-w-[11rem] flex-1">
          <Icon name="search" size="sm" class="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-gray-400" />
          <input v-model.trim="search" class="input w-full pl-10" :placeholder="t('admin.upstream.search')" :aria-label="t('admin.upstream.search')" @input="debouncedLoad" />
        </div>
        <div class="w-full sm:w-40">
          <Select
            v-model="status"
            :options="statusOptions"
            :aria-label="t('admin.upstream.filterStatus')"
            @change="page = 1; load()"
          />
        </div>
        <button type="button" class="btn btn-secondary inline-flex items-center gap-2" :disabled="loading" @click="load"><Icon name="refresh" size="sm" :class="{ 'animate-spin': loading }" /><span class="hidden sm:inline">{{ t('admin.upstream.refresh') }}</span></button>
      </div>

      <div v-if="error" role="alert" class="card border-red-200 p-4 text-sm text-red-600 dark:border-red-900 dark:text-red-400">{{ t('admin.upstream.loadFailed') }} <button type="button" class="ml-2 underline" @click="load">{{ t('admin.upstream.retry') }}</button></div>
      <div v-if="loading && !providers.length" class="card py-16 text-center text-sm text-gray-500 dark:text-gray-400">{{ t('admin.upstream.loading') }}</div>
      <div v-else-if="!visibleProviders.length" class="card py-16 text-center text-sm text-gray-500 dark:text-gray-400">{{ t('admin.upstream.empty') }}</div>

      <section v-else class="space-y-4" :aria-label="t('admin.upstream.pageTitle')">
        <article v-for="provider in visibleProviders" :key="provider.id" class="card overflow-hidden" :data-provider-id="provider.id">
          <div class="flex flex-wrap items-start justify-between gap-3 border-b border-gray-100 px-4 py-4 dark:border-dark-700 sm:px-5">
            <div class="flex min-w-0 items-start gap-3">
              <span class="flex h-10 w-10 shrink-0 items-center justify-center rounded-xl bg-primary-100 text-primary-600 dark:bg-primary-900/30 dark:text-primary-400"><Icon name="server" size="md" /></span>
              <div class="min-w-0">
                <div class="flex flex-wrap items-center gap-2">
                  <h2 class="break-all text-base font-bold text-gray-900 dark:text-white">{{ provider.name }}</h2>
                  <span class="rounded-full bg-gray-100 px-2 py-0.5 text-[11px] font-medium text-gray-600 dark:bg-dark-700 dark:text-gray-300">{{ provider.site_type === 'newapi' ? 'New API' : 'Sub2API' }}</span>
                </div>
                <a :href="provider.base_url" target="_blank" rel="noopener noreferrer" class="mt-1 inline-block break-all text-xs text-primary-600 hover:underline dark:text-primary-400">{{ provider.base_url }} ↗</a>
              </div>
            </div>
            <div class="ml-auto flex items-center gap-2">
              <span class="rounded-full px-2.5 py-1 text-xs font-semibold" :class="provider.status === 'connected' ? 'bg-emerald-50 text-emerald-700 dark:bg-emerald-900/20 dark:text-emerald-400' : 'bg-red-50 text-red-700 dark:bg-red-900/20 dark:text-red-400'"><span class="mr-1">●</span>{{ provider.status === 'connected' ? t('admin.upstream.connected') : t('admin.upstream.disconnected') }}</span>
              <button type="button" class="rounded-lg p-2 text-gray-500 hover:bg-gray-100 hover:text-primary-600 dark:hover:bg-dark-700" :aria-label="t('admin.upstream.edit')" @click="editing = provider; showDialog = true"><Icon name="edit" size="sm" /></button>
            </div>
          </div>

          <div class="grid grid-cols-1 divide-y divide-gray-100 p-4 dark:divide-dark-700 sm:grid-cols-3 sm:divide-x sm:divide-y-0 sm:px-5">
            <div v-for="amount in providerAmounts(provider)" :key="amount.key" class="min-w-0 py-2 first:pt-0 last:pb-0 sm:px-4 sm:py-0 sm:first:pl-0 sm:last:pr-0">
              <p class="text-xs font-medium text-gray-500 dark:text-gray-400">{{ t(amount.label) }}</p>
              <p class="mt-1 break-all text-xl font-bold tabular-nums" :class="amount.key === 'balance' ? 'text-primary-600 dark:text-primary-400' : 'text-gray-900 dark:text-white'">{{ amount.value === null ? '—' : yuan(amount.value * provider.exchange_rate) }}</p>
              <p class="mt-0.5 text-[11px] text-gray-400">{{ amount.value === null ? t('admin.upstream.unknown') : money(amount.value) + ' USD' }}</p>
            </div>
          </div>
          <p class="px-4 pb-3 text-right text-xs text-gray-500 dark:text-gray-400 sm:px-5">{{ t('admin.upstream.totalConsumption') }} <strong class="ml-1 tabular-nums text-gray-700 dark:text-gray-200">{{ provider.metrics.total_spend === null ? '—' : money(provider.metrics.total_spend) }}</strong></p>
          <div class="flex flex-wrap items-center justify-between gap-2 border-t border-gray-100 bg-gray-50/60 px-4 py-3 text-xs text-gray-500 dark:border-dark-700 dark:bg-dark-800/40 dark:text-gray-400 sm:px-5">
            <span>{{ t('admin.upstream.updated') }} {{ formattedTime(provider.updated_at) }}</span>
            <div class="flex items-center gap-3">
              <a :href="provider.base_url" target="_blank" rel="noopener noreferrer" class="font-medium text-gray-700 hover:text-primary-600 dark:text-gray-300 dark:hover:text-primary-400">{{ t('admin.upstream.visitSite') }} ↗</a>
              <button type="button" class="btn btn-secondary py-1 text-xs" :disabled="refreshingId === provider.id" @click="refresh(provider)"><Icon name="refresh" size="sm" :class="{ 'animate-spin': refreshingId === provider.id }" />{{ t('admin.upstream.refresh') }}</button>
            </div>
          </div>
        </article>
      </section>

      <footer v-if="pages > 1" class="flex items-center justify-between gap-3 text-xs text-gray-500 dark:text-gray-400">
        <span>{{ t('admin.upstream.pagination', { page, pages, total }) }}</span>
        <div class="flex gap-2"><button class="btn btn-secondary" :disabled="page <= 1 || loading" @click="page--; load()">{{ t('admin.upstream.previous') }}</button><button class="btn btn-secondary" :disabled="page >= pages || loading" @click="page++; load()">{{ t('admin.upstream.next') }}</button></div>
      </footer>
      <ProviderDialog :show="showDialog" :provider="editing" @close="showDialog = false" @saved="afterSave" />
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import { listProviders, refreshProvider, type Provider } from '@/api/admin/upstream'
import { useAppStore } from '@/stores/app'
import AppLayout from '@/components/layout/AppLayout.vue'
import Icon from '@/components/icons/Icon.vue'
import Select from '@/components/common/Select.vue'
import ProviderDialog from '@/components/admin/provider/ProviderDialog.vue'
import { dateRange } from './operationsUtils'

const { t } = useI18n()
const appStore = useAppStore()
const providers = ref<Provider[]>([])
const total = ref(0)
const providerCount = ref(0)
const connectedCount = ref(0)
const totalBalance = ref(0)
const userBalance = ref(0)
const page = ref(1)
const pages = ref(1)
const search = ref('')
const status = ref('all')
const statusOptions = computed(() => [
  { value: 'all', label: t('admin.upstream.allStatus') },
  { value: 'connected', label: t('admin.upstream.connected') },
  { value: 'disconnected', label: t('admin.upstream.disconnected') }
])
const loading = ref(false)
const error = ref(false)
const refreshingId = ref<string | null>(null)
const editing = ref<Provider | null>(null)
const showDialog = ref(false)
let searchTimer: ReturnType<typeof setTimeout> | undefined
let balanceTimer: ReturnType<typeof setInterval> | undefined
let balanceRefreshRunning = false
let disposed = false
const balanceRefreshInterval = 60_000
const money = (amount: number) => new Intl.NumberFormat(undefined, { style: 'currency', currency: 'USD', maximumFractionDigits: 2 }).format(amount)
const yuan = (amount: number) => new Intl.NumberFormat('zh-CN', { style: 'currency', currency: 'CNY', maximumFractionDigits: 2 }).format(amount)
const visibleProviders = computed(() => providers.value)
const metrics = computed(() => [
  { key: 'count', label: 'admin.upstream.providerCount', icon: 'server' as const, iconClass: 'bg-primary-100 text-primary-600 dark:bg-primary-900/30 dark:text-primary-400', value: String(providerCount.value) },
  { key: 'connected', label: 'admin.upstream.connectedCount', icon: 'check' as const, iconClass: 'bg-emerald-100 text-emerald-600 dark:bg-emerald-900/30 dark:text-emerald-400', value: String(connectedCount.value) },
  { key: 'upstream', label: 'admin.upstream.totalBalance', icon: 'creditCard' as const, iconClass: 'bg-blue-100 text-blue-600 dark:bg-blue-900/30 dark:text-blue-400', value: money(totalBalance.value) },
  { key: 'users', label: 'admin.upstream.userBalance', icon: 'users' as const, iconClass: 'bg-violet-100 text-violet-600 dark:bg-violet-900/30 dark:text-violet-400', value: money(userBalance.value) }
])
function providerAmounts(provider: Provider) {
  return [
    { key: 'balance', label: 'admin.upstream.balance', value: provider.metrics.balance },
    { key: 'today', label: 'admin.upstream.todayConsumption', value: provider.metrics.today_spend },
    { key: 'recharge', label: 'admin.upstream.totalRecharge', value: provider.metrics.total_recharge }
  ]
}
function formattedTime(value?: string) {
  if (!value) return '—'
  const time = new Date(value)
  return Number.isNaN(time.getTime()) ? '—' : new Intl.DateTimeFormat(undefined, { dateStyle: 'short', timeStyle: 'short' }).format(time)
}
async function load() {
  loading.value = true
  error.value = false
  try {
    const today = dateRange(1)
    const [list, operations] = await Promise.all([
      listProviders(page.value, search.value, status.value === 'all' ? '' : status.value),
      adminAPI.dashboard.getOperationsDashboard({ start_date: today.start, end_date: today.end, trend_days: 7, timezone: Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC', group_id: 0 })
    ])
    providers.value = list.items ?? []
    total.value = list.total
    pages.value = Math.max(1, list.pages)
    providerCount.value = list.overview.provider_count
    connectedCount.value = list.overview.connected_count
    totalBalance.value = list.overview.upstream_total_balance
    userBalance.value = operations.metrics.total_user_balance
  } catch (reason) {
    error.value = true
    console.error('Provider list failed:', reason)
  } finally { loading.value = false }
}
async function refresh(provider: Provider) {
  refreshingId.value = provider.id
  try { await refreshProvider(provider.id); await load(); appStore.showSuccess(t('admin.upstream.refreshed')) }
  catch (reason) { appStore.showError(t('admin.upstream.refreshFailed')); console.error('Provider refresh failed:', reason) }
  finally { refreshingId.value = null }
}
async function refreshBalances() {
  if (disposed || document.hidden || balanceRefreshRunning || loading.value || refreshingId.value) return
  balanceRefreshRunning = true
  try {
    // Listing returns the last saved metrics. Probe the visible page to fetch
    // current upstream balances, then reload the overview and user balance.
    for (const provider of providers.value) {
      if (disposed || document.hidden) break
      try { await refreshProvider(provider.id) }
      catch (reason) { console.error('Automatic provider refresh failed:', reason) }
    }
    if (!disposed && !document.hidden) await load()
  } finally { balanceRefreshRunning = false }
}
function handleVisibilityChange() {
  if (!document.hidden) void refreshBalances()
}
function afterSave() { showDialog.value = false; void load() }
function debouncedLoad() { if (searchTimer) clearTimeout(searchTimer); searchTimer = setTimeout(() => { page.value = 1; void load() }, 250) }
onMounted(() => {
  void load().then(() => { void refreshBalances() })
  balanceTimer = setInterval(() => { void refreshBalances() }, balanceRefreshInterval)
  document.addEventListener('visibilitychange', handleVisibilityChange)
})
onUnmounted(() => {
  disposed = true
  if (searchTimer) clearTimeout(searchTimer)
  if (balanceTimer) clearInterval(balanceTimer)
  document.removeEventListener('visibilitychange', handleVisibilityChange)
})
</script>
