<template>
  <div class="usage-filters" :class="flat ? 'usage-filters--flat' : 'card'">
    <!-- Actions first; on phones the filter fields fold behind the "Filter" button. -->
    <div v-if="showActions" class="usage-filters-actions">
      <button
        type="button"
        class="btn btn-secondary usage-filters-button usage-filters-toggle"
        :aria-expanded="filtersOpen"
        data-testid="usage-filters-toggle"
        @click="filtersOpen = !filtersOpen"
      >
        <svg class="h-4 w-4" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="1.8" aria-hidden="true">
          <path stroke-linecap="round" stroke-linejoin="round" d="M3 5h18l-7 8.5V19l-4 2v-7.5L3 5z" />
        </svg>
        {{ t('common.filter') }}
        <span v-if="activeFilterCount > 0" class="usage-filters-count">{{ activeFilterCount }}</span>
      </button>
      <div class="usage-filters-buttons">
        <button type="button" @click="$emit('refresh')" class="btn btn-secondary usage-filters-button">
          {{ t('common.refresh') }}
        </button>
        <button type="button" @click="$emit('reset')" class="btn btn-secondary usage-filters-button">
          {{ t('common.reset') }}
        </button>
        <slot name="after-reset" />
      </div>
      <div v-if="mode === 'usage'" class="usage-filters-primary">
        <button type="button" @click="$emit('cleanup')" class="btn btn-danger usage-filters-button">
          {{ t('admin.usage.cleanup.button') }}
        </button>
        <button type="button" @click="$emit('export')" :disabled="exporting" class="btn btn-primary usage-filters-button">
          {{ t('usage.exportExcel') }}
        </button>
      </div>
    </div>

    <div class="usage-filters-grid" :class="{ 'is-open': filtersOpen || !showActions }">
      <!-- User Search -->
      <div ref="userSearchRef" class="usage-filter-dropdown usage-filter-field is-wide relative">
        <label class="input-label">{{ t('admin.usage.userFilter') }}</label>
        <input
          v-model="userKeyword"
          type="text"
          class="input pr-8"
          :placeholder="t('admin.usage.searchUserPlaceholder')"
          @input="debounceUserSearch"
          @focus="showUserDropdown = true"
        />
        <button
          v-if="filters.user_id"
          type="button"
          @click="clearUser"
          class="absolute right-2 top-9 text-gray-400"
          aria-label="Clear user filter"
        >
          ✕
        </button>
        <div
          v-if="showUserDropdown && (userResults.length > 0 || userKeyword)"
          class="usage-filter-menu absolute z-50 mt-1 max-h-60 w-full overflow-auto"
        >
          <button
            v-for="u in userResults"
            :key="u.id"
            type="button"
            @click="selectUser(u)"
            class="usage-filter-option"
          >
            <span class="min-w-0 break-all">{{ u.email }}<span v-if="u.deleted" class="ml-1 text-xs text-gray-400">（{{ t('admin.usage.userDeletedBadge') }}）</span></span>
            <span class="ml-2 shrink-0 text-xs text-gray-400">#{{ u.id }}</span>
          </button>
        </div>
      </div>

      <!-- API Key Search -->
      <div ref="apiKeySearchRef" class="usage-filter-dropdown usage-filter-field is-wide relative">
        <label class="input-label">{{ t('usage.apiKeyFilter') }}</label>
        <input
          v-model="apiKeyKeyword"
          type="text"
          class="input pr-8"
          :placeholder="t('admin.usage.searchApiKeyPlaceholder')"
          @input="debounceApiKeySearch"
          @focus="onApiKeyFocus"
        />
        <button
          v-if="filters.api_key_id"
          type="button"
          @click="onClearApiKey"
          class="absolute right-2 top-9 text-gray-400"
          aria-label="Clear API key filter"
        >
          ✕
        </button>
        <div
          v-if="showApiKeyDropdown && apiKeyResults.length > 0"
          class="usage-filter-menu absolute z-50 mt-1 max-h-60 w-full overflow-auto"
        >
          <button
            v-for="k in apiKeyResults"
            :key="k.id"
            type="button"
            @click="selectApiKey(k)"
            class="usage-filter-option"
          >
            <span class="truncate">{{ k.name || `#${k.id}` }}</span>
            <span class="ml-2 shrink-0 text-xs text-gray-400">#{{ k.id }}</span>
          </button>
        </div>
      </div>

      <!-- Model Filter -->
      <div class="usage-filter-field">
        <label class="input-label">{{ t('usage.model') }}</label>
        <Select :model-value="filters.model ?? null" @update:model-value="filters.model = $event" :options="modelOptions" searchable @change="emitChange" />
      </div>

      <!-- Account Filter -->
      <div ref="accountSearchRef" class="usage-filter-dropdown usage-filter-field is-wide relative">
        <label class="input-label">{{ t('admin.usage.account') }}</label>
        <input
          v-model="accountKeyword"
          type="text"
          class="input pr-8"
          :placeholder="t('admin.usage.searchAccountPlaceholder')"
          @input="debounceAccountSearch"
          @focus="showAccountDropdown = true"
        />
        <button
          v-if="filters.account_id"
          type="button"
          @click="clearAccount"
          class="absolute right-2 top-9 text-gray-400"
          aria-label="Clear account filter"
        >
          ✕
        </button>
        <div
          v-if="showAccountDropdown && (accountResults.length > 0 || accountKeyword)"
          class="usage-filter-menu absolute z-50 mt-1 max-h-60 w-full overflow-auto"
        >
          <button
            v-for="a in accountResults"
            :key="a.id"
            type="button"
            @click="selectAccount(a)"
            class="usage-filter-option"
          >
            <span class="truncate">{{ a.name }}</span>
            <span class="ml-2 shrink-0 text-xs text-gray-400">#{{ a.id }}</span>
          </button>
        </div>
      </div>

      <!-- Request Type Filter (usage only) -->
      <div v-if="mode !== 'errors'" class="usage-filter-field">
        <label class="input-label">{{ t('usage.type') }}</label>
        <Select :model-value="filters.request_type ?? null" @update:model-value="filters.request_type = $event" :options="requestTypeOptions" @change="emitChange" />
      </div>

      <!-- Billing Type Filter (usage only) -->
      <div v-if="mode !== 'errors'" class="usage-filter-field">
        <label class="input-label">{{ t('admin.usage.billingType') }}</label>
        <Select :model-value="filters.billing_type ?? null" @update:model-value="filters.billing_type = $event" :options="billingTypeOptions" @change="emitChange" />
      </div>

      <!-- Billing Mode Filter (usage only；用户排行的 user-breakdown 接口不支持该维度) -->
      <div v-if="mode === 'usage'" class="usage-filter-field">
        <label class="input-label">{{ t('admin.usage.billingMode') }}</label>
        <Select :model-value="filters.billing_mode ?? null" @update:model-value="filters.billing_mode = $event" :options="billingModeOptions" @change="emitChange" />
      </div>

      <div v-if="mode === 'usage'" class="usage-filter-field">
        <label class="input-label">{{ t('admin.usage.upstreamModelAudit') }}</label>
        <Select :model-value="filters.upstream_model_mismatch ?? null" @update:model-value="filters.upstream_model_mismatch = $event" :options="upstreamModelMismatchOptions" @change="emitChange" />
      </div>

      <!-- Error Phase Filter (errors only) -->
      <div v-if="mode === 'errors'" class="usage-filter-field">
        <label class="input-label">{{ t('admin.ops.errorLog.type') }}</label>
        <Select :model-value="filters.error_phase ?? null" @update:model-value="filters.error_phase = $event" :options="errorPhaseOptions" @change="emitChange" />
      </div>

      <!-- Error Category Filter (errors only) -->
      <div v-if="mode === 'errors'" class="usage-filter-field">
        <label class="input-label">{{ t('usage.errors.category') }}</label>
        <Select :model-value="filters.error_category ?? null" @update:model-value="filters.error_category = $event" :options="errorCategoryOptions" @change="emitChange" />
      </div>

      <!-- Status Code Filter (errors only) -->
      <div v-if="mode === 'errors'" class="usage-filter-field">
        <label class="input-label">{{ t('admin.ops.errorLog.status') }}</label>
        <Select :model-value="filters.status_code ?? null" @update:model-value="filters.status_code = $event" :options="statusCodeOptions" @change="emitChange" />
      </div>

      <!-- Group Filter -->
      <div class="usage-filter-field">
        <label class="input-label">{{ t('admin.usage.group') }}</label>
        <Select :model-value="filters.group_id ?? null" @update:model-value="filters.group_id = $event" :options="groupOptions" searchable @change="emitChange" />
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, onUnmounted, toRef, watch, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import Select, { type SelectOption } from '@/components/common/Select.vue'
import { COMMON_ERROR_STATUS_CODES } from '@/utils/errorBadges'
import type { SimpleApiKey, SimpleUser } from '@/api/admin/usage'

