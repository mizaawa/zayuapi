<template>
  <AppLayout :class="{ 'degradation-sort-open': sortPlatform !== null }">
    <div class="dgm-page">
      <div class="dgm-toolbar">
        <div class="segment-group dgm-tabs" role="tablist" :aria-label="t('admin.degradation.title')">
          <button
            v-for="tab in tabs"
            :key="tab.key"
            type="button"
            role="tab"
            class="dgm-tab"
            :class="activeTab === tab.key ? 'segment-active' : 'segment-idle'"
            :aria-selected="activeTab === tab.key"
            :data-testid="`degradation-tab-${tab.key}`"
            @click="activeTab = tab.key"
          >
            <Icon :name="tab.icon" size="sm" />
            {{ tab.label }}
          </button>
        </div>
        <div v-if="activeTab === 'monitors'" class="dgm-toolbar-actions">
          <button type="button" class="btn btn-secondary" :disabled="loading" :title="t('admin.degradation.refresh')" @click="reload()">
            <Icon name="refresh" size="sm" :class="{ 'animate-spin': loading }" />
            <span class="hidden sm:inline">{{ t('admin.degradation.refresh') }}</span>
          </button>
          <button type="button" class="btn btn-primary" data-testid="degradation-add-group" @click="openCreate">
            <Icon name="plus" size="sm" />
            {{ t('admin.degradation.addGroup') }}
          </button>
        </div>
      </div>

      <div v-if="loaded && !featureEnabled" class="dgm-notice" role="status">
        <Icon name="exclamationTriangle" size="sm" />
        <span>{{ t('admin.degradation.disabledNotice') }}</span>
        <router-link to="/admin/features" class="dgm-notice-link">{{ t('admin.degradation.enableLink') }} →</router-link>
      </div>

      <!-- Tab: monitors -->
      <section v-show="activeTab === 'monitors'" class="space-y-4" data-testid="degradation-monitors-tab">
        <div v-if="loading && !loaded" class="dgm-empty card"><LoadingSpinner /></div>
        <div v-else-if="!monitors.length" class="card">
          <EmptyState
            :title="t('admin.degradation.emptyTitle')"
            :description="t('admin.degradation.emptyDescription')"
            :action-text="t('admin.degradation.addGroup')"
            @action="openCreate"
          />
        </div>

        <section
          v-for="platform in platformSections"
          :key="platform.key"
          class="dgm-platform card"
          :class="`is-${platform.key}`"
          :data-platform="platform.key"
          data-testid="degradation-admin-platform"
        >
          <header class="dgm-platform-head">
            <button
              type="button"
              class="dgm-platform-toggle"
              :aria-expanded="isExpanded(platform.key)"
              @click="toggleExpanded(platform.key)"
            >
              <Icon name="chevronRight" size="sm" class="dgm-chevron" :class="{ 'is-open': isExpanded(platform.key) }" />
              <span class="dgm-platform-icon"><PlatformIcon :platform="platform.iconKey" size="sm" /></span>
              <span class="dgm-platform-name">{{ platform.label }}</span>
              <span class="dgm-platform-count">{{ t('admin.degradation.channelCount', { count: platform.monitors.length }) }}</span>
              <span v-if="platform.running" class="dgm-platform-running">
                <span class="dgm-pulse" aria-hidden="true"></span>
                {{ t('admin.degradation.runningCount', { count: platform.running }) }}
              </span>
            </button>
            <button type="button" class="btn btn-secondary btn-sm" :data-testid="`degradation-sort-${platform.key}`" @click="openSort(platform.key)">
              <Icon name="arrowsUpDown" size="sm" />
              {{ t('admin.degradation.sort') }}
            </button>
          </header>

          <ul v-if="isExpanded(platform.key)" class="dgm-rows">
            <li v-for="monitor in platform.monitors" :key="monitor.id" class="dgm-row" data-testid="degradation-admin-row">
              <div class="dgm-row-main">
                <div class="dgm-row-title">
                  <span class="dgm-row-name">{{ monitor.name || monitor.group_name }}</span>
                  <span v-if="monitor.name && monitor.name !== monitor.group_name" class="dgm-badge">{{ monitor.group_name }}</span>
                  <span v-if="!monitor.enabled" class="dgm-badge is-muted">{{ t('admin.degradation.paused') }}</span>
                  <span v-if="monitor.group_deleted" class="dgm-badge is-danger">{{ t('admin.degradation.groupDeleted') }}</span>
                  <span v-else-if="monitor.group_status && monitor.group_status !== 'active'" class="dgm-badge is-warn">{{ t('admin.degradation.groupInactive') }}</span>
                </div>
                <div class="dgm-row-meta">
                  <span class="dgm-chip is-model">{{ monitor.model }}</span>
                  <span class="dgm-chip">{{ effortText(monitor) }}</span>
                  <span class="dgm-chip">{{ t(`degradationCheck.protocols.${monitor.protocol}`) }}</span>
                  <span class="dgm-chip">{{ intervalText(monitor) }}</span>
                  <span class="dgm-chip">{{ monitor.random_prompt ? t('admin.degradation.randomPrompt') : (monitor.prompt_name || t('admin.degradation.followDefault')) }}</span>
                </div>
              </div>
              <div class="dgm-row-status">
                <span v-if="monitor.running" class="dgm-status is-running"><span class="dgm-pulse" aria-hidden="true"></span>{{ t('admin.degradation.running') }}</span>
                <span v-else-if="monitor.last_result_status" class="dgm-status" :class="`is-${monitor.last_result_status}`" :title="monitor.last_result_error || undefined">
                  {{ t(`admin.degradation.resultStatus.${monitor.last_result_status}`) }}
                  <small>{{ formatRelativeTime(monitor.last_result_at) }}</small>
                </span>
                <span v-else class="dgm-status is-idle">{{ t('admin.degradation.neverRun') }}</span>
                <small class="dgm-stored">{{ t('admin.degradation.stored', { count: monitor.stored_runs ?? 0 }) }}</small>
              </div>
              <div class="dgm-row-actions">
                <button
                  type="button"
                  class="dgm-action"
                  :disabled="monitor.running || runningIds.has(monitor.id) || !featureEnabled"
                  :title="t('admin.degradation.runNow')"
                  :aria-label="t('admin.degradation.runNow')"
                  data-testid="degradation-run-now"
                  @click="runNow(monitor)"
                >
                  <Icon name="play" size="sm" />
                </button>
                <button type="button" class="dgm-action" :title="t('admin.degradation.edit')" :aria-label="t('admin.degradation.edit')" data-testid="degradation-edit" @click="openEdit(monitor)">
                  <Icon name="edit" size="sm" />
                </button>
                <button type="button" class="dgm-action is-danger" :title="t('admin.degradation.delete')" :aria-label="t('admin.degradation.delete')" data-testid="degradation-delete" @click="deleting = monitor">
                  <Icon name="trash" size="sm" />
                </button>
              </div>
            </li>
          </ul>
        </section>
      </section>

      <!-- Tab: configuration -->
      <section v-show="activeTab === 'config'" class="space-y-6" data-testid="degradation-config-tab">
        <div class="card">
          <div class="dgm-card-head">
            <div class="min-w-0">
              <h2 class="dgm-card-title dgm-title-row">
                {{ t('admin.degradation.config.presetsTitle') }}
                <span v-if="prompts.length" class="dgm-badge" data-testid="degradation-preset-count">{{ prompts.length }}</span>
              </h2>
              <p class="dgm-card-hint">{{ t('admin.degradation.config.presetsHint') }}</p>
            </div>
            <button type="button" class="btn btn-secondary btn-sm shrink-0" data-testid="degradation-create-preset" @click="openPrompt(null)">
              <Icon name="plus" size="sm" />
              {{ t('admin.degradation.config.createPreset') }}
            </button>
          </div>
          <p v-if="!prompts.length" class="dgm-card-body text-sm text-gray-500">{{ t('admin.degradation.config.emptyPresets') }}</p>
          <!-- Five rows stay in view and the rest scroll inside the card; the default preset leads. -->
          <div v-else ref="presetScroller" class="dgm-presets-scroll" data-testid="degradation-presets-scroll">
            <TransitionGroup tag="div" name="dgm-preset" class="dgm-presets" role="radiogroup" :aria-label="t('admin.degradation.config.presetsTitle')">
              <div
                v-for="prompt in orderedPrompts"
                :key="prompt.id"
                class="dgm-preset"
                :class="{ 'is-default': prompt.id === effectiveDefaultPromptID }"
                :data-preset-id="prompt.id"
                data-testid="degradation-preset"
              >
                <button
                  type="button"
                  role="radio"
                  class="dgm-preset-select"
                  :aria-checked="prompt.id === effectiveDefaultPromptID"
                  :disabled="savingDefault"
                  :title="t('admin.degradation.config.setDefault')"
                  @click="setDefaultPrompt(prompt.id)"
                >
                  <span class="dgm-preset-body">
                    <span class="dgm-radio" aria-hidden="true"></span>
                    <span class="dgm-preset-text">
                      <span class="dgm-preset-name">
                        <span class="dgm-preset-label" :title="prompt.name">{{ prompt.name }}</span>
                        <span v-if="prompt.id === effectiveDefaultPromptID" class="dgm-badge is-primary">{{ t('admin.degradation.config.defaultPreset') }}</span>
                        <span v-if="prompt.builtin_key" class="dgm-badge">{{ t('admin.degradation.config.builtin') }}</span>
                      </span>
                      <span class="dgm-preset-prompt">{{ prompt.prompt }}</span>
                    </span>
                  </span>
                </button>
                <button type="button" class="dgm-action" :title="t('admin.degradation.config.editPreset')" :aria-label="`${t('admin.degradation.config.editPreset')} ${prompt.name}`" data-testid="degradation-edit-preset" @click="openPrompt(prompt)">
                  <Icon name="edit" size="sm" />
                </button>
              </div>
            </TransitionGroup>
          </div>
        </div>

        <form class="card" data-testid="degradation-config-form" @submit.prevent="saveConfig">
          <div class="dgm-card-head">
            <div class="min-w-0">
              <h2 class="dgm-card-title">{{ t('admin.degradation.config.settingsTitle') }}</h2>
              <p class="dgm-card-hint">{{ t('admin.degradation.config.settingsHint') }}</p>
            </div>
          </div>
          <div class="dgm-card-body grid gap-5 md:grid-cols-2">
            <label class="block">
              <span class="input-label">{{ t('admin.degradation.config.defaultInterval') }}</span>
              <input v-model.number="configForm.default_interval_minutes" type="number" min="5" max="10080" step="1" required class="input" data-testid="degradation-config-interval" />
              <span class="input-hint block">{{ t('admin.degradation.config.defaultIntervalHint') }}</span>
            </label>
            <label class="block">
              <span class="input-label">{{ t('admin.degradation.config.timeout') }}</span>
              <input v-model.number="configForm.timeout_minutes" type="number" min="1" max="60" step="1" required class="input" data-testid="degradation-config-timeout" />
              <span class="input-hint block">{{ t('admin.degradation.config.timeoutHint') }}</span>
            </label>
            <label class="block">
              <span class="input-label">{{ t('admin.degradation.config.maxRetries') }}</span>
              <input v-model.number="configForm.max_retries" type="number" min="0" max="5" step="1" required class="input" data-testid="degradation-config-retries" />
              <span class="input-hint block">{{ t('admin.degradation.config.maxRetriesHint') }}</span>
            </label>
            <label class="block">
              <span class="input-label">{{ t('admin.degradation.config.maxDisplay') }}</span>
              <input v-model.number="configForm.max_display_per_group" type="number" min="1" max="100" step="1" required class="input" data-testid="degradation-config-display" />
              <span class="input-hint block">{{ t('admin.degradation.config.maxDisplayHint') }}</span>
            </label>
            <div class="md:col-span-2 md:max-w-md">
              <span class="input-label">{{ t('admin.degradation.config.defaultEffort') }}</span>
              <Select v-model="configForm.default_reasoning_effort" :options="defaultEffortOptions" data-testid="degradation-config-effort" />
              <span class="input-hint block">{{ t('admin.degradation.config.defaultEffortHint') }}</span>
            </div>
          </div>
          <div class="dgm-card-foot">
            <span v-if="configDirty" class="dgm-unsaved">{{ t('admin.degradation.config.unsaved') }}</span>
            <button type="submit" class="btn btn-primary" :disabled="savingConfig || !configDirty" data-testid="degradation-config-save">
              {{ savingConfig ? t('common.saving') : t('admin.degradation.config.save') }}
            </button>
          </div>
        </form>
      </section>
    </div>

    <DegradationMonitorFormDialog
      :show="showForm"
      :monitor="editing"
      :prompts="prompts"
      :config="savedConfig"
      @close="closeForm"
      @saved="reload(true)"
    />

    <DegradationPromptDialog
      :show="showPromptDialog"
      :prompt="editingPrompt"
      @close="showPromptDialog = false"
      @saved="onPromptSaved"
      @deleted="onPromptDeleted"
    />

    <ConfirmDialog
      :show="deleting !== null"
      :title="t('admin.degradation.deleteTitle')"
      :message="t('admin.degradation.deleteConfirm', { name: deleting ? (deleting.name || deleting.group_name) : '' })"
      :confirm-text="t('common.delete')"
      :loading="deleteBusy"
      danger
      @confirm="confirmDelete"
      @cancel="deleting = null"
    />

    <BaseDialog :show="sortPlatform !== null" :title="sortTitle" width="normal" @close="closeSort">
      <div class="space-y-4">
        <p class="text-sm text-gray-500">{{ t('admin.degradation.sortHint') }}</p>
        <SortableScrollArea :label="sortTitle">
          <VueDraggable
            v-model="sortItems"
            :animation="200"
            handle=".dgm-drag-handle"
            :scroll="true"
            :scroll-sensitivity="60"
            :scroll-speed="12"
            :force-fallback="true"
            :fallback-on-body="true"
            :fallback-tolerance="4"
            ghost-class="sort-order-ghost"
            chosen-class="sort-order-chosen"
            fallback-class="sort-order-fallback"
            class="space-y-2"
            data-testid="degradation-sort-list"
          >
            <div v-for="item in sortItems" :key="item.id" class="sort-order-item flex items-center gap-3 rounded-lg border p-3">
              <PlatformIcon :platform="sortIconKey" size="sm" class="flex-none" />
              <div class="min-w-0 flex-1">
                <div class="truncate font-medium">{{ item.name || item.group_name }}</div>
                <div class="sort-order-meta truncate text-xs">{{ item.model }}</div>
              </div>
              <span class="sort-order-meta flex-none text-xs">#{{ item.id }}</span>
              <button
                type="button"
                class="sort-order-handle dgm-drag-handle flex h-11 w-11 flex-none cursor-grab touch-none items-center justify-center rounded-md active:cursor-grabbing"
                :aria-label="t('admin.degradation.sort')"
              >
                <Icon name="menu" size="md" />
              </button>
            </div>
          </VueDraggable>
        </SortableScrollArea>
      </div>
      <template #footer>
        <div class="flex justify-end gap-3">
          <button type="button" class="btn btn-secondary" @click="closeSort">{{ t('common.cancel') }}</button>
          <button type="button" class="btn btn-primary" :disabled="sortSaving || !sortItems.length" data-testid="degradation-sort-save" @click="saveSort">
            {{ sortSaving ? t('common.saving') : t('common.save') }}
          </button>
        </div>
      </template>
    </BaseDialog>
  </AppLayout>
