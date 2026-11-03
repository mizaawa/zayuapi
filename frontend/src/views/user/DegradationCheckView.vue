<template>
  <AppLayout>
    <div class="dg-page">
      <section class="dg-hero" aria-labelledby="degradation-title">
        <span class="dg-hero-glyph" aria-hidden="true"><Icon name="beaker" size="md" :stroke-width="2" /></span>
        <div class="dg-hero-text">
          <h1 id="degradation-title" class="dg-hero-title">{{ t('degradationCheck.title') }}</h1>
          <p class="dg-hero-subtitle">{{ t('degradationCheck.subtitle') }}</p>
        </div>
        <div class="dg-hero-actions">
          <span v-if="updatedAt" class="dg-hero-updated">{{ t('degradationCheck.updatedAt', { time: updatedAt }) }}</span>
          <button
            type="button"
            class="dg-refresh"
            :disabled="refreshing"
            :title="t('degradationCheck.refresh')"
            :aria-label="t('degradationCheck.refresh')"
            @click="refresh"
          >
            <Icon name="refresh" size="sm" :class="{ 'animate-spin': refreshing }" />
          </button>
        </div>
        <p class="dg-hero-hint">{{ t('degradationCheck.reviewHint') }}</p>
      </section>

      <div v-if="loading && !loadedOnce" class="dg-state" role="status">
        <span class="dg-state-spinner" aria-hidden="true"></span>
        {{ t('degradationCheck.loading') }}
      </div>
      <div v-else-if="loadFailed && !platforms.length" class="dg-state is-error" role="alert">
        {{ t('degradationCheck.loadFailed') }}
      </div>
      <div v-else-if="overview && !overview.enabled" class="dg-state">
        {{ t('degradationCheck.disabled') }}
      </div>
      <div v-else-if="!platforms.length" class="dg-state is-empty" data-testid="degradation-empty">
        <Icon name="sparkles" size="xl" />
        <strong>{{ t('degradationCheck.empty') }}</strong>
        <span>{{ t('degradationCheck.emptyHint') }}</span>
      </div>

      <section
        v-for="platform in platforms"
        :key="platform.key"
        class="dg-platform"
        :class="`is-${platform.key}`"
        :data-platform="platform.key"
        data-testid="degradation-platform"
      >
        <header class="dg-platform-head">
          <span class="dg-platform-icon"><PlatformIcon :platform="platform.iconKey" size="md" /></span>
          <h2 class="dg-platform-title">{{ platform.label }}</h2>
          <span class="dg-platform-count">{{ t('degradationCheck.channels', { count: platform.monitors.length }) }}</span>
        </header>

        <article
          v-for="monitor in platform.monitors"
          :key="monitor.id"
          class="dg-monitor"
          data-testid="degradation-monitor"
        >
          <header class="dg-monitor-head">
            <div class="dg-monitor-title-row">
              <h3 class="dg-monitor-title">{{ monitor.name }}</h3>
              <span v-if="monitor.name !== monitor.group_name" class="dg-monitor-group">{{ monitor.group_name }}</span>
            </div>
            <div class="dg-monitor-tags">
              <span class="dg-tag is-model">{{ monitor.model }}</span>
              <span class="dg-tag">{{ effortLabel(monitor.reasoning_effort) }}</span>
              <span class="dg-tag">{{ protocolLabel(monitor.protocol) }}</span>
              <span class="dg-tag is-soft">{{ t('degradationCheck.everyMinutes', { n: monitor.interval_minutes }) }}</span>
              <span class="dg-tag is-soft">{{ monitor.last_run_at ? t('degradationCheck.lastRun', { time: formatRelativeTime(monitor.last_run_at) }) : t('degradationCheck.neverRun') }}</span>
            </div>
          </header>

          <div v-if="!monitor.runs.length" class="dg-monitor-empty">
            <strong>{{ t('degradationCheck.noRuns') }}</strong>
            <span>{{ t('degradationCheck.noRunsHint') }}</span>
          </div>
          <template v-else>
            <div
              :ref="(el) => registerGrid(monitor.id, el)"
              class="dg-grid"
              :style="{ '--dg-columns': String(columnsFor(monitor.id)) }"
            >
              <DegradationRunCard
                v-for="run in visibleRuns(monitor)"
                :key="run.id"
                :run="run"
                :title="monitor.name"
                @open="openDetail(monitor.name, $event)"
              />
            </div>
            <div v-if="monitor.runs.length > columnsFor(monitor.id)" class="dg-expand-row">
              <button type="button" class="dg-expand" :aria-expanded="isExpanded(monitor.id)" @click="toggleExpanded(monitor.id)">
                {{ isExpanded(monitor.id) ? t('degradationCheck.collapse') : t('degradationCheck.expandAll', { count: monitor.runs.length }) }}
                <Icon name="chevronDown" size="sm" class="dg-expand-icon" :class="{ 'is-open': isExpanded(monitor.id) }" />
              </button>
            </div>
          </template>
        </article>
      </section>
    </div>

    <DegradationRunDetailDialog :run-id="detailRunId" :title="detailTitle" @close="detailRunId = null" />
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch, type ComponentPublicInstance } from 'vue'
import { useI18n } from 'vue-i18n'
import { getOverview, type DegradationOverview, type DegradationUserMonitor } from '@/api/degradation'
import AppLayout from '@/components/layout/AppLayout.vue'
import Icon from '@/components/icons/Icon.vue'
import PlatformIcon from '@/components/common/PlatformIcon.vue'
import DegradationRunCard from '@/components/degradation/DegradationRunCard.vue'
import DegradationRunDetailDialog from '@/components/degradation/DegradationRunDetailDialog.vue'
import { useAppStore } from '@/stores/app'
import { extractApiErrorMessage } from '@/utils/apiError'
import { degradationPosterQueue } from '@/utils/degradationPoster'
import {
  DEGRADATION_PLATFORM_ORDER,
  degradationColumnsForWidth,
  degradationPlatformKey,
} from '@/utils/degradationPreview'
import { formatRelativeTime, formatTime } from '@/utils/format'
import type { GroupPlatform } from '@/types'

