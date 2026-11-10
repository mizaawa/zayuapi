<template>
  <section class="card flex h-[700px] min-w-0 flex-col overflow-hidden p-0" data-testid="recharge-risk-panel" :aria-busy="loading">
    <div class="ops-panel-head">
      <div class="flex min-w-0 items-center gap-3">
        <span class="ops-panel-icon bg-amber-100 text-amber-600 dark:bg-amber-900/30 dark:text-amber-400"><Icon name="exclamationTriangle" size="sm" :stroke-width="2" /></span>
        <div class="min-w-0">
          <h3 class="ops-panel-title truncate">{{ t('admin.operations.rechargeRisk.title') }}</h3>
          <p class="ops-panel-hint">{{ t('admin.operations.rechargeRisk.readOnlyHint') }}</p>
        </div>
      </div>
      <span class="ops-count shrink-0">{{ t('admin.operations.rechargeRisk.latest25') }}</span>
    </div>
    <div class="relative min-h-0 flex-1 overflow-y-auto overscroll-contain" data-testid="recharge-risk-scroll">
      <div v-if="loading" class="absolute inset-0 z-10 flex items-center justify-center bg-white/75 dark:bg-dark-800/75" role="status"><LoadingSpinner size="md" /><span class="sr-only">{{ t('admin.operations.loading') }}</span></div>
      <button v-for="event in events" :key="event.id" type="button" class="ops-row ops-row-hover flex w-full items-center justify-between gap-3 px-4 py-3 text-left focus-visible:bg-primary-50 focus-visible:outline-none dark:focus-visible:bg-primary-900/20" :data-risk-id="event.id" @click="selected = event">
        <span class="flex min-w-0 items-center gap-2.5"><span class="h-2 w-2 shrink-0 rounded-full" :class="event.result === 'success' ? 'bg-amber-500' : 'bg-red-500'" aria-hidden="true" /><span class="min-w-0"><span class="block truncate text-xs font-semibold text-gray-800 dark:text-gray-100" :title="riskTitle(event.type)">{{ riskTitle(event.type) }}</span><span class="block truncate text-[11px] text-gray-500 dark:text-gray-400" :title="event.user_email">{{ event.user_email || '—' }}</span></span></span>
        <span class="shrink-0 text-[11px] tabular-nums text-gray-400">{{ formatTime(event.last_at) }}</span>
      </button>
      <div v-if="error" class="px-4 py-8 text-center text-xs text-gray-500 dark:text-gray-400">
        <p>{{ t('admin.operations.rechargeRisk.loadFailed') }}</p>
        <button type="button" class="mt-2 font-medium text-primary-600 hover:underline dark:text-primary-400" @click="$emit('retry')">{{ t('admin.operations.retry') }}</button>
      </div>
      <div v-else-if="!events.length && !loading" class="ops-empty text-xs">
        <Icon name="shield" size="lg" class="text-emerald-400" />
        <p>{{ t('admin.operations.rechargeRisk.empty') }}</p>
      </div>
    </div>
    <BaseDialog :show="selected !== null" :title="t('admin.operations.rechargeRisk.details')" width="normal" @close="selected = null">
      <div v-if="selected" class="grid grid-cols-1 gap-4 text-sm sm:grid-cols-2">
        <div class="min-w-0 sm:col-span-2"><p class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.operations.rechargeRisk.account') }}</p><p class="mt-1 break-all font-medium text-gray-900 dark:text-white">{{ selected.user_email }}</p></div>
        <div><p class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.operations.rechargeRisk.type') }}</p><p class="mt-1 font-medium text-gray-900 dark:text-white">{{ riskTitle(selected.type) }}</p></div>
        <div><p class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.operations.rechargeRisk.count') }}</p><p class="mt-1 font-semibold tabular-nums text-gray-900 dark:text-white">{{ selected.order_count || selected.count }}</p></div>
        <div><p class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.operations.rechargeRisk.balance') }}</p><p class="mt-1 font-semibold tabular-nums text-gray-900 dark:text-white">{{ formatMoney(selected.user_balance) }}</p></div>
        <div><p class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.operations.rechargeRisk.totalSpend') }}</p><p class="mt-1 font-semibold tabular-nums text-gray-900 dark:text-white">{{ formatMoney(selected.total_consumption) }}</p></div>
        <div class="sm:col-span-2"><p class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.operations.rechargeRisk.timeRange') }}</p><p class="mt-1 tabular-nums text-gray-900 dark:text-white">{{ formatTime(selected.first_at) }} — {{ formatTime(selected.last_at) }}</p></div>
        <div class="sm:col-span-2"><p class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.operations.rechargeRisk.outcome') }}</p><p class="mt-1 font-medium text-gray-900 dark:text-white">{{ t(`admin.operations.rechargeRisk.${selected.result === 'success' ? 'succeeded' : 'failed'}`) }}</p></div>
      </div>
      <template #footer><div class="flex justify-end"><button type="button" class="btn btn-secondary" @click="selected = null">{{ t('common.close') }}</button></div></template>
    </BaseDialog>
  </section>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import type { RechargeRiskEvent } from '@/views/admin/operationsRechargeTypes'
import { formatMoney } from '@/views/admin/operationsUtils'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'

// timeZone is the site zone the page reports in; the browser's when omitted.
const props = defineProps<{ events: RechargeRiskEvent[]; loading: boolean; error: boolean; timeZone?: string }>()
defineEmits<{ retry: [] }>()
const { t } = useI18n()
const selected = ref<RechargeRiskEvent | null>(null)
function riskTitle(type: string): string { return t(`admin.operations.rechargeRisk.types.${type}`, type) }
function formatTime(value: string): string {
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? '—' : new Intl.DateTimeFormat(undefined, { dateStyle: 'short', timeStyle: 'short', timeZone: props.timeZone }).format(date)
}
</script>