</template>
<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { VueDraggable } from 'vue-draggable-plus'
import { adminAPI } from '@/api/admin'
import type { DegradationConfig, DegradationEffort, DegradationMonitor, DegradationPrompt } from '@/api/admin/degradation'
import AppLayout from '@/components/layout/AppLayout.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import PlatformIcon from '@/components/common/PlatformIcon.vue'
import Select from '@/components/common/Select.vue'
import SortableScrollArea from '@/components/common/SortableScrollArea.vue'
import Icon from '@/components/icons/Icon.vue'
import DegradationMonitorFormDialog from '@/components/admin/degradation/DegradationMonitorFormDialog.vue'
import DegradationPromptDialog from '@/components/admin/degradation/DegradationPromptDialog.vue'
import { useAppStore } from '@/stores/app'
import { extractApiErrorMessage } from '@/utils/apiError'
import { DEGRADATION_PLATFORM_ORDER, degradationPlatformKey } from '@/utils/degradationPreview'
import { formatRelativeTime } from '@/utils/format'
import type { GroupPlatform } from '@/types'

type TabKey = 'monitors' | 'config'

const EFFORTS: DegradationEffort[] = ['none', 'minimal', 'low', 'medium', 'high', 'xhigh', 'max']
const POLL_INTERVAL_MS = 5000
const DEFAULT_CONFIG: DegradationConfig = {
  default_interval_minutes: 60,
  timeout_minutes: 10,
  max_retries: 1,
  max_display_per_group: 25,
  default_reasoning_effort: 'high',
  default_prompt_id: 0,
}

