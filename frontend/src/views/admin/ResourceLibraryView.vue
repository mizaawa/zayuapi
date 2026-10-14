<template>
  <AppLayout>
    <div class="mx-auto max-w-[1600px] space-y-5">
      <!-- Header -->
      <div class="card p-4 sm:p-6">
        <div class="flex flex-wrap items-start justify-between gap-3">
          <div class="min-w-0">
            <h1 class="text-lg font-semibold text-gray-900 dark:text-white sm:text-xl">
              {{ t('admin.resourceLibrary.title') }}
            </h1>
            <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
              {{ t('admin.resourceLibrary.description') }}
            </p>
          </div>
          <button type="button" class="btn btn-secondary" :disabled="loading" @click="refresh">
            <Icon name="refresh" size="sm" class="mr-1.5" :class="{ 'animate-spin': loading }" />
            {{ t('common.refresh') }}
          </button>
        </div>

        <!-- Tabs: generated results / archived copies -->
        <div class="mt-4 flex flex-wrap gap-2 border-t border-gray-100 pt-4 dark:border-dark-700">
          <button
            v-for="tab in tabs"
            :key="tab.key"
            type="button"
            class="btn"
            :class="activeTab === tab.key ? 'btn-primary' : 'btn-secondary'"
            @click="switchTab(tab.key)"
          >
            <Icon :name="tab.icon" :size="'sm'" class="mr-1.5" />
            {{ t(`admin.resourceLibrary.tabs.${tab.key}`) }}
            <span class="ml-2 rounded-full bg-black/10 px-2 py-0.5 text-[11px] dark:bg-white/10">{{ tab.count }}</span>
          </button>
        </div>
      </div>

      <!-- Filters -->
      <div class="card p-4 sm:p-6">
        <div class="flex flex-wrap items-end gap-4">
          <div class="w-full sm:w-auto sm:min-w-[240px]">
            <label class="input-label">{{ t('admin.resourceLibrary.filters.q') }}</label>
            <div class="relative">
              <Icon name="search" size="md" class="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-gray-400" />
              <input
                v-model.trim="filters.q"
                type="text"
                class="input pl-10"
                :placeholder="t('admin.resourceLibrary.filters.qPlaceholder')"
                @keyup.enter="search"
              />
            </div>
          </div>

          <div class="w-full sm:w-auto sm:min-w-[150px]">
            <label class="input-label">{{ t('admin.resourceLibrary.filters.kind') }}</label>
            <Select v-model="filters.kind" :options="kindOptions" @change="search" />
          </div>

          <div class="w-full sm:w-auto sm:min-w-[190px]">
            <label class="input-label">{{ t('admin.resourceLibrary.filters.model') }}</label>
            <Select v-model="filters.model" :options="modelOptions" @change="search" />
          </div>

          <div class="w-full sm:w-auto sm:min-w-[190px]">
            <label class="input-label">{{ t('admin.resourceLibrary.filters.group') }}</label>
            <Select v-model="filters.group" :options="groupOptions" @change="search" />
          </div>

          <div class="flex w-full flex-wrap items-center gap-2 sm:w-auto">
            <button type="button" class="btn btn-primary" :disabled="loading" @click="search">
              {{ t('common.search') }}
            </button>
            <button type="button" class="btn btn-secondary" :disabled="loading" @click="resetFilters">
              {{ t('common.reset') }}
            </button>
          </div>
        </div>
        <p v-if="truncated" class="mt-3 text-xs text-amber-600 dark:text-amber-400">
          {{ t('admin.resourceLibrary.truncated') }}
        </p>
      </div>

      <!-- Loading -->
      <div v-if="loading && !hasItems" class="card flex items-center justify-center p-12">
        <div class="flex flex-col items-center gap-3">
          <div class="h-8 w-8 animate-spin rounded-full border-b-2 border-primary-600"></div>
          <p class="text-sm text-gray-500 dark:text-gray-400">{{ t('common.loading') }}</p>
        </div>
      </div>

      <!-- Empty -->
      <div v-else-if="!hasItems" class="card flex flex-col items-center justify-center p-12">
        <Icon name="grid" size="xl" class="mb-4 h-12 w-12 text-gray-300 dark:text-dark-600" />
        <p class="text-center text-sm font-medium text-gray-500 dark:text-gray-400">{{ emptyText }}</p>
      </div>

      <!-- Cards: one column on phones, up to four on wide screens -->
      <div v-else class="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-3 2xl:grid-cols-4">
        <article v-for="item in liveItems" :key="liveKey(item)" class="card flex flex-col overflow-hidden">
          <button
            type="button"
            class="relative block w-full overflow-hidden bg-gray-100 dark:bg-dark-800"
            :aria-label="t('admin.resourceLibrary.actions.preview')"
            @click="openPreview(item)"
          >
            <div class="flex h-44 items-center justify-center sm:h-48">
              <img
                v-if="item.kind === 'image' && thumbs[liveKey(item)]"
                :src="thumbs[liveKey(item)]"
                :alt="item.name"
                class="h-full w-full object-cover"
                loading="lazy"
              />
              <span v-else class="flex flex-col items-center gap-2 text-gray-400">
                <Icon :name="item.kind === 'video' ? 'play' : 'cloud'" size="xl" class="h-10 w-10" />
                <span class="text-xs">
                  {{ item.kind === 'video' ? t('admin.resourceLibrary.kinds.video') : t('admin.resourceLibrary.thumbLoading') }}
                </span>
              </span>
            </div>
            <span class="absolute left-2 top-2 rounded-md bg-black/60 px-2 py-0.5 text-[11px] font-medium text-white backdrop-blur">
              {{ t(`admin.resourceLibrary.kinds.${item.kind}`) }}
            </span>
            <span v-if="item.saved_id" class="absolute right-2 top-2 rounded-md bg-emerald-600/90 px-2 py-0.5 text-[11px] font-medium text-white">
              {{ t('admin.resourceLibrary.savedBadge') }}
            </span>
            <span v-if="billingBadge(item.billing_state)" class="absolute bottom-2 left-2 rounded-md px-2 py-0.5 text-[11px] font-medium text-white" :class="billingBadge(item.billing_state)?.className">
              {{ billingBadge(item.billing_state)?.label }}
            </span>
          </button>

          <div class="flex flex-1 flex-col gap-3 p-4">
            <div class="min-w-0">
              <p class="truncate font-mono text-sm font-medium text-gray-900 dark:text-white" :title="item.name">{{ item.name }}</p>
              <p class="mt-0.5 truncate text-xs text-gray-500 dark:text-gray-400" :title="item.user_email">
                {{ item.user_email || '—' }} <span class="text-gray-400">· #{{ item.user_id }}</span>
              </p>
            </div>

            <dl class="grid grid-cols-2 gap-x-3 gap-y-1.5 text-xs">
              <div class="min-w-0">
                <dt class="text-gray-400">{{ t('admin.resourceLibrary.fields.createdAt') }}</dt>
                <dd class="truncate text-gray-700 dark:text-gray-200">{{ formatTime(item.created_at) }}</dd>
              </div>
              <div class="min-w-0">
                <dt class="text-gray-400">{{ t('admin.resourceLibrary.fields.expiresAt') }}</dt>
                <dd class="truncate text-gray-700 dark:text-gray-200">{{ formatTime(item.expires_at) }}</dd>
              </div>
              <div class="min-w-0">
                <dt class="text-gray-400">{{ t('admin.resourceLibrary.fields.model') }}</dt>
                <dd class="truncate text-gray-700 dark:text-gray-200" :title="item.model">{{ item.model || '—' }}</dd>
              </div>
              <div class="min-w-0">
                <dt class="text-gray-400">{{ t('admin.resourceLibrary.fields.group') }}</dt>
                <dd class="truncate text-gray-700 dark:text-gray-200" :title="item.group_name">
                  {{ item.group_name || (item.group_id ? `#${item.group_id}` : '—') }}
                </dd>
              </div>
              <div class="min-w-0">
                <dt class="text-gray-400">{{ t('admin.resourceLibrary.fields.size') }}</dt>
                <dd class="truncate text-gray-700 dark:text-gray-200">{{ formatSize(item.size_bytes) }}</dd>
              </div>
              <div class="min-w-0">
                <dt class="text-gray-400">{{ t('admin.resourceLibrary.fields.cost') }}</dt>
                <dd class="truncate text-gray-700 dark:text-gray-200">{{ formatCost(item.cost) }}</dd>
              </div>
            </dl>

            <p class="resource-prompt rounded-lg bg-gray-50 p-2 text-xs leading-relaxed text-gray-600 dark:bg-dark-900 dark:text-gray-300">
              {{ item.prompt || '—' }}
            </p>

            <div class="mt-auto flex flex-wrap gap-2 pt-1">
              <button type="button" class="card-action btn btn-secondary" @click="openPreview(item)">
                <Icon name="eye" size="sm" class="mr-1" />{{ t('admin.resourceLibrary.actions.preview') }}
              </button>
              <button type="button" class="card-action btn btn-secondary" @click="copyPrompt(item.prompt)">
                <Icon name="copy" size="sm" class="mr-1" />{{ t('admin.resourceLibrary.actions.copyPrompt') }}
              </button>
              <button type="button" class="card-action btn btn-secondary" :disabled="busy[liveKey(item)]" @click="downloadLive(item)">
                <Icon name="download" size="sm" class="mr-1" />{{ t('admin.resourceLibrary.actions.download') }}
              </button>
              <button
                type="button"
                class="card-action btn"
                :class="item.saved_id ? 'btn-secondary' : 'btn-primary'"
                :disabled="busy[liveKey(item)]"
                @click="save(item)"
              >
                <Icon name="book" size="sm" class="mr-1" />
                {{ item.saved_id ? t('admin.resourceLibrary.actions.saved') : t('admin.resourceLibrary.actions.save') }}
              </button>
              <button type="button" class="card-action btn btn-secondary" :disabled="busy[liveKey(item)]" @click="askDeleteLive(item)">
                <Icon name="trash" size="sm" class="mr-1" />{{ t('common.delete') }}
              </button>
            </div>
          </div>
        </article>        <article v-for="item in savedItems" :key="item.id" class="card flex flex-col overflow-hidden">
          <button
            type="button"
            class="relative block w-full overflow-hidden bg-gray-100 dark:bg-dark-800"
            :aria-label="t('admin.resourceLibrary.actions.preview')"
            @click="openSavedPreview(item)"
          >
            <div class="flex h-44 items-center justify-center sm:h-48">
              <img
                v-if="item.kind === 'image' && thumbs[savedKey(item)]"
                :src="thumbs[savedKey(item)]"
                :alt="item.name"
                class="h-full w-full object-cover"
                loading="lazy"
              />
              <span v-else class="flex flex-col items-center gap-2 text-gray-400">
                <Icon :name="item.kind === 'video' ? 'play' : 'cloud'" size="xl" class="h-10 w-10" />
                <span class="text-xs">
                  {{ item.kind === 'video' ? t('admin.resourceLibrary.kinds.video') : t('admin.resourceLibrary.thumbLoading') }}
                </span>
              </span>
            </div>
            <span class="absolute left-2 top-2 rounded-md bg-black/60 px-2 py-0.5 text-[11px] font-medium text-white backdrop-blur">
              {{ t(`admin.resourceLibrary.kinds.${item.kind}`) }}
            </span>
            <span class="absolute right-2 top-2 rounded-md bg-emerald-600/90 px-2 py-0.5 text-[11px] font-medium text-white">
              {{ t('admin.resourceLibrary.archivedBadge') }}
            </span>
          </button>

          <div class="flex flex-1 flex-col gap-3 p-4">
            <div class="min-w-0">
              <p class="truncate font-mono text-sm font-medium text-gray-900 dark:text-white" :title="item.name">{{ item.name }}</p>
              <p class="mt-0.5 truncate text-xs text-gray-500 dark:text-gray-400" :title="item.user_email">
                {{ item.user_email || '—' }} <span class="text-gray-400">· #{{ item.user_id }}</span>
              </p>
            </div>

            <dl class="grid grid-cols-2 gap-x-3 gap-y-1.5 text-xs">
              <div class="min-w-0">
                <dt class="text-gray-400">{{ t('admin.resourceLibrary.fields.createdAt') }}</dt>
                <dd class="truncate text-gray-700 dark:text-gray-200">{{ formatISO(item.generated_at) }}</dd>
              </div>
              <div class="min-w-0">
                <dt class="text-gray-400">{{ t('admin.resourceLibrary.fields.savedAt') }}</dt>
                <dd class="truncate text-gray-700 dark:text-gray-200">{{ formatISO(item.created_at) }}</dd>
              </div>
              <div class="min-w-0">
                <dt class="text-gray-400">{{ t('admin.resourceLibrary.fields.model') }}</dt>
                <dd class="truncate text-gray-700 dark:text-gray-200" :title="item.model">{{ item.model || '—' }}</dd>
              </div>
              <div class="min-w-0">
                <dt class="text-gray-400">{{ t('admin.resourceLibrary.fields.group') }}</dt>
                <dd class="truncate text-gray-700 dark:text-gray-200" :title="item.group_name">
                  {{ item.group_name || (item.group_id ? `#${item.group_id}` : '—') }}
                </dd>
              </div>
              <div class="min-w-0">
                <dt class="text-gray-400">{{ t('admin.resourceLibrary.fields.size') }}</dt>
                <dd class="truncate text-gray-700 dark:text-gray-200">{{ formatSize(item.size_bytes) }}</dd>
              </div>
              <div class="min-w-0">
                <dt class="text-gray-400">{{ t('admin.resourceLibrary.fields.cost') }}</dt>
                <dd class="truncate text-gray-700 dark:text-gray-200">{{ formatCost(item.cost) }}</dd>
              </div>
            </dl>

            <p class="resource-prompt rounded-lg bg-gray-50 p-2 text-xs leading-relaxed text-gray-600 dark:bg-dark-900 dark:text-gray-300">
              {{ item.prompt || '—' }}
            </p>

            <div class="mt-auto flex flex-wrap gap-2 pt-1">
              <button type="button" class="card-action btn btn-secondary" @click="openSavedPreview(item)">
                <Icon name="eye" size="sm" class="mr-1" />{{ t('admin.resourceLibrary.actions.preview') }}
              </button>
              <button type="button" class="card-action btn btn-secondary" @click="copyPrompt(item.prompt)">
                <Icon name="copy" size="sm" class="mr-1" />{{ t('admin.resourceLibrary.actions.copyPrompt') }}
              </button>
              <button type="button" class="card-action btn btn-secondary" :disabled="busy[savedKey(item)]" @click="downloadSaved(item)">
                <Icon name="download" size="sm" class="mr-1" />{{ t('admin.resourceLibrary.actions.download') }}
              </button>
              <button type="button" class="card-action btn btn-secondary" @click="askDeleteSaved(item)">
                <Icon name="trash" size="sm" class="mr-1" />{{ t('common.delete') }}
              </button>
            </div>
          </div>
        </article>
      </div>

      <div v-if="total > 0" class="card p-3 sm:p-4">
        <Pagination
          :total="total"
          :page="page"
          :page-size="pageSize"
          @update:page="onPageChange"
          @update:pageSize="onPageSizeChange"
        />
      </div>
    </div>

    <!-- Preview -->
    <BaseDialog :show="preview.open" :title="preview.name || t('admin.resourceLibrary.actions.preview')" width="wide" @close="closePreview">
      <div v-if="preview.loading" class="flex items-center justify-center py-16">
        <div class="flex flex-col items-center gap-3">
          <div class="h-8 w-8 animate-spin rounded-full border-b-2 border-primary-600"></div>
          <p class="text-sm text-gray-500 dark:text-gray-400">{{ t('common.loading') }}</p>
        </div>
      </div>
      <div v-else class="space-y-4">
        <div v-if="preview.url" class="flex max-h-[60vh] items-center justify-center overflow-hidden rounded-xl bg-black p-2">
          <img
            v-if="preview.kind === 'image'"
            :src="preview.url"
            :alt="preview.name"
            class="max-h-[calc(60vh-1rem)] max-w-full object-contain"
          />
          <video
            v-else
            :src="preview.url"
            class="max-h-[calc(60vh-1rem)] w-full"
            controls
            autoplay
            playsinline
          ></video>
        </div>
        <p v-else class="rounded-xl bg-gray-50 p-6 text-center text-sm text-gray-500 dark:bg-dark-900 dark:text-gray-400">
          {{ t('admin.resourceLibrary.previewUnavailable') }}
        </p>

        <dl class="grid grid-cols-1 gap-3 text-sm sm:grid-cols-2 lg:grid-cols-3">
          <div v-for="field in previewFields" :key="field.label" class="min-w-0 overflow-hidden rounded-xl bg-gray-50 p-3 dark:bg-dark-900">
            <dt class="break-words text-xs font-medium uppercase tracking-wide text-gray-400">{{ field.label }}</dt>
            <dd class="mt-1 break-all text-gray-800 dark:text-gray-100">{{ field.value }}</dd>
          </div>
        </dl>

        <div>
          <h4 class="mb-1.5 text-xs font-bold uppercase tracking-wider text-gray-400">
            {{ t('admin.resourceLibrary.fields.prompt') }}
          </h4>
          <pre class="max-h-48 overflow-auto whitespace-pre-wrap break-words rounded-xl bg-gray-50 p-3 font-mono text-xs leading-relaxed text-gray-700 dark:bg-dark-900 dark:text-gray-300">{{ preview.prompt || '—' }}</pre>
        </div>
      </div>

      <template #footer>
        <button type="button" class="btn btn-secondary" @click="closePreview">{{ t('common.close') }}</button>
        <button type="button" class="btn btn-secondary" @click="copyPrompt(preview.prompt)">
          <Icon name="copy" size="sm" class="mr-1" />{{ t('admin.resourceLibrary.actions.copyPrompt') }}
        </button>
      </template>
    </BaseDialog>

    <ConfirmDialog
      :show="confirmDelete.open"
      :title="t('admin.resourceLibrary.deleteConfirm.title')"
      :message="confirmDelete.message"
      :confirm-text="t('common.delete')"
      :cancel-text="t('common.cancel')"
      :loading="confirmDelete.loading"
      danger
      @confirm="confirmDeleteAction"
      @cancel="closeDeleteConfirm"
    />
  </AppLayout>