type ModelValue = Record<string, any>

interface Props {
  modelValue: ModelValue
  exporting: boolean
  startDate: string
  endDate: string
  showActions?: boolean
  modelOptions?: string[]
  /**
   * errors 模式:隐藏用量专属字段/按钮,显示错误类型+状态码(错误请求 tab 用)
   * ranking 模式:同 usage 但隐藏计费模式筛选与清理/导出按钮(用户排行 tab 用)
   */
  mode?: 'usage' | 'errors' | 'ranking'
  /** 嵌入统一卡片内使用：去掉自身卡片外观 */
  flat?: boolean
}

const props = withDefaults(defineProps<Props>(), {
  showActions: true,
  mode: 'usage',
  flat: false
})
const emit = defineEmits([
  'update:modelValue',
  'change',
  'refresh',
  'reset',
  'export',
  'cleanup'
])

const { t } = useI18n()
const filters = toRef(props, 'modelValue')

const userSearchRef = ref<HTMLElement | null>(null)
const apiKeySearchRef = ref<HTMLElement | null>(null)
const accountSearchRef = ref<HTMLElement | null>(null)

const userKeyword = ref('')
const userResults = ref<SimpleUser[]>([])
const showUserDropdown = ref(false)
let userSearchTimeout: ReturnType<typeof setTimeout> | null = null
let userSearchSequence = 0

