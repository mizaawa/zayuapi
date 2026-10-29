<template>
  <!-- Hero: the balance leads, today's numbers sit beside it. -->
  <section class="dash-hero" :class="{ 'dash-hero-simple': isSimple }">
    <div v-if="!isSimple" class="dash-balance">
      <p class="dash-balance-label">{{ t('dashboard.balance') }}</p>
      <p class="dash-balance-figure" :class="balanceToneClass">${{ formatBalance(balance) }}</p>
      <p class="dash-balance-caption">
        {{ t('dashboard.keysSummary', { total: stats.total_api_keys || 0, active: stats.active_api_keys || 0 }) }}
      </p>
    </div>

    <div class="dash-today">
      <header class="dash-today-head">
        <h2 class="dash-section-title">{{ t('dashboard.today') }}</h2>
        <p class="dash-live">
          <span class="dash-live-dot" :class="{ 'is-active': (stats.rpm || 0) > 0 }" aria-hidden="true"></span>
          {{ t('dashboard.liveThroughput', { rpm: formatTokens(stats.rpm || 0), tpm: formatTokens(stats.tpm || 0) }) }}
        </p>
      </header>

      <dl class="dash-kpis">
        <div class="dash-kpi">
          <dt>{{ t('dashboard.todayRequests') }}</dt>
          <dd class="dash-kpi-value">{{ formatNumber(stats.today_requests || 0) }}</dd>
          <dd class="dash-kpi-sub" :title="stats.totals_updated_at">{{ t('common.total') }}: {{ formatTotal(stats.total_requests, formatNumber) }}</dd>
        </div>
        <div class="dash-kpi">
          <dt>{{ t('dashboard.todayCost') }}</dt>
          <dd class="dash-kpi-value" :title="t('dashboard.actual')">${{ formatCost(stats.today_actual_cost || 0) }}</dd>
          <dd class="dash-kpi-sub">
            <span
              v-if="(stats.today_cost || 0) > (stats.today_actual_cost || 0)"
              class="dash-kpi-list-price"
              :title="t('dashboard.standard')"
            >${{ formatCost(stats.today_cost || 0) }}</span>
            <span :title="stats.totals_updated_at || t('dashboard.actual')">{{ t('common.total') }}: {{ formatTotal(stats.total_actual_cost, (value) => '$' + formatCost(value)) }}</span>
          </dd>
        </div>
        <div class="dash-kpi">
          <dt>{{ t('dashboard.todayTokens') }}</dt>
          <dd class="dash-kpi-value">{{ formatTokens(stats.today_tokens || 0) }}</dd>
          <dd class="dash-kpi-sub" :title="stats.totals_updated_at">{{ t('common.total') }}: {{ formatTotal(stats.total_tokens, formatTokens) }}</dd>
        </div>
        <div class="dash-kpi">
          <dt>{{ t('dashboard.avgResponse') }}</dt>
          <dd class="dash-kpi-value">{{ formatTotal(stats.average_duration_ms, formatDuration) }}</dd>
          <dd class="dash-kpi-sub">{{ t('dashboard.averageTime') }}</dd>
        </div>
      </dl>

      <!-- Token mix: same entity colours as the token trend chart. -->
      <div class="dash-mix">
        <div class="dash-mix-head">
          <span>{{ t('dashboard.tokenMix') }}</span>
          <span v-if="tokenMix.total === 0" class="dash-mix-empty">{{ t('dashboard.noTokensToday') }}</span>
        </div>
        <div class="dash-mix-bar" role="img" :aria-label="tokenMixLabel">
          <span
            v-for="part in tokenMix.parts.filter((item) => item.value > 0)"
            :key="part.key"
            class="dash-mix-segment"
            :style="{ width: part.percent + '%', background: part.color }"
            :title="`${part.label} ${formatTokens(part.value)}`"
          ></span>
        </div>
        <ul class="dash-mix-legend">
          <li v-for="part in tokenMix.parts" :key="part.key" :class="{ 'is-zero': part.value === 0 }">
            <span class="dash-mix-swatch" :style="{ background: part.color }" aria-hidden="true"></span>
            <span class="dash-mix-name">{{ part.label }}</span>
            <span class="dash-mix-value">{{ formatTokens(part.value) }}</span>
          </li>
        </ul>
      </div>
    </div>
  </section>

  <!-- Per-platform spend with any quota windows the admin configured. -->
  <section v-if="!isSimple && platformCards.length > 0" class="dash-panel">
    <header class="dash-panel-head">
      <h2 class="dash-section-title">{{ t('dashboard.platformBreakdown') }}</h2>
      <span class="dash-panel-meta">{{ t('dashboard.platformCount', { count: sortedPlatforms.length }) }}</span>
    </header>
    <ul class="dash-platforms">
      <li v-for="item in platformCards" :key="item.platform" class="dash-platform">
        <div class="dash-platform-row">
          <span class="dash-platform-icon" :style="{ '--chip-accent': platformAccentColor(item.platform) }">
            <Icon v-if="item.isOther" name="cube" size="sm" />
            <PlatformIcon v-else :platform="item.platform as GroupPlatform" size="sm" />
          </span>
          <div class="dash-platform-name">
            <span class="dash-platform-title">{{ item.isOther ? t('dashboard.platformOther') : platformLabel(item.platform) }}</span>
            <span v-if="!item.isOther" class="dash-platform-meta">
              <span>{{ t('dashboard.requests') }} {{ formatTotal(item.total_requests, formatNumber) }}</span>
              <span>{{ t('dashboard.tokens') }} {{ formatTotal(item.total_tokens, formatTokens) }}</span>
            </span>
          </div>
          <div class="dash-platform-share" :title="t('dashboard.shareOfSpend', { percent: sharePercent(item) })">
            <span class="dash-share-track">
              <span class="dash-share-fill" :style="{ width: sharePercent(item) + '%' }"></span>
            </span>
            <span class="dash-share-percent">{{ sharePercent(item) }}%</span>
          </div>
          <div class="dash-platform-cost">
            <span class="dash-platform-total" :title="t('dashboard.actual')">
              {{ formatTotal(item.total_actual_cost, (value) => '$' + formatCost(value)) }}
            </span>
            <span class="dash-platform-today">{{ t('dashboard.todayCost') }} ${{ formatCost(item.today_actual_cost) }}</span>
          </div>
        </div>

        <!-- Quota windows: only when a limit is configured for this platform. -->
        <div v-if="hasAnyLimit(item.quota) && !item.isOther" class="dash-quota">
          <template v-for="w in (['daily', 'weekly', 'monthly'] as const)" :key="w">
            <div v-if="quotaVal(item.quota, `${w}_limit_usd`) != null" class="dash-quota-window">
              <template v-if="(quotaVal(item.quota, `${w}_limit_usd`) as number) === 0">
                <div class="dash-quota-head">
                  <span>{{ t(`dashboard.platformQuota.${w}`) }}</span>
                  <span class="text-red-600">{{ t('dashboard.platformQuota.disabled') }}</span>
                </div>
                <div class="dash-meter"><div class="dash-meter-fill w-full bg-red-500" /></div>
              </template>
              <template v-else>
                <div class="dash-quota-head">
                  <span>{{ t(`dashboard.platformQuota.${w}`) }}</span>
                  <span class="tabular-nums">
                    ${{ formatUsd((quotaVal(item.quota, `${w}_usage_usd`) as number) ?? 0) }} / ${{ formatUsd(quotaVal(item.quota, `${w}_limit_usd`) as number) }}
                  </span>
                </div>
                <div class="dash-meter">
                  <div
                    class="dash-meter-fill"
                    :class="quotaBarClass(calcPercent((quotaVal(item.quota, `${w}_usage_usd`) as number) ?? 0, quotaVal(item.quota, `${w}_limit_usd`) as number))"
                    :style="{ width: calcPercent((quotaVal(item.quota, `${w}_usage_usd`) as number) ?? 0, quotaVal(item.quota, `${w}_limit_usd`) as number) + '%' }"
                  />
                </div>
                <p v-if="quotaVal(item.quota, `${w}_window_resets_at`)" class="dash-quota-reset">
                  {{ t('dashboard.platformQuota.resetsAt', { time: formatResetTime(quotaVal(item.quota, `${w}_window_resets_at`) as string) }) }}
                </p>
              </template>
            </div>
          </template>
        </div>
      </li>
    </ul>
  </section>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import PlatformIcon from '@/components/common/PlatformIcon.vue'