</template>
<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  adminAPI,
  type ResourceGroupOption,
  type ResourceItem,
  type ResourceKind,
  type ResourceListQuery,
  type SavedResource
} from '@/api/admin'
import AppLayout from '@/components/layout/AppLayout.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import Pagination from '@/components/common/Pagination.vue'
import Select from '@/components/common/Select.vue'
import Icon from '@/components/icons/Icon.vue'
import { useClipboard } from '@/composables/useClipboard'
import { useAppStore } from '@/stores'
import { formatDateTime } from '@/utils/format'

type TabKey = 'live' | 'saved'

const { t } = useI18n()
const appStore = useAppStore()
const { copyToClipboard } = useClipboard()

const activeTab = ref<TabKey>('live')
const loading = ref(false)
const total = ref(0)
const page = ref(1)
const pageSize = ref(20)
const models = ref<string[]>([])
const groups = ref<ResourceGroupOption[]>([])
const truncated = ref(false)
const liveTotal = ref(0)
const savedTotal = ref(0)
const liveItems = ref<ResourceItem[]>([])
const savedItems = ref<SavedResource[]>([])
const busy = reactive<Record<string, boolean>>({})

const filters = reactive<{ q: string; kind: '' | ResourceKind; model: string; group: string }>({
  q: '',
  kind: '',
  model: '',
  group: ''
})

