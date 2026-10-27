<template>
  <section class="card flex h-[620px] min-w-0 flex-col overflow-visible p-0" :aria-label="t('admin.operations.recharge.ordersTitle')" data-testid="recharge-orders-panel">
    <div class="flex flex-wrap items-center justify-between gap-3 border-b border-gray-100 px-4 py-4 dark:border-dark-700 sm:px-5">
      <div class="min-w-0">
        <h3 class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('admin.operations.recharge.ordersTitle') }}</h3>
        <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.operations.recharge.ordersHint') }}</p>
      </div>
      <div class="segment-group shrink-0" :aria-label="t('admin.operations.recharge.timeRange')">
        <button v-for="range in ranges" :key="range.days" type="button" class="rounded-full px-2.5 py-1.5 text-xs font-medium transition-colors sm:px-3"
          :class="days === range.days ? 'segment-active' : 'segment-idle'"
          :aria-pressed="days === range.days" :data-order-days="range.days" @click="$emit('select-days', range.days)">{{ t(range.label) }}</button>
      </div>
    </div>

    <div class="relative z-10 space-y-3 border-b border-gray-100 px-4 py-3 dark:border-dark-700 sm:px-5">
      <div class="relative">
        <Icon name="search" size="sm" class="pointer-events-none absolute left-3.5 top-1/2 -translate-y-1/2 text-gray-400" />
        <input :value="search" type="search" class="input ops-search"
          :placeholder="t('admin.operations.recharge.searchEmail')" :aria-label="t('admin.operations.recharge.searchEmail')" role="combobox" aria-autocomplete="list" aria-controls="recharge-order-suggestions" :aria-expanded="suggestionsOpen && Boolean(search.trim())" data-testid="recharge-order-search"
          @input="$emit('update:search', ($event.target as HTMLInputElement).value)" @focus="$emit('focus-search')" @blur="$emit('blur-search')" @keydown.esc="($event.target as HTMLInputElement).blur()" />
        <div v-if="suggestionsOpen && search.trim()" id="recharge-order-suggestions" role="listbox" class="absolute left-0 right-0 top-full z-30 mt-1 max-h-48 overflow-y-auto ops-popover py-1" data-testid="recharge-order-suggestions">
          <p v-if="suggestionsLoading" class="flex items-center gap-2 px-3 py-2 text-xs text-gray-500" role="status"><LoadingSpinner size="sm" />{{ t('admin.operations.loading') }}</p>
          <button v-for="user in suggestions" :key="user.id" type="button" role="option" class="block w-full truncate px-3 py-2 text-left text-sm text-gray-700 hover:bg-primary-50 focus:bg-primary-50 dark:text-gray-200 dark:hover:bg-dark-700 dark:focus:bg-dark-700" :aria-selected="false" @mousedown.prevent @click="$emit('select-suggestion', user)">{{ user.email }}</button>
          <p v-if="!suggestionsLoading && !suggestions.length" class="px-3 py-2 text-xs text-gray-500">{{ t('admin.operations.noMatchingEmails') }}</p>
        </div>
      </div>
      <div class="flex flex-wrap gap-x-3 gap-y-2">
        <div class="segment-group max-w-full overflow-x-auto" :aria-label="t('admin.operations.recharge.status')">
          <button v-for="option in statuses" :key="option.value" type="button" class="shrink-0 rounded-full px-2.5 py-1 text-xs font-medium transition-colors"
            :class="status === option.value ? 'segment-active' : 'segment-idle'"
            :aria-pressed="status === option.value" :data-order-status="option.value" @click="$emit('select-status', option.value)">{{ t(option.label) }}</button>
        </div>
        <div class="segment-group max-w-full overflow-x-auto" :aria-label="t('admin.operations.recharge.channel')">
          <button v-for="option in channels" :key="option.value" type="button" class="shrink-0 rounded-full px-2.5 py-1 text-xs font-medium transition-colors"
            :class="channel === option.value ? 'segment-active' : 'segment-idle'"
            :aria-pressed="channel === option.value" :data-order-channel="option.value" @click="$emit('select-channel', option.value)">{{ t(option.label) }}</button>
        </div>
      </div>
    </div>

    <div class="hidden border-b border-gray-100 bg-gray-50/80 px-4 py-2.5 text-[11px] font-medium text-gray-500 dark:border-dark-700 dark:bg-dark-800/40 dark:text-gray-400 sm:grid sm:grid-cols-[minmax(0,1.5fr)_minmax(0,.9fr)_minmax(0,.8fr)_minmax(0,.85fr)_minmax(0,1fr)] sm:gap-3 sm:px-5">
      <span>{{ t('admin.operations.recharge.email') }}</span><span>{{ t('admin.operations.recharge.amount') }}</span><span>{{ t('admin.operations.recharge.status') }}</span><span>{{ t('admin.operations.recharge.channel') }}</span><span class="text-right">{{ t('admin.operations.recharge.time') }}</span>
    </div>
    <div class="relative min-h-0 flex-1 overflow-y-auto overscroll-contain" :aria-busy="loading" data-testid="recharge-orders-scroll-region">
      <div v-if="loading" class="absolute inset-0 z-10 flex items-center justify-center bg-white/75 dark:bg-dark-800/75" role="status"><LoadingSpinner size="md" /><span class="sr-only">{{ t('admin.operations.loading') }}</span></div>
      <div v-for="order in orders" :key="order.id" class="border-b border-gray-100 px-4 py-3 last:border-b-0 dark:border-dark-700/70 sm:grid sm:grid-cols-[minmax(0,1.5fr)_minmax(0,.9fr)_minmax(0,.8fr)_minmax(0,.85fr)_minmax(0,1fr)] sm:items-center sm:gap-3 sm:px-5" :data-recharge-order="order.id">
        <div class="flex min-w-0 items-start justify-between gap-3 sm:block">
          <span class="min-w-0 break-all text-sm font-medium text-gray-900 dark:text-white" :title="order.user_email">{{ order.user_email || '—' }}</span>
          <span class="shrink-0 text-sm font-semibold tabular-nums text-gray-900 dark:text-white sm:hidden">{{ formatMoney(order.amount, order.currency) }}</span>
        </div>
        <span class="hidden text-sm font-semibold tabular-nums text-gray-900 dark:text-white sm:block">{{ formatMoney(order.amount, order.currency) }}</span>
        <div class="mt-2 flex flex-wrap items-center gap-2 sm:mt-0">
          <span class="inline-flex rounded-full px-2 py-0.5 text-[11px] font-medium" :class="order.status === 'paid' ? 'bg-emerald-50 text-emerald-700 dark:bg-emerald-900/20 dark:text-emerald-400' : 'bg-amber-50 text-amber-700 dark:bg-amber-900/20 dark:text-amber-400'">{{ t(`admin.operations.recharge.${order.status}`) }}</span>
          <span class="inline-flex rounded-full bg-gray-100 px-2 py-0.5 text-[11px] font-medium text-gray-600 dark:bg-dark-700 dark:text-gray-300 sm:hidden">{{ t(`admin.operations.recharge.${order.channel}`) }}</span>
          <span class="ml-auto text-[11px] tabular-nums text-gray-500 dark:text-gray-400 sm:hidden">{{ formatTime(order.created_at) }}</span>
        </div>
        <span class="hidden text-xs text-gray-600 dark:text-gray-300 sm:block">{{ t(`admin.operations.recharge.${order.channel}`) }}</span>
        <span class="hidden text-right text-xs tabular-nums text-gray-500 dark:text-gray-400 sm:block">{{ formatTime(order.created_at) }}</span>
      </div>
      <div v-if="error && !loading" class="px-4 py-12 text-center text-sm text-gray-500 dark:text-gray-400">
        <p>{{ t('admin.operations.recharge.loadFailed') }}</p>
        <button type="button" class="mt-2 font-medium text-primary-600 hover:underline dark:text-primary-400" @click="$emit('retry')">{{ t('admin.operations.retry') }}</button>
      </div>
      <p v-else-if="!orders.length && !loading" class="px-4 py-12 text-center text-sm text-gray-400">{{ t('admin.operations.recharge.empty') }}</p>
    </div>
    <div class="flex items-center justify-between gap-3 border-t border-gray-100 px-4 py-3 text-xs text-gray-500 dark:border-dark-700 dark:text-gray-400 sm:px-5">
      <span>{{ t('admin.operations.recharge.pageSummary', { page, pages, total }) }}</span>
      <div class="flex shrink-0 gap-2">
        <button type="button" class="btn btn-secondary btn-sm" :disabled="page <= 1 || loading" @click="$emit('select-page', page - 1)">{{ t('admin.operations.recharge.previous') }}</button>
        <button type="button" class="btn btn-secondary btn-sm" :disabled="page >= pages || loading" @click="$emit('select-page', page + 1)">{{ t('admin.operations.recharge.next') }}</button>
      </div>
    </div>
  </section>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type { SimpleUser } from '@/api/admin/usage'
