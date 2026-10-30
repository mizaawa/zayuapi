<template>
  <AppLayout>
    <div class="cs-page">
      <!-- Overall health first: one line that says whether anything is wrong right now. -->
      <section
        class="cs-summary"
        :class="`is-${summaryTone}`"
        aria-labelledby="channel-status-summary-title"
        data-testid="channel-status-summary"
      >
        <span class="cs-summary-glyph" aria-hidden="true">
          <Icon :name="summaryGlyph" size="md" :stroke-width="2.25" :class="{ 'animate-spin': summaryTone === 'loading' }" />
        </span>
        <div class="cs-summary-text">
          <h2 id="channel-status-summary-title" class="cs-summary-title" aria-live="polite">{{ summaryTitle }}</h2>
          <p v-if="summaryDetail" class="cs-summary-detail">{{ summaryDetail }}</p>
          <p v-if="countdownVisible" class="cs-refresh-note is-inline">{{ refreshNote }}</p>
        </div>

        <ul v-if="statusCounts.length" class="cs-counts" :aria-label="t('channelStatus.counts.label')">
          <li v-for="count in statusCounts" :key="count.key" class="cs-count" :class="`is-${count.key}`">
            <span class="cs-count-dot" aria-hidden="true"></span>
            <span>{{ count.label }}</span>
            <strong>{{ count.value }}</strong>
          </li>
        </ul>

        <div class="cs-refresh">
          <span v-if="countdownVisible" class="cs-refresh-note is-side">{{ refreshNote }}</span>
          <button
            type="button"
            class="cs-refresh-button"
            :disabled="refreshing"
            :aria-label="t('common.refresh')"
            :title="t('common.refresh')"
            @click="manualRefresh"
          >
            <Icon name="refresh" size="sm" :class="{ 'animate-spin': refreshing }" />
          </button>
        </div>

        <p v-if="items.length" class="cs-legend">
          <span class="cs-legend-swatches" aria-hidden="true">
            <span class="cs-legend-swatch is-operational"></span>
            <span class="cs-legend-swatch is-degraded"></span>
            <span class="cs-legend-swatch is-down"></span>
            <span class="cs-legend-swatch is-empty"></span>
          </span>
          <span>{{ t('channelStatus.legend') }}</span>
        </p>
      </section>

      <aside
        v-if="announcement"
        class="cs-notice"
        aria-labelledby="channel-status-notice-title"
        data-testid="channel-status-notice"
      >
        <span class="cs-notice-icon" aria-hidden="true"><Icon name="megaphone" size="md" /></span>
        <div class="min-w-0">
          <h2 id="channel-status-notice-title" class="cs-notice-title">
            {{ t('channelStatus.announcementTitle') }}
          </h2>
          <p class="cs-notice-text">{{ announcement }}</p>
        </div>
      </aside>

      <section
        :aria-label="t('channelStatus.title')"
        data-testid="channel-status-grid"
      >
        <MonitorCardGrid :items="items" :loading="loading" />
      </section>

      <footer class="cs-footer">
        <span>{{ t('channelStatus.poweredBy') }}</span>
        <a
          href="https://mizaawa.com"
          target="_blank"
          rel="noopener noreferrer"
          class="cs-footer-link"
        >
          mizaawa.com
        </a>
      </footer>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { extractApiErrorMessage } from '@/utils/apiError'
import {
  list as listChannelMonitorViews,
  type UserMonitorView,
} from '@/api/channelMonitor'
import AppLayout from '@/components/layout/AppLayout.vue'
import Icon from '@/components/icons/Icon.vue'
import MonitorCardGrid from '@/components/user/monitor/MonitorCardGrid.vue'
import { DEFAULT_INTERVAL_SECONDS } from '@/constants/channelMonitor'
import { useAutoRefresh } from '@/composables/useAutoRefresh'
import { useChannelMonitorFormat } from '@/composables/useChannelMonitorFormat'
import { monitorHealthBucket, type MonitorHealthBucket } from '@/utils/channelMonitorHealth'

type SummaryTone = 'loading' | 'error' | 'empty' | 'unknown' | 'outage' | 'degraded' | 'operational'

const { t } = useI18n()
const appStore = useAppStore()
const { formatRelativeTime } = useChannelMonitorFormat()
const announcement = computed(() =>
  appStore.cachedPublicSettings?.channel_monitor_announcement?.trim() || ''
)

const items = ref<UserMonitorView[]>([])
const loading = ref(false)
const refreshing = ref(false)
const loadedOnce = ref(false)
const loadFailed = ref(false)
let abortController: AbortController | null = null