const thumbs = reactive<Record<string, string>>({})
const objectUrls = new Set<string>()
const controller = new AbortController()

const preview = reactive<{
  open: boolean
  loading: boolean
  url: string
  kind: ResourceKind
  name: string
  prompt: string
  fields: { label: string; value: string }[]
}>({ open: false, loading: false, url: '', kind: 'image', name: '', prompt: '', fields: [] })

const confirmDelete = reactive<{
  open: boolean
  loading: boolean
  message: string
  action: (() => Promise<void>) | null
}>({ open: false, loading: false, message: '', action: null })

// Each tab keeps its own total; the active tab's `total` must not leak into the
// other tab's badge.
const tabs = computed((): { key: TabKey; icon: 'grid' | 'inbox'; count: number }[] => [
  { key: 'live', icon: 'grid', count: liveTotal.value },
  { key: 'saved', icon: 'inbox', count: savedTotal.value }
])

const kindOptions = computed(() => [
  { value: '', label: t('admin.resourceLibrary.filters.allKinds') },
  { value: 'image', label: t('admin.resourceLibrary.kinds.image') },
  { value: 'video', label: t('admin.resourceLibrary.kinds.video') }
])

const modelOptions = computed(() => [
  { value: '', label: t('admin.resourceLibrary.filters.allModels') },
  ...models.value.map(model => ({ value: model, label: model }))
])