import type { RechargeChannel, RechargeOrder, RechargeStatus } from '@/views/admin/operationsRechargeTypes'
import { formatMoney } from '@/views/admin/operationsUtils'
import Icon from '@/components/icons/Icon.vue'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'

defineProps<{
  orders: RechargeOrder[]
  loading: boolean
  error: boolean
  page: number
  pages: number
  total: number
  search: string
  suggestions: SimpleUser[]
  suggestionsOpen: boolean
  suggestionsLoading: boolean
  status: 'all' | RechargeStatus
  channel: 'all' | RechargeChannel
  days: 1 | 7 | 30
}>()
defineEmits<{
  'update:search': [value: string]
  'focus-search': []
  'blur-search': []
  'select-suggestion': [user: SimpleUser]
  'select-status': [value: 'all' | RechargeStatus]
  'select-channel': [value: 'all' | RechargeChannel]
  'select-days': [value: 1 | 7 | 30]
  'select-page': [value: number]
  retry: []
}>()

const { t, locale } = useI18n()
const ranges = [
  { days: 1, label: 'admin.operations.recharge.today' },
  { days: 7, label: 'admin.operations.recharge.last7Days' },
  { days: 30, label: 'admin.operations.recharge.last30Days' }
] as const
const statuses = [
  { value: 'all', label: 'admin.operations.recharge.all' },
  { value: 'paid', label: 'admin.operations.recharge.paid' },
  { value: 'unpaid', label: 'admin.operations.recharge.unpaid' }
] as const
const channels = [
  { value: 'all', label: 'admin.operations.recharge.all' },
  { value: 'code', label: 'admin.operations.recharge.code' },
  { value: 'direct', label: 'admin.operations.recharge.direct' },
  { value: 'manual', label: 'admin.operations.recharge.manual' }
] as const

function formatTime(value: string): string {
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? '—' : new Intl.DateTimeFormat(locale?.value || undefined, { dateStyle: 'short', timeStyle: 'short' }).format(date)
}
</script>
