<template>
  <section class="card flex min-w-0 flex-col overflow-hidden p-0" :aria-label="t('admin.operations.rechargeRanking.title')" data-testid="recharge-ranking-panel" :aria-busy="loading">
    <header class="ops-panel-head flex-nowrap">
      <div class="flex min-w-0 items-center gap-3">
        <span class="ops-panel-icon bg-amber-100 text-amber-600 dark:bg-amber-900/30 dark:text-amber-400"><Icon name="trophy" size="sm" :stroke-width="2" /></span>
        <div class="min-w-0">
          <h3 class="ops-panel-title">{{ t('admin.operations.rechargeRanking.title') }}</h3>
          <p class="ops-panel-hint" data-testid="recharge-ranking-window">{{ windowHint }}</p>
        </div>
      </div>
      <span class="ops-count shrink-0">{{ t('admin.operations.rechargeRanking.top50') }}</span>
    </header>
    <div class="grid grid-cols-2 gap-2 px-4 pt-3 sm:px-5">
      <div class="ops-stat">
        <p class="ops-stat-label">{{ t('admin.operations.rechargeRanking.totalAmount') }}</p>
        <p class="ops-stat-value text-emerald-700 dark:text-emerald-400" data-testid="recharge-ranking-total">{{ ranking ? formatMoney(ranking.total_amount) : '—' }}</p>
      </div>
      <div class="ops-stat">
        <p class="ops-stat-label">{{ t('admin.operations.rechargeRanking.totalUsers') }}</p>
        <p class="ops-stat-value" data-testid="recharge-ranking-users">{{ ranking ? ranking.total_users : '—' }}</p>
      </div>
    </div>
    <div class="ops-table-head mt-3 flex items-center justify-between gap-3 px-4 py-2.5 sm:px-5">
      <span>{{ t('admin.operations.rechargeRanking.user') }}</span><span>{{ t('admin.operations.rechargeRanking.amount') }}</span>
    </div>
    <div class="relative max-h-[420px] min-h-40 flex-1 overflow-y-auto overscroll-contain xl:max-h-none" data-testid="recharge-ranking-scroll">
      <div v-if="loading" class="absolute inset-0 z-10 flex items-center justify-center bg-white/75 dark:bg-dark-800/75" role="status"><LoadingSpinner size="md" /><span class="sr-only">{{ t('admin.operations.loading') }}</span></div>
      <!-- A failed refresh keeps the last ranking below the notice. -->
      <div v-if="error && !loading" class="ops-row px-4 py-4 text-center text-sm text-gray-500 dark:text-gray-400" role="alert">
        <p>{{ t('admin.operations.rechargeRanking.loadFailed') }}</p>
        <button type="button" class="mt-1 font-medium text-primary-600 hover:underline dark:text-primary-400" @click="$emit('retry')">{{ t('admin.operations.retry') }}</button>
      </div>
      <div v-for="(entry, index) in users" :key="entry.user_id" class="ops-row ops-row-hover flex items-center gap-3 px-4 py-2 sm:px-5" :data-ranking-user="entry.user_id">
        <span class="ops-rank" :class="rankClass(index)" :data-rank="index + 1">{{ index + 1 }}</span>
        <div class="min-w-0 flex-1">
          <p class="truncate text-sm font-medium text-gray-900 dark:text-white" :title="entry.email">{{ entry.email }}</p>
          <span class="ops-share" aria-hidden="true"><span :style="{ width: shareWidth(entry.amount) }" /></span>
        </div>
        <div class="shrink-0 text-right" :title="shareTitle(entry.amount)">
          <p class="text-sm font-semibold tabular-nums text-gray-900 dark:text-white">{{ formatMoney(entry.amount) }}</p>
          <p class="text-[11px] tabular-nums text-gray-500 dark:text-gray-400">{{ t('admin.operations.rechargeRanking.count', { count: entry.recharge_count }) }}</p>
        </div>
      </div>
      <div v-if="!error && !users.length && !loading" class="ops-empty">
        <Icon name="trophy" size="lg" class="text-primary-300" />
        <p>{{ t('admin.operations.rechargeRanking.empty') }}</p>
      </div>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { OperationsRechargeRankingResponse } from '@/api/admin/dashboard'
import Icon from '@/components/icons/Icon.vue'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import { formatMoney } from '@/views/admin/operationsUtils'

const props = defineProps<{
  ranking: OperationsRechargeRankingResponse | null
  loading: boolean
  error: boolean
}>()
defineEmits<{ retry: [] }>()
const { t } = useI18n()

const users = computed(() => props.ranking?.users ?? [])
const topAmount = computed(() => users.value[0]?.amount ?? 0)
const windowHint = computed(() => props.ranking
  ? t('admin.operations.rechargeRanking.window', { date: props.ranking.date, timezone: props.ranking.site_timezone })
  : t('admin.operations.rechargeRanking.hint'))

function rankClass(index: number): string {
  return ['ops-rank-gold', 'ops-rank-silver', 'ops-rank-bronze'][index] ?? ''
}
// Bars compare each user with the top payer; the title gives the share of today's total.
function shareWidth(amount: number): string {
  return topAmount.value > 0 ? `${Math.max(4, (amount / topAmount.value) * 100)}%` : '0%'
}
function shareTitle(amount: number): string {
  const total = props.ranking?.total_amount ?? 0
  return total > 0 ? t('admin.operations.rechargeRanking.share', { percent: (amount / total * 100).toFixed(1) }) : ''
}
</script>

<style scoped>
.ops-share {
  position: relative;
  display: block;
  width: 100%;
  max-width: 12rem;
  height: 0.3125rem;
  margin-top: 0.375rem;
  overflow: hidden;
  border-radius: 999px;
  background: color-mix(in srgb, var(--md-sys-color-surface-container) 85%, transparent);
}

.ops-share > span {
  position: absolute;
  inset: 0 auto 0 0;
  border-radius: inherit;
  background: linear-gradient(90deg, #79b37b, #e9b824);
}
</style>