const { t, te } = useI18n()
const appStore = useAppStore()

const activeTab = ref<TabKey>('monitors')
const monitors = ref<DegradationMonitor[]>([])
const prompts = ref<DegradationPrompt[]>([])
const savedConfig = ref<DegradationConfig>({ ...DEFAULT_CONFIG })
const configForm = reactive<DegradationConfig>({ ...DEFAULT_CONFIG })
const featureEnabled = ref(true)
const loading = ref(false)
const loaded = ref(false)
// Platform lists start collapsed every time the page opens.
const expanded = reactive(new Set<string>())
const runningIds = reactive(new Set<number>())
const showForm = ref(false)
const editing = ref<DegradationMonitor | null>(null)
const deleting = ref<DegradationMonitor | null>(null)
const deleteBusy = ref(false)
const showPromptDialog = ref(false)
const editingPrompt = ref<DegradationPrompt | null>(null)
const savingConfig = ref(false)
const savingDefault = ref(false)
const sortPlatform = ref<string | null>(null)
const sortItems = ref<DegradationMonitor[]>([])
const sortSaving = ref(false)
const presetScroller = ref<HTMLElement | null>(null)
let pollTimer: ReturnType<typeof setTimeout> | null = null
let abortController: AbortController | null = null

const tabs = computed(() => [
  { key: 'monitors' as const, label: t('admin.degradation.tabs.monitors'), icon: 'server' as const },
  { key: 'config' as const, label: t('admin.degradation.tabs.config'), icon: 'cog' as const },
])