const groupOptions = computed(() => [
  { value: '', label: t('admin.resourceLibrary.filters.allGroups') },
  ...groups.value.map(group => ({ value: group.id, label: group.name || `#${group.id}` }))
])

const hasItems = computed(() =>
  activeTab.value === 'live' ? liveItems.value.length > 0 : savedItems.value.length > 0
)
const emptyText = computed(() =>
  activeTab.value === 'saved' ? t('admin.resourceLibrary.emptySaved') : t('admin.resourceLibrary.emptyLive')
)
const previewFields = computed(() => preview.fields)

function liveKey(item: ResourceItem): string {
  return `live:${item.kind}:${item.task_id}:${item.index}`
}

function savedKey(item: SavedResource): string {
  return `saved:${item.id}`
}

function formatTime(seconds: number): string {
  if (!seconds) return '—'
  return formatDateTime(new Date(seconds * 1000))
}

function formatISO(value: string | undefined): string {
  if (!value) return '—'
  return formatDateTime(new Date(value))
}

function formatSize(bytes: number): string {
  if (!bytes || bytes < 0) return '—'
  const units = ['B', 'KB', 'MB', 'GB']
  let value = bytes
  let unit = 0
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024
    unit += 1
  }
  return `${value.toFixed(unit === 0 ? 0 : 1)} ${units[unit]}`
}

