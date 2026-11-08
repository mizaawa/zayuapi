<template>
  <!-- The card variant fills an account card stat tile: the value inherits the
       tile's type, the probe button sits at the tile's right edge and a
       short status line goes below instead of beside the value. -->
  <div
    v-if="eligible"
    :class="isCard
      ? 'grid w-full min-w-0 grid-cols-[minmax(0,1fr)_auto] items-center gap-x-1'
      : 'flex h-6 min-w-[7rem] items-center gap-1'"
    :data-variant="variant"
  >
    <HelpTooltip
      :class="isCard ? '!ml-0 min-w-0 justify-self-start' : '-ml-1'"
      width-class="w-max max-w-[calc(100vw-2rem)]"
      data-testid="upstream-billing-details"
    >
      <template #trigger>
        <span
          v-if="isCard"
          class="block max-w-full cursor-help truncate border-b border-dotted border-[#d7c16a]"
          :class="hasEffectiveRate ? 'tabular-nums' : ['text-xs font-semibold', statusClass || 'text-[#8a7d5b]']"
          data-testid="upstream-billing-rate"
        >
          {{ cardPrimaryValue }}
        </span>
        <span
          v-else
          class="cursor-help border-b border-dotted border-gray-300 text-sm font-medium dark:border-dark-600"
          :class="hasEffectiveRate ? 'font-mono text-gray-800 dark:text-gray-200' : statusClass || 'text-gray-400 dark:text-gray-500'"
          data-testid="upstream-billing-rate"
        >
          {{ primaryValue }}
        </span>
      </template>
      <div class="space-y-1">
        <template v-if="stale && lastDetectedRate != null">
          <p data-testid="upstream-billing-last-rate">
            {{ t('admin.accounts.upstreamBilling.lastDetectedRate', { value: lastDetectedRate }) }}
          </p>
          <p data-testid="upstream-billing-last-time">
            {{ t('admin.accounts.upstreamBilling.lastDetectedAt', { value: formatDate(snapshot?.received_at || snapshot?.observed_multiplier_at) }) }}
          </p>
          <p data-testid="upstream-billing-elapsed">
            {{ t('admin.accounts.upstreamBilling.elapsedSince', { value: elapsedSinceLastSuccess }) }}
          </p>
        </template>
        <template v-else-if="logData">
          <p>{{ t('admin.accounts.upstreamBilling.newapiGroup', { group: logData.group || '-' }) }}</p>
          <p>{{ t('admin.accounts.upstreamBilling.newapiConsensus', { count: snapshot?.multiplier_match_count ?? 0, required: logData.consensus_required }) }}</p>
          <p v-if="logCorroboration" data-testid="upstream-billing-corroborated">
            {{ t('admin.accounts.upstreamBilling.newapiCorroborated', { source: logCorroboration }) }}
          </p>
          <p>{{ t('admin.accounts.upstreamBilling.lastDetectedRate', { value: logData.resolved_rate_multiplier }) }}</p>
          <p>{{ t('admin.accounts.upstreamBilling.lastDetectedAt', { value: formatDate(snapshot?.observed_multiplier_at || snapshot?.received_at) }) }}</p>
        </template>
        <template v-else-if="usageData">
          <p>{{ t('admin.accounts.upstreamBilling.newapiGroup', { group: usageData.group }) }}</p>
          <p>{{ t('admin.accounts.upstreamBilling.newapiUsageRate', { value: usageData.resolved_rate_multiplier }) }}</p>
          <p>{{ t('admin.accounts.upstreamBilling.lastDetectedAt', { value: formatDate(snapshot?.observed_multiplier_at || snapshot?.received_at) }) }}</p>
        </template>
        <template v-else-if="catalogData">
          <p data-testid="upstream-billing-catalog-group">
            {{
              catalogData.source === 'model_inference'
                ? t('admin.accounts.upstreamBilling.newapiInferredGroup', { group: catalogData.group })
                : t('admin.accounts.upstreamBilling.newapiGroup', { group: catalogData.group })
            }}
          </p>
          <p data-testid="upstream-billing-catalog-basis">
            {{
              catalogData.source === 'model_inference'
                ? t('admin.accounts.upstreamBilling.newapiModelInferenceBasis', { count: catalogData.model_count ?? '-' })
                : t('admin.accounts.upstreamBilling.newapiGroupPricingBasis')
            }}
          </p>
          <p>{{ t('admin.accounts.upstreamBilling.newapiPricingRate', { value: catalogData.resolved_rate_multiplier }) }}</p>
          <p>{{ t('admin.accounts.upstreamBilling.lastDetectedAt', { value: formatDate(snapshot?.observed_multiplier_at || snapshot?.received_at) }) }}</p>
        </template>
        <template v-else-if="hasEffectiveRate && billingData">
          <p>{{ t('admin.accounts.upstreamBilling.groupRate', { value: billingData.group_rate_multiplier }) }}</p>
          <p v-if="billingData.user_rate_multiplier != null">
            {{ t('admin.accounts.upstreamBilling.userRate', { value: billingData.user_rate_multiplier }) }}
          </p>
          <p>
            {{
              billingData.peak_rate_enabled
                ? t('admin.accounts.upstreamBilling.peakRate', {
                    start: billingData.peak_start,
                    end: billingData.peak_end,
                    value: billingData.peak_rate_multiplier,
                    timezone: billingData.timezone
                  })
                : t('admin.accounts.upstreamBilling.noPeakRate')
            }}
          </p>
          <p>{{ t('admin.accounts.upstreamBilling.effectiveRate', { value: currentEffectiveRate ?? '-' }) }}</p>
          <p>{{ t('admin.accounts.upstreamBilling.updatedAt', { value: formatDate(snapshot?.received_at) }) }}</p>
        </template>
        <template v-else-if="observedRate != null">
          <p>{{ t('admin.accounts.upstreamBilling.lastDetectedRate', { value: observedRate }) }}</p>
          <p>{{ t('admin.accounts.upstreamBilling.lastDetectedAt', { value: formatDate(snapshot?.observed_multiplier_at || snapshot?.received_at) }) }}</p>
        </template>
        <p v-else>{{ statusLabel || '-' }}<span v-if="snapshot?.status === 'waiting_logs'"> · {{ t('admin.accounts.upstreamBilling.waitingLogs') }}</span></p>
        <p v-if="candidateGroups" data-testid="upstream-billing-candidates">
          {{ t('admin.accounts.upstreamBilling.newapiCandidates', { groups: candidateGroups }) }}
        </p>
        <p v-if="probeReason" class="text-amber-300" data-testid="upstream-billing-reason">{{ probeReason }}</p>
        <p v-if="upstreamVersion" data-testid="upstream-billing-version">
          {{ t('admin.accounts.upstreamBilling.upstreamVersion', { version: upstreamVersion }) }}
        </p>
        <p
          v-if="probeEnabled && globalProbeEnabled !== false && nextProbeAt"
          data-testid="upstream-billing-next-probe"
        >
          {{ t('admin.accounts.upstreamBilling.nextProbeAt', { value: formatDate(nextProbeAt) }) }}
        </p>
        <p class="mt-2 border-t border-white/15 pt-2" data-testid="upstream-billing-probe-state">
          {{ t('admin.accounts.upstreamBilling.accountProbeState') }}
          <span :class="probeEnabled ? 'text-emerald-400' : 'text-red-400'">
            {{ probeEnabled ? t('admin.accounts.upstreamBilling.enabled') : t('admin.accounts.upstreamBilling.disabled') }}
          </span>
        </p>
        <p
          v-if="globalProbeEnabled === false"
          class="mt-1"
          data-testid="upstream-billing-global-probe-state"
        >
          {{ t('admin.accounts.upstreamBilling.globalProbeState') }}
          <span class="text-red-400">{{ t('admin.accounts.upstreamBilling.disabled') }}</span>
        </p>
      </div>
    </HelpTooltip>
    <span
      v-if="hasEffectiveRate && (statusLabel || unsyncedDeclaration)"
      :class="[
        statusClass || (isCard ? 'text-[#a96700]' : ''),
        isCard ? 'col-span-2 row-start-2 truncate text-[0.6875rem] leading-4' : 'whitespace-nowrap text-[10px]'
      ]"
      class="font-medium"
      data-testid="upstream-billing-status"
    >
      {{ isCard ? cardStatusLabel || t('admin.accounts.upstreamBilling.short.notSynced') : statusLabel || t('admin.accounts.upstreamBilling.notSynced') }}
    </span>
    <button
      type="button"
      class="inline-flex h-6 w-6 flex-shrink-0 items-center justify-center rounded transition-colors disabled:cursor-not-allowed disabled:opacity-50"
      :class="isCard
        ? 'col-start-2 row-start-1 rounded-lg text-[#8a7d5b] hover:bg-[#fff3bd] hover:text-[#7e5600]'
        : 'text-blue-600 hover:bg-blue-50 dark:text-blue-400 dark:hover:bg-blue-900/30'"
      :disabled="probing"
      :aria-label="t('admin.accounts.upstreamBilling.manualProbe')"
      :title="t('admin.accounts.upstreamBilling.manualProbe')"
      data-testid="upstream-billing-probe"
      @click="$emit('probe')"
    >
      <Icon name="refresh" size="xs" :class="{ 'animate-spin': probing }" />
    </button>
  </div>
  <span v-else class="text-sm text-gray-400 dark:text-dark-500">-</span>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import HelpTooltip from '@/components/common/HelpTooltip.vue'