const apiKeyKeyword = ref('')
const apiKeyResults = ref<SimpleApiKey[]>([])
const showApiKeyDropdown = ref(false)
let apiKeySearchTimeout: ReturnType<typeof setTimeout> | null = null

interface SimpleAccount {
  id: number
  name: string
}
const accountKeyword = ref('')
const accountResults = ref<SimpleAccount[]>([])
const showAccountDropdown = ref(false)
let accountSearchTimeout: ReturnType<typeof setTimeout> | null = null

const modelOptions = computed<SelectOption[]>(() => [
  { value: null, label: t('admin.usage.allModels') },
  ...(props.modelOptions ?? []).map((m) => ({ value: m, label: m })),
])
const groupOptions = ref<SelectOption[]>([{ value: null, label: t('admin.usage.allGroups') }])

const requestTypeOptions = ref<SelectOption[]>([
  { value: null, label: t('admin.usage.allTypes') },
  { value: 'ws_v2', label: t('usage.ws') },
  { value: 'live', label: t('usage.live') },
  { value: 'stream', label: t('usage.stream') },
  { value: 'sync', label: t('usage.sync') },
  { value: 'cyber', label: t('usage.cyber') }
])

const billingTypeOptions = ref<SelectOption[]>([
  { value: null, label: t('admin.usage.allBillingTypes') },
  { value: 0, label: t('admin.usage.billingTypeBalance') },
  { value: 1, label: t('admin.usage.billingTypeSubscription') }
])

