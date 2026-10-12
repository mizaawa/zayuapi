<template>
  <AppLayout :class="{ 'channel-monitor-sort-open': showSortDialog }">
    <TablePageLayout>
      <template #filters>
        <MonitorFiltersBar
          v-model:search="searchQuery"
          v-model:provider="providerFilter"
          v-model:enabled="enabledFilter"
          :loading="loading"
          @reload="reload"
          @create="openCreateDialog"
          @manage-templates="showTemplateManager = true"
          @sort="openSortDialog"
          @search-input="handleSearch"
        />
      </template>

      <template #table>
        <DataTable
          :columns="columns"
          :data="monitors"
          :loading="loading"
          :row-class="monitorRowClass"
          :row-style="monitorRowStyle"
        >
          <template #cell-name="{ row, value }">
            <div class="flex min-w-0 max-w-full items-center gap-1.5">
              <span class="min-w-0 break-words font-medium text-gray-900 dark:text-white">{{ value || row.group_name }}</span>
              <HelpTooltip v-if="!row.group_id && row.api_key_decrypt_failed" :content="t('admin.channelMonitor.apiKeyDecryptFailed')">
                <Icon name="exclamationTriangle" size="sm" class="text-red-500" />
              </HelpTooltip>
            </div>
          </template>

          <template #cell-provider="{ row }">
            <span class="inline-flex items-center rounded-md px-2 py-0.5 text-xs font-medium" :class="providerBadgeClass(row.provider)">
              {{ providerLabel(row.provider) }}
            </span>
          </template>

          <template #cell-group_name="{ row }">
            <MonitorGroupLabel
              v-if="row.group_name"
              :name="row.group_name"
              :rate-multiplier="row.group_rate_multiplier"
            />
            <span v-else class="text-sm text-gray-400">-</span>
          </template>

          <template #cell-primary_model="{ row }">
            <MonitorPrimaryModelCell :row="row" />
          </template>

          <template #cell-availability_7d="{ row }">
            <span class="text-sm text-gray-900 dark:text-gray-100">{{ formatAvailability(row) }}</span>
          </template>

          <template #cell-latency="{ row }">
            <span class="text-sm text-gray-900 dark:text-gray-100">{{ formatLatency(row.primary_latency_ms) }}</span>
          </template>

          <template #cell-enabled="{ row }">
            <Toggle :modelValue="row.enabled" @update:modelValue="toggleEnabled(row)" />
          </template>

          <template #cell-actions="{ row }">
            <MonitorActionsCell
              :row="row"
              :running="runningId === row.id"
              :duplicating="duplicatingIds.has(row.id)"
              @run="handleRunNow"
              @duplicate="handleDuplicate"
              @edit="openEditDialog"
              @delete="handleDelete"
            />
          </template>

          <template #empty>
            <EmptyState
              :title="t('admin.channelMonitor.noMonitorsYet')"
              :description="t('admin.channelMonitor.createFirstMonitor')"
              :action-text="t('admin.channelMonitor.createButton')"
              @action="openCreateDialog"
            />
          </template>
        </DataTable>
      </template>

      <template #pagination>
        <Pagination
          v-if="pagination.total > 0"
          :page="pagination.page"
          :total="pagination.total"
          :page-size="pagination.page_size"
          @update:page="onPageChange"
          @update:pageSize="onPageSizeChange"
        />
      </template>
    </TablePageLayout>

    <MonitorFormDialog
      :show="showDialog"
      :monitor="editing"
      @close="closeDialog"
      @saved="reload"
    />

    <MonitorTemplateManagerDialog
      :show="showTemplateManager"
      @close="showTemplateManager = false"
      @updated="reload"
    />

    <MonitorRunResultDialog
      :show="showRunResult"
      :results="runResults"
      @close="showRunResult = false"
    />

    <BaseDialog
      :show="showSortDialog"
      :title="t('admin.channelMonitor.sortOrder')"
      width="normal"
      @close="closeSortDialog"
    >
      <div class="space-y-4">
        <p class="text-sm text-gray-500 dark:text-gray-400">
          {{ t('admin.channelMonitor.sortOrderHint') }}
        </p>

        <div
          v-if="sortLoading"
          class="flex min-h-40 items-center justify-center gap-2 text-sm text-gray-500 dark:text-gray-400"
          data-testid="channel-monitor-sort-loading"
        >
          <Icon name="refresh" size="md" class="animate-spin" />
          {{ t('common.loading') }}
        </div>

        <SortableScrollArea v-else :label="t('admin.channelMonitor.sortOrder')">
          <VueDraggable
            v-if="sortableMonitors.length > 0"
            v-model="sortableMonitors"
            :animation="200"
            handle=".channel-monitor-drag-handle"
            :scroll="true"
            :scroll-sensitivity="60"
            :scroll-speed="12"
            :force-fallback="true"
            :fallback-on-body="true"
            :fallback-tolerance="4"
            ghost-class="sort-order-ghost"
            chosen-class="sort-order-chosen"
            fallback-class="sort-order-fallback"
            class="channel-monitor-sort-list space-y-2"
            data-testid="channel-monitor-sort-list"
          >
            <div
              v-for="monitor in sortableMonitors"
              :key="monitor.id"
              :data-monitor-id="monitor.id"
              class="sort-order-item channel-monitor-sort-item flex items-center gap-2 rounded-lg border p-3 transition-shadow hover:shadow-md sm:gap-3"
            >
              <ProviderIcon :provider="monitor.provider" :size="18" class="flex-none" />
              <div class="min-w-0 flex-1">
                <div class="flex min-w-0 items-center gap-2">
                  <div class="truncate font-medium">
                    {{ monitor.name || monitor.group_name }}
                  </div>
                  <span class="sort-order-meta hidden flex-none text-xs sm:inline">
                    {{ providerLabel(monitor.provider) }}
                  </span>
                </div>
                <div class="sort-order-meta truncate text-xs">
                  {{ monitor.primary_model }}
                </div>
              </div>
              <span class="sort-order-meta flex-none text-xs sm:text-sm">#{{ monitor.id }}</span>
              <button
                type="button"
                class="sort-order-handle channel-monitor-drag-handle flex h-11 w-11 flex-none cursor-grab touch-none items-center justify-center rounded-md transition-colors active:cursor-grabbing"
                :title="t('admin.channelMonitor.sortOrder')"
                :aria-label="t('admin.channelMonitor.sortOrder')"
              >
                <Icon name="menu" size="md" />
              </button>
            </div>
          </VueDraggable>

          <div
            v-else
            class="rounded-lg border border-dashed border-gray-200 px-3 py-2 text-sm text-gray-400 dark:border-dark-600 dark:text-gray-500"
          >
            {{ t('admin.channelMonitor.noMonitorsYet') }}
          </div>
        </SortableScrollArea>
      </div>

      <template #footer>
        <div class="flex justify-end gap-3 pt-4">
          <button type="button" class="btn btn-secondary" @click="closeSortDialog">
            {{ t('common.cancel') }}
          </button>
          <button
            data-testid="channel-monitor-sort-save"
            class="btn btn-primary"
            :disabled="sortLoading || sortSubmitting || sortableCount === 0"
            @click="saveSortOrder"
          >
            <Icon v-if="sortSubmitting" name="refresh" size="sm" class="mr-2 animate-spin" />
            {{ sortSubmitting ? t('common.saving') : t('common.save') }}
          </button>
        </div>
      </template>
    </BaseDialog>

    <ConfirmDialog
      :show="showDeleteDialog"
      :title="t('common.delete')"
      :message="deleteConfirmMessage"
      :confirm-text="t('common.delete')"
      :cancel-text="t('common.cancel')"
      :danger="true"
      @confirm="confirmDelete"
      @cancel="showDeleteDialog = false"
    />
  </AppLayout>