function formatCost(cost: number | null | undefined): string {
  if (cost === null || cost === undefined) return '—'
  return `$${Number(cost.toFixed(6))}`
}

function formatDetailValue(value: unknown): string {
  if (value === null || value === undefined || value === '') return '—'
  if (typeof value === 'object') return JSON.stringify(value)
  return String(value)
}

// The backend emits machine keys (quality, ratio, reference_images, ...); they
// must never reach the UI verbatim. Unknown keys fall back to the raw key so a
// future backend addition is still visible rather than silently dropped.
function detailLabel(key: string): string {
  const i18nKey = `admin.resourceLibrary.details.${key}`
  const translated = t(i18nKey)
  return translated === i18nKey ? key : translated
}

function detailFields(details: Record<string, unknown> | undefined): { label: string; value: string }[] {
  return Object.entries(details ?? {}).map(([key, value]) => ({
    label: detailLabel(key),
    value: formatDetailValue(value)
  }))
}

function billingBadge(state: string | undefined): { label: string; className: string } | null {
  if (state === 'prepaid') return { label: t('admin.resourceLibrary.billing.pending'), className: 'bg-amber-500/90' }
  if (state === 'refunded') return { label: t('admin.resourceLibrary.billing.refunded'), className: 'bg-gray-600/90' }
  return null
}
function releaseThumbs() {
  for (const url of objectUrls) URL.revokeObjectURL(url)
  objectUrls.clear()
  for (const key of Object.keys(thumbs)) delete thumbs[key]
}