// 错误类型对应后端 phase 参数(与错误表"类型"徽章同语义)
const errorPhaseOptions = computed<SelectOption[]>(() => [
  { value: null, label: t('admin.usage.allTypes') },
  { value: 'upstream', label: t('admin.ops.errorLog.typeUpstream') },
  { value: 'account_auth', label: t('admin.ops.errorLog.typeAccountAuth') },
  { value: 'request', label: t('admin.ops.errorLog.typeRequest') },
  { value: 'auth', label: t('admin.ops.errorLog.typeAuth') },
  { value: 'routing', label: t('admin.ops.errorLog.typeRouting') },
  { value: 'internal', label: t('admin.ops.errorLog.typeInternal') },
])

// 分类码同用户端 /usage 错误筛选;"other" 无法反查为过滤条件,刻意不列
const errorCategoryCodes = ['auth', 'rate_limit', 'quota', 'invalid_request', 'service_unavailable', 'upstream', 'internal', 'cyber']

const errorCategoryOptions = computed<SelectOption[]>(() => [
  { value: null, label: t('usage.errors.allCategories') },
  ...errorCategoryCodes.map((c) => ({ value: c, label: t('usage.errors.categories.' + c) })),
])

const statusCodeOptions = computed<SelectOption[]>(() => [
  { value: null, label: t('usage.errors.allStatuses') },
  ...COMMON_ERROR_STATUS_CODES.map((c) => ({ value: c, label: String(c) })),
])

const billingModeOptions = ref<SelectOption[]>([
  { value: null, label: t('admin.usage.allBillingModes') },
  { value: 'token', label: t('admin.usage.billingModeToken') },
  { value: 'per_request', label: t('admin.usage.billingModePerRequest') },
  { value: 'image', label: t('admin.usage.billingModeImage') },
  { value: 'video', label: t('admin.usage.billingModeVideo') }
])

const upstreamModelMismatchOptions = ref<SelectOption[]>([
  { value: null, label: t('admin.usage.allUpstreamModelAudit') },
  { value: true, label: t('admin.usage.upstreamModelMismatchOnly') },
  { value: false, label: t('admin.usage.upstreamModelMatchedOnly') }
])

const emitChange = () => emit('change')

// Phones show the fields only after the Filter button is pressed; the count keeps active filters visible.
const filtersOpen = ref(false)
const isActiveValue = (value: unknown) => value !== null && value !== undefined && value !== ''
const activeFilterCount = computed(() => {
  const f = filters.value
  const keys = ['user_id', 'api_key_id', 'model', 'account_id', 'group_id']
  if (props.mode !== 'errors') keys.push('request_type', 'billing_type')
  if (props.mode === 'usage') keys.push('billing_mode', 'upstream_model_mismatch')
  if (props.mode === 'errors') keys.push('error_phase', 'error_category', 'status_code')
  return keys.filter((key) => isActiveValue(f[key])).length
})

const clearPendingUserSearch = () => {
  if (userSearchTimeout) {
    clearTimeout(userSearchTimeout)
    userSearchTimeout = null
  }
  userSearchSequence += 1
}

const debounceUserSearch = () => {
  clearPendingUserSearch()
  const query = userKeyword.value.trim()
  if (!query) {
    userResults.value = []
    return
  }

  const sequence = userSearchSequence
  userSearchTimeout = setTimeout(async () => {
    userSearchTimeout = null
    try {
      const results = await adminAPI.usage.searchUsers(query)
      if (sequence === userSearchSequence) {
        userResults.value = results.sort((a, b) => Number(a.deleted) - Number(b.deleted))
      }
    } catch {
      if (sequence === userSearchSequence) {
        userResults.value = []
      }
    }
  }, 300)
}