</template>

<script setup lang="ts">
import { ref, reactive, computed, onMounted, onUnmounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { extractApiErrorMessage } from '@/utils/apiError'
import { adminAPI } from '@/api/admin'
import type {
  ChannelMonitor,
  CheckResult,
  ListParams,
  Provider,
} from '@/api/admin/channelMonitor'
import type { Column } from '@/components/common/types'
import AppLayout from '@/components/layout/AppLayout.vue'
import TablePageLayout from '@/components/layout/TablePageLayout.vue'
import DataTable from '@/components/common/DataTable.vue'
import Pagination from '@/components/common/Pagination.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import HelpTooltip from '@/components/common/HelpTooltip.vue'
import Icon from '@/components/icons/Icon.vue'
import Toggle from '@/components/common/Toggle.vue'
import MonitorFiltersBar from '@/components/admin/monitor/MonitorFiltersBar.vue'
import MonitorFormDialog from '@/components/admin/monitor/MonitorFormDialog.vue'
import MonitorTemplateManagerDialog from '@/components/admin/monitor/MonitorTemplateManagerDialog.vue'
import MonitorRunResultDialog from '@/components/admin/monitor/MonitorRunResultDialog.vue'
import MonitorPrimaryModelCell from '@/components/admin/monitor/MonitorPrimaryModelCell.vue'
import MonitorActionsCell from '@/components/admin/monitor/MonitorActionsCell.vue'
import MonitorGroupLabel from '@/components/admin/monitor/MonitorGroupLabel.vue'
import ProviderIcon from '@/components/user/monitor/ProviderIcon.vue'
import { getPersistedPageSize } from '@/composables/usePersistedPageSize'
import { useChannelMonitorFormat } from '@/composables/useChannelMonitorFormat'
import { PROVIDERS } from '@/constants/channelMonitor'
import { VueDraggable } from 'vue-draggable-plus'
import SortableScrollArea from '@/components/common/SortableScrollArea.vue'

const { t } = useI18n()
const appStore = useAppStore()
const {
  providerLabel,
  providerBadgeClass,
  formatLatency,
  formatAvailability,
} = useChannelMonitorFormat()

const monitors = ref<ChannelMonitor[]>([])
const loading = ref(false)
const runningId = ref<number | null>(null)
const searchQuery = ref('')
const providerFilter = ref<Provider | ''>('')
const enabledFilter = ref<'' | 'true' | 'false'>('')
const pagination = reactive({ page: 1, page_size: getPersistedPageSize(), total: 0 })

const showDialog = ref(false)
const showTemplateManager = ref(false)
const editing = ref<ChannelMonitor | null>(null)
const showDeleteDialog = ref(false)
const deleting = ref<ChannelMonitor | null>(null)
const showRunResult = ref(false)
const runResults = ref<CheckResult[]>([])
const duplicatingIds = reactive(new Set<number>())
const showSortDialog = ref(false)
const sortLoading = ref(false)
const sortSubmitting = ref(false)

const sortableMonitors = ref<ChannelMonitor[]>([])
const sortableCount = computed(() => sortableMonitors.value.length)

let abortController: AbortController | null = null
let searchTimeout: ReturnType<typeof setTimeout> | null = null
let sortLoadGeneration = 0

const SORT_PAGE_SIZE = 100

const columns = computed<Column[]>(() => [
  { key: 'name', label: t('admin.channelMonitor.columns.name'), sortable: false },
  { key: 'provider', label: t('admin.channelMonitor.columns.provider'), sortable: false },
  { key: 'group_name', label: t('admin.channelMonitor.columns.group'), sortable: false },
  { key: 'primary_model', label: t('admin.channelMonitor.columns.primaryModel'), sortable: false },
  { key: 'availability_7d', label: t('admin.channelMonitor.columns.availability7d'), sortable: false },
  { key: 'latency', label: t('admin.channelMonitor.columns.latency'), sortable: false },
  { key: 'enabled', label: t('admin.channelMonitor.columns.enabled'), sortable: false },
  { key: 'actions', label: t('admin.channelMonitor.columns.actions'), sortable: false },
])

const deleteConfirmMessage = computed(() => {
  const name = deleting.value?.name || deleting.value?.group_name || ''
  return t('admin.channelMonitor.deleteConfirm', { name })
})

function monitorRowClass(row: ChannelMonitor): string[] {
  return ['channel-monitor-glow', `channel-monitor-glow-${row.provider || 'neutral'}`]
}

function monitorRowStyle(row: ChannelMonitor): Record<string, string> {
  const seed = Math.abs(Number(row.id) || 1)
  return {
    '--monitor-glow-delay': `${-((seed * 137) % 3100)}ms`,
    '--monitor-glow-duration': `${3600 + ((seed * 193) % 1900)}ms`,
  }
}

async function reload() {
  if (abortController) abortController.abort()
  const ctrl = new AbortController()
  abortController = ctrl
  loading.value = true
  try {
    const params: ListParams = {
      page: pagination.page,
      page_size: pagination.page_size,
    }
    if (providerFilter.value) params.provider = providerFilter.value
    if (enabledFilter.value === 'true') params.enabled = true
    if (enabledFilter.value === 'false') params.enabled = false
    if (searchQuery.value.trim()) params.search = searchQuery.value.trim()

    const res = await adminAPI.channelMonitor.list(params, { signal: ctrl.signal })
    if (ctrl.signal.aborted || abortController !== ctrl) return
    monitors.value = res.items || []
    pagination.total = res.total
  } catch (err: unknown) {
    const e = err as { name?: string; code?: string }
    if (e?.name === 'AbortError' || e?.code === 'ERR_CANCELED') return
    appStore.showError(extractApiErrorMessage(err, t('admin.channelMonitor.loadError')))
  } finally {
    if (abortController === ctrl) {
      loading.value = false
      abortController = null
    }
  }
}

function handleSearch() {
  if (searchTimeout) clearTimeout(searchTimeout)
  searchTimeout = setTimeout(() => {
    pagination.page = 1
    reload()
  }, 300)
}

function onPageChange(page: number) {
  pagination.page = page
  reload()
}

function onPageSizeChange(size: number) {
  pagination.page_size = size
  pagination.page = 1
  reload()
}

function openCreateDialog() {
  editing.value = null
  showDialog.value = true
}

function openEditDialog(row: ChannelMonitor) {
  editing.value = row
  showDialog.value = true
}

function closeDialog() {
  showDialog.value = false
  editing.value = null
}

async function loadAllMonitorsForSort(): Promise<ChannelMonitor[]> {
  const monitorsByID = new Map<number, ChannelMonitor>()
  let page = 1
  let pages = 1

  do {
    const response = await adminAPI.channelMonitor.list({ page, page_size: SORT_PAGE_SIZE })
    for (const monitor of response.items || []) monitorsByID.set(monitor.id, monitor)
    pages = Math.max(
      1,
      response.pages || Math.ceil(response.total / Math.max(1, response.page_size))
    )
    page += 1
  } while (page <= pages)

  return [...monitorsByID.values()].sort(
    (left, right) => left.sort_order - right.sort_order || left.id - right.id
  )
}

async function openSortDialog() {
  const generation = ++sortLoadGeneration
  showSortDialog.value = true
  sortLoading.value = true
  sortableMonitors.value = []

  try {
    const allMonitors = await loadAllMonitorsForSort()
    if (generation !== sortLoadGeneration) return

    sortableMonitors.value = allMonitors.filter(monitor => PROVIDERS.includes(monitor.provider))
  } catch (err: unknown) {
    if (generation !== sortLoadGeneration) return
    appStore.showError(extractApiErrorMessage(err, t('admin.channelMonitor.sortOrderLoadError')))
    closeSortDialog()
  } finally {
    if (generation === sortLoadGeneration) sortLoading.value = false
  }
}

function closeSortDialog() {
  sortLoadGeneration += 1
  showSortDialog.value = false
  sortLoading.value = false
  sortableMonitors.value = []
}

async function saveSortOrder() {
  if (sortSubmitting.value || sortableCount.value === 0) return

  const updates = sortableMonitors.value.map((monitor, index) => ({
    id: monitor.id,
    sort_order: index * 10,
  }))

  sortSubmitting.value = true
  try {
    await adminAPI.channelMonitor.updateSortOrder(updates)
    appStore.showSuccess(t('admin.channelMonitor.sortOrderUpdated'))
    closeSortDialog()
    void reload()
  } catch (err: unknown) {
    appStore.showError(extractApiErrorMessage(err, t('admin.channelMonitor.sortOrderUpdateError')))
  } finally {
    sortSubmitting.value = false
  }
}

async function toggleEnabled(row: ChannelMonitor) {
  const next = !row.enabled
  try {
    await adminAPI.channelMonitor.update(row.id, { enabled: next })
    row.enabled = next
  } catch (err: unknown) {
    appStore.showError(extractApiErrorMessage(err, t('common.error')))
  }
}

async function handleRunNow(row: ChannelMonitor) {
  if (runningId.value != null) return
  runningId.value = row.id
  try {
    const res = await adminAPI.channelMonitor.runNow(row.id)
    runResults.value = res.results || []
    showRunResult.value = true
    appStore.showSuccess(t('admin.channelMonitor.runSuccess'))
    // Refresh row to get latest status from backend
    void reload()
  } catch (err: unknown) {
    appStore.showError(extractApiErrorMessage(err, t('admin.channelMonitor.runFailed')))
  } finally {
    runningId.value = null
  }
}

async function handleDuplicate(row: ChannelMonitor) {
  if (!row.group_id && row.api_key_decrypt_failed) {
    appStore.showError(t('admin.channelMonitor.duplicateKeyUnavailable'))
    return
  }
  if (duplicatingIds.has(row.id)) return

  duplicatingIds.add(row.id)
  try {
    const duplicate = await adminAPI.channelMonitor.duplicate(row.id)
    appStore.showSuccess(t('admin.channelMonitor.duplicateSuccess', { name: duplicate.name }))
    await reload()
  } catch (err: unknown) {
    appStore.showError(extractApiErrorMessage(err, t('admin.channelMonitor.duplicateFailed')))
  } finally {
    duplicatingIds.delete(row.id)
  }
}

function handleDelete(row: ChannelMonitor) {
  deleting.value = row
  showDeleteDialog.value = true
}

async function confirmDelete() {
  if (!deleting.value) return
  try {
    await adminAPI.channelMonitor.del(deleting.value.id)
    appStore.showSuccess(t('admin.channelMonitor.deleteSuccess'))
    showDeleteDialog.value = false
    deleting.value = null
    reload()
  } catch (err: unknown) {
    appStore.showError(extractApiErrorMessage(err, t('common.error')))
  }
}

onMounted(reload)
onUnmounted(() => {
  if (searchTimeout) clearTimeout(searchTimeout)
  abortController?.abort()
})
</script>

<style scoped>
:global(.channel-monitor-sort-open .channel-monitor-glow) {
  animation-play-state: paused;
}

:deep(.channel-monitor-glow) {
  --monitor-glow-color: 57 197 187;
  animation: channel-monitor-pulse var(--monitor-glow-duration) ease-in-out infinite;
  animation-delay: var(--monitor-glow-delay);
}
:deep(.channel-monitor-glow-openai) { --monitor-glow-color: 16 185 129; }
:deep(.channel-monitor-glow-anthropic) { --monitor-glow-color: 249 115 22; }
:deep(.channel-monitor-glow-gemini) { --monitor-glow-color: 14 165 233; }
:deep(.channel-monitor-glow-grok) { --monitor-glow-color: 148 163 184; }
:deep(.channel-monitor-glow-custom) { --monitor-glow-color: 107 114 128; }

@keyframes channel-monitor-pulse {
  0%, 100% { box-shadow: 0 0 0 rgb(var(--monitor-glow-color) / 0); }
  42% { box-shadow: 0 0 1.1rem rgb(var(--monitor-glow-color) / 0.08); }
  67% { box-shadow: 0 0 0.45rem rgb(var(--monitor-glow-color) / 0.035); }
}

:global(.dark) :deep(.channel-monitor-glow) {
  animation-name: channel-monitor-pulse-dark;
}

@keyframes channel-monitor-pulse-dark {
  0%, 100% { box-shadow: 0 0 0 rgb(var(--monitor-glow-color) / 0); }
  42% { box-shadow: 0 0 1.25rem rgb(var(--monitor-glow-color) / 0.16); }
  67% { box-shadow: 0 0 0.55rem rgb(var(--monitor-glow-color) / 0.07); }
}

@media (prefers-reduced-motion: reduce) {
  :deep(.channel-monitor-glow) { animation: none; }
}
</style>