function revokePreview() {
  if (preview.url) URL.revokeObjectURL(preview.url)
  preview.url = ''
}

async function loadThumbs() {
  releaseThumbs()
  // Only images get an inline thumbnail; videos preview on demand so a page of
  // cards never downloads gigabytes of video.
  const queue = activeTab.value === 'live' ? liveItems.value.filter(item => item.kind === 'image') : []
  const savedQueue = activeTab.value === 'saved' ? savedItems.value.filter(item => item.kind === 'image') : []
  const workers = Array.from({ length: 4 }, async () => {
    for (;;) {
      const next = queue.shift()
      if (next) {
        try {
          const blob = await adminAPI.resourceLibrary.fetchItemContent(next.kind, next.task_id, next.index, controller.signal)
          const url = URL.createObjectURL(blob)
          objectUrls.add(url)
          thumbs[liveKey(next)] = url
        } catch {
          // Expired or unavailable objects keep their placeholder.
        }
        continue
      }
      const archived = savedQueue.shift()
      if (!archived) return
      try {
        const blob = await adminAPI.resourceLibrary.fetchSavedContent(archived.id, controller.signal)
        const url = URL.createObjectURL(blob)
        objectUrls.add(url)
        thumbs[savedKey(archived)] = url
      } catch {
        // Unreadable archive entries keep their placeholder.
      }
    }
  })
  await Promise.all(workers)
}

function queryParams(): ResourceListQuery {
  return {
    kind: filters.kind,
    model: filters.model || undefined,
    group_id: filters.group ? Number(filters.group) : undefined,
    q: filters.q || undefined,
    page: page.value,
    page_size: pageSize.value
  }
}

async function load() {
  loading.value = true
  try {
    const params = queryParams()
    if (activeTab.value === 'live') {
      const result = await adminAPI.resourceLibrary.listItems(params)
      liveItems.value = result.items ?? []
      total.value = result.total ?? 0
      liveTotal.value = result.total ?? 0
      models.value = result.models ?? []
      groups.value = result.groups ?? []
      truncated.value = result.truncated === true
    } else {
      const result = await adminAPI.resourceLibrary.listSaved(params)
      savedItems.value = result.items ?? []
      total.value = result.total ?? 0
      savedTotal.value = result.total ?? 0
      models.value = result.models ?? []
      groups.value = result.groups ?? []
      truncated.value = false
    }
  } catch (error) {
    const err = error as { status?: number }
    appStore.showError(err.status === 404 ? t('admin.resourceLibrary.disabled') : t('admin.resourceLibrary.loadFailed'))
    liveItems.value = []
    savedItems.value = []
    total.value = 0
  } finally {
    loading.value = false
  }
  void loadThumbs()
}

async function refresh() {
  await load()
  if (activeTab.value === 'live') {
    await loadSavedCount()
  }
}