function platformLabel(key: string): string {
  return te(`degradationCheck.platforms.${key}`) ? t(`degradationCheck.platforms.${key}`) : key
}

function iconKeyFor(key: string): GroupPlatform {
  return (key === 'custom' ? 'composite' : key) as GroupPlatform
}

// Only platforms that own at least one check group are listed.
const platformSections = computed(() => {
  const groups = new Map<string, DegradationMonitor[]>()
  for (const monitor of monitors.value) {
    const key = degradationPlatformKey(monitor.group_platform)
    const list = groups.get(key) ?? []
    list.push(monitor)
    groups.set(key, list)
  }
  const order = [...DEGRADATION_PLATFORM_ORDER] as string[]
  const rank = (key: string) => (order.indexOf(key) === -1 ? 99 : order.indexOf(key))
  return [...groups.entries()]
    .sort(([a], [b]) => rank(a) - rank(b))
    .map(([key, items]) => ({
      key,
      iconKey: iconKeyFor(key),
      label: platformLabel(key),
      monitors: [...items].sort((a, b) => a.sort_order - b.sort_order || a.id - b.id),
      running: items.filter((item) => item.running).length,
    }))
})

const effectiveDefaultPromptID = computed(() => {
  const id = savedConfig.value.default_prompt_id
  if (id > 0 && prompts.value.some((prompt) => prompt.id === id)) return id
  return prompts.value[0]?.id ?? 0
})

// The default preset leads the list; the others keep their saved order.
const orderedPrompts = computed(() => {
  const id = effectiveDefaultPromptID.value
  const selected = prompts.value.find((prompt) => prompt.id === id)
  if (!selected) return prompts.value
  return [selected, ...prompts.value.filter((prompt) => prompt.id !== id)]
})

const defaultEffortOptions = computed(() => EFFORTS.map((effort) => ({ value: effort, label: t(`degradationCheck.efforts.${effort}`) })))