const REFRESH_INTERVAL_MS = 60_000

const { t, te } = useI18n()
const appStore = useAppStore()

const overview = ref<DegradationOverview | null>(null)
const loading = ref(false)
const refreshing = ref(false)
const loadedOnce = ref(false)
const loadFailed = ref(false)
const updatedAt = ref('')
const expanded = reactive(new Set<number>())
const columns = reactive(new Map<number, number>())
const detailRunId = ref<number | null>(null)
const detailTitle = ref('')
const gridObservers = new Map<number, { element: Element; observer: ResizeObserver }>()
let abortController: AbortController | null = null
let refreshTimer: ReturnType<typeof setInterval> | null = null

interface PlatformSection {
  key: string
  iconKey: GroupPlatform
  label: string
  monitors: DegradationUserMonitor[]
}

const platforms = computed<PlatformSection[]>(() => {
  const groups = new Map<string, DegradationUserMonitor[]>()
  for (const monitor of overview.value?.monitors ?? []) {
    const key = degradationPlatformKey(monitor.platform)
    const list = groups.get(key) ?? []
    // A check only shows up once its output is complete.
    list.push({ ...monitor, runs: monitor.runs.filter((run) => run.status !== 'running') })
    groups.set(key, list)
  }
  const order = [...DEGRADATION_PLATFORM_ORDER] as string[]
  return [...groups.entries()]
    .sort(([a], [b]) => (order.indexOf(a) === -1 ? 99 : order.indexOf(a)) - (order.indexOf(b) === -1 ? 99 : order.indexOf(b)))
    .map(([key, monitors]) => ({
      key,
      iconKey: (key === 'custom' ? 'composite' : key) as GroupPlatform,
      label: te(`degradationCheck.platforms.${key}`) ? t(`degradationCheck.platforms.${key}`) : key,
      monitors,
    }))
})

