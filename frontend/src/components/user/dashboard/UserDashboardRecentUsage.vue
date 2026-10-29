<template>
  <section class="dash-recent-panel">
    <header class="dash-recent-head">
      <div class="flex items-center gap-2">
        <h2 class="dash-recent-title">{{ t('dashboard.recentUsage') }}</h2>
        <span class="dash-recent-range">{{ t('dashboard.last7Days') }}</span>
      </div>
      <router-link v-if="!loading && data.length > 0" to="/usage" class="dash-recent-link">
        {{ t('dashboard.viewAllUsage') }}
        <Icon name="chevronRight" size="sm" />
      </router-link>
    </header>

    <div v-if="loading" class="flex items-center justify-center py-12">
      <LoadingSpinner size="lg" />
    </div>
    <div v-else-if="data.length === 0" class="py-6">
      <EmptyState :title="t('dashboard.noUsageRecords')" :description="t('dashboard.startUsingApi')" />
    </div>
    <ul v-else class="dash-recent-list">
      <li v-for="log in data" :key="log.id" class="dash-recent-row">
        <span class="dash-recent-icon" aria-hidden="true">
          <ModelIcon :model="log.model" size="18px" />
        </span>
        <div class="min-w-0 flex-1">
          <p class="dash-recent-model" :title="log.model">{{ log.model }}</p>
          <p class="dash-recent-meta">
            <span :title="formatDateTime(log.created_at)">{{ formatRelativeTime(log.created_at) }}</span>
            <span v-if="log.api_key?.name" class="truncate">{{ log.api_key.name }}</span>
          </p>
        </div>
        <div class="dash-recent-numbers">
          <p class="dash-recent-cost">
            <span :title="t('dashboard.actual')">${{ formatCost(log.actual_cost) }}</span>
            <span
              v-if="log.total_cost > log.actual_cost"
              class="dash-recent-list-price"
              :title="t('dashboard.standard')"
            >${{ formatCost(log.total_cost) }}</span>
          </p>
          <p class="dash-recent-tokens">{{ (log.input_tokens + log.output_tokens).toLocaleString() }} tokens</p>
        </div>
      </li>
    </ul>
  </section>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import ModelIcon from '@/components/common/ModelIcon.vue'
import Icon from '@/components/icons/Icon.vue'
import { formatDateTime, formatRelativeTime } from '@/utils/format'
import type { UsageLog } from '@/types'

defineProps<{
  data: UsageLog[]
  loading: boolean
}>()
const { t } = useI18n()
const formatCost = (c: number) => c.toFixed(4)
</script>

<style scoped>
.dash-recent-panel {
  height: 100%;
  padding: 1.25rem;
  border-radius: 1.5rem;
  background: var(--md-sys-color-surface);
  box-shadow: var(--theme-highlight), var(--theme-elevation-1);
}

.dash-recent-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 0.75rem;
  margin-bottom: 0.75rem;
}

.dash-recent-title {
  font-size: 0.9375rem;
  font-weight: 700;
  color: var(--md-sys-color-on-surface);
}

.dash-recent-range {
  padding: 0.125rem 0.5rem;
  border-radius: 999px;
  background: #faf6e8;
  font-size: 0.6875rem;
  color: var(--md-sys-color-on-surface-variant);
}

.dash-recent-link {
  display: inline-flex;
  align-items: center;
  gap: 0.125rem;
  font-size: 0.8125rem;
  font-weight: 600;
  color: #7e5600;
}

.dash-recent-link:hover {
  color: #523a0d;
}

.dash-recent-list {
  display: flex;
  flex-direction: column;
}

.dash-recent-row {
  display: flex;
  align-items: center;
  gap: 0.75rem;
  padding: 0.75rem 0.5rem;
  border-radius: 0.875rem;
  transition: background-color 150ms ease;
}

.dash-recent-row + .dash-recent-row {
  box-shadow: inset 0 1px 0 rgb(215 193 106 / 0.22);
}

.dash-recent-row:hover {
  background: #fffbe8;
}

.dash-recent-icon {
  display: inline-flex;
  width: 2.25rem;
  height: 2.25rem;
  flex-shrink: 0;
  align-items: center;
  justify-content: center;
  border-radius: 0.75rem;
  background: #faf6e8;
}

.dash-recent-model {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 0.875rem;
  font-weight: 600;
  color: var(--md-sys-color-on-surface);
}

.dash-recent-meta {
  display: flex;
  min-width: 0;
  gap: 0.625rem;
  font-size: 0.75rem;
  color: var(--md-sys-color-on-surface-variant);
}

.dash-recent-numbers {
  flex-shrink: 0;
  text-align: right;
}

.dash-recent-cost {
  display: flex;
  justify-content: flex-end;
  gap: 0.375rem;
  font-size: 0.875rem;
  font-weight: 650;
  font-variant-numeric: tabular-nums;
  color: var(--md-sys-color-on-surface);
}

.dash-recent-list-price {
  font-weight: 400;
  text-decoration: line-through;
  color: var(--md-sys-color-on-surface-variant);
}

.dash-recent-tokens {
  font-size: 0.75rem;
  font-variant-numeric: tabular-nums;
  color: var(--md-sys-color-on-surface-variant);
}

@media (min-width: 768px) {
  .dash-recent-panel {
    padding: 1.5rem;
  }
}
</style>