const configDirty = computed(() => (
  configForm.default_interval_minutes !== savedConfig.value.default_interval_minutes ||
  configForm.timeout_minutes !== savedConfig.value.timeout_minutes ||
  configForm.max_retries !== savedConfig.value.max_retries ||
  configForm.max_display_per_group !== savedConfig.value.max_display_per_group ||
  configForm.default_reasoning_effort !== savedConfig.value.default_reasoning_effort
))

const sortTitle = computed(() => t('admin.degradation.sortTitle', { platform: sortPlatform.value ? platformLabel(sortPlatform.value) : '' }))
const sortIconKey = computed(() => iconKeyFor(sortPlatform.value ?? 'custom'))

function effortText(monitor: DegradationMonitor): string {
  const effort = monitor.effective_reasoning_effort
  const label = te(`degradationCheck.efforts.${effort}`) ? t(`degradationCheck.efforts.${effort}`) : effort
  return monitor.reasoning_effort ? label : `${label} · ${t('admin.degradation.followDefault')}`
}

function intervalText(monitor: DegradationMonitor): string {
  const label = t('admin.degradation.everyMinutes', { n: monitor.effective_interval_minutes })
  return monitor.interval_minutes > 0 ? label : `${label} · ${t('admin.degradation.followDefault')}`
}

function isExpanded(key: string): boolean {
  return expanded.has(key)
}

function toggleExpanded(key: string) {
  if (expanded.has(key)) expanded.delete(key)
  else expanded.add(key)
}

function applyConfig(config: DegradationConfig) {
  savedConfig.value = { ...config }
  Object.assign(configForm, config)
}

// While a check runs, the list refreshes until every result is in.
function schedulePoll() {
  if (pollTimer) clearTimeout(pollTimer)
  pollTimer = null
  if (!monitors.value.some((monitor) => monitor.running) && runningIds.size === 0) return
  pollTimer = setTimeout(() => {
    if (!document.hidden) void reload(true)
    else schedulePoll()
  }, POLL_INTERVAL_MS)
}

async function reload(silent = false) {
  abortController?.abort()
  const controller = new AbortController()
  abortController = controller
  if (!silent) loading.value = true
  try {
    const data = await adminAPI.degradation.listMonitors({ signal: controller.signal })
    if (controller.signal.aborted) return
    monitors.value = data.items || []
    featureEnabled.value = data.enabled
    for (const id of [...runningIds]) {
      if (!monitors.value.find((monitor) => monitor.id === id)?.running) runningIds.delete(id)
    }
    if (!configDirty.value) applyConfig(data.config)
    else savedConfig.value = { ...data.config }
    loaded.value = true
  } catch (err: unknown) {
    const requestError = err as { name?: string; code?: string }
    if (requestError?.name === 'AbortError' || requestError?.code === 'ERR_CANCELED') return
    appStore.showError(extractApiErrorMessage(err, t('admin.degradation.loadFailed')))
  } finally {
    if (abortController === controller) {
      loading.value = false
      abortController = null
      schedulePoll()
    }
  }
}

async function loadPrompts() {
  try {
    prompts.value = await adminAPI.degradation.listPrompts()
  } catch (err: unknown) {
    appStore.showError(extractApiErrorMessage(err, t('admin.degradation.loadFailed')))
  }
}

function openCreate() {
  editing.value = null
  showForm.value = true
}

function openEdit(monitor: DegradationMonitor) {
  editing.value = monitor
  showForm.value = true
}

function closeForm() {
  showForm.value = false
  editing.value = null
}

async function runNow(monitor: DegradationMonitor) {
  if (runningIds.has(monitor.id)) return
  runningIds.add(monitor.id)
  try {
    await adminAPI.degradation.runMonitor(monitor.id)
    monitor.running = true
    appStore.showSuccess(t('admin.degradation.runQueued'))
    schedulePoll()
  } catch (err: unknown) {
    runningIds.delete(monitor.id)
    appStore.showError(extractApiErrorMessage(err, t('admin.degradation.runFailed')))
  }
}

async function confirmDelete() {
  const target = deleting.value
  if (!target || deleteBusy.value) return
  deleteBusy.value = true
  try {
    await adminAPI.degradation.deleteMonitor(target.id)
    appStore.showSuccess(t('admin.degradation.deleted'))
    deleting.value = null
    await reload(true)
  } catch (err: unknown) {
    appStore.showError(extractApiErrorMessage(err, t('admin.degradation.deleteFailed')))
  } finally {
    deleteBusy.value = false
  }
}

function openPrompt(prompt: DegradationPrompt | null) {
  editingPrompt.value = prompt
  showPromptDialog.value = true
}

function scrollBehavior(): 'auto' | 'smooth' {
  return window.matchMedia('(prefers-reduced-motion: reduce)').matches ? 'auto' : 'smooth'
}

// New presets land after the five visible rows, so bring the saved one into view.
async function onPromptSaved(prompt: DegradationPrompt) {
  await loadPrompts()
  await nextTick()
  presetScroller.value?.querySelector<HTMLElement>(`[data-preset-id="${prompt.id}"]`)?.scrollIntoView({ block: 'nearest', behavior: scrollBehavior() })
}