function effortLabel(effort: string): string {
  const key = `degradationCheck.efforts.${effort}`
  return effort && te(key) ? t(key) : effort || t('degradationCheck.efforts.default')
}

function protocolLabel(protocol: string): string {
  const key = `degradationCheck.protocols.${protocol}`
  return te(key) ? t(key) : protocol
}

function columnsFor(id: number): number {
  return columns.get(id) ?? 1
}

function isExpanded(id: number): boolean {
  return expanded.has(id)
}

function toggleExpanded(id: number) {
  if (expanded.has(id)) expanded.delete(id)
  else expanded.add(id)
}

function visibleRuns(monitor: DegradationUserMonitor) {
  return isExpanded(monitor.id) ? monitor.runs : monitor.runs.slice(0, columnsFor(monitor.id))
}

function registerGrid(id: number, el: Element | ComponentPublicInstance | null) {
  const element = el instanceof Element ? el : null
  const current = gridObservers.get(id)
  if (current?.element === element) return
  current?.observer.disconnect()
  gridObservers.delete(id)
  if (!element) return
  const measure = () => {
    const width = element.clientWidth
    if (width > 0) columns.set(id, degradationColumnsForWidth(width))
  }
  measure()
  if (typeof ResizeObserver === 'undefined') return
  const observer = new ResizeObserver(measure)
  observer.observe(element)
  gridObservers.set(id, { element, observer })
}

function openDetail(title: string, id: number) {
  detailTitle.value = title
  detailRunId.value = id
}

// While the dialog plays an animation, gallery posters wait so it runs smoothly.
watch(detailRunId, (id) => {
  if (id === null) degradationPosterQueue.resume()
  else degradationPosterQueue.pause()
})

async function reload(silent = false) {
  abortController?.abort()
  const controller = new AbortController()
  abortController = controller
  if (!silent) loading.value = true
  try {
    const data = await getOverview({ signal: controller.signal })
    if (controller.signal.aborted) return
    overview.value = data
    loadFailed.value = false
    updatedAt.value = formatTime(new Date())
  } catch (err: unknown) {
    const requestError = err as { name?: string; code?: string }
    if (requestError?.name === 'AbortError' || requestError?.code === 'ERR_CANCELED') return
    loadFailed.value = true
    appStore.showError(extractApiErrorMessage(err, t('degradationCheck.loadFailed')))
  } finally {
    if (abortController === controller) {
      loading.value = false
      loadedOnce.value = true
      abortController = null
    }
  }
}

async function refresh() {
  if (refreshing.value) return
  refreshing.value = true
  try {
    await reload(true)
  } finally {
    refreshing.value = false
  }
}

onMounted(() => {
  void reload()
  refreshTimer = setInterval(() => {
    if (!document.hidden && detailRunId.value === null) void reload(true)
  }, REFRESH_INTERVAL_MS)
})

onBeforeUnmount(() => {
  abortController?.abort()
  if (refreshTimer) clearInterval(refreshTimer)
  degradationPosterQueue.resume()
  for (const { observer } of gridObservers.values()) observer.disconnect()
  gridObservers.clear()
})
</script>

<style scoped>
.dg-page {
  --dg-ink: #3f3420;
  --dg-ink-soft: #6b6045;
  display: flex;
  width: 100%;
  max-width: 88rem;
  flex-direction: column;
  gap: 1.25rem;
  margin: 0 auto;
  padding-bottom: 2rem;
  color: var(--dg-ink);
}

.dg-hero {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr) auto;
  grid-template-areas:
    'glyph text actions'
    'hint hint hint';
  align-items: center;
  gap: 0.875rem;
  padding: 1.25rem;
  border-radius: 1.75rem;
  background:
    radial-gradient(40rem 12rem at 0% 0%, rgb(233 184 36 / 0.22), transparent 70%),
    radial-gradient(30rem 10rem at 100% 100%, rgb(121 179 123 / 0.16), transparent 70%),
    #fffbe8;
  box-shadow: var(--theme-highlight), var(--theme-elevation-1);
}

.dg-hero-glyph {
  grid-area: glyph;
  display: inline-flex;
  width: 2.75rem;
  height: 2.75rem;
  align-items: center;
  justify-content: center;
  border-radius: 1rem;
  background: var(--md-sys-color-primary);
  color: var(--md-sys-color-on-primary);
  box-shadow: 0 6px 14px -8px rgb(151 104 0 / 0.6);
  transform: rotate(-6deg);
}

.dg-hero-text { grid-area: text; min-width: 0; }

.dg-hero-title {
  font-size: 1.25rem;
  font-weight: 750;
  line-height: 1.3;
}

.dg-hero-subtitle {
  margin-top: 0.125rem;
  font-size: 0.8125rem;
  line-height: 1.5;
  color: var(--dg-ink-soft);
}

.dg-hero-actions {
  grid-area: actions;
  display: flex;
  align-items: center;
  gap: 0.5rem;
  align-self: start;
}

.dg-hero-updated {
  font-size: 0.75rem;
  font-variant-numeric: tabular-nums;
  color: var(--dg-ink-soft);
}

.dg-refresh {
  display: inline-flex;
  width: 2.25rem;
  height: 2.25rem;
  align-items: center;
  justify-content: center;
  border-radius: 999px;
  background: rgb(255 254 248 / 0.9);
  color: #7e5600;
  box-shadow: var(--theme-highlight), 0 1px 2px rgb(63 52 32 / 0.08);
}

.dg-refresh:hover:not(:disabled) { background: #fffef8; }

.dg-hero-hint {
  grid-area: hint;
  padding-top: 0.75rem;
  font-size: 0.75rem;
  color: var(--dg-ink-soft);
  box-shadow: inset 0 1px 0 rgb(215 193 106 / 0.3);
}

.dg-state {
  display: flex;
  min-height: 10rem;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 0.5rem;
  padding: 2rem 1rem;
  border-radius: 1.5rem;
  background: var(--md-sys-color-surface);
  color: var(--dg-ink-soft);
  text-align: center;
  box-shadow: var(--theme-highlight), var(--theme-elevation-1);
}

.dg-state.is-error { color: #7f3424; }
.dg-state.is-empty strong { color: var(--dg-ink); }

.dg-state-spinner {
  width: 1.5rem;
  height: 1.5rem;
  border: 2.5px solid rgb(233 184 36 / 0.3);
  border-top-color: var(--md-sys-color-primary);
  border-radius: 999px;
  animation: dg-page-spin 0.9s linear infinite;
}

.dg-platform {
  --dg-accent: 201 154 18;
  display: flex;
  flex-direction: column;
  gap: 0.875rem;
}

.dg-platform.is-openai { --dg-accent: 16 163 127; }
.dg-platform.is-anthropic { --dg-accent: 217 119 87; }
.dg-platform.is-gemini { --dg-accent: 66 133 244; }
.dg-platform.is-grok { --dg-accent: 82 82 91; }
.dg-platform.is-custom { --dg-accent: 120 113 108; }

.dg-platform-head {
  display: flex;
  align-items: center;
  gap: 0.625rem;
  padding: 0 0.25rem;
}

.dg-platform-icon {
  display: inline-flex;
  width: 2.125rem;
  height: 2.125rem;
  align-items: center;
  justify-content: center;
  border-radius: 0.75rem;
  background: rgb(var(--dg-accent) / 0.12);
  color: rgb(var(--dg-accent));
}

.dg-platform-title {
  font-size: 1.0625rem;
  font-weight: 750;
}

.dg-platform-count {
  padding: 0.125rem 0.5rem;
  border-radius: 999px;
  background: rgb(var(--dg-accent) / 0.1);
  font-size: 0.75rem;
  font-weight: 600;
  color: rgb(var(--dg-accent));
}

.dg-monitor {
  display: flex;
  flex-direction: column;
  gap: 0.875rem;
  padding: 1rem;
  border-radius: 1.5rem;
  background: color-mix(in srgb, var(--md-sys-color-surface) 70%, var(--md-sys-color-surface-container));
  box-shadow: var(--theme-highlight), var(--theme-elevation-1), inset 3px 0 0 rgb(var(--dg-accent) / 0.55);
}

.dg-monitor-head {
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
}

.dg-monitor-title-row {
  display: flex;
  min-width: 0;
  flex-wrap: wrap;
  align-items: baseline;
  gap: 0.5rem;
}

.dg-monitor-title {
  font-size: 0.9375rem;
  font-weight: 700;
  overflow-wrap: anywhere;
}

.dg-monitor-group {
  font-size: 0.75rem;
  color: var(--dg-ink-soft);
}

.dg-monitor-tags {
  display: flex;
  flex-wrap: wrap;
  gap: 0.375rem;
}

.dg-tag {
  display: inline-flex;
  max-width: 100%;
  align-items: center;
  padding: 0.125rem 0.5rem;
  border-radius: 999px;
  background: var(--md-sys-color-surface);
  font-size: 0.6875rem;
  font-weight: 600;
  color: var(--dg-ink);
  box-shadow: inset 0 0 0 1px rgb(215 193 106 / 0.35);
  overflow-wrap: anywhere;
}

.dg-tag.is-model {
  background: rgb(var(--dg-accent) / 0.12);
  color: rgb(var(--dg-accent));
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  box-shadow: none;
}

.dg-tag.is-soft {
  background: transparent;
  color: var(--dg-ink-soft);
  font-weight: 500;
}

.dg-monitor-empty {
  display: flex;
  flex-direction: column;
  gap: 0.25rem;
  padding: 1.25rem;
  border-radius: 1rem;
  background: var(--md-sys-color-surface);
  font-size: 0.8125rem;
  color: var(--dg-ink-soft);
  text-align: center;
}

.dg-grid {
  display: grid;
  grid-template-columns: repeat(var(--dg-columns, 1), minmax(0, 1fr));
  gap: 1rem;
}

.dg-expand-row {
  display: flex;
  justify-content: center;
}

.dg-expand {
  display: inline-flex;
  align-items: center;
  gap: 0.25rem;
  padding: 0.3125rem 0.875rem;
  border-radius: 999px;
  background: var(--md-sys-color-surface);
  font-size: 0.75rem;
  font-weight: 650;
  color: #7e5600;
  box-shadow: var(--theme-highlight), 0 1px 2px rgb(63 52 32 / 0.08);
  transition: background-color 160ms ease;
}

.dg-expand:hover { background: var(--md-sys-color-surface-container); }

.dg-expand-icon { transition: transform 200ms ease; }
.dg-expand-icon.is-open { transform: rotate(180deg); }

@keyframes dg-page-spin {
  to { transform: rotate(360deg); }
}

@media (max-width: 639px) {
  .dg-hero {
    grid-template-columns: auto minmax(0, 1fr);
    grid-template-areas:
      'glyph text'
      'actions actions'
      'hint hint';
    padding: 1rem;
    border-radius: 1.5rem;
  }

  .dg-hero-actions { justify-content: space-between; }
  .dg-monitor { padding: 0.75rem; border-radius: 1.25rem; }
  .dg-grid { gap: 0.75rem; }
}

@media (prefers-reduced-motion: reduce) {
  .dg-expand-icon { transition: none; }
}
</style>
