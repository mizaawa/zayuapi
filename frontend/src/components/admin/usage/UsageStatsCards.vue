<template>
  <section class="usage-summary" data-testid="usage-summary">
    <div class="usage-summary-item is-requests">
      <span class="usage-summary-icon" aria-hidden="true"><Icon name="document" size="md" /></span>
      <div class="usage-summary-body">
        <p class="usage-summary-label">{{ t('usage.totalRequests') }}</p>
        <p class="usage-summary-value">{{ formatCount(stats?.total_requests) }}</p>
        <p class="usage-summary-sub">{{ t(cumulative ? 'usage.summaryCumulative' : 'usage.summaryRange') }}</p>
      </div>
    </div>

    <div class="usage-summary-item is-duration">
      <span class="usage-summary-icon" aria-hidden="true"><Icon name="clock" size="md" /></span>
      <div class="usage-summary-body">
        <p class="usage-summary-label">{{ t('usage.avgDuration') }}</p>
        <p class="usage-summary-value">{{ formatDuration(stats?.average_duration_ms || 0) }}</p>
        <p class="usage-summary-sub">{{ t('usage.perRequest') }}</p>
      </div>
    </div>

    <div class="usage-summary-item is-cost">
      <span class="usage-summary-icon" aria-hidden="true"><Icon name="dollar" size="md" /></span>
      <div class="usage-summary-body">
        <p class="usage-summary-label">{{ t('usage.totalCost') }}</p>
        <p class="usage-summary-value is-money" :title="t('usage.actualCost')">{{ formatMoney(stats?.total_actual_cost) }}</p>
        <p class="usage-summary-chips">
          <span class="usage-summary-chip" :title="t('usage.standardCost')">
            {{ t('usage.standardCost') }}
            <span :class="{ 'line-through': strikeStandardCost }">{{ formatMoney(stats?.total_cost) }}</span>
          </span>
          <span v-if="showAccountCost && totalAccountCost != null" class="usage-summary-chip is-account">
            {{ t('usage.accountCost') }} {{ formatMoney(totalAccountCost) }}
          </span>
        </p>
      </div>
    </div>

    <!-- Tokens: the total, its mix as a stacked bar, and every part spelled out (no hover needed). -->
    <div class="usage-summary-item is-tokens">
      <span class="usage-summary-icon" aria-hidden="true"><Icon name="cube" size="md" /></span>
      <div class="usage-summary-body">
        <p class="usage-summary-label">
          {{ t('usage.totalTokens') }}
          <span class="usage-summary-cache">{{ t('usage.cacheTotal') }} {{ formatTokens(stats?.total_cache_tokens || 0) }}</span>
        </p>
        <p class="usage-summary-value">{{ formatTokens(stats?.total_tokens || 0) }}</p>
        <div class="usage-summary-bar" role="img" :aria-label="tokenMixLabel">
          <span
            v-for="part in visibleParts"
            :key="part.key"
            :style="{ width: part.percent + '%', background: part.color }"
          ></span>
        </div>
        <ul class="usage-summary-legend" :aria-label="t('usage.cacheBreakdown')">
          <li v-for="part in tokenParts" :key="part.key" :class="{ 'is-zero': part.value === 0 }">
            <span class="usage-summary-swatch" :style="{ background: part.color }" aria-hidden="true"></span>
            <span class="usage-summary-legend-name">{{ part.label }}</span>
            <span class="usage-summary-legend-value">{{ formatTokens(part.value) }}</span>
          </li>
        </ul>
      </div>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { AdminUsageStatsResponse } from '@/api/admin/usage'
import type { UsageStatsResponse } from '@/types'
import Icon from '@/components/icons/Icon.vue'

const props = withDefaults(defineProps<{
  stats: (AdminUsageStatsResponse | UsageStatsResponse) | null
  showAccountCost?: boolean
  strikeStandardCost?: boolean
  cumulative?: boolean
}>(), {
  showAccountCost: true,
  strikeStandardCost: false,
  cumulative: false,
})

const { t } = useI18n()