async function onPromptDeleted() {
  await Promise.all([loadPrompts(), reload(true)])
}

// Choosing the default preset applies immediately; the settings form below
// keeps its own save button so unsaved edits there are never sent here.
async function setDefaultPrompt(id: number) {
  if (savingDefault.value || id === effectiveDefaultPromptID.value) return
  savingDefault.value = true
  try {
    const saved = await adminAPI.degradation.updateConfig({ ...savedConfig.value, default_prompt_id: id })
    savedConfig.value = { ...saved }
    configForm.default_prompt_id = saved.default_prompt_id
    appStore.showSuccess(t('admin.degradation.config.saved'))
    // The new default moves to the top of the list; scroll up to show it there.
    await nextTick()
    const scroller = presetScroller.value
    if (scroller && scroller.scrollTop > 0) scroller.scrollTo({ top: 0, behavior: scrollBehavior() })
  } catch (err: unknown) {
    appStore.showError(extractApiErrorMessage(err, t('admin.degradation.config.saveFailed')))
  } finally {
    savingDefault.value = false
  }
}

function validConfig(config: DegradationConfig): boolean {
  const integer = (value: number, min: number, max: number) => Number.isInteger(value) && value >= min && value <= max
  return integer(config.default_interval_minutes, 5, 10080) &&
    integer(config.timeout_minutes, 1, 60) &&
    integer(config.max_retries, 0, 5) &&
    integer(config.max_display_per_group, 1, 100) &&
    EFFORTS.includes(config.default_reasoning_effort)
}

async function saveConfig() {
  if (savingConfig.value) return
  const payload: DegradationConfig = { ...configForm, default_prompt_id: savedConfig.value.default_prompt_id }
  if (!validConfig(payload)) {
    appStore.showError(t('admin.degradation.config.invalid'))
    return
  }
  savingConfig.value = true
  try {
    applyConfig(await adminAPI.degradation.updateConfig(payload))
    appStore.showSuccess(t('admin.degradation.config.saved'))
    void reload(true)
  } catch (err: unknown) {
    appStore.showError(extractApiErrorMessage(err, t('admin.degradation.config.saveFailed')))
  } finally {
    savingConfig.value = false
  }
}

function openSort(platform: string) {
  sortPlatform.value = platform
  sortItems.value = [...(platformSections.value.find((section) => section.key === platform)?.monitors ?? [])]
}

function closeSort() {
  sortPlatform.value = null
  sortItems.value = []
}

async function saveSort() {
  if (sortSaving.value || !sortItems.value.length) return
  sortSaving.value = true
  try {
    await adminAPI.degradation.updateSortOrder(sortItems.value.map((item, index) => ({ id: item.id, sort_order: (index + 1) * 10 })))
    appStore.showSuccess(t('admin.degradation.sortSaved'))
    closeSort()
    await reload(true)
  } catch (err: unknown) {
    appStore.showError(extractApiErrorMessage(err, t('admin.degradation.sortFailed')))
  } finally {
    sortSaving.value = false
  }
}

onMounted(() => {
  void reload()
  void loadPrompts()
})

onBeforeUnmount(() => {
  abortController?.abort()
  if (pollTimer) clearTimeout(pollTimer)
})
</script>
<style scoped>
.dgm-page {
  display: flex;
  flex-direction: column;
  gap: 1.25rem;
}

.dgm-toolbar {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 0.75rem;
}

.dgm-tabs .dgm-tab {
  display: inline-flex;
  align-items: center;
  gap: 0.375rem;
  padding: 0.5rem 1.125rem;
  border-radius: 999px;
  font-size: 0.875rem;
  transition: background-color 160ms ease, color 160ms ease;
}

.dgm-toolbar-actions {
  display: flex;
  gap: 0.5rem;
}

.dgm-notice {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 0.5rem;
  padding: 0.75rem 1rem;
  border-radius: 1rem;
  background: #fff3bd;
  font-size: 0.8125rem;
  color: #65460b;
}

.dgm-notice-link {
  font-weight: 650;
  text-decoration: underline;
  text-underline-offset: 3px;
}

.dgm-empty {
  display: flex;
  min-height: 10rem;
  align-items: center;
  justify-content: center;
}

.dgm-platform {
  --dgm-accent: 201 154 18;
  overflow: hidden;
}

.dgm-platform.is-openai { --dgm-accent: 16 163 127; }
.dgm-platform.is-anthropic { --dgm-accent: 217 119 87; }
.dgm-platform.is-gemini { --dgm-accent: 66 133 244; }
.dgm-platform.is-grok { --dgm-accent: 82 82 91; }
.dgm-platform.is-custom { --dgm-accent: 120 113 108; }

.dgm-platform-head {
  display: flex;
  align-items: center;
  gap: 0.75rem;
  padding: 0.75rem 1rem;
}

.dgm-platform-toggle {
  display: flex;
  min-width: 0;
  flex: 1 1 auto;
  flex-wrap: wrap;
  align-items: center;
  gap: 0.5rem;
  border-radius: 0.75rem;
  text-align: left;
}

.dgm-platform-toggle:focus-visible {
  outline: 2px solid var(--md-sys-color-primary);
  outline-offset: 4px;
}

