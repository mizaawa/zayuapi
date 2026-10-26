<template>
  <AppLayout>
    <div class="space-y-6">
      <section class="grid grid-cols-1 gap-4 sm:grid-cols-2" :aria-label="t('admin.upstream.overview')">
        <div class="card flex items-center gap-4 p-5">
          <span class="rounded-lg bg-blue-100 p-2 text-blue-600 dark:bg-blue-900/30 dark:text-blue-400"><Icon name="server" size="md" :stroke-width="2" /></span>
          <div class="min-w-0">
            <p class="text-xs font-medium text-gray-500 dark:text-gray-400">{{ t('admin.upstream.totalBalance') }}</p>
            <p class="mt-1 break-all text-xl font-bold tabular-nums text-gray-900 dark:text-white">{{ money(balanceSummary?.total_usd ?? 0) }}</p>
            <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.upstream.balanceCoverage', { known: balanceSummary?.known_count ?? 0, unknown: balanceSummary?.unknown_count ?? 0 }) }}</p>
          </div>
        </div>
        <div class="card flex items-center gap-4 p-5">
          <span class="rounded-lg bg-emerald-100 p-2 text-emerald-600 dark:bg-emerald-900/30 dark:text-emerald-400"><Icon name="creditCard" size="md" :stroke-width="2" /></span>
          <div class="min-w-0">
            <p class="text-xs font-medium text-gray-500 dark:text-gray-400">{{ t('admin.upstream.userBalance') }}</p>
            <p class="mt-1 break-all text-xl font-bold tabular-nums text-gray-900 dark:text-white">{{ money(userBalance) }}</p>
            <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.upstream.userBalanceHint') }}</p>
          </div>
        </div>
      </section>

      <section class="card overflow-hidden">
        <div class="flex flex-wrap items-center gap-3 border-b border-gray-100 p-4 dark:border-dark-700">
          <div class="relative min-w-[12rem] flex-1">
            <Icon name="search" size="sm" class="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-gray-400" />
            <input v-model="search" class="input w-full pl-10" :placeholder="t('admin.upstream.search')" @input="debouncedLoad" />
          </div>
          <select v-model="status" class="input w-full sm:w-36" @change="changeFilter">
            <option value="">{{ t('admin.upstream.allStatus') }}</option>
            <option value="active">{{ t('admin.upstream.active') }}</option>
            <option value="inactive">{{ t('admin.upstream.inactive') }}</option>
            <option value="error">{{ t('admin.upstream.error') }}</option>
          </select>
          <button class="btn btn-secondary inline-flex items-center gap-2" :disabled="loading" @click="load"><Icon name="refresh" size="sm" />{{ t('admin.upstream.refresh') }}</button>
          <button class="btn btn-primary inline-flex items-center gap-2" @click="providerAccount = null; showProvider = true"><Icon name="plus" size="sm" />{{ t('admin.upstream.add') }}</button>
        </div>
        <div v-if="loading" class="py-16 text-center text-sm text-gray-500 dark:text-gray-400">{{ t('admin.upstream.loading') }}</div>
        <div v-else-if="!accounts.length" class="py-16 text-center text-sm text-gray-500 dark:text-gray-400">{{ t('admin.upstream.empty') }}</div>
        <div v-else class="divide-y divide-gray-100 dark:divide-dark-700">
          <article v-for="account in accounts" :key="account.id" :data-account-id="account.id" class="p-4 transition-colors hover:bg-gray-50 dark:hover:bg-dark-800/60">
            <div class="flex flex-wrap items-start justify-between gap-4">
              <div class="flex min-w-0 items-start gap-3">
                <span class="mt-0.5 rounded-lg bg-gray-100 p-2 text-gray-600 dark:bg-dark-700 dark:text-gray-300"><PlatformIcon :platform="account.platform" size="lg" /></span>
                <div class="min-w-0">
                  <div class="flex flex-wrap items-center gap-2">
                    <h2 class="break-all text-sm font-semibold text-gray-900 dark:text-white">{{ account.name }}</h2>
                    <span class="rounded-full px-2 py-0.5 text-[11px] font-medium" :class="account.status === 'active' ? 'bg-emerald-50 text-emerald-700 dark:bg-emerald-900/20 dark:text-emerald-400' : account.status === 'error' ? 'bg-red-50 text-red-700 dark:bg-red-900/20 dark:text-red-400' : 'bg-gray-100 text-gray-600 dark:bg-dark-700 dark:text-gray-300'">{{ t(`admin.upstream.${account.status}`) }}</span>
                  </div>
                  <p class="mt-1 break-all text-xs text-gray-500 dark:text-gray-400">{{ account.credentials?.base_url || account.platform }} · #{{ account.id }}</p>
                  <p v-if="account.groups?.length" class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ account.groups.map(group => group.name).join(' · ') }}</p>
                </div>
              </div>
              <div class="text-left sm:text-right">
                <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.upstream.balance') }}</p>
                <p class="mt-0.5 text-lg font-bold tabular-nums" :class="balanceOf(account) === null ? 'text-gray-400' : 'text-gray-900 dark:text-white'">{{ balanceOf(account) === null ? '—' : money(balanceOf(account)!) }}</p>
                <p class="text-[11px] text-gray-400">{{ balanceOf(account) === null ? t('admin.upstream.unknown') : t('admin.upstream.manualSnapshot') }}</p>
              </div>
            </div>
            <div v-if="editingBalanceId === account.id" class="mt-4 flex flex-wrap items-center gap-2 rounded-lg bg-gray-50 p-3 dark:bg-dark-800">
              <label class="text-xs font-medium text-gray-600" :for="`upstream-balance-${account.id}`">{{ t('admin.upstream.balanceUsd') }}</label>
              <input :id="`upstream-balance-${account.id}`" v-model="balanceInput" type="number" min="0" max="1000000000000" step="0.01" class="input w-40" :placeholder="t('admin.upstream.unknown')" />
              <button class="btn btn-primary" :disabled="savingBalance" @click="saveBalance(account)">{{ t('common.save') }}</button>
              <button class="btn btn-secondary" @click="editingBalanceId = null">{{ t('common.cancel') }}</button>
              <span class="text-xs text-gray-500">{{ t('admin.upstream.clearHint') }}</span>
            </div>
            <div class="mt-4 flex flex-wrap items-center gap-2 border-t border-gray-100 pt-3 dark:border-dark-700">
              <button class="btn btn-secondary text-xs" @click="openEdit(account)">{{ t('admin.upstream.edit') }}</button>
              <button class="btn btn-secondary text-xs" @click="openBalance(account)">{{ t('admin.upstream.recordBalance') }}</button>
              <button class="btn btn-secondary text-xs" :disabled="busyId === account.id" @click="discover(account)">{{ t('admin.upstream.discover') }}</button>
              <button class="btn btn-secondary text-xs" :disabled="busyId === account.id" @click="test(account)">{{ t('admin.upstream.test') }}</button>
              <button class="btn btn-secondary text-xs" :disabled="busyId === account.id" @click="toggle(account)">{{ account.status === 'inactive' ? t('admin.upstream.enable') : t('admin.upstream.disable') }}</button>
              <button class="ml-auto text-xs font-medium text-red-600 hover:text-red-700" @click="deleting = account">{{ t('common.delete') }}</button>
            </div>
            <div v-if="discovered[account.id]?.length" class="mt-3 rounded-lg bg-blue-50 p-3 text-xs text-blue-800">
              <strong>{{ t('admin.upstream.discovered', { count: discovered[account.id].length }) }}</strong>
              <p class="mt-1 break-all">{{ discovered[account.id].join(' · ') }}</p>
            </div>
          </article>
        </div>
        <footer class="flex flex-wrap items-center justify-between gap-3 border-t border-gray-100 p-4 text-xs text-gray-500 dark:border-dark-700">
          <span>{{ t('admin.upstream.pagination', { page, pages: Math.max(1, pages), total }) }}</span>
          <div class="flex gap-2">
            <button class="btn btn-secondary" :disabled="page <= 1 || loading" @click="page--; load()">{{ t('admin.upstream.previous') }}</button>
            <button class="btn btn-secondary" :disabled="page >= pages || loading" @click="page++; load()">{{ t('admin.upstream.next') }}</button>
          </div>
        </footer>
      </section>

      <UpstreamAccountModal :show="showProvider" :account="providerAccount" :proxies="proxies" :groups="groups" @close="showProvider = false" @saved="afterChange" />
      <EditAccountModal :show="showEdit" :account="editing" :proxies="proxies" :groups="groups" @close="showEdit = false" @updated="afterChange" />
      <ConfirmDialog :show="!!deleting" :title="t('admin.upstream.deleteTitle')" :message="t('admin.upstream.deleteConfirm', { name: deleting?.name ?? '' })" :confirm-text="t('common.delete')" :cancel-text="t('common.cancel')" :danger="true" @confirm="remove" @cancel="deleting = null" />
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { onMounted, onUnmounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import { getUpstreamBalanceSummary, setUpstreamBalance, type UpstreamBalanceSummary } from '@/api/admin/upstream'
import type { Account, AdminGroup, Proxy } from '@/types'
import { useAppStore } from '@/stores/app'
import AppLayout from '@/components/layout/AppLayout.vue'
import Icon from '@/components/icons/Icon.vue'
import PlatformIcon from '@/components/common/PlatformIcon.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import { EditAccountModal } from '@/components/account'
import UpstreamAccountModal from '@/components/account/UpstreamAccountModal.vue'
import { dateRange } from './operationsUtils'

const { t } = useI18n()
const appStore = useAppStore()
const accounts = ref<Account[]>([])
const groups = ref<AdminGroup[]>([])
const proxies = ref<Proxy[]>([])
const balanceSummary = ref<UpstreamBalanceSummary | null>(null)
const userBalance = ref(0)
const loading = ref(false)
const savingBalance = ref(false)
const page = ref(1)
const pages = ref(1)
const total = ref(0)
const search = ref('')
const status = ref('')
const showProvider = ref(false)
const providerAccount = ref<Account | null>(null)
const showEdit = ref(false)
const editing = ref<Account | null>(null)
const deleting = ref<Account | null>(null)
const editingBalanceId = ref<number | null>(null)
const balanceInput = ref('')
const busyId = ref<number | null>(null)
const discovered = ref<Record<number, string[]>>({})
let searchTimer: ReturnType<typeof setTimeout> | undefined
let requestId = 0
const money = (value: number) => new Intl.NumberFormat(undefined, { style: 'currency', currency: 'USD', maximumFractionDigits: 2 }).format(value)
const balanceOf = (account: Account): number | null => {
  const value = account.extra?.upstream_balance_usd
  return typeof value === 'number' && Number.isFinite(value) ? value : null
}

async function load() {
  const id = ++requestId
  loading.value = true
  try {
    const today = dateRange(1)
    const [list, balances, operations] = await Promise.all([
      adminAPI.accounts.list(page.value, 10, { search: search.value.trim(), status: status.value }),
      getUpstreamBalanceSummary(),
      adminAPI.dashboard.getOperationsDashboard({
        start_date: today.start, end_date: today.end, trend_days: 7,
        timezone: Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC', group_id: 0
      })
    ])
    if (id !== requestId) return
    accounts.value = list.items ?? []
    total.value = list.total
    pages.value = list.pages
    balanceSummary.value = balances
    userBalance.value = operations.metrics.total_user_balance
  } catch (error) {
    if (id === requestId) appStore.showError(t('admin.upstream.loadFailed'))
    console.error('Failed to load upstream management:', error)
  } finally {
    if (id === requestId) loading.value = false
  }
}
function changeFilter() { page.value = 1; void load() }
function debouncedLoad() { if (searchTimer) clearTimeout(searchTimer); searchTimer = setTimeout(changeFilter, 250) }
function openEdit(account: Account) {
  if (account.platform === 'custom' && account.type === 'apikey') {
    providerAccount.value = account
    showProvider.value = true
  } else {
    editing.value = account
    showEdit.value = true
  }
}
function openBalance(account: Account) { editingBalanceId.value = account.id; balanceInput.value = balanceOf(account)?.toString() ?? '' }
async function saveBalance(account: Account) {
  const text = String(balanceInput.value ?? '').trim()
  const amount = text === '' ? null : Number(text)
  if (amount !== null && (!Number.isFinite(amount) || amount < 0 || amount > 1e12)) { appStore.showError(t('admin.upstream.invalidBalance')); return }
  savingBalance.value = true
  try { await setUpstreamBalance(account.id, amount); editingBalanceId.value = null; appStore.showSuccess(t('admin.upstream.saved')); await load() }
  catch (error) { console.error(error); appStore.showError(t('admin.upstream.saveFailed')) }
  finally { savingBalance.value = false }
}
async function discover(account: Account) {
  busyId.value = account.id
  try { discovered.value[account.id] = (await adminAPI.accounts.syncUpstreamModels(account.id)).models; appStore.showSuccess(t('admin.upstream.discovered', { count: discovered.value[account.id].length })) }
  catch (error) { console.error(error); appStore.showError(t('admin.upstream.discoverFailed')) }
  finally { busyId.value = null }
}
async function test(account: Account) {
  busyId.value = account.id
  try { const result = await adminAPI.accounts.testAccount(account.id); if (result.success) appStore.showSuccess(t('admin.upstream.testDone')); else appStore.showError(result.message || t('admin.upstream.testFailed')); await load() }
  catch (error) { console.error(error); appStore.showError(t('admin.upstream.testFailed')) }
  finally { busyId.value = null }
}
async function toggle(account: Account) {
  busyId.value = account.id
  try { await adminAPI.accounts.toggleStatus(account.id, account.status === 'inactive' ? 'active' : 'inactive'); await load() }
  catch (error) { console.error(error); appStore.showError(t('admin.upstream.saveFailed')) }
  finally { busyId.value = null }
}
async function remove() {
  const account = deleting.value
  if (!account) return
  deleting.value = null
  try { await adminAPI.accounts.delete(account.id); if (accounts.value.length === 1 && page.value > 1) page.value--; await load(); appStore.showSuccess(t('admin.upstream.deleted')) }
  catch (error) { console.error(error); appStore.showError(t('admin.upstream.deleteFailed')) }
}
function afterChange() { showProvider.value = false; showEdit.value = false; void load() }
onMounted(() => { void load(); void Promise.all([adminAPI.groups.getAll(), adminAPI.proxies.getAll()]).then(([g, p]) => { groups.value = g; proxies.value = p }).catch(console.error) })
onUnmounted(() => { requestId++; if (searchTimer) clearTimeout(searchTimer) })
</script>