import Icon from '@/components/icons/Icon.vue'
import { formatMultiplier } from '@/utils/formatters'
import type {
  Account,
  UpstreamBillingCatalogData,
  UpstreamBillingData,
  UpstreamBillingLogData,
  UpstreamBillingProbeSnapshot,
  UpstreamBillingUsageData
} from '@/types'

const props = withDefaults(defineProps<{
  account: Account
  now: number
  probing?: boolean
  globalProbeEnabled?: boolean
  /** "card" fits an account card stat tile; "table" is the list column. */
  variant?: 'table' | 'card'
}>(), {
  globalProbeEnabled: true,
  variant: 'table'
})

defineEmits<{
  (event: 'probe'): void
}>()

const { t } = useI18n()
const CLOCK_SKEW_TOLERANCE_MS = 5 * 60 * 1000
// 探测资格已放宽到全部 API-key 平台（上游是 sub2api 即可应答）。
const eligible = computed(() => props.account.type === 'apikey')
const snapshot = computed<UpstreamBillingProbeSnapshot | undefined>(() => props.account.extra?.upstream_billing_probe)
const data = computed(() => snapshot.value?.data)
type ProbeData = NonNullable<UpstreamBillingProbeSnapshot['data']>
const isConsumptionLogData = (value: ProbeData): value is UpstreamBillingLogData =>
  'source' in value && value.source === 'consumption_log'