.dgm-chevron {
  color: var(--md-sys-color-on-surface-variant);
  transition: transform 200ms ease;
}

.dgm-chevron.is-open { transform: rotate(90deg); }

.dgm-platform-icon {
  display: inline-flex;
  width: 2rem;
  height: 2rem;
  align-items: center;
  justify-content: center;
  border-radius: 0.75rem;
  background: rgb(var(--dgm-accent) / 0.12);
  color: rgb(var(--dgm-accent));
}

.dgm-platform-name {
  font-size: 0.9375rem;
  font-weight: 700;
  color: var(--md-sys-color-on-surface);
}

.dgm-platform-count {
  padding: 0.0625rem 0.5rem;
  border-radius: 999px;
  background: rgb(var(--dgm-accent) / 0.1);
  font-size: 0.75rem;
  font-weight: 600;
  color: rgb(var(--dgm-accent));
}

.dgm-platform-running {
  display: inline-flex;
  align-items: center;
  gap: 0.375rem;
  font-size: 0.75rem;
  color: #7e5600;
}

.dgm-rows {
  display: flex;
  flex-direction: column;
  padding: 0 0.75rem 0.75rem;
}

.dgm-row {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto auto;
  align-items: center;
  gap: 1rem;
  padding: 0.875rem 0.75rem;
  border-radius: 1rem;
  transition: background-color 160ms ease;
}

.dgm-row + .dgm-row { box-shadow: inset 0 1px 0 rgb(215 193 106 / 0.22); }
.dgm-row:hover { background: color-mix(in srgb, var(--md-sys-color-surface-container) 55%, transparent); }

.dgm-row-main { min-width: 0; }

.dgm-row-title {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 0.375rem;
}

.dgm-row-name {
  font-size: 0.875rem;
  font-weight: 700;
  color: var(--md-sys-color-on-surface);
  overflow-wrap: anywhere;
}

.dgm-row-meta {
  display: flex;
  flex-wrap: wrap;
  gap: 0.375rem;
  margin-top: 0.375rem;
}

.dgm-chip {
  display: inline-flex;
  max-width: 100%;
  padding: 0.0625rem 0.5rem;
  border-radius: 999px;
  background: var(--md-sys-color-surface-container);
  font-size: 0.6875rem;
  font-weight: 600;
  color: var(--md-sys-color-on-surface-variant);
  overflow-wrap: anywhere;
}

.dgm-chip.is-model {
  background: rgb(var(--dgm-accent) / 0.12);
  color: rgb(var(--dgm-accent));
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
}

.dgm-badge {
  display: inline-flex;
  padding: 0 0.4375rem;
  border-radius: 999px;
  background: var(--md-sys-color-surface-container);
  font-size: 0.6875rem;
  font-weight: 600;
  color: var(--md-sys-color-on-surface-variant);
}