const debounceApiKeySearch = () => {
  if (apiKeySearchTimeout) clearTimeout(apiKeySearchTimeout)
  apiKeySearchTimeout = setTimeout(async () => {
    try {
      apiKeyResults.value = await adminAPI.usage.searchApiKeys(
        filters.value.user_id,
        apiKeyKeyword.value || ''
      )
    } catch {
      apiKeyResults.value = []
    }
  }, 300)
}

const selectUser = async (u: SimpleUser) => {
  clearPendingUserSearch()
  userKeyword.value = u.email
  showUserDropdown.value = false
  filters.value.user_id = u.id
  clearApiKey()

  // Auto-load API keys for this user
  try {
    apiKeyResults.value = await adminAPI.usage.searchApiKeys(u.id, '')
  } catch {
    apiKeyResults.value = []
  }

  emitChange()
}

const clearUser = () => {
  clearPendingUserSearch()
  userKeyword.value = ''
  userResults.value = []
  showUserDropdown.value = false
  filters.value.user_id = undefined
  clearApiKey()
  emitChange()
}

const selectApiKey = (k: SimpleApiKey) => {
  apiKeyKeyword.value = k.name || String(k.id)
  showApiKeyDropdown.value = false
  filters.value.api_key_id = k.id
  emitChange()
}

const clearApiKey = () => {
  apiKeyKeyword.value = ''
  apiKeyResults.value = []
  showApiKeyDropdown.value = false
  filters.value.api_key_id = undefined
}

const onClearApiKey = () => {
  clearApiKey()
  emitChange()
}

const debounceAccountSearch = () => {
  if (accountSearchTimeout) clearTimeout(accountSearchTimeout)
  accountSearchTimeout = setTimeout(async () => {
    if (!accountKeyword.value) {
      accountResults.value = []
      return
    }
    try {
      const res = await adminAPI.accounts.list(1, 20, { search: accountKeyword.value })
      accountResults.value = res.items.map((a) => ({ id: a.id, name: a.name }))
    } catch {
      accountResults.value = []
    }
  }, 300)
}

const selectAccount = (a: SimpleAccount) => {
  accountKeyword.value = a.name
  showAccountDropdown.value = false
  filters.value.account_id = a.id
  emitChange()
}

const clearAccount = () => {
  accountKeyword.value = ''
  accountResults.value = []
  showAccountDropdown.value = false
  filters.value.account_id = undefined
  emitChange()
}

const onApiKeyFocus = () => {
  showApiKeyDropdown.value = true
  // Trigger search if no results yet
  if (apiKeyResults.value.length === 0) {
    debounceApiKeySearch()
  }
}

const onDocumentClick = (e: MouseEvent) => {
  const target = e.target as Node | null
  if (!target) return

  const clickedInsideUser = userSearchRef.value?.contains(target) ?? false
  const clickedInsideApiKey = apiKeySearchRef.value?.contains(target) ?? false
  const clickedInsideAccount = accountSearchRef.value?.contains(target) ?? false

  if (!clickedInsideUser) showUserDropdown.value = false
  if (!clickedInsideApiKey) showApiKeyDropdown.value = false
  if (!clickedInsideAccount) showAccountDropdown.value = false
}

watch(
  () => props.startDate,
  (value) => {
    filters.value.start_date = value
  },
  { immediate: true }
)

watch(
  () => props.endDate,
  (value) => {
    filters.value.end_date = value
  },
  { immediate: true }
)

watch(
  () => filters.value.user_id,
  (userId) => {
    if (!userId) {
      clearPendingUserSearch()
      userKeyword.value = ''
      userResults.value = []
    }
  }
)

watch(
  () => filters.value.api_key_id,
  (apiKeyId) => {
    if (!apiKeyId) {
      apiKeyKeyword.value = ''
      apiKeyResults.value = []
    }
  }
)

watch(
  () => filters.value.account_id,
  (accountId) => {
    if (!accountId) {
      accountKeyword.value = ''
      accountResults.value = []
    }
  }
)