const totalAccountCost = computed(() => {
  const stats = props.stats as (AdminUsageStatsResponse & { total_account_cost?: number }) | null
  return stats?.total_account_cost ?? null
})
const showAccountCost = computed(() => props.showAccountCost)
const strikeStandardCost = computed(() => props.strikeStandardCost)

// Same entity colours as the dashboards' token mix and trend charts.
const TOKEN_COLORS = {
  input: '#3b82f6',
  output: '#10b981',
  cacheWrite: '#f59e0b',
  cacheRead: '#06b6d4',
} as const

const safe = (value: number | null | undefined): number => {
  const numberValue = Number(value)
  return Number.isFinite(numberValue) && numberValue > 0 ? numberValue : 0
}

const tokenParts = computed(() => {
  const stats = props.stats
  const parts = [
    { key: 'input', label: t('usage.in'), value: safe(stats?.total_input_tokens), color: TOKEN_COLORS.input },
    { key: 'output', label: t('usage.out'), value: safe(stats?.total_output_tokens), color: TOKEN_COLORS.output },
    { key: 'cacheWrite', label: t('usage.cacheCreationTokensLabel'), value: safe(stats?.total_cache_creation_tokens), color: TOKEN_COLORS.cacheWrite },
    { key: 'cacheRead', label: t('usage.cacheReadTokensLabel'), value: safe(stats?.total_cache_read_tokens), color: TOKEN_COLORS.cacheRead },
  ]
  const sum = parts.reduce((acc, part) => acc + part.value, 0)
  return parts.map((part) => ({ ...part, percent: sum > 0 ? (part.value / sum) * 100 : 0 }))
})

const visibleParts = computed(() => tokenParts.value.filter((part) => part.value > 0))

const tokenMixLabel = computed(() =>
  tokenParts.value.map((part) => `${part.label} ${formatTokens(part.value)}`).join(', ')
)

const formatCount = (value: number | null | undefined): string => safe(value).toLocaleString()

const formatMoney = (value: number | null | undefined): string => {
  const numberValue = Number(value)
  const amount = Number.isFinite(numberValue) ? numberValue : 0
  return `$${amount.toLocaleString(undefined, { minimumFractionDigits: 4, maximumFractionDigits: 4 })}`
}

const formatDuration = (ms: number) =>
  ms < 1000 ? `${ms.toFixed(0)}ms` : `${(ms / 1000).toFixed(2)}s`

function formatTokens(value: number): string {
  if (value >= 1e9) return (value / 1e9).toFixed(2) + 'B'
  if (value >= 1e6) return (value / 1e6).toFixed(2) + 'M'
  if (value >= 1e3) return (value / 1e3).toFixed(2) + 'K'
  return value.toLocaleString()
}
</script>

<style scoped>
/* Phones: two compact tiles, then spend and tokens on their own rows.
 * Desktop: one row, with the token mix given the most room. */
.usage-summary {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  grid-template-areas:
    'requests duration'
    'cost cost'
    'tokens tokens';
  gap: 0.75rem;
}

.usage-summary-item {
  display: flex;
  min-width: 0;
  align-items: flex-start;
  gap: 0.75rem;
  padding: 0.875rem 1rem;
  border-radius: 1.25rem;
  background: var(--md-sys-color-surface);
  box-shadow: var(--theme-highlight), var(--theme-elevation-1);
}

.usage-summary-item.is-requests { grid-area: requests; }
.usage-summary-item.is-duration { grid-area: duration; }
.usage-summary-item.is-cost { grid-area: cost; }
.usage-summary-item.is-tokens { grid-area: tokens; }

.usage-summary-icon {
  display: inline-flex;
  width: 2.25rem;
  height: 2.25rem;
  flex-shrink: 0;
  align-items: center;
  justify-content: center;
  border-radius: 0.75rem;
}

.is-requests .usage-summary-icon { background: #faf6e8; color: #5b5238; }
.is-duration .usage-summary-icon { background: #ede9fe; color: #6d28d9; }
.is-cost .usage-summary-icon { background: #e4f0df; color: #386344; }
.is-tokens .usage-summary-icon { background: #fff3bd; color: #7e5600; }

.usage-summary-body {
  container-type: inline-size;
  display: flex;
  min-width: 0;
  flex: 1;
  flex-direction: column;
  gap: 0.125rem;
}

.usage-summary-label {
  display: flex;
  min-width: 0;
  flex-wrap: wrap;
  align-items: center;
  gap: 0.25rem 0.5rem;
  font-size: 0.75rem;
  font-weight: 600;
  color: var(--md-sys-color-on-surface-variant);
}

/* Sized from the tile's own width (cqi) so "128,734" fits a narrow phone
 * tile instead of being cut off, while wide tiles keep the full size. */
.usage-summary-value {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 1.375rem;
  font-size: clamp(1.125rem, 19cqi, 1.375rem);
  font-weight: 750;
  line-height: 1.75rem;
  letter-spacing: -0.01em;
  color: var(--md-sys-color-on-surface);
}

.usage-summary-value.is-money {
  color: #2e5038;
}

.usage-summary-sub {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 0.75rem;
  color: #9b8c62;
}

.usage-summary-chips {
  display: flex;
  flex-wrap: wrap;
  gap: 0.375rem;
  margin-top: 0.25rem;
}

.usage-summary-chip {
  display: inline-flex;
  max-width: 100%;
  align-items: center;
  gap: 0.25rem;
  padding: 0.0625rem 0.5rem;
  border-radius: 999px;
  background: #faf6e8;
  font-size: 0.6875rem;
  font-variant-numeric: tabular-nums;
  color: #5b5238;
  white-space: nowrap;
}

.usage-summary-chip.is-account {
  background: #ffedd5;
  color: #9a3412;
}

.usage-summary-cache {
  padding: 0 0.4375rem;
  border-radius: 999px;
  background: #e0f7fa;
  font-size: 0.6875rem;
  font-weight: 600;
  font-variant-numeric: tabular-nums;
  color: #0e7490;
}

/* Stacked bar: 2px surface gaps between parts instead of strokes. */
.usage-summary-bar {
  display: flex;
  height: 0.4375rem;
  gap: 2px;
  margin-top: 0.375rem;
  overflow: hidden;
  border-radius: 999px;
  background: #eee5c9;
}

.usage-summary-bar > span {
  min-width: 2px;
  height: 100%;
}

.usage-summary-legend {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 0.25rem 0.875rem;
  margin-top: 0.5rem;
  font-size: 0.6875rem;
}

.usage-summary-legend li {
  display: flex;
  min-width: 0;
  align-items: center;
  gap: 0.3125rem;
  color: var(--md-sys-color-on-surface-variant);
}

.usage-summary-legend li.is-zero {
  opacity: 0.55;
}

.usage-summary-swatch {
  width: 0.4375rem;
  height: 0.4375rem;
  flex-shrink: 0;
  border-radius: 0.125rem;
}

.usage-summary-legend-name {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.usage-summary-legend-value {
  margin-left: auto;
  font-weight: 650;
  font-variant-numeric: tabular-nums;
  color: var(--md-sys-color-on-surface);
}

@media (min-width: 768px) {
  .usage-summary {
    grid-template-areas:
      'requests duration'
      'tokens cost';
    gap: 1rem;
  }

  .usage-summary-item {
    padding: 1rem 1.125rem;
  }
}

@media (min-width: 1280px) {
  .usage-summary {
    grid-template-columns: minmax(0, 1fr) minmax(0, 1.5fr) minmax(0, 1.2fr) minmax(0, 0.9fr);
    grid-template-areas: 'requests tokens cost duration';
  }
}

@media (min-width: 1536px) {
  .usage-summary-value {
    font-size: clamp(1.125rem, 19cqi, 1.5rem);
    line-height: 2rem;
  }

  .usage-summary-legend {
    grid-template-columns: repeat(4, minmax(0, auto));
    justify-content: start;
  }
}
</style>