.dgm-badge.is-muted { background: #f1eee4; color: #857a5c; }
.dgm-badge.is-warn { background: #fff3bd; color: #7e5600; }
.dgm-badge.is-danger { background: #fdf1ec; color: #a2402c; }
.dgm-badge.is-primary { background: var(--md-sys-color-primary); color: var(--md-sys-color-on-primary); }

.dgm-row-status {
  display: flex;
  min-width: 6.5rem;
  flex-direction: column;
  align-items: flex-end;
  gap: 0.125rem;
}

.dgm-status {
  display: inline-flex;
  align-items: center;
  gap: 0.375rem;
  font-size: 0.75rem;
  font-weight: 650;
}

.dgm-status small {
  font-weight: 500;
  color: var(--md-sys-color-on-surface-variant);
}

.dgm-status.is-success { color: #2e7d4f; }
.dgm-status.is-failed { color: #b4513d; }
.dgm-status.is-running { color: #7e5600; }
.dgm-status.is-idle { color: var(--md-sys-color-on-surface-variant); font-weight: 500; }

.dgm-stored {
  font-size: 0.6875rem;
  color: var(--md-sys-color-on-surface-variant);
}

.dgm-row-actions {
  display: flex;
  gap: 0.25rem;
}

.dgm-action {
  display: inline-flex;
  width: 2.25rem;
  height: 2.25rem;
  flex-shrink: 0;
  align-items: center;
  justify-content: center;
  border-radius: 0.75rem;
  color: var(--md-sys-color-on-surface-variant);
  transition: background-color 160ms ease, color 160ms ease;
}

.dgm-action:hover:not(:disabled) {
  background: var(--md-sys-color-surface-container-high);
  color: var(--md-sys-color-on-surface);
}

.dgm-action.is-danger:hover:not(:disabled) {
  background: #fdf1ec;
  color: #a2402c;
}

.dgm-action:disabled {
  cursor: not-allowed;
  opacity: 0.45;
}

.dgm-pulse {
  width: 0.5rem;
  height: 0.5rem;
  border-radius: 999px;
  background: var(--md-sys-color-primary);
  animation: dgm-pulse 1.2s ease-in-out infinite;
}

.dgm-card-head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 1rem;
  padding: 1rem 1.5rem;
  box-shadow: inset 0 -1px 0 rgb(215 193 106 / 0.22);
}

.dgm-card-title {
  font-size: 1.0625rem;
  font-weight: 700;
  color: var(--md-sys-color-on-surface);
}

.dgm-card-hint {
  margin-top: 0.25rem;
  font-size: 0.8125rem;
  color: var(--md-sys-color-on-surface-variant);
}

.dgm-card-body { padding: 1.25rem 1.5rem; }

.dgm-card-foot {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 0.75rem;
  padding: 0 1.5rem 1.25rem;
}

.dgm-unsaved {
  font-size: 0.75rem;
  color: #7e5600;
}

.dgm-title-row {
  display: flex;
  align-items: center;
  gap: 0.5rem;
}

/* Rows share one height so the list always ends right after the fifth row. */
.dgm-presets-scroll {
  --dgm-preset-row: 5.5rem;
  --dgm-preset-gap: 0.5rem;
  --dgm-presets-pad: 1rem;
  max-height: calc(var(--dgm-presets-pad) + (var(--dgm-preset-row) + var(--dgm-preset-gap)) * 5);
  margin-bottom: 0.75rem;
  padding: var(--dgm-presets-pad) 1.25rem 0.5rem;
  overflow-y: auto;
}

.dgm-presets {
  display: flex;
  flex-direction: column;
  gap: var(--dgm-preset-gap);
}

.dgm-preset {
  display: flex;
  height: var(--dgm-preset-row);
  flex-shrink: 0;
  align-items: center;
  gap: 0.5rem;
  padding: 0.25rem 0.25rem 0.25rem 0;
  border-radius: 1rem;
  background: var(--md-sys-color-surface-container);
  transition: box-shadow 160ms ease, background-color 160ms ease;
}

.dgm-preset-move {
  transition: transform 280ms var(--theme-ease-out, ease), box-shadow 160ms ease, background-color 160ms ease;
}

.dgm-preset.is-default {
  background: var(--md-sys-color-surface);
  box-shadow: inset 0 0 0 2px var(--md-sys-color-primary);
}

.dgm-preset-select {
  display: flex;
  min-width: 0;
  flex: 1 1 auto;
  align-self: stretch;
  align-items: center;
  padding: 0 0.75rem;
  border-radius: 1rem;
  text-align: left;
}

.dgm-preset-select:focus-visible {
  outline: 2px solid var(--md-sys-color-primary);
  outline-offset: 2px;
}

.dgm-preset-body {
  display: flex;
  min-width: 0;
  flex: 1 1 auto;
  align-items: flex-start;
  gap: 0.75rem;
}

.dgm-radio {
  width: 1.125rem;
  height: 1.125rem;
  flex-shrink: 0;
  margin-top: 0.125rem;
  border: 2px solid var(--md-sys-color-outline);
  border-radius: 999px;
  background: var(--md-sys-color-surface);
}

.dgm-preset.is-default .dgm-radio {
  border-color: var(--md-sys-color-primary);
  background: var(--md-sys-color-primary);
  box-shadow: inset 0 0 0 3px var(--md-sys-color-surface);
}

.dgm-preset-text {
  display: flex;
  min-width: 0;
  flex: 1 1 auto;
  flex-direction: column;
  gap: 0.25rem;
}

.dgm-preset-name {
  display: flex;
  min-width: 0;
  align-items: center;
  gap: 0.375rem;
  font-size: 0.875rem;
  font-weight: 700;
  line-height: 1.375rem;
  color: var(--md-sys-color-on-surface);
}

.dgm-preset-label {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.dgm-preset-name .dgm-badge {
  flex-shrink: 0;
  line-height: 1.5;
}

.dgm-preset-prompt {
  display: -webkit-box;
  overflow: hidden;
  font-size: 0.75rem;
  line-height: 1.125rem;
  color: var(--md-sys-color-on-surface-variant);
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
}

@keyframes dgm-pulse {
  0%, 100% { opacity: 1; }
  50% { opacity: 0.35; }
}

@media (max-width: 767px) {
  .dgm-row {
    grid-template-columns: minmax(0, 1fr) auto;
    grid-template-areas:
      'main main'
      'status actions';
    gap: 0.625rem;
  }

  .dgm-row-main { grid-area: main; }
  .dgm-row-status { grid-area: status; min-width: 0; align-items: flex-start; }
  .dgm-row-actions { grid-area: actions; }
  .dgm-platform-head { padding: 0.75rem; }
  .dgm-card-head { padding: 1rem; }
  .dgm-card-body { padding: 1rem; }
  .dgm-card-foot { padding: 0 1rem 1rem; }
  .dgm-presets-scroll { --dgm-presets-pad: 0.75rem; padding-inline: 0.75rem; }
  .dgm-toolbar { align-items: stretch; }
  .dgm-tabs { width: 100%; }
  .dgm-tabs .dgm-tab { flex: 1 1 0; justify-content: center; }
  .dgm-toolbar-actions { width: 100%; }
  .dgm-toolbar-actions .btn-primary { flex: 1 1 auto; }
}

@media (prefers-reduced-motion: reduce) {
  .dgm-chevron,
  .dgm-pulse { transition: none; animation: none; }
  .dgm-preset-move { transition: none; }
}
</style>