async function loadSavedCount() {
  try {
    const result = await adminAPI.resourceLibrary.listSaved({ page: 1, page_size: 1 })
    savedTotal.value = result.total ?? 0
  } catch {
    savedTotal.value = 0
  }
}

function search() {
  page.value = 1
  void load()
}

function resetFilters() {
  filters.q = ''
  filters.kind = ''
  filters.model = ''
  filters.group = ''
  page.value = 1
  void load()
}

function switchTab(tab: TabKey) {
  if (activeTab.value === tab) return
  activeTab.value = tab
  page.value = 1
  filters.model = ''
  filters.group = ''
  revokePreview()
  void load().then(() => {
    if (tab === 'saved') {
      void loadSavedCount()
    }
  })
}

function onPageChange(next: number) {
  page.value = next
  void load()
}

function onPageSizeChange(size: number) {
  pageSize.value = size
  page.value = 1
  void load()
}

async function copyPrompt(prompt: string) {
  await copyToClipboard(prompt || '', t('admin.resourceLibrary.promptCopied'))
}

function triggerDownload(blob: Blob, name: string) {
  const url = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = url
  link.download = name || 'resource'
  document.body.appendChild(link)
  link.click()
  document.body.removeChild(link)
  URL.revokeObjectURL(url)
}

async function downloadLive(item: ResourceItem) {
  const key = liveKey(item)
  busy[key] = true
  try {
    triggerDownload(await adminAPI.resourceLibrary.downloadItem(item.kind, item.task_id, item.index), item.name)
  } catch {
    appStore.showError(t('admin.resourceLibrary.downloadFailed'))
  } finally {
    busy[key] = false
  }
}

async function downloadSaved(item: SavedResource) {
  const key = savedKey(item)
  busy[key] = true
  try {
    triggerDownload(await adminAPI.resourceLibrary.downloadSaved(item.id), item.name)
  } catch {
    appStore.showError(t('admin.resourceLibrary.downloadFailed'))
  } finally {
    busy[key] = false
  }
}

async function save(item: ResourceItem) {
  if (item.saved_id) {
    switchTab('saved')
    return
  }
  const key = liveKey(item)
  busy[key] = true
  try {
    const result = await adminAPI.resourceLibrary.saveItem(item.kind, item.task_id, item.index)
    item.saved_id = result.item.id
    savedTotal.value += 1
    appStore.showSuccess(result.created ? t('admin.resourceLibrary.saved') : t('admin.resourceLibrary.alreadySaved'))
  } catch (error) {
    const err = error as { status?: number }
    appStore.showError(err.status === 413 ? t('admin.resourceLibrary.saveTooLarge') : t('admin.resourceLibrary.saveFailed'))
  } finally {
    busy[key] = false
  }
}

function askDeleteLive(item: ResourceItem) {
  confirmDelete.message = t('admin.resourceLibrary.deleteConfirm.liveMessage', { name: item.name })
  confirmDelete.action = async () => {
    await adminAPI.resourceLibrary.deleteItem(item.kind, item.task_id)
  }
  confirmDelete.open = true
}

function askDeleteSaved(item: SavedResource) {
  confirmDelete.message = t('admin.resourceLibrary.deleteConfirm.savedMessage', { name: item.name })
  confirmDelete.action = async () => {
    await adminAPI.resourceLibrary.deleteSaved(item.id)
  }
  confirmDelete.open = true
}

async function confirmDeleteAction() {
  if (!confirmDelete.action) return
  confirmDelete.loading = true
  try {
    await confirmDelete.action()
    appStore.showSuccess(t('admin.resourceLibrary.deleted'))
    closeDeleteConfirm()
    await load()
    if (activeTab.value === 'live') await loadSavedCount()
  } catch {
    appStore.showError(t('admin.resourceLibrary.deleteFailed'))
  } finally {
    confirmDelete.loading = false
  }
}