const autoRefresh = useAutoRefresh({
  storageKey: 'channel-status-auto-refresh',
  intervals: [30, 60, 120] as const,
  defaultInterval: DEFAULT_INTERVAL_SECONDS,
  onRefresh: () => reload(true),
  shouldPause: () => document.hidden || loading.value,
})

const countdownVisible = computed(() =>
  autoRefresh.enabled.value && autoRefresh.countdown.value > 0 && !refreshing.value
)
const refreshNote = computed(() => t('channelStatus.refreshIn', { n: autoRefresh.countdown.value }))

// Same latency rules as the row icons, so the summary never disagrees with the list.
const counts = computed<Record<MonitorHealthBucket, number>>(() => {
  const result: Record<MonitorHealthBucket, number> = { operational: 0, degraded: 0, down: 0, unknown: 0 }
  for (const item of items.value) {
    result[monitorHealthBucket(item.primary_status, item.primary_latency_ms)]++
  }
  return result
})

const statusCounts = computed(() => (['operational', 'degraded', 'down', 'unknown'] as const)
  .filter(key => counts.value[key] > 0)
  .map(key => ({ key, label: t(`channelStatus.counts.${key}`), value: counts.value[key] })))

const summaryTone = computed<SummaryTone>(() => {
  if (!items.value.length) {
    if (loading.value || !loadedOnce.value) return 'loading'
    return loadFailed.value ? 'error' : 'empty'
  }
  if (counts.value.down > 0) return 'outage'
  if (counts.value.degraded > 0) return 'degraded'
  if (counts.value.operational === 0) return 'unknown'
  return 'operational'
})

const summaryTitle = computed(() => (
  summaryTone.value === 'error' ? t('channelStatus.loadError') : t(`channelStatus.summary.${summaryTone.value}`)
))

const summaryGlyph = computed(() => {
  switch (summaryTone.value) {
    case 'operational':
      return 'check' as const
    case 'degraded':
      return 'exclamationTriangle' as const
    case 'outage':
      return 'x' as const
    case 'error':
      return 'exclamationCircle' as const
    case 'loading':
      return 'refresh' as const
    default:
      return 'server' as const
  }
})

const lastCheckedAt = computed(() => {
  let latest = ''
  let latestTime = 0
  for (const item of items.value) {
    const checkedAt = item.timeline?.[0]?.checked_at
    const time = checkedAt ? Date.parse(checkedAt) : NaN
    if (Number.isFinite(time) && time > latestTime) {
      latestTime = time
      latest = checkedAt as string
    }
  }
  return latest
})

const summaryDetail = computed(() => {
  const total = items.value.length
  if (!total) return ''
  return lastCheckedAt.value
    ? t('channelStatus.summary.detail', { total, time: formatRelativeTime(lastCheckedAt.value) }, total)
    : t('channelStatus.summary.detailNoCheck', { total }, total)
})

async function reload(silent = false) {
  abortController?.abort()
  const ctrl = new AbortController()
  abortController = ctrl
  if (!silent) loading.value = true

  try {
    const response = await listChannelMonitorViews({ signal: ctrl.signal })
    if (ctrl.signal.aborted || abortController !== ctrl) return
    items.value = response.items || []
    loadFailed.value = false
  } catch (err: unknown) {
    const requestError = err as { name?: string; code?: string }
    if (requestError?.name === 'AbortError' || requestError?.code === 'ERR_CANCELED') return
    if (abortController === ctrl) loadFailed.value = true
    appStore.showError(extractApiErrorMessage(err, t('channelStatus.loadError')))
  } finally {
    if (abortController === ctrl) {
      if (!silent) loading.value = false
      loadedOnce.value = true
      autoRefresh.resetCountdown()
      abortController = null
    }
  }
}

async function manualRefresh() {
  if (refreshing.value) return
  refreshing.value = true
  try {
    await reload(true)
  } finally {
    refreshing.value = false
  }
}

watch(
  () => appStore.cachedPublicSettings?.channel_monitor_enabled,
  (enabled) => {
    if (enabled === false) autoRefresh.stop()
    else if (autoRefresh.enabled.value) autoRefresh.start()
  },
)

onMounted(() => {
  void reload(false)
  if (appStore.cachedPublicSettings?.channel_monitor_enabled !== false) {
    autoRefresh.setEnabled(true)
  }
})

onBeforeUnmount(() => {
  abortController?.abort()
})
</script>

<style scoped>
.cs-page {
  --cs-ink: #3f3420;
  --cs-ink-soft: #6b6045;
  --cs-ok: #4f9a66;
  --cs-warn: #e2a417;
  --cs-down: #d4614a;
  --cs-empty: #ebe2c4;
  display: flex;
  width: 100%;
  max-width: 80rem;
  flex-direction: column;
  gap: 1rem;
  margin: 0 auto;
  padding-bottom: 2rem;
  color: var(--cs-ink);
}

/* ---------- Summary ---------- */
.cs-summary {
  --tone-bg: #fffbe8;
  --tone-glow: rgb(233 184 36 / 0.18);
  --tone-ink: var(--cs-ink);
  --tone-glyph: #c99a12;
  display: grid;
  grid-template-columns: auto minmax(0, 1fr) auto;
  grid-template-areas:
    'glyph text refresh'
    'counts counts counts'
    'legend legend legend';
  align-items: center;
  gap: 0.875rem;
  padding: 1.25rem;
  border-radius: 1.75rem;
  background:
    radial-gradient(40rem 12rem at 0% 0%, var(--tone-glow), transparent 70%),
    var(--tone-bg);
  box-shadow: var(--theme-highlight), var(--theme-elevation-1);
}

.cs-summary.is-operational {
  --tone-bg: #f4f9ef;
  --tone-glow: rgb(121 179 123 / 0.28);
  --tone-ink: #2e5038;
  --tone-glyph: var(--cs-ok);
}

.cs-summary.is-degraded {
  --tone-bg: #fff8e0;
  --tone-glow: rgb(233 184 36 / 0.32);
  --tone-ink: #65460b;
  --tone-glyph: var(--cs-warn);
}

.cs-summary.is-outage,
.cs-summary.is-error {
  --tone-bg: #fdf1ec;
  --tone-glow: rgb(212 97 74 / 0.22);
  --tone-ink: #7f3424;
  --tone-glyph: var(--cs-down);
}

.cs-summary-text {
  min-width: 0;
  grid-area: text;
}

.cs-summary-glyph {
  position: relative;
  grid-area: glyph;
  align-self: start;
  display: inline-flex;
  width: 2.75rem;
  height: 2.75rem;
  flex-shrink: 0;
  align-items: center;
  justify-content: center;
  border-radius: 999px;
  background: var(--tone-glyph);
  color: #fffef8;
  box-shadow: 0 6px 14px -8px var(--tone-glyph);
}

.is-loading .cs-summary-glyph,
.is-empty .cs-summary-glyph,
.is-unknown .cs-summary-glyph {
  background: #fff3bd;
  color: #976800;
  box-shadow: none;
}

/* A live check result pulses once in a while; nothing moves while loading or empty. */
.is-operational .cs-summary-glyph::after,
.is-degraded .cs-summary-glyph::after,
.is-outage .cs-summary-glyph::after {
  position: absolute;
  inset: 0;
  border-radius: inherit;
  box-shadow: 0 0 0 0 var(--tone-glyph);
  content: '';
  animation: cs-pulse 2.8s ease-out infinite;
}

.cs-summary-title {
  font-size: 1.25rem;
  font-weight: 750;
  line-height: 1.3;
  color: var(--tone-ink);
}

.cs-summary-detail {
  margin-top: 0.125rem;
  font-size: 0.8125rem;
  line-height: 1.25rem;
  color: var(--cs-ink-soft);
}

.cs-counts {
  display: flex;
  grid-area: counts;
  flex-wrap: wrap;
  gap: 0.375rem;
}

.cs-count {
  display: inline-flex;
  align-items: center;
  gap: 0.375rem;
  padding: 0.3125rem 0.75rem 0.3125rem 0.625rem;
  border-radius: 999px;
  background: rgb(255 254 248 / 0.85);
  font-size: 0.8125rem;
  color: var(--cs-ink-soft);
  box-shadow: inset 0 1px 0 rgb(255 255 255 / 0.9), 0 1px 2px rgb(63 52 32 / 0.05);
}

.cs-count strong {
  font-weight: 750;
  font-variant-numeric: tabular-nums;
  color: var(--cs-ink);
}

.cs-count-dot {
  width: 0.5rem;
  height: 0.5rem;
  border-radius: 999px;
  background: var(--cs-empty);
}

.cs-count.is-operational .cs-count-dot { background: var(--cs-ok); }
.cs-count.is-degraded .cs-count-dot { background: var(--cs-warn); }
.cs-count.is-down .cs-count-dot { background: var(--cs-down); }
.cs-count.is-unknown .cs-count-dot { background: #cfc29a; }

.cs-refresh {
  display: flex;
  grid-area: refresh;
  align-items: center;
  align-self: start;
  gap: 0.5rem;
}

.cs-refresh-note {
  font-size: 0.75rem;
  font-variant-numeric: tabular-nums;
  color: var(--cs-ink-soft);
}

.cs-refresh-note.is-side {
  display: none;
}

.cs-refresh-button {
  display: inline-flex;
  width: 2.25rem;
  height: 2.25rem;
  align-items: center;
  justify-content: center;
  border-radius: 999px;
  background: rgb(255 254 248 / 0.85);
  color: #7e5600;
  box-shadow: inset 0 1px 0 rgb(255 255 255 / 0.9), 0 1px 2px rgb(63 52 32 / 0.06);
  transition: background-color 160ms ease;
}

.cs-refresh-button:hover:not(:disabled) {
  background: #fffef8;
}

.cs-legend {
  display: flex;
  grid-area: legend;
  flex-wrap: wrap;
  align-items: center;
  gap: 0.5rem;
  padding-top: 0.875rem;
  font-size: 0.75rem;
  color: var(--cs-ink-soft);
  box-shadow: inset 0 1px 0 rgb(215 193 106 / 0.28);
}

.cs-legend-swatches {
  display: inline-flex;
  gap: 2px;
}

.cs-legend-swatch {
  width: 0.375rem;
  height: 0.875rem;
  border-radius: 2px;
  background: var(--cs-empty);
}

.cs-legend-swatch.is-operational { background: var(--cs-ok); }
.cs-legend-swatch.is-degraded { background: var(--cs-warn); }
.cs-legend-swatch.is-down { background: var(--cs-down); }

/* ---------- Notice ---------- */
.cs-notice {
  display: flex;
  align-items: flex-start;
  gap: 0.875rem;
  padding: 1rem 1.25rem;
  border-radius: 1.5rem;
  background: linear-gradient(120deg, #fff3bd, #fff8dc 60%, #fffbe8);
  box-shadow: var(--theme-highlight), var(--theme-elevation-1);
}

.cs-notice-icon {
  display: inline-flex;
  width: 2.5rem;
  height: 2.5rem;
  flex-shrink: 0;
  align-items: center;
  justify-content: center;
  border-radius: 0.875rem;
  background: #e9b824;
  color: #3f3420;
  transform: rotate(-8deg);
}

.cs-notice-title {
  font-size: 0.9375rem;
  font-weight: 700;
  line-height: 1.5rem;
  color: #65460b;
}

.cs-notice-text {
  margin-top: 0.125rem;
  white-space: pre-line;
  font-size: 0.875rem;
  line-height: 1.6;
  overflow-wrap: anywhere;
  color: #5b5238;
}

/* ---------- Footer ---------- */
.cs-footer {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: center;
  gap: 0.375rem;
  padding-top: 0.5rem;
  font-size: 0.75rem;
  color: var(--cs-ink-soft);
}

.cs-footer-link {
  font-weight: 650;
  color: #7e5600;
  text-decoration: underline;
  text-decoration-color: rgb(201 154 18 / 0.45);
  text-underline-offset: 4px;
  transition: color 160ms ease, text-decoration-color 160ms ease;
}

.cs-footer-link:hover {
  color: #3f3420;
  text-decoration-color: currentColor;
}

@keyframes cs-pulse {
  0% { box-shadow: 0 0 0 0 var(--tone-glyph); opacity: 0.45; }
  70%,
  100% { box-shadow: 0 0 0 0.75rem var(--tone-glyph); opacity: 0; }
}

@media (min-width: 900px) {
  .cs-summary {
    grid-template-columns: auto minmax(0, 1fr) auto auto;
    grid-template-areas:
      'glyph text counts refresh'
      'legend legend legend legend';
    column-gap: 1rem;
  }

  .cs-counts {
    justify-content: flex-end;
  }

  .cs-summary-glyph,
  .cs-refresh {
    align-self: center;
  }

  .cs-refresh-note.is-inline {
    display: none;
  }

  .cs-refresh-note.is-side {
    display: inline;
  }
}

@media (max-width: 639px) {
  .cs-summary {
    padding: 1rem;
    border-radius: 1.5rem;
  }

  .cs-summary-title {
    font-size: 1.0625rem;
  }

  .cs-summary-glyph {
    width: 2.375rem;
    height: 2.375rem;
  }

  .cs-notice {
    padding: 0.875rem 1rem;
  }

  .cs-notice-icon {
    width: 2.25rem;
    height: 2.25rem;
  }
}

@media (prefers-reduced-motion: reduce) {
  .cs-summary-glyph::after {
    animation: none !important;
  }
}
</style>
