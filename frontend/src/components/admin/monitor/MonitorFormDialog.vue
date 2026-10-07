<template>
  <BaseDialog
    :show="show"
    :title="editing ? t('admin.channelMonitor.editTitle') : t('admin.channelMonitor.createTitle')"
    width="wide"
    @close="$emit('close')"
  >
    <form id="channel-monitor-form" @submit.prevent="handleSubmit" class="space-y-5">
      <div>
        <label class="input-label">{{ t('admin.channelMonitor.form.name') }}</label>
        <input
          v-model="form.name"
          type="text"
          class="input"
          :placeholder="t('admin.channelMonitor.form.namePlaceholder')"
          aria-describedby="channel-monitor-name-hint"
        />
        <p id="channel-monitor-name-hint" class="mt-1 text-xs text-gray-400">
          {{ t('admin.channelMonitor.form.nameHint') }}
        </p>
      </div>

      <div>
        <label class="input-label">{{ t('admin.channelMonitor.form.provider') }} <span class="text-red-500">*</span></label>
        <div class="grid grid-cols-2 gap-3 sm:grid-cols-3 md:grid-cols-5">
          <button
            v-for="opt in providerOptions"
            :key="opt.value"
            type="button"
            :data-testid="`monitor-provider-${opt.value}`"
            :aria-pressed="form.provider === opt.value"
            class="flex min-h-11 items-center justify-center gap-2 rounded-lg border-2 px-3 py-2.5 text-sm font-medium transition-colors"
            :class="providerPickerClass(opt.value, form.provider === opt.value)"
            @click="selectProvider(opt.value)"
          >
            <ProviderIcon :provider="opt.value" :size="18" />
            <span>{{ opt.label }}</span>
          </button>
        </div>
      </div>

      <div>
        <label class="input-label">
          {{ t('admin.channelMonitor.form.group') }} <span class="text-red-500">*</span>
        </label>
        <Select
          id="channel-monitor-group"
          data-testid="monitor-group-select"
          v-model="form.group_id"
          :options="groupOptions"
          :disabled="groupsLoading"
          :placeholder="groupSelectPlaceholder"
          :empty-text="t('admin.channelMonitor.form.noGroupsAvailable')"
          searchable
        >
          <template #selected="{ option }">
            <MonitorGroupLabel
              v-if="option"
              :name="String(option.label)"
              :rate-multiplier="optionRateMultiplier(option.rate_multiplier)"
              :platform-label="form.provider === PROVIDER_CUSTOM ? String(option.platform_label || '') : ''"
            />
            <span v-else class="text-gray-400 dark:text-gray-500">
              {{ groupSelectPlaceholder }}
            </span>
          </template>
          <template #option="{ option }">
            <MonitorGroupLabel
              :name="String(option.label)"
              :rate-multiplier="optionRateMultiplier(option.rate_multiplier)"
              :platform-label="form.provider === PROVIDER_CUSTOM ? String(option.platform_label || '') : ''"
            />
          </template>
        </Select>
      </div>

      <div v-if="isOpenAICompatibleProvider(form.provider)" class="rounded-lg border border-blue-100 bg-blue-50/50 p-3 dark:border-blue-500/20 dark:bg-blue-500/10">
        <label class="input-label">{{ t('admin.channelMonitor.form.apiMode') }}</label>
        <div class="grid gap-3 sm:grid-cols-2">
          <button
            v-for="opt in apiModeOptions"
            :key="opt.value"
            type="button"
            :aria-pressed="form.api_mode === opt.value"
            class="rounded-lg border-2 px-3 py-2 text-left transition-colors"
            :class="apiModeButtonClass(opt.value)"
            @click="form.api_mode = opt.value"
          >
            <span class="block text-sm font-semibold">{{ opt.label }}</span>
            <span class="mt-0.5 block text-xs opacity-80">{{ opt.hint }}</span>
          </button>
        </div>
      </div>

      <div>
        <label class="input-label">{{ t('admin.channelMonitor.form.primaryModel') }} <span class="text-red-500">*</span></label>
        <input
          v-model="form.primary_model"
          data-testid="monitor-primary-model"
          type="text"
          required
          class="input font-medium"
          :class="getPlatformTextClass(form.provider)"
          :placeholder="t('admin.channelMonitor.form.primaryModelPlaceholder')"
        />
      </div>

      <div>
        <label class="input-label">{{ t('admin.channelMonitor.form.extraModels') }}</label>
        <ModelTagInput
          :models="form.extra_models"
          :platform="form.provider"
          :placeholder="t('admin.channelMonitor.form.extraModelsPlaceholder')"
          @update:models="form.extra_models = $event"
        />
      </div>

      <div>
        <label class="input-label">{{ t('admin.channelMonitor.form.intervalSeconds') }} <span class="text-red-500">*</span></label>
        <input v-model.number="form.interval_seconds" type="number" min="15" max="3600" required class="input" />
        <p class="mt-1 text-xs text-gray-400">{{ t('admin.channelMonitor.form.intervalSecondsHint') }}</p>
      </div>

      <div>
        <label class="input-label">{{ t('admin.channelMonitor.form.jitterSeconds') }}</label>
        <input v-model.number="form.jitter_seconds" type="number" min="0" :max="maxJitterSeconds" class="input" />
        <p class="mt-1 text-xs text-gray-400">{{ t('admin.channelMonitor.form.jitterSecondsHint') }}</p>
      </div>

      <div class="flex items-center justify-between">
        <label class="input-label mb-0">{{ t('admin.channelMonitor.form.enabled') }}</label>
        <Toggle v-model="form.enabled" />
      </div>

      <div class="flex items-center justify-between gap-3">
        <label for="channel-monitor-simulate-requests" class="input-label mb-0">
          {{ t('admin.channelMonitor.form.simulateRequests') }}
        </label>
        <Toggle
          id="channel-monitor-simulate-requests"
          v-model="form.simulate_requests"
          :aria-label="t('admin.channelMonitor.form.simulateRequests')"
        />
      </div>

      <!-- 高级设置区：请求模板 + 自定义 headers/body -->
      <details class="rounded-lg border border-gray-200 bg-gray-50/50 p-3 dark:border-dark-700 dark:bg-dark-900/30">
        <summary class="cursor-pointer text-sm font-medium text-gray-700 dark:text-gray-300">
          {{ t('admin.channelMonitor.advanced.section') }}
        </summary>
        <p class="mt-1 text-xs text-gray-400">{{ t('admin.channelMonitor.advanced.sectionHint') }}</p>

        <div class="mt-4 space-y-4">
          <div>
            <label class="input-label">{{ t('admin.channelMonitor.templateField.label') }}</label>
            <Select
              v-model="templateSelectValue"
              :options="templateOptions"
              :placeholder="t('admin.channelMonitor.templateField.placeholder')"
            />
            <p class="mt-1 text-xs text-gray-400">{{ t('admin.channelMonitor.templateField.applyHint') }}</p>
          </div>

          <MonitorAdvancedRequestConfig
            :provider="form.provider"
            :api-mode="form.api_mode"
            :extra-headers="form.extra_headers"
            :body-override-mode="form.body_override_mode"
            :body-override="form.body_override"
            @update:extra-headers="form.extra_headers = $event"
            @update:body-override-mode="form.body_override_mode = $event"
            @update:body-override="form.body_override = $event"
          />
        </div>
      </details>
    </form>

    <template #footer>
      <div class="flex justify-end gap-3">
        <button @click="$emit('close')" type="button" class="btn btn-secondary">
          {{ t('common.cancel') }}
        </button>
        <button
          type="submit"
          form="channel-monitor-form"
          :disabled="submitting"
          class="btn btn-primary"
        >
          {{ submitting
            ? t('common.submitting')
            : editing ? t('common.update') : t('common.create') }}
        </button>
      </div>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { ref, reactive, computed, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { extractApiErrorMessage } from '@/utils/apiError'
import { adminAPI } from '@/api/admin'
import type {
  BodyOverrideMode,
  ChannelMonitor,
  CreateParams,
  APIMode,
  Provider,
  UpdateParams,
} from '@/api/admin/channelMonitor'
import type { ChannelMonitorTemplate } from '@/api/admin/channelMonitorTemplate'
import type { AdminGroup, GroupPlatform } from '@/types'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Toggle from '@/components/common/Toggle.vue'
import Select from '@/components/common/Select.vue'
import ModelTagInput from '@/components/admin/channel/ModelTagInput.vue'
import { getPlatformTextClass } from '@/components/admin/channel/types'
import MonitorGroupLabel from '@/components/admin/monitor/MonitorGroupLabel.vue'
import MonitorAdvancedRequestConfig from '@/components/admin/monitor/MonitorAdvancedRequestConfig.vue'
import ProviderIcon from '@/components/user/monitor/ProviderIcon.vue'
import { useChannelMonitorFormat } from '@/composables/useChannelMonitorFormat'
import {
  PROVIDER_OPENAI,
  PROVIDER_ANTHROPIC,
  PROVIDER_GEMINI,
  PROVIDER_GROK,
  PROVIDER_CUSTOM,
  API_MODE_CHAT_COMPLETIONS,
  API_MODE_RESPONSES,
  DEFAULT_GROK_MODEL,
  DEFAULT_INTERVAL_SECONDS,
} from '@/constants/channelMonitor'

const props = defineProps<{
  show: boolean
  monitor: ChannelMonitor | null
}>()

const emit = defineEmits<{
  (e: 'close'): void
  (e: 'saved'): void
}>()

const { t } = useI18n()
const appStore = useAppStore()
const { providerPickerClass } = useChannelMonitorFormat()

// System-configured default interval for new monitors. Falls back to the static
// constant when public settings haven't loaded yet or store the legacy 0 value.
const systemDefaultInterval = computed<number>(() => {
  const configured = appStore.cachedPublicSettings?.channel_monitor_default_interval_seconds
  return configured && configured > 0 ? configured : DEFAULT_INTERVAL_SECONDS
})

// editing is true when we have an existing monitor
const editing = computed<ChannelMonitor | null>(() => props.monitor)

const submitting = ref(false)

interface MonitorForm {
  name: string
  provider: Provider
  group_id: number | null
  api_mode: APIMode
  primary_model: string
  extra_models: string[]
  interval_seconds: number
  jitter_seconds: number
  enabled: boolean
  simulate_requests: boolean
  // 高级设置快照
  template_id: number | null
  extra_headers: Record<string, string>
  body_override_mode: BodyOverrideMode
  body_override: Record<string, unknown> | null
}

const form = reactive<MonitorForm>({
  name: '',
  provider: PROVIDER_ANTHROPIC,
  group_id: null,
  api_mode: API_MODE_CHAT_COMPLETIONS,
  primary_model: '',
  extra_models: [],
  interval_seconds: systemDefaultInterval.value,
  jitter_seconds: 0,
  enabled: true,
  simulate_requests: false,
  template_id: null,
  extra_headers: {},
  body_override_mode: 'off',
  body_override: null,
})

// jitter 上限与后端校验一致：interval - jitter 不得低于最小检测间隔 15 秒。
const maxJitterSeconds = computed<number>(() => Math.max(0, (form.interval_seconds || 0) - 15))

let suppressFormWatchers = false

interface GroupSelectOption extends Record<string, unknown> {
  value: number
  label: string
  platform: GroupPlatform | null
  platform_label: string
  rate_multiplier: number | null
  disabled?: boolean
}

const groupsCache = ref<AdminGroup[]>([])
const groupsLoading = ref(false)
let groupsLoaded = false

const groupSelectPlaceholder = computed(() => (
  groupsLoading.value
    ? t('common.loading')
    : t('admin.channelMonitor.form.groupPlaceholder')
))

function groupMatchesProvider(group: AdminGroup): boolean {
  return form.provider === PROVIDER_CUSTOM
    ? group.platform === 'composite'
    : group.platform === form.provider
}

const groupOptions = computed<GroupSelectOption[]>(() => {
  const options: GroupSelectOption[] = groupsCache.value
    .filter(groupMatchesProvider)
    .map(group => ({
      value: group.id,
      label: group.name,
      platform: group.platform,
      platform_label: t(`admin.groups.platforms.${group.platform}`),
      rate_multiplier: group.rate_multiplier,
    }))

  const monitor = props.monitor
  if (
    monitor?.group_id != null
    && !options.some(option => option.value === monitor.group_id)
    && monitor.group_name
  ) {
    options.push({
      value: monitor.group_id,
      label: monitor.group_name,
      platform: null,
      platform_label: '',
      rate_multiplier: monitor.group_rate_multiplier,
      disabled: true,
    })
  }

  return options
})

function optionRateMultiplier(value: unknown): number | null {
  return typeof value === 'number' && Number.isFinite(value) ? value : null
}

async function loadGroups() {
  if (groupsLoaded || groupsLoading.value) return
  groupsLoading.value = true
  try {
    groupsCache.value = await adminAPI.groups.getAll()
    groupsLoaded = true
  } catch (err: unknown) {
    appStore.showError(extractApiErrorMessage(err, t('admin.channelMonitor.form.groupLoadError')))
  } finally {
    groupsLoading.value = false
  }
}

// 可用模板列表（进入 dialog 时一次性拉取 cache；按 provider / api mode 过滤）。
const templatesCache = ref<ChannelMonitorTemplate[]>([])
const templatesLoading = ref(false)

const templateOptions = computed(() => {
  const items = templatesCache.value.filter((t) => {
    if (t.provider !== form.provider) return false
    if (!isOpenAICompatibleProvider(form.provider)) return true
    return normalizeAPIMode(t.api_mode) === form.api_mode
  })
  return [
    { value: '', label: t('admin.channelMonitor.templateField.none') },
    ...items.map((t) => ({ value: String(t.id), label: templateOptionLabel(t) })),
  ]
})

async function loadTemplates() {
  if (templatesCache.value.length > 0) return
  templatesLoading.value = true
  try {
    const { items } = await adminAPI.channelMonitorTemplate.list()
    templatesCache.value = items
  } catch (err: unknown) {
    // 模板拉取失败不阻塞监控表单，用户可以不选模板
    console.warn('load monitor templates failed', err)
  } finally {
    templatesLoading.value = false
  }
}

// 模板下拉绑定：value 是 string（Select 组件约束），需要与 number | null 互转。
const templateSelectValue = computed<string>({
  get: () => (form.template_id == null ? '' : String(form.template_id)),
  set: (raw: string) => {
    if (raw === '') {
      form.template_id = null
      return
    }
    const id = Number(raw)
    if (!Number.isFinite(id)) return
    form.template_id = id
    // 应用模板 = 拷贝快照
    const tpl = templatesCache.value.find((t) => t.id === id)
    if (tpl) {
      suppressFormWatchers = true
      form.api_mode = normalizeAPIMode(tpl.api_mode)
      form.template_id = id
      form.extra_headers = { ...(tpl.extra_headers || {}) }
      form.body_override_mode = tpl.body_override_mode
      form.body_override = tpl.body_override ? { ...tpl.body_override } : null
      suppressFormWatchers = false
    }
  },
})

const apiModeOptions = computed<{ value: APIMode; label: string; hint: string }[]>(() => [
  {
    value: API_MODE_CHAT_COMPLETIONS,
    label: t('admin.channelMonitor.form.apiModeChatCompletions'),
    hint: t('admin.channelMonitor.form.apiModeChatCompletionsHint'),
  },
  {
    value: API_MODE_RESPONSES,
    label: t('admin.channelMonitor.form.apiModeResponses'),
    hint: t('admin.channelMonitor.form.apiModeResponsesHint'),
  },
])

function normalizeAPIMode(mode: APIMode | undefined | null): APIMode {
  return mode === API_MODE_RESPONSES ? API_MODE_RESPONSES : API_MODE_CHAT_COMPLETIONS
}

function isOpenAICompatibleProvider(provider: Provider): boolean {
  return provider === PROVIDER_OPENAI || provider === PROVIDER_CUSTOM
}

function apiModeButtonClass(mode: APIMode): string {
  const active = form.api_mode === mode
  if (active) {
    return 'border-primary-500 bg-white text-primary-700 shadow-sm dark:border-primary-400 dark:bg-primary-500/15 dark:text-primary-300'
  }
  return 'border-blue-100 bg-white/70 text-gray-600 hover:border-primary-300 dark:border-dark-700 dark:bg-dark-800 dark:text-gray-400'
}

function templateOptionLabel(tpl: ChannelMonitorTemplate): string {
  if (!isOpenAICompatibleProvider(tpl.provider)) return tpl.name
  const labelKey = normalizeAPIMode(tpl.api_mode) === API_MODE_RESPONSES
    ? 'admin.channelMonitor.form.apiModeResponses'
    : 'admin.channelMonitor.form.apiModeChatCompletions'
  return `${tpl.name} · ${t(labelKey)}`
}

function clearRequestSnapshot() {
  form.template_id = null
  form.extra_headers = {}
  form.body_override_mode = 'off'
  form.body_override = null
}

interface ProviderOption {
  value: Provider
  label: string
}

const providerOptions = computed<ProviderOption[]>(() => [
  { value: PROVIDER_ANTHROPIC, label: t('monitorCommon.providers.anthropic') },
  { value: PROVIDER_OPENAI, label: t('monitorCommon.providers.openai') },
  { value: PROVIDER_GEMINI, label: t('monitorCommon.providers.gemini') },
  { value: PROVIDER_GROK, label: t('monitorCommon.providers.grok') },
  { value: PROVIDER_CUSTOM, label: t('monitorCommon.providers.custom') },
])

function selectProvider(provider: Provider) {
  if (form.provider === provider) return
  const previousProvider = form.provider
  const clearGrokModel =
    previousProvider === PROVIDER_GROK && form.primary_model === DEFAULT_GROK_MODEL
  form.provider = provider
  if (provider === PROVIDER_GROK) {
    if (!form.primary_model.trim()) form.primary_model = DEFAULT_GROK_MODEL
    return
  }
  if (clearGrokModel) form.primary_model = ''
}

// A selected group belongs to the previous platform, so require an explicit
// choice after switching platform. Request templates are provider-specific too.
watch(() => form.provider, () => {
  if (suppressFormWatchers) return
  form.group_id = null
  if (!isOpenAICompatibleProvider(form.provider)) {
    form.api_mode = API_MODE_CHAT_COMPLETIONS
  }
  clearRequestSnapshot()
}, { flush: 'sync' })

watch(() => form.api_mode, () => {
  if (suppressFormWatchers) return
  if (isOpenAICompatibleProvider(form.provider)) {
    clearRequestSnapshot()
  }
}, { flush: 'sync' })

function resetForm() {
  suppressFormWatchers = true
  form.name = ''
  form.provider = PROVIDER_ANTHROPIC
  form.group_id = null
  form.api_mode = API_MODE_CHAT_COMPLETIONS
  form.primary_model = ''
  form.extra_models = []
  form.interval_seconds = systemDefaultInterval.value
  form.jitter_seconds = 0
  form.enabled = true
  form.simulate_requests = false
  form.template_id = null
  form.extra_headers = {}
  form.body_override_mode = 'off'
  form.body_override = null
  suppressFormWatchers = false
}

function loadFromMonitor(m: ChannelMonitor) {
  suppressFormWatchers = true
  form.name = m.name
  form.provider = m.provider
  form.group_id = m.group_id
  form.api_mode = normalizeAPIMode(m.api_mode)
  form.primary_model = m.primary_model
  form.extra_models = [...(m.extra_models || [])]
  form.interval_seconds = m.interval_seconds || systemDefaultInterval.value
  form.jitter_seconds = m.jitter_seconds || 0
  form.enabled = m.enabled
  form.simulate_requests = m.simulate_requests ?? false
  form.template_id = m.template_id ?? null
  form.extra_headers = { ...(m.extra_headers || {}) }
  form.body_override_mode = m.body_override_mode || 'off'
  form.body_override = m.body_override ? { ...m.body_override } : null
  suppressFormWatchers = false
}

// Re-sync form whenever the dialog is opened or the target monitor changes.
// Group and template lists are cached after the first successful load.
watch(
  () => [props.show, props.monitor] as const,
  ([show, m]) => {
    if (!show) return
    void loadGroups()
    void loadTemplates()
    if (m) loadFromMonitor(m)
    else resetForm()
  },
  { immediate: true },
)

function buildPayload(): CreateParams {
  return {
    name: form.name.trim(),
    provider: form.provider,
    group_id: form.group_id as number,
    api_mode: isOpenAICompatibleProvider(form.provider) ? form.api_mode : API_MODE_CHAT_COMPLETIONS,
    primary_model: form.primary_model.trim(),
    extra_models: form.extra_models,
    enabled: form.enabled,
    simulate_requests: form.simulate_requests,
    interval_seconds: form.interval_seconds,
    jitter_seconds: form.jitter_seconds || 0,
    template_id: form.template_id,
    extra_headers: form.extra_headers,
    body_override_mode: form.body_override_mode,
    body_override: form.body_override,
  }
}

async function handleSubmit() {
  if (submitting.value) return
  if (form.group_id == null) {
    appStore.showError(t('admin.channelMonitor.groupRequired'))
    return
  }
  if (!form.primary_model.trim()) {
    appStore.showError(t('admin.channelMonitor.primaryModelRequired'))
    return
  }

  submitting.value = true
  try {
    const target = editing.value
    if (target) {
      const req: UpdateParams = { ...buildPayload() }
      // template_id=null 用 clear_template=true 明确告诉后端清空（pointer 语义）
      if (form.template_id == null) {
        req.clear_template = true
        delete req.template_id
      }
      await adminAPI.channelMonitor.update(target.id, req)
      appStore.showSuccess(t('admin.channelMonitor.updateSuccess'))
    } else {
      await adminAPI.channelMonitor.create(buildPayload())
      appStore.showSuccess(t('admin.channelMonitor.createSuccess'))
    }
    emit('saved')
    emit('close')
  } catch (err: unknown) {
    appStore.showError(extractApiErrorMessage(err, t('common.error')))
  } finally {
    submitting.value = false
  }
}
</script>