function closeDeleteConfirm() {
  confirmDelete.open = false
  confirmDelete.loading = false
  confirmDelete.action = null
}
function livePreviewFields(item: ResourceItem): { label: string; value: string }[] {
  return [
    { label: t('admin.resourceLibrary.fields.createdAt'), value: formatTime(item.created_at) },
    { label: t('admin.resourceLibrary.fields.expiresAt'), value: formatTime(item.expires_at) },
    { label: t('admin.resourceLibrary.fields.userEmail'), value: item.user_email || '—' },
    { label: t('admin.resourceLibrary.fields.userId'), value: `#${item.user_id}${item.api_key_id ? ` · Key #${item.api_key_id}` : ''}` },
    { label: t('admin.resourceLibrary.fields.model'), value: item.model || '—' },
    { label: t('admin.resourceLibrary.fields.group'), value: item.group_name || (item.group_id ? `#${item.group_id}` : '—') },
    { label: t('admin.resourceLibrary.fields.size'), value: formatSize(item.size_bytes) },
    { label: t('admin.resourceLibrary.fields.cost'), value: formatCost(item.cost) },
    { label: t('admin.resourceLibrary.fields.taskId'), value: item.task_id },
    { label: t('admin.resourceLibrary.fields.billingId'), value: item.billing_id || '—' },
    { label: t('admin.resourceLibrary.fields.contentType'), value: item.content_type || '—' },
    { label: t('admin.resourceLibrary.fields.fileName'), value: item.name },
    ...detailFields(item.details)
  ]
}

function savedPreviewFields(item: SavedResource): { label: string; value: string }[] {
  return [
    { label: t('admin.resourceLibrary.fields.createdAt'), value: formatISO(item.generated_at) },
    { label: t('admin.resourceLibrary.fields.sourceExpiresAt'), value: formatISO(item.source_expires_at) },
    { label: t('admin.resourceLibrary.fields.savedAt'), value: formatISO(item.created_at) },
    { label: t('admin.resourceLibrary.fields.savedBy'), value: item.saved_by_email || `#${item.saved_by}` },
    { label: t('admin.resourceLibrary.fields.userEmail'), value: item.user_email || '—' },
    { label: t('admin.resourceLibrary.fields.userId'), value: `#${item.user_id}${item.api_key_id ? ` · Key #${item.api_key_id}` : ''}` },
    { label: t('admin.resourceLibrary.fields.model'), value: item.model || '—' },
    { label: t('admin.resourceLibrary.fields.group'), value: item.group_name || (item.group_id ? `#${item.group_id}` : '—') },
    { label: t('admin.resourceLibrary.fields.size'), value: formatSize(item.size_bytes) },
    { label: t('admin.resourceLibrary.fields.cost'), value: formatCost(item.cost) },
    { label: t('admin.resourceLibrary.fields.taskId'), value: item.source_task_id },
    { label: t('admin.resourceLibrary.fields.billingId'), value: item.billing_id || '—' },
    { label: t('admin.resourceLibrary.fields.contentType'), value: item.content_type || '—' },
    { label: t('admin.resourceLibrary.fields.fileName'), value: item.name },
    { label: t('admin.resourceLibrary.fields.sha256'), value: item.sha256 },
    ...detailFields(item.details)
  ]
}

async function openPreview(item: ResourceItem) {
  revokePreview()
  Object.assign(preview, {
    open: true,
    loading: true,
    kind: item.kind,
    name: item.name,
    prompt: item.prompt,
    fields: livePreviewFields(item),
    url: ''
  })
  try {
    const blob = await adminAPI.resourceLibrary.fetchItemContent(item.kind, item.task_id, item.index)
    const url = URL.createObjectURL(blob)
    objectUrls.add(url)
    preview.url = url
  } catch {
    preview.url = ''
  } finally {
    preview.loading = false
  }
}

async function openSavedPreview(item: SavedResource) {
  revokePreview()
  Object.assign(preview, {
    open: true,
    loading: true,
    kind: item.kind,
    name: item.name,
    prompt: item.prompt,
    fields: savedPreviewFields(item),
    url: ''
  })
  try {
    const blob = await adminAPI.resourceLibrary.fetchSavedContent(item.id)
    const url = URL.createObjectURL(blob)
    objectUrls.add(url)
    preview.url = url
  } catch {
    preview.url = ''
  } finally {
    preview.loading = false
  }
}

function closePreview() {
  preview.open = false
  preview.loading = false
  revokePreview()
}

onMounted(() => {
  void refresh()
})

onBeforeUnmount(() => {
  controller.abort()
  releaseThumbs()
  revokePreview()
})
</script>

<style scoped>
/* Tailwind 3.4 here does not register the line-clamp plugin, so the prompt
   preview is bounded explicitly instead of with `line-clamp-*`. */
.resource-prompt {
  display: -webkit-box;
  -webkit-box-orient: vertical;
  -webkit-line-clamp: 3;
  overflow: hidden;
}

/* Card actions wrap to two columns on phones and stay compact on desktop. */
.card-action {
  flex: 1 1 40%;
  padding: 0.375rem 0.625rem;
  border-radius: 0.75rem;
  font-size: 0.75rem;
}
</style>