<template>
  <AppLayout>
    <div class="upstream-page space-y-6">
      <section class="grid grid-cols-2 gap-3 sm:gap-4 xl:grid-cols-4" :aria-label="t('admin.upstream.overview')">
        <article v-for="metric in metrics" :key="metric.key" class="upstream-metric card" :data-testid="`${metric.key}-metric`">
          <span class="upstream-metric-icon" :class="metric.tone"><Icon :name="metric.icon" size="md" /></span>
          <div class="min-w-0 flex-1">
            <p class="truncate text-xs font-medium text-gray-500">{{ t(metric.label) }}</p>
            <div class="mt-1 flex min-h-8 items-center text-xl font-bold tabular-nums text-gray-900 sm:text-2xl">
              <LoadingSpinner v-if="metric.loading" size="sm" :data-testid="`${metric.key}-balance-loading`" />
              <span v-else class="truncate" :title="metric.value">{{ metric.value }}</span>
            </div>
            <p v-if="metric.key === 'connected' && providerCount > 0" class="mt-0.5 text-[11px] font-medium text-emerald-700">{{ t('admin.upstream.connectionRate', { rate: connectionRate }) }}</p>
          </div>
        </article>
      </section>

      <div class="upstream-toolbar card">
        <div class="relative min-w-[12rem] flex-1">
          <Icon name="search" size="sm" class="pointer-events-none absolute left-4 top-1/2 -translate-y-1/2 text-gray-400" />
          <input v-model.trim="search" type="search" class="input w-full pl-11" :placeholder="t('admin.upstream.search')" :aria-label="t('admin.upstream.search')" @input="debouncedLoad" />
        </div>
        <div class="w-full sm:w-44">
          <Select v-model="status" :options="statusOptions" :aria-label="t('admin.upstream.filterStatus')" @change="page = 1; load()" />
        </div>
        <button type="button" class="btn btn-secondary" :disabled="loading" :aria-label="t('admin.upstream.refresh')" @click="load()"><Icon name="refresh" size="sm" :class="{ 'animate-spin': loading }" /><span class="hidden sm:inline">{{ t('admin.upstream.refresh') }}</span></button>
        <button type="button" class="btn btn-primary shrink-0" @click="openCreate"><Icon name="plus" size="sm" />{{ t('admin.upstream.add') }}</button>
      </div>

      <div v-if="error" role="alert" class="flex flex-wrap items-center justify-between gap-3 rounded-2xl bg-red-50 px-4 py-3 text-sm text-red-700">
        <span class="flex items-center gap-2"><Icon name="exclamationCircle" size="sm" />{{ t('admin.upstream.loadFailed') }}</span>
        <button type="button" class="font-semibold underline-offset-2 hover:underline" @click="load()">{{ t('admin.upstream.retry') }}</button>
      </div>

      <div v-if="loading && !providers.length" class="grid gap-4 lg:grid-cols-2" role="status" :aria-label="t('admin.upstream.loading')">
        <div v-for="index in 2" :key="index" class="card space-y-4 p-5">
          <div class="flex items-center gap-3"><div class="skeleton h-11 w-11 rounded-2xl" /><div class="flex-1 space-y-2"><div class="skeleton h-4 w-1/3" /><div class="skeleton h-3 w-1/2" /></div></div>
          <div class="skeleton h-20 w-full rounded-2xl" />
        </div>
      </div>
      <div v-else-if="!visibleProviders.length" class="card empty-state">
        <span class="upstream-empty-icon"><Icon name="server" size="xl" /></span>
        <p class="empty-state-title">{{ t('admin.upstream.empty') }}</p>
        <p v-if="providerCount === 0" class="empty-state-description">{{ t('admin.upstream.emptyHint') }}</p>
        <button v-if="providerCount === 0" type="button" class="btn btn-primary mt-5" @click="openCreate"><Icon name="plus" size="sm" />{{ t('admin.upstream.add') }}</button>
      </div>

      <section v-else class="grid gap-4 lg:grid-cols-2" :aria-label="t('admin.upstream.pageTitle')">
        <article v-for="provider in visibleProviders" :key="provider.id" class="provider-card card" :class="{ 'provider-card-offline': provider.status === 'disconnected' }" :data-provider-id="provider.id">
          <div class="flex items-start gap-3 px-5 pt-5">
            <span class="provider-avatar" :class="provider.site_type === 'newapi' ? 'provider-avatar-newapi' : ''" aria-hidden="true">{{ initials(provider.name) }}</span>
            <div class="min-w-0 flex-1">
              <div class="flex flex-wrap items-center gap-1.5">
                <h2 class="min-w-0 break-all text-base font-bold text-gray-900">{{ provider.name }}</h2>
                <span class="badge badge-gray">{{ provider.site_type === 'newapi' ? 'New API' : 'Sub2API' }}</span>
              </div>
              <a :href="safeHref(provider.base_url)" target="_blank" rel="noopener noreferrer" class="mt-1 inline-flex max-w-full items-center gap-1 text-xs text-gray-500 hover:text-primary-700">
                <Icon name="globe" size="xs" class="shrink-0" /><span class="truncate font-mono">{{ hostOf(provider.base_url) }}</span>
              </a>
            </div>
            <div class="flex shrink-0 items-center gap-0.5">
              <button type="button" class="provider-icon-button" :aria-label="t('admin.upstream.edit')" :title="t('admin.upstream.edit')" @click="openEdit(provider)"><Icon name="edit" size="sm" /></button>
              <button type="button" class="provider-icon-button provider-icon-danger" :aria-label="t('admin.upstream.delete')" :title="t('admin.upstream.delete')" :disabled="deletingId === provider.id" @click="deleting = provider"><Icon name="trash" size="sm" /></button>
            </div>
          </div>

          <div class="flex flex-wrap items-center gap-1.5 px-5 pt-3">
            <span class="provider-status" :class="provider.status === 'connected' ? 'provider-status-online' : 'provider-status-offline'">
              <span class="provider-status-dot" />{{ provider.status === 'connected' ? t('admin.upstream.connected') : t('admin.upstream.disconnected') }}
            </span>
            <span v-if="provider.low_balance_alert" class="badge badge-warning" data-testid="provider-low-balance-alert">{{ t('admin.upstream.lowBalanceAlert') }}</span>
            <span v-if="provider.exchange_rate !== 1" class="badge badge-primary tabular-nums">×{{ provider.exchange_rate }}</span>
          </div>

          <div class="provider-balance mx-5 mt-4">
            <p class="text-xs font-medium text-gray-600">{{ t('admin.upstream.balance') }}</p>
            <p class="mt-1 break-all text-3xl font-bold tabular-nums tracking-tight" :class="provider.low_balance_alert ? 'text-amber-700' : 'text-gray-900'">{{ provider.metrics.balance === null ? '—' : yuan(provider.metrics.balance * provider.exchange_rate) }}</p>
            <p class="mt-0.5 text-xs tabular-nums text-gray-500">{{ provider.metrics.balance === null ? t('admin.upstream.unknown') : money(provider.metrics.balance) + ' USD' }}</p>
          </div>

          <dl class="grid grid-cols-3 gap-2 px-5 pb-5 pt-3">
            <div v-for="amount in secondaryAmounts(provider)" :key="amount.key" class="provider-stat">
              <dt class="truncate text-[11px] font-medium text-gray-500">{{ t(amount.label) }}</dt>
              <dd class="mt-0.5 truncate text-sm font-semibold tabular-nums text-gray-900" :title="amount.value === null ? '' : money(amount.value)">{{ amount.value === null ? '—' : money(amount.value) }}</dd>
            </div>
          </dl>

          <p v-if="provider.status === 'disconnected' && provider.last_error" class="mx-5 mt-3 flex items-start gap-2 rounded-xl bg-red-50 px-3 py-2 text-xs leading-relaxed text-red-700" role="status"><Icon name="exclamationCircle" size="sm" class="mt-px shrink-0" /><span class="min-w-0 break-words">{{ provider.last_error }}</span></p>
          <p v-if="provider.notes" class="mx-5 mt-3 line-clamp-2 text-xs text-gray-500" :title="provider.notes">{{ provider.notes }}</p>

          <footer class="mt-4 flex flex-wrap items-center justify-between gap-2 px-5 pb-4 pt-3 text-xs text-gray-500 provider-footer">
            <span class="inline-flex items-center gap-1"><Icon name="clock" size="xs" />{{ t('admin.upstream.updated') }} {{ formattedTime(provider.updated_at) }}</span>
            <div class="flex items-center gap-2">
              <a :href="safeHref(provider.base_url)" target="_blank" rel="noopener noreferrer" class="inline-flex items-center gap-1 rounded-lg px-2 py-1 font-medium text-gray-600 hover:bg-primary-50 hover:text-primary-700">{{ t('admin.upstream.visitSite') }}<Icon name="externalLink" size="xs" /></a>
              <button type="button" class="btn btn-secondary btn-sm" :disabled="refreshingId === provider.id" @click="refresh(provider)"><Icon name="refresh" size="xs" :class="{ 'animate-spin': refreshingId === provider.id }" />{{ t('admin.upstream.refresh') }}</button>
            </div>
          </footer>
        </article>
      </section>

      <footer v-if="pages > 1" class="flex flex-wrap items-center justify-between gap-3 text-xs text-gray-500">
        <span class="tabular-nums">{{ t('admin.upstream.pagination', { page, pages, total }) }}</span>
        <div class="flex gap-2">
          <button type="button" class="btn btn-secondary btn-sm" :disabled="page <= 1 || loading" @click="page--; load()"><Icon name="chevronLeft" size="xs" />{{ t('admin.upstream.previous') }}</button>
          <button type="button" class="btn btn-secondary btn-sm" :disabled="page >= pages || loading" @click="page++; load()">{{ t('admin.upstream.next') }}<Icon name="chevronRight" size="xs" /></button>
        </div>
      </footer>
      <ProviderDialog :show="showDialog" :provider="editing" @close="showDialog = false" @saved="afterSave" />
      <ConfirmDialog
        :show="deleting !== null"
        :title="t('admin.upstream.delete')"
        :message="t('admin.upstream.deleteConfirm', { name: deleting?.name ?? '' })"
        :confirm-text="t('admin.upstream.delete')"
        :danger="true"
        :loading="deletingId !== null"
        @confirm="confirmDelete"
        @cancel="deleting = null"
      />
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import { deleteProvider, listProviders, refreshProvider, type Provider } from '@/api/admin/upstream'
import { useAppStore } from '@/stores/app'
import AppLayout from '@/components/layout/AppLayout.vue'
import Icon from '@/components/icons/Icon.vue'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
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
const userBalance = ref<number | null>(null)
const userBalanceLoading = ref(true)
const upstreamBalanceLoading = ref(true)
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
const deleting = ref<Provider | null>(null)
const deletingId = ref<string | null>(null)
const showDialog = ref(false)
let searchTimer: ReturnType<typeof setTimeout> | undefined
let balanceTimer: ReturnType<typeof setInterval> | undefined
let userBalanceTimer: ReturnType<typeof setInterval> | undefined
let userBalanceRequestInFlight = false
let listSequence = 0
let disposed = false
const balanceRefreshInterval = 10_000
const finite = (amount: number) => (Number.isFinite(amount) ? amount : 0)
const money = (amount: number) => new Intl.NumberFormat('en-US', { style: 'currency', currency: 'USD', maximumFractionDigits: 2 }).format(finite(amount))
const yuan = (amount: number) => new Intl.NumberFormat('zh-CN', { style: 'currency', currency: 'CNY', maximumFractionDigits: 2 }).format(finite(amount))
const visibleProviders = computed(() => providers.value)
const connectionRate = computed(() => (providerCount.value ? Math.round((connectedCount.value / providerCount.value) * 100) : 0))
const metrics = computed(() => [
  { key: 'count', label: 'admin.upstream.providerCount', icon: 'server' as const, tone: 'tone-honey', value: String(providerCount.value), loading: false },
  { key: 'connected', label: 'admin.upstream.connectedCount', icon: 'checkCircle' as const, tone: 'tone-garden', value: String(connectedCount.value), loading: false },
  { key: 'upstream', label: 'admin.upstream.totalBalance', icon: 'creditCard' as const, tone: 'tone-sky', value: yuan(totalBalance.value), loading: upstreamBalanceLoading.value },
  { key: 'users', label: 'admin.upstream.userBalance', icon: 'users' as const, tone: 'tone-rose', value: userBalance.value === null ? '—' : money(userBalance.value), loading: userBalanceLoading.value }
])
function secondaryAmounts(provider: Provider) {
  return [
    { key: 'today', label: 'admin.upstream.todayConsumption', value: provider.metrics.today_spend },
    { key: 'recharge', label: 'admin.upstream.totalRecharge', value: provider.metrics.total_recharge },
    { key: 'total', label: 'admin.upstream.totalConsumption', value: provider.metrics.total_spend }
  ]
}
// The backend only stores HTTPS origins, but refuse any other scheme here too
// so a legacy or tampered record can never render as a javascript: link.
function safeHref(raw: string): string | undefined {
  try {
    const url = new URL(raw)
    return url.protocol === 'https:' && !url.username && !url.password ? url.origin : undefined
  } catch {
    return undefined
  }
}
function hostOf(raw: string): string {
  try { return new URL(raw).host } catch { return raw }
}
function initials(name: string): string {
  const trimmed = name.trim()
  return trimmed ? Array.from(trimmed).slice(0, 2).join('').toUpperCase() : '?'
}
function formattedTime(value?: string) {
  if (!value) return '—'
  const time = new Date(value)
  return Number.isNaN(time.getTime()) ? '—' : new Intl.DateTimeFormat(undefined, { dateStyle: 'short', timeStyle: 'short' }).format(time)
}
function openCreate() { editing.value = null; showDialog.value = true }
function openEdit(provider: Provider) { editing.value = provider; showDialog.value = true }
async function load(silent = false) {
  // A slow silent poll must not overwrite the result of a newer filter or page change.
  const sequence = ++listSequence
  if (!silent) { loading.value = true; error.value = false }
  try {
    const list = await listProviders(page.value, search.value, status.value === 'all' ? '' : status.value)
    if (disposed || sequence !== listSequence) return
    providers.value = list.items ?? []
    total.value = list.total
    pages.value = Math.max(1, list.pages)
    if (page.value > pages.value) page.value = pages.value
    providerCount.value = list.overview.provider_count
    connectedCount.value = list.overview.connected_count
    totalBalance.value = list.overview.upstream_total_balance
    upstreamBalanceLoading.value = false
  } catch (reason) {
    if (!silent && sequence === listSequence) error.value = true
    console.error('Provider list failed:', reason)
  } finally {
    if (!silent && sequence === listSequence) { loading.value = false; upstreamBalanceLoading.value = false }
  }
}
async function loadUserBalance(silent = false) {
  if (userBalanceRequestInFlight) return
  userBalanceRequestInFlight = true
  if (!silent) userBalanceLoading.value = true
  try {
    const today = dateRange(1)
    const operations = await adminAPI.dashboard.getOperationsDashboard({ start_date: today.start, end_date: today.end, trend_days: 7, timezone: Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC', group_id: 0 })
    if (!disposed) userBalance.value = operations.metrics.total_user_balance
  } catch (reason) {
    console.error('User balance load failed:', reason)
  } finally {
    userBalanceRequestInFlight = false
    if (!disposed && !silent) userBalanceLoading.value = false
  }
}
async function refresh(provider: Provider) {
  if (refreshingId.value) return
  refreshingId.value = provider.id
  try {
    await refreshProvider(provider.id)
    await load()
    appStore.showSuccess(t('admin.upstream.refreshed'))
  } catch (reason) {
    // The backend saves the disconnected state and error; show it on the card.
    await load(true)
    appStore.showError(t('admin.upstream.refreshFailed'))
    console.error('Provider refresh failed:', reason)
  } finally { refreshingId.value = null }
}
async function confirmDelete() {
  const provider = deleting.value
  if (!provider || deletingId.value) return
  deletingId.value = provider.id
  try {
    await deleteProvider(provider.id)
    deleting.value = null
    if (providers.value.length === 1 && page.value > 1) page.value--
    await load()
    appStore.showSuccess(t('admin.upstream.deleted'))
  } catch (reason) {
    appStore.showError(t('admin.upstream.deleteFailed'))
    console.error('Provider deletion failed:', reason)
  } finally { deletingId.value = null }
}
function handleVisibilityChange() {
  if (!document.hidden) { void load(true); void loadUserBalance(true) }
}
function afterSave() { showDialog.value = false; void load() }
function debouncedLoad() { if (searchTimer) clearTimeout(searchTimer); searchTimer = setTimeout(() => { page.value = 1; void load() }, 250) }
onMounted(() => {
  void loadUserBalance()
  void load()
  balanceTimer = setInterval(() => { if (!document.hidden && !loading.value && !refreshingId.value) void load(true) }, balanceRefreshInterval)
  userBalanceTimer = setInterval(() => { if (!document.hidden) void loadUserBalance(true) }, 30_000)
  document.addEventListener('visibilitychange', handleVisibilityChange)
})
onUnmounted(() => {
  disposed = true
  if (searchTimer) clearTimeout(searchTimer)
  if (balanceTimer) clearInterval(balanceTimer)
  if (userBalanceTimer) clearInterval(userBalanceTimer)
  document.removeEventListener('visibilitychange', handleVisibilityChange)
})
</script>

<style scoped>
.upstream-metric { display: flex; align-items: flex-start; gap: 0.875rem; min-width: 0; padding: 1rem 1.125rem; }
.upstream-metric-icon { display: flex; flex-shrink: 0; align-items: center; justify-content: center; width: 2.75rem; height: 2.75rem; border-radius: 1rem; }
.tone-honey { background: #fff3bd; color: #7e5600; }
.tone-garden { background: #e4f0df; color: #386344; }
.tone-sky { background: #e0f0fb; color: #1f5f8b; }
.tone-rose { background: #fbe6e1; color: #9a3b2b; }
.upstream-toolbar { display: flex; flex-wrap: wrap; align-items: center; gap: 0.75rem; padding: 0.75rem; }
.upstream-empty-icon { display: flex; align-items: center; justify-content: center; width: 4.5rem; height: 4.5rem; margin-bottom: 1rem; border-radius: 1.5rem; background: #fff3bd; color: #976800; }
.provider-card { display: flex; flex-direction: column; min-width: 0; overflow: hidden; }
.provider-card:hover { box-shadow: var(--theme-highlight), var(--theme-elevation-2); }
.provider-card-offline { background: linear-gradient(180deg, rgb(254 242 242 / 0.7), var(--md-sys-color-surface) 9rem); }
.provider-avatar { display: flex; flex-shrink: 0; align-items: center; justify-content: center; width: 2.75rem; height: 2.75rem; border-radius: 1rem; background: linear-gradient(135deg, #f8d65d, #e9b824); color: #3f3420; font-size: 0.875rem; font-weight: 800; box-shadow: inset 0 1px 0 rgb(255 255 255 / 0.55), 0 6px 14px -8px rgb(151 104 0 / 0.6); }
.provider-avatar-newapi { background: linear-gradient(135deg, #a5cc9f, #5c9865); color: #fff; }
.provider-icon-button { display: flex; align-items: center; justify-content: center; width: 2rem; height: 2rem; border-radius: 0.75rem; color: var(--md-sys-color-on-surface-variant); transition: background-color 150ms ease, color 150ms ease; }
.provider-icon-button:hover:not(:disabled) { background: var(--md-sys-color-surface-container); color: #7e5600; }
.provider-icon-danger:hover:not(:disabled) { background: #fef2f2; color: #b91c1c; }
.provider-icon-button:disabled { cursor: not-allowed; opacity: 0.5; }
.provider-status { display: inline-flex; align-items: center; gap: 0.375rem; padding: 0.1875rem 0.625rem; border-radius: 999px; font-size: 0.75rem; font-weight: 600; }
.provider-status-online { background: #e4f0df; color: #2e5038; }
.provider-status-offline { background: #fee2e2; color: #991b1b; }
.provider-status-dot { width: 0.4375rem; height: 0.4375rem; border-radius: 999px; background: currentColor; }
.provider-status-online .provider-status-dot { box-shadow: 0 0 0 3px rgb(92 152 101 / 0.25); animation: provider-pulse 2.4s ease-in-out infinite; }
@keyframes provider-pulse { 0%, 100% { box-shadow: 0 0 0 2px rgb(92 152 101 / 0.3); } 50% { box-shadow: 0 0 0 5px rgb(92 152 101 / 0.05); } }
.provider-balance { padding: 0.875rem 1rem; border-radius: 1.25rem; background: linear-gradient(135deg, #fffbe8, #fff3bd); box-shadow: inset 0 1px 0 rgb(255 255 255 / 0.8); }
.provider-stat { min-width: 0; padding: 0.5rem 0.625rem; border-radius: 0.875rem; background: var(--md-sys-color-surface-container); background: color-mix(in srgb, var(--md-sys-color-surface-container) 55%, var(--md-sys-color-surface)); }
.provider-footer { margin-top: auto; background: color-mix(in srgb, var(--md-sys-color-surface-container) 35%, transparent); }
@media (prefers-reduced-motion: reduce) { .provider-status-online .provider-status-dot { animation: none; } }
</style>