onMounted(async () => {
  document.addEventListener('click', onDocumentClick)
  try {
    const gs = await adminAPI.groups.list(1, 1000)
    groupOptions.value.push(...gs.items.map((g: any) => ({ value: g.id, label: g.name })))
  } catch {
    // Ignore filter option loading errors (page still usable)
  }
})

onUnmounted(() => {
  clearPendingUserSearch()
  document.removeEventListener('click', onDocumentClick)
})

// 供外部(如用户排行下钻)在程序化设置 user_id 后回显选中的用户邮箱
const setUserKeyword = (email: string) => {
  clearPendingUserSearch()
  userKeyword.value = email
  userResults.value = []
  showUserDropdown.value = false
}

const getUserSearchRevision = () => userSearchSequence

defineExpose({ getUserSearchRevision, setUserKeyword })
</script>

<style scoped>
.usage-filters--flat {
  padding: 0;
}

.usage-filters.card {
  padding: 1.25rem;
}

.usage-filters-actions {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 0.5rem;
}

.usage-filters-buttons {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 0.5rem;
}

/* Phones: the two weighty actions share a full-width row below. */
.usage-filters-primary {
  display: grid;
  flex: 1 1 100%;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 0.5rem;
}

.usage-filters-button {
  padding: 0.5rem 0.875rem;
  border-radius: 0.875rem;
  font-size: 0.8125rem;
}

.usage-filters-count {
  display: inline-flex;
  min-width: 1.125rem;
  height: 1.125rem;
  align-items: center;
  justify-content: center;
  padding: 0 0.3125rem;
  border-radius: 999px;
  background: var(--md-sys-color-primary);
  font-size: 0.6875rem;
  font-weight: 700;
  color: var(--md-sys-color-on-primary);
}

/* Phones: two fields per row, search boxes full width; folded until asked for. */
.usage-filters-grid {
  display: none;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 0.75rem;
  margin-top: 1rem;
}

.usage-filters-grid.is-open {
  display: grid;
}

.usage-filter-field {
  min-width: 0;
}

/* Search boxes go first and full width, so the selects pair up below them. */
.usage-filter-field.is-wide {
  order: -1;
  grid-column: span 2 / span 2;
}

.usage-filters-grid :deep(.input-label) {
  margin-bottom: 0.375rem;
  font-size: 0.75rem;
  color: var(--md-sys-color-on-surface-variant);
}

.usage-filter-menu {
  padding: 0.375rem 0;
  border-radius: 1rem;
  background: var(--md-sys-color-surface);
  box-shadow: var(--theme-highlight), var(--theme-elevation-3);
}

.usage-filter-option {
  display: flex;
  width: calc(100% - 0.75rem);
  margin: 0 0.375rem;
  align-items: center;
  justify-content: space-between;
  padding: 0.5rem 0.75rem;
  border-radius: 0.75rem;
  font-size: 0.8125rem;
  text-align: left;
  color: var(--md-sys-color-on-surface);
}

.usage-filter-option:hover {
  background: color-mix(in srgb, var(--md-sys-color-primary) 14%, var(--md-sys-color-surface));
}

@media (min-width: 768px) {
  .usage-filters-actions {
    justify-content: flex-end;
  }

  .usage-filters-toggle {
    display: none;
  }

  .usage-filters-primary {
    display: flex;
    flex: none;
  }

  .usage-filters-grid,
  .usage-filters-grid.is-open {
    display: grid;
    grid-template-columns: repeat(3, minmax(0, 1fr));
  }

  .usage-filter-field.is-wide {
    order: 0;
    grid-column: auto;
  }
}

@media (min-width: 1280px) {
  .usage-filters-grid,
  .usage-filters-grid.is-open {
    grid-template-columns: repeat(4, minmax(0, 1fr));
  }
}

@media (min-width: 1536px) {
  .usage-filters-grid,
  .usage-filters-grid.is-open {
    grid-template-columns: repeat(5, minmax(0, 1fr));
  }
}
</style>