import type { UserDashboardStats as UserStatsType } from '@/api/usage'
import type { GroupPlatform, PlatformQuotaItem } from '@/types'
import { platformAccentColor } from '@/utils/platformColors'

interface FusedPlatformCard {
  platform: string
  total_actual_cost: number
  today_actual_cost: number
  total_requests: number
  total_tokens: number
  isOther?: boolean
  quota?: PlatformQuotaItem
}

const props = defineProps<{
  stats: UserStatsType
  balance: number
  isSimple: boolean
  platformQuotas?: PlatformQuotaItem[] | null
}>()
const { t } = useI18n()
const formatTotal = (value: number | undefined, formatter: (value: number) => string) =>
  props.stats.totals_pending ? '-' : formatter(value ?? 0)

// Same thresholds as the header and profile balance, so one balance never wears two tones.
const balanceToneClass = computed(() => {
  const value = props.balance
  if (!Number.isFinite(value) || value < 0) return 'balance-tone-negative'
  if (value <= 10) return 'balance-tone-low'
  if (value > 100) return 'balance-tone-premium'
  return 'balance-tone-default'
})

// Entity colours match TokenUsageTrend so input/output/cache read the same across the page.
const TOKEN_MIX_COLORS = {
  input: '#3b82f6',
  output: '#10b981',
  cacheWrite: '#f59e0b',
  cacheRead: '#06b6d4'
} as const

const tokenMix = computed(() => {
  const parts = [
    { key: 'input', label: t('dashboard.input'), value: props.stats.today_input_tokens || 0, color: TOKEN_MIX_COLORS.input },
    { key: 'output', label: t('dashboard.output'), value: props.stats.today_output_tokens || 0, color: TOKEN_MIX_COLORS.output },
    { key: 'cacheWrite', label: t('dashboard.cacheWrite'), value: props.stats.today_cache_creation_tokens || 0, color: TOKEN_MIX_COLORS.cacheWrite },
    { key: 'cacheRead', label: t('dashboard.cacheRead'), value: props.stats.today_cache_read_tokens || 0, color: TOKEN_MIX_COLORS.cacheRead }
  ]
  const total = parts.reduce((sum, part) => sum + part.value, 0)
  return {
    total,
    parts: parts.map((part) => ({ ...part, percent: total > 0 ? (part.value / total) * 100 : 0 }))
  }
})

const tokenMixLabel = computed(() =>
  tokenMix.value.parts.map((part) => `${part.label} ${formatTokens(part.value)}`).join(', ')
)

const PLATFORM_LABELS: Record<string, string> = {
  anthropic: 'Claude',
  openai: 'OpenAI',
  gemini: 'Gemini',
  antigravity: 'Antigravity',
  grok: 'Grok',
  custom: 'Custom'
}

const platformLabel = (p: string) => PLATFORM_LABELS[p] ?? p

const sortedPlatforms = computed(() => {
  const list = props.stats?.by_platform ?? []
  return [...list].sort((a, b) => b.total_actual_cost - a.total_actual_cost)
})

// 处理"各平台之和 < 总值"的差值：后端按平台聚合时过滤了无法归属平台的行
// （group 与 account 都缺 platform）。这里把差值作为"其他"卡片显式展示，
// 避免今日/累计总值与平台拆分加总对不上、用户困惑。
const OTHER_THRESHOLD = 0.0001
const platformCards = computed<FusedPlatformCard[]>(() => {
  // 建立 by_platform Map
  const byPlat = new Map<string, (typeof sortedPlatforms.value)[number]>()
  for (const item of props.stats?.by_platform ?? []) byPlat.set(item.platform, item)

  // 建立 quota Map
  const byQuota = new Map<string, PlatformQuotaItem>()
  for (const q of props.platformQuotas ?? []) byQuota.set(q.platform, q)

  // union 平台集合。后端 by_platform / quota 接口均不会返回 platform='__other__'，
  // 无需显式排除；__other__ 由下方差值补差逻辑单独追加。
  const platforms = new Set<string>([...byPlat.keys(), ...byQuota.keys()])

  const PLATFORM_ORDER = ['anthropic', 'openai', 'gemini', 'antigravity', 'grok', 'custom']
  const cards: FusedPlatformCard[] = []

  for (const p of platforms) {
    const stat = byPlat.get(p)
    cards.push({
      platform: p,
      total_actual_cost: stat?.total_actual_cost ?? 0,
      today_actual_cost: stat?.today_actual_cost ?? 0,
      total_requests: stat?.total_requests ?? 0,
      total_tokens: stat?.total_tokens ?? 0,
      quota: byQuota.get(p),
    })
  }

  // 排序：按 PLATFORM_ORDER，未知平台按名称排序
  cards.sort((a, b) => {
    const ai = PLATFORM_ORDER.indexOf(a.platform)
    const bi = PLATFORM_ORDER.indexOf(b.platform)
    if (ai === -1 && bi === -1) return a.platform.localeCompare(b.platform)
    if (ai === -1) return 1
    if (bi === -1) return -1
    return ai - bi
  })

  // __other__ 补差逻辑：只对 by_platform 有 usage 数据的总和计算
  const total = props.stats?.total_actual_cost ?? 0
  const today = props.stats?.today_actual_cost ?? 0
  const sumTotal = cards.reduce((s, c) => s + c.total_actual_cost, 0)
  const sumToday = cards.reduce((s, c) => s + c.today_actual_cost, 0)
  const diffTotal = Math.max(0, total - sumTotal)
  const diffToday = Math.max(0, today - sumToday)

  if (diffTotal > OTHER_THRESHOLD || diffToday > OTHER_THRESHOLD) {
    cards.push({
      platform: '__other__',
      total_actual_cost: diffTotal,
      today_actual_cost: diffToday,
      total_requests: 0,
      total_tokens: 0,
      isOther: true,
    })
  }

  return cards
})

const platformSpendTotal = computed(() =>
  platformCards.value.reduce((sum, card) => sum + Math.max(card.total_actual_cost, 0), 0)
)

function sharePercent(card: FusedPlatformCard): number {
  if (props.stats.totals_pending || platformSpendTotal.value <= 0) return 0
  return Math.round((Math.max(card.total_actual_cost, 0) / platformSpendTotal.value) * 100)
}

// Quota helpers

type QuotaWindow = 'daily' | 'weekly' | 'monthly'
type QuotaField = `${QuotaWindow}_limit_usd` | `${QuotaWindow}_usage_usd` | `${QuotaWindow}_window_resets_at`

function quotaVal(q: PlatformQuotaItem | undefined, key: QuotaField): PlatformQuotaItem[QuotaField] {
  return q?.[key]
}

function hasAnyLimit(q: PlatformQuotaItem | undefined): boolean {
  if (!q) return false
  return q.daily_limit_usd != null || q.weekly_limit_usd != null || q.monthly_limit_usd != null
}

function calcPercent(usage: number, limit: number): number {
  if (!limit || limit <= 0) return 0
  return Math.min(100, Math.max(0, Math.round((usage / limit) * 100)))
}

function quotaBarClass(p: number): string {
  if (p >= 95) return 'bg-red-500'
  if (p >= 75) return 'bg-amber-500'
  return 'bg-accent-500'
}

// 与 formatBalance 一致使用 Intl.NumberFormat 做半偶舍入，避免 toFixed 在不同 JS 引擎
// 下偶发截断而非四舍五入（与后端展示精度不一致）。
const usdFormatter = new Intl.NumberFormat('en-US', {
  minimumFractionDigits: 2,
  maximumFractionDigits: 2,
})
function formatUsd(n: number): string {
  if (!Number.isFinite(n)) return '0.00'
  return usdFormatter.format(n)
}

function formatResetTime(iso: string | null | undefined): string {
  if (!iso) return ''
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return iso
  return d.toLocaleString(undefined, {
    month: 'numeric',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
    hour12: false,
  })
}

const formatBalance = (b: number) =>
  new Intl.NumberFormat('en-US', {
    minimumFractionDigits: 2,
    maximumFractionDigits: 2
  }).format(b)

const formatNumber = (n: number) => n.toLocaleString()
const formatCost = (c: number) => c.toFixed(4)
const formatTokens = (t: number) => {
  if (t >= 1_000_000) return `${(t / 1_000_000).toFixed(1)}M`
  if (t >= 1000) return `${(t / 1000).toFixed(1)}K`
  return t.toString()
}
const formatDuration = (ms: number) => ms >= 1000 ? `${(ms / 1000).toFixed(2)}s` : `${ms.toFixed(0)}ms`
</script>

<style scoped>
.dash-hero {
  display: grid;
  gap: 1.25rem;
  padding: 1.25rem;
  border-radius: 1.75rem;
  background: var(--md-sys-color-surface);
  box-shadow: var(--theme-highlight), var(--theme-elevation-1);
}

.dash-balance {
  position: relative;
  display: flex;
  flex-direction: column;
  justify-content: center;
  gap: 0.375rem;
  overflow: hidden;
  padding: 1.25rem 1.375rem;
  border-radius: 1.25rem;
  background: #fff7d6;
}

.dash-balance-label {
  font-size: 0.875rem;
  font-weight: 600;
  color: #6b6045;
}

/* The one hero figure on this page. */
.dash-balance-figure {
  font-size: clamp(2.5rem, 6vw, 3.5rem);
  font-weight: 700;
  line-height: 1.05;
  letter-spacing: -0.02em;
  color: #3f3420;
  overflow-wrap: anywhere;
}

.dash-balance-caption {
  font-size: 0.8125rem;
  color: #6b6045;
}

.dash-today {
  display: flex;
  min-width: 0;
  flex-direction: column;
  gap: 1rem;
}

.dash-today-head {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 0.5rem;
}

.dash-section-title {
  font-size: 0.9375rem;
  font-weight: 700;
  color: var(--md-sys-color-on-surface);
}

.dash-live {
  display: inline-flex;
  align-items: center;
  gap: 0.375rem;
  padding: 0.1875rem 0.625rem;
  border-radius: 999px;
  background: #faf6e8;
  font-size: 0.75rem;
  color: var(--md-sys-color-on-surface-variant);
}

.dash-live-dot {
  width: 0.4375rem;
  height: 0.4375rem;
  border-radius: 999px;
  background: #9b8c62;
}

.dash-live-dot.is-active {
  background: #5c9865;
  box-shadow: 0 0 0 3px rgb(92 152 101 / 0.22);
}

.dash-kpis {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 0.625rem;
}

.dash-kpi {
  display: flex;
  min-width: 0;
  flex-direction: column;
  gap: 0.25rem;
  padding: 0.875rem 1rem;
  border-radius: 1rem;
  background: #fffbe8;
}

.dash-kpi dt {
  font-size: 0.75rem;
  font-weight: 500;
  color: var(--md-sys-color-on-surface-variant);
}

.dash-kpi-value {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 1.375rem;
  font-weight: 700;
  line-height: 1.75rem;
  color: var(--md-sys-color-on-surface);
}

.dash-kpi-sub {
  display: flex;
  flex-wrap: wrap;
  gap: 0 0.5rem;
  font-size: 0.75rem;
  color: var(--md-sys-color-on-surface-variant);
}

.dash-kpi-list-price {
  text-decoration: line-through;
  opacity: 0.75;
}

.dash-mix {
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
}

.dash-mix-head {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: 0.5rem;
  font-size: 0.75rem;
  font-weight: 600;
  color: var(--md-sys-color-on-surface-variant);
}

.dash-mix-empty {
  font-weight: 400;
}

/* Stacked bar: 2px surface gaps between segments instead of strokes. */
.dash-mix-bar {
  display: flex;
  gap: 2px;
  height: 0.625rem;
  overflow: hidden;
  border-radius: 999px;
  background: #eee5c9;
}

.dash-mix-segment {
  min-width: 2px;
  height: 100%;
}

.dash-mix-legend {
  display: flex;
  flex-wrap: wrap;
  gap: 0.375rem 1rem;
  font-size: 0.75rem;
}

.dash-mix-legend li {
  display: inline-flex;
  align-items: center;
  gap: 0.375rem;
  color: var(--md-sys-color-on-surface-variant);
}

.dash-mix-legend li.is-zero {
  opacity: 0.55;
}

.dash-mix-swatch {
  width: 0.5rem;
  height: 0.5rem;
  border-radius: 0.125rem;
}

.dash-mix-value {
  font-weight: 650;
  font-variant-numeric: tabular-nums;
  color: var(--md-sys-color-on-surface);
}

/* ---------- Platform breakdown ---------- */
.dash-panel {
  padding: 1.25rem;
  border-radius: 1.5rem;
  background: var(--md-sys-color-surface);
  box-shadow: var(--theme-highlight), var(--theme-elevation-1);
}

.dash-panel-head {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: 0.5rem;
  margin-bottom: 0.75rem;
}

.dash-panel-meta {
  font-size: 0.75rem;
  color: var(--md-sys-color-on-surface-variant);
}

.dash-platforms {
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
}

.dash-platform {
  padding: 0.75rem 0.875rem;
  border-radius: 1rem;
  background: #fffbe8;
}

.dash-platform-row {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr) auto;
  align-items: center;
  gap: 0.75rem;
}

.dash-platform-icon {
  display: inline-flex;
  width: 2.25rem;
  height: 2.25rem;
  align-items: center;
  justify-content: center;
  border-radius: 0.75rem;
  color: color-mix(in srgb, var(--chip-accent) 70%, #3f3420);
  background: color-mix(in srgb, var(--chip-accent) 14%, var(--md-sys-color-surface));
}

.dash-platform-name {
  display: flex;
  min-width: 0;
  flex-direction: column;
  gap: 0.125rem;
}

.dash-platform-title {
  font-size: 0.875rem;
  font-weight: 650;
  color: var(--md-sys-color-on-surface);
}

.dash-platform-meta {
  display: flex;
  flex-wrap: wrap;
  gap: 0 0.75rem;
  font-size: 0.75rem;
  color: var(--md-sys-color-on-surface-variant);
}

.dash-platform-share {
  display: none;
  align-items: center;
  gap: 0.5rem;
}

.dash-share-track {
  display: block;
  width: 9rem;
  height: 0.375rem;
  overflow: hidden;
  border-radius: 999px;
  background: #e4f0df;
}

.dash-share-fill {
  display: block;
  height: 100%;
  border-radius: inherit;
  background: #5c9865;
}

.dash-share-percent {
  width: 2.5rem;
  text-align: right;
  font-size: 0.75rem;
  font-variant-numeric: tabular-nums;
  color: var(--md-sys-color-on-surface-variant);
}

.dash-platform-cost {
  display: flex;
  flex-direction: column;
  align-items: flex-end;
  gap: 0.125rem;
}

.dash-platform-total {
  font-size: 0.9375rem;
  font-weight: 700;
  font-variant-numeric: tabular-nums;
  color: var(--md-sys-color-on-surface);
}

.dash-platform-today {
  font-size: 0.75rem;
  font-variant-numeric: tabular-nums;
  color: var(--md-sys-color-on-surface-variant);
}

.dash-quota {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(10rem, 1fr));
  gap: 0.5rem 1rem;
  margin-top: 0.75rem;
  padding-top: 0.75rem;
  box-shadow: inset 0 1px 0 rgb(215 193 106 / 0.3);
}

.dash-quota-window {
  display: flex;
  flex-direction: column;
  gap: 0.25rem;
}

.dash-quota-head {
  display: flex;
  justify-content: space-between;
  gap: 0.5rem;
  font-size: 0.75rem;
  color: var(--md-sys-color-on-surface-variant);
}

.dash-quota-reset {
  font-size: 0.6875rem;
  color: var(--md-sys-color-on-surface-variant);
  opacity: 0.8;
}

.dash-meter {
  height: 0.375rem;
  overflow: hidden;
  border-radius: 999px;
  background: #eee5c9;
}

.dash-meter-fill {
  height: 100%;
  border-radius: inherit;
  transition: width 300ms ease;
}

@media (min-width: 640px) {
  .dash-platform-row {
    grid-template-columns: auto minmax(0, 1fr) auto auto;
  }

  .dash-platform-share {
    display: inline-flex;
  }
}

@media (min-width: 768px) {
  .dash-hero {
    padding: 1.5rem;
  }

  .dash-kpis {
    grid-template-columns: repeat(4, minmax(0, 1fr));
  }
}

@media (min-width: 1024px) {
  .dash-hero {
    grid-template-columns: minmax(16rem, 0.38fr) minmax(0, 1fr);
    gap: 1.5rem;
  }

  .dash-hero.dash-hero-simple {
    grid-template-columns: minmax(0, 1fr);
  }

  .dash-kpis {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}

@media (min-width: 1280px) {
  .dash-kpis {
    grid-template-columns: repeat(4, minmax(0, 1fr));
  }
}
</style>