const isTokenUsageData = (value: ProbeData): value is UpstreamBillingUsageData =>
  'source' in value && value.source === 'token_usage'
// group_pricing / model_inference carry a rate only when the group resolved;
// an ambiguous model match carries candidates instead.
const isCatalogData = (value: ProbeData): value is UpstreamBillingCatalogData =>
  'source' in value && (value.source === 'group_pricing' || value.source === 'model_inference') && 'resolved_rate_multiplier' in value
const logData = computed<UpstreamBillingLogData | null>(() => {
  const value = data.value
  return value && isConsumptionLogData(value) ? value : null
})
const usageData = computed<UpstreamBillingUsageData | null>(() => {
  const value = data.value
  return value && isTokenUsageData(value) ? value : null
})
const catalogData = computed<UpstreamBillingCatalogData | null>(() => {
  const value = data.value
  return value && isCatalogData(value) ? value : null
})
const catalogSourceLabel = (source: unknown) =>
  source === 'group_pricing' || source === 'model_inference'
    ? t(`admin.accounts.upstreamBilling.catalogSource.${source}`)
    : ''
const logCorroboration = computed(() => catalogSourceLabel(logData.value?.corroborated_by))
const candidateGroups = computed(() => {
  const value = data.value
  if (!value || !('candidates' in value) || !Array.isArray(value.candidates)) return ''
  return value.candidates
    .filter(candidate => typeof candidate?.group === 'string' && typeof candidate.rate_multiplier === 'number' && Number.isFinite(candidate.rate_multiplier))
    .map(candidate => `${candidate.group} (${formatMultiplier(candidate.rate_multiplier)}x)`)
    .join(', ')
})
const upstreamVersion = computed(() => {
  const value = snapshot.value?.upstream_version
  return snapshot.value?.protocol === 'newapi' && typeof value === 'string' ? value.trim() : ''
})
// Reasons the backend records in last_error. The two waiting reasons are
// already told by the status line.
const PROBE_REASONS = new Set([
  'http_error',
  'cloudflare_blocked',
  'request_failed',
  'request_build_failed',
  'empty_response',
  'response_too_large',
  'response_read_failed',
  'invalid_response',
  'unsupported',
  'unsupported_logs_endpoint',
  'logs_rejected',
  'consumption_logs_truncated',
  'invalid_base_url',
  'missing_api_key',
  'proxy_unavailable',
  'transport_unavailable'
])
const probeReason = computed(() => {
  const code = snapshot.value?.last_error
  if (!code || code === 'waiting_for_consumption_logs' || code === 'waiting_for_multiplier_consensus') return ''
  if (!PROBE_REASONS.has(code)) return t('admin.accounts.upstreamBilling.reasons.other', { code })
  return t(`admin.accounts.upstreamBilling.reasons.${code}`, { status: snapshot.value?.http_status || '-' })
})
const billingData = computed<UpstreamBillingData | null>(() => {
  const value = data.value
  return value && 'billing_scope' in value ? value : null
})
const observedRate = computed(() => {
  if (snapshot.value?.protocol !== 'newapi') return null
  const value = snapshot.value.observed_multiplier
  return typeof value === 'number' && Number.isFinite(value) && value >= 0 ? value : null
})
const probeEnabled = computed(() => props.account.extra?.upstream_billing_probe_enabled === true)
const nextProbeAt = computed(() => {
  const value = snapshot.value?.next_probe_at
  return typeof value === 'string' && Number.isFinite(Date.parse(value)) ? value : ''
})
const receivedAt = computed(() => {
  const value = snapshot.value?.received_at || (snapshot.value?.protocol === 'newapi' ? snapshot.value.observed_multiplier_at : undefined)
  return typeof value === 'string' ? Date.parse(value) : Number.NaN
})
const freshUntil = computed(() => {
  if (typeof snapshot.value?.fresh_until === 'string') return Date.parse(snapshot.value.fresh_until)
  if (snapshot.value?.status !== 'ok' || typeof snapshot.value.next_probe_at !== 'string') return Number.NaN
  const nextProbeAt = Date.parse(snapshot.value.next_probe_at)
  return Number.isFinite(nextProbeAt) && nextProbeAt > receivedAt.value
    ? receivedAt.value + 2 * (nextProbeAt - receivedAt.value)
    : Number.NaN
})
const validTimestamps = computed(() => {
  if (!Number.isFinite(receivedAt.value) || receivedAt.value > props.now + CLOCK_SKEW_TOLERANCE_MS) return false
  return Number.isFinite(freshUntil.value) && freshUntil.value > receivedAt.value
})
const stale = computed(() => {
  if (!snapshot.value) return false
  if (!Number.isFinite(receivedAt.value)) return snapshot.value.status === 'ok'
  if (!validTimestamps.value) return true
  return props.now > freshUntil.value
})
const parseMinute = (value?: string) => {
  if (typeof value !== 'string') return null
  const match = /^(\d{2}):(\d{2})$/.exec(value)
  if (!match) return null
  const hour = Number(match[1])
  const minute = Number(match[2])
  return hour < 24 && minute < 60 ? hour * 60 + minute : null
}
const minuteInTimeZone = (timestamp: number, timeZone?: string) => {
  if (!timeZone) return null
  try {
    const parts = new Intl.DateTimeFormat('en-GB', {
      timeZone,
      hour: '2-digit',
      minute: '2-digit',
      hourCycle: 'h23'
    }).formatToParts(new Date(timestamp))
    const hour = Number(parts.find(part => part.type === 'hour')?.value)
    const minute = Number(parts.find(part => part.type === 'minute')?.value)
    return Number.isInteger(hour) && Number.isInteger(minute) ? hour * 60 + minute : null
  } catch {
    return null
  }
}
// The New API sources that state a rate: consumption log, usage route, catalog.
const newapiRateData = computed(() => logData.value ?? usageData.value ?? catalogData.value)
const newapiRate = computed(() => {
  const rate = newapiRateData.value?.resolved_rate_multiplier
  return typeof rate === 'number' && Number.isFinite(rate) && rate >= 0 ? rate : null
})
const currentEffectiveRate = computed(() => {
  if (newapiRateData.value) return newapiRate.value
  if (observedRate.value != null) return observedRate.value
  const billing = billingData.value
  if (!billing) return null
  if (billing.billing_scope !== 'token') return null
  const base = billing.resolved_rate_multiplier
  if (typeof base !== 'number' || !Number.isFinite(base) || base < 0) return null
  if (typeof billing.peak_rate_enabled !== 'boolean') return null
  if (!billing.peak_rate_enabled) return base
  const start = parseMinute(billing.peak_start)
  const end = parseMinute(billing.peak_end)
  const minute = minuteInTimeZone(props.now, billing.timezone)
  const peak = billing.peak_rate_multiplier
  if (start == null || end == null || minute == null || start >= end || typeof peak !== 'number' || !Number.isFinite(peak) || peak < 0) return null
  const value = minute >= start && minute < end ? base * peak : base
  return Number.isFinite(value) ? value : null
})
const lastDetectedRate = computed(() => {
  if (newapiRateData.value) return newapiRate.value == null ? null : Number(newapiRate.value.toPrecision(12))
  if (observedRate.value != null) return Number(observedRate.value.toPrecision(12))
  const value = billingData.value?.effective_rate_multiplier
  return typeof value === 'number' && Number.isFinite(value) && value >= 0
    ? Number(value.toPrecision(12))
    : null
})
const elapsedSinceLastSuccess = computed(() => {
  if (!Number.isFinite(receivedAt.value)) return '-'
  const elapsedMinutes = Math.max(0, Math.floor((props.now - receivedAt.value) / 60_000))
  if (elapsedMinutes < 1) return t('admin.accounts.upstreamBilling.justNow')
  if (elapsedMinutes < 60) return t('admin.accounts.upstreamBilling.minutesAgo', { count: elapsedMinutes })
  const elapsedHours = Math.floor(elapsedMinutes / 60)
  if (elapsedHours < 24) return t('admin.accounts.upstreamBilling.hoursAgo', { count: elapsedHours })
  return t('admin.accounts.upstreamBilling.daysAgo', { count: Math.floor(elapsedHours / 24) })
})
const effectiveRate = computed(() => {
  if (!validTimestamps.value || stale.value || !['ok', 'failed', 'waiting_consensus'].includes(snapshot.value?.status ?? '')) return '-'
  const value = currentEffectiveRate.value
  return value == null ? '-' : `${formatMultiplier(value)}x`
})
const statusLabel = computed(() => {
  if (!snapshot.value) return t('admin.accounts.upstreamBilling.notProbed')
  if (snapshot.value.status === 'waiting_logs') return t('admin.accounts.upstreamBilling.noDeclaration')
  if (snapshot.value.status === 'waiting_consensus') return t('admin.accounts.upstreamBilling.waitingConsensus')
  if (snapshot.value.status === 'unsupported') return t('admin.accounts.upstreamBilling.unsupported')
  if (stale.value) return t('admin.accounts.upstreamBilling.stale')
  if (snapshot.value.status === 'failed') return t('admin.accounts.upstreamBilling.failed')
  return ''
})
const statusClass = computed(() => {
  if (!snapshot.value) return 'text-gray-400 dark:text-gray-500'
  if (snapshot.value.status === 'unsupported') return 'text-gray-500 dark:text-gray-400'
  if (stale.value) return 'text-amber-600 dark:text-amber-400'
  if (snapshot.value.status === 'failed') return 'text-red-600 dark:text-red-400'
  return ''
})
const hasEffectiveRate = computed(() => effectiveRate.value !== '-')
const unsyncedDeclaration = computed(() => {
  if (!hasEffectiveRate.value) return false
  if (props.account.extra?.upstream_billing_rate_sync_enabled !== true) return true
  const declared = newapiRateData.value?.resolved_rate_multiplier ?? billingData.value?.resolved_rate_multiplier ?? observedRate.value
  if (typeof declared !== 'number' || !Number.isFinite(declared) || declared <= 0 || declared > 100) return true
  const current = props.account.rate_multiplier
  return typeof current !== 'number' || !Number.isFinite(current) || Math.abs(current - Math.round(declared * 10_000) / 10_000) > 0.00005
})
const primaryValue = computed(() => hasEffectiveRate.value ? effectiveRate.value : statusLabel.value || '-')
const isCard = computed(() => props.variant === 'card')
// A stat tile is a third of a phone card: short words, the full ones stay in the tooltip.
const cardStatusLabel = computed(() => {
  if (!snapshot.value) return t('admin.accounts.upstreamBilling.short.notProbed')
  if (snapshot.value.status === 'waiting_logs') return t('admin.accounts.upstreamBilling.short.noDeclaration')
  if (snapshot.value.status === 'waiting_consensus') return t('admin.accounts.upstreamBilling.short.waitingConsensus')
  if (snapshot.value.status === 'unsupported') return t('admin.accounts.upstreamBilling.short.unsupported')
  if (stale.value) return t('admin.accounts.upstreamBilling.short.stale')
  if (snapshot.value.status === 'failed') return t('admin.accounts.upstreamBilling.short.failed')
  return ''
})
const cardPrimaryValue = computed(() => hasEffectiveRate.value ? effectiveRate.value : cardStatusLabel.value || '-')
const formatDate = (value?: string) => value
  ? new Date(value).toLocaleString(undefined, {
      month: '2-digit',
      day: '2-digit',
      hour: '2-digit',
      minute: '2-digit'
    })
  : '-'
</script>
