<template>
  <BaseDialog
    :show="show"
    :title="editing ? t('admin.degradation.form.editTitle') : t('admin.degradation.form.createTitle')"
    width="wide"
    @close="emit('close')"
  >
    <form id="degradation-monitor-form" class="space-y-5" @submit.prevent="submit">
      <div>
        <label class="input-label">{{ t('admin.degradation.form.platform') }} <span class="text-red-500">*</span></label>
        <div class="dg-platform-picker">
          <button
            v-for="option in platformOptions"
            :key="option.value"
            type="button"
            class="dg-platform-option"
            :class="{ 'is-active': form.platform === option.value }"
            :aria-pressed="form.platform === option.value"
            :data-testid="`degradation-platform-${option.value}`"
            @click="selectPlatform(option.value)"
          >
            <PlatformIcon :platform="option.value" size="sm" />
            <span>{{ option.label }}</span>
          </button>
        </div>
      </div>

      <div class="grid gap-4 sm:grid-cols-2">
        <div>
          <label class="input-label">{{ t('admin.degradation.form.group') }} <span class="text-red-500">*</span></label>
          <Select
            v-model="form.group_id"
            :options="groupOptions"
            :disabled="groupsLoading"
            :placeholder="groupsLoading ? t('common.loading') : t('admin.degradation.form.groupPlaceholder')"
            :empty-text="t('admin.degradation.form.noGroups')"
            searchable
            data-testid="degradation-group-select"
          />
        </div>
        <div>
          <label class="input-label" for="degradation-monitor-name">{{ t('admin.degradation.form.name') }}</label>
          <input
            id="degradation-monitor-name"
            v-model="form.name"
            type="text"
            maxlength="100"
            class="input"
            :placeholder="t('admin.degradation.form.namePlaceholder')"
          />
        </div>
      </div>

      <div>
        <label class="input-label">{{ t('admin.degradation.form.model') }} <span class="text-red-500">*</span></label>
        <Select
          v-model="form.model"
          :options="modelOptions"
          :placeholder="t('admin.degradation.form.modelPlaceholder')"
          :creatable-prefix="t('admin.degradation.form.useModel')"
          searchable
          creatable
          data-testid="degradation-model-select"
        />
        <p class="input-hint">{{ t('admin.degradation.form.modelHint') }}</p>
      </div>

      <div>
        <label class="input-label">{{ t('admin.degradation.form.protocol') }}</label>
        <div class="dg-protocol-picker">
          <button
            v-for="option in protocolOptions"
            :key="option.value"
            type="button"
            class="dg-protocol-option"
            :class="{ 'is-active': form.protocol === option.value }"
            :aria-pressed="form.protocol === option.value"
            :data-testid="`degradation-protocol-${option.value}`"
            @click="selectProtocol(option.value)"
          >
            <span class="dg-protocol-name">{{ option.label }}</span>
            <span v-if="option.value === defaultProtocol" class="dg-protocol-default">{{ t('admin.degradation.form.defaultTag') }}</span>
          </button>
        </div>
        <p class="input-hint">{{ t('admin.degradation.form.protocolHint') }}</p>
      </div>

      <div class="grid gap-4 sm:grid-cols-2">
        <div>
          <label class="input-label">{{ t('admin.degradation.form.effort') }}</label>
          <Select v-model="form.reasoning_effort" :options="effortOptions" data-testid="degradation-effort-select" />
        </div>
        <div>
          <label class="input-label" for="degradation-monitor-interval">{{ t('admin.degradation.form.interval') }}</label>
          <input
            id="degradation-monitor-interval"
            v-model="form.interval"
            type="number"
            min="5"
            max="10080"
            step="1"
            inputmode="numeric"
            class="input"
            :placeholder="t('admin.degradation.form.intervalPlaceholder', { n: config.default_interval_minutes })"
          />
          <p class="input-hint">{{ t('admin.degradation.form.intervalHint') }}</p>
        </div>
      </div>

      <div>
        <label class="input-label">{{ t('admin.degradation.form.prompt') }}</label>
        <Select v-model="promptSelection" :options="promptOptions" :disabled="form.random_prompt" searchable />
        <div class="dg-toggle-row mt-3">
          <div class="min-w-0">
            <label class="text-sm font-medium text-gray-700" for="degradation-random-prompt">{{ t('admin.degradation.form.randomPrompt') }}</label>
            <p class="input-hint">{{ t('admin.degradation.form.randomPromptHint') }}</p>
          </div>
          <Toggle id="degradation-random-prompt" v-model="form.random_prompt" class="shrink-0" />
        </div>
      </div>

      <div class="dg-toggle-row">
        <div class="min-w-0">
          <label class="text-sm font-medium text-gray-700" for="degradation-enabled">{{ t('admin.degradation.form.enabled') }}</label>
          <p class="input-hint">{{ t('admin.degradation.form.enabledHint') }}</p>
        </div>
        <Toggle id="degradation-enabled" v-model="form.enabled" class="shrink-0" />
      </div>

      <section class="dg-storage" :aria-label="t('admin.degradation.form.storage')">
        <div class="min-w-0">
          <h4 class="dg-storage-title">
            <Icon name="database" size="sm" />
            {{ t('admin.degradation.form.storage') }}
          </h4>
          <p class="input-hint">{{ t('admin.degradation.form.storageHint') }}</p>
          <p v-if="!editing" class="dg-storage-result is-muted">{{ t('admin.degradation.form.storageUnavailable') }}</p>
          <p v-else-if="storage" class="dg-storage-result" data-testid="degradation-storage-stats">
            {{ t('admin.degradation.form.storageStats', { count: storage.count, size: formatBytes(storage.bytes) }) }}
          </p>
        </div>
        <div class="dg-storage-actions">
          <button type="button" class="btn btn-secondary btn-sm" :disabled="!editing || storageLoading" data-testid="degradation-storage-count" @click="loadStorage">
            <Icon name="chartBar" size="sm" :class="{ 'animate-pulse': storageLoading }" />
            {{ t('admin.degradation.form.storageCount') }}
          </button>
          <button type="button" class="btn btn-danger btn-sm" :disabled="!editing || releasing" data-testid="degradation-storage-release" @click="showReleaseConfirm = true">
            <Icon name="trash" size="sm" />
            {{ t('admin.degradation.form.releaseAll') }}
          </button>
        </div>
      </section>
    </form>

    <template #footer>
      <div class="flex justify-end gap-3">
        <button type="button" class="btn btn-secondary" @click="emit('close')">{{ t('common.cancel') }}</button>
        <button type="submit" form="degradation-monitor-form" class="btn btn-primary" :disabled="submitting" data-testid="degradation-monitor-submit">
          {{ submitting ? t('common.submitting') : editing ? t('admin.degradation.form.save') : t('admin.degradation.form.create') }}
        </button>
      </div>
    </template>
  </BaseDialog>

  <ConfirmDialog
    :show="showReleaseConfirm"
    :title="t('admin.degradation.form.releaseTitle')"
    :message="t('admin.degradation.form.releaseConfirm')"
    :confirm-text="t('admin.degradation.form.releaseAll')"
    :loading="releasing"
    danger
    @confirm="releaseAll"
    @cancel="showReleaseConfirm = false"
  />
</template>

<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import type {
  DegradationConfig,
  DegradationEffort,
  DegradationMonitor,
  DegradationMonitorPayload,
  DegradationPrompt,
  DegradationProtocol,
  DegradationStorageStats,
} from '@/api/admin/degradation'
import type { AdminGroup } from '@/types'
import BaseDialog from '@/components/common/BaseDialog.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import PlatformIcon from '@/components/common/PlatformIcon.vue'
import Select from '@/components/common/Select.vue'
import Toggle from '@/components/common/Toggle.vue'
import Icon from '@/components/icons/Icon.vue'
import { useAppStore } from '@/stores/app'
import { extractApiErrorMessage } from '@/utils/apiError'
import { formatBytes } from '@/utils/format'

type PlatformValue = 'anthropic' | 'openai' | 'gemini' | 'grok' | 'composite'

const EFFORTS: DegradationEffort[] = ['none', 'minimal', 'low', 'medium', 'high', 'xhigh', 'max']

const props = defineProps<{
  show: boolean
  monitor: DegradationMonitor | null
  prompts: DegradationPrompt[]
  config: DegradationConfig
}>()

const emit = defineEmits<{
  (e: 'close'): void
  (e: 'saved'): void
}>()

const { t } = useI18n()
const appStore = useAppStore()

const editing = computed(() => props.monitor !== null)
const submitting = ref(false)
const groups = ref<AdminGroup[]>([])
const groupsLoading = ref(false)
let groupsLoaded = false
const modelCandidates = ref<string[]>([])
let modelRequest = 0
const storage = ref<DegradationStorageStats | null>(null)
const storageLoading = ref(false)
const releasing = ref(false)
const showReleaseConfirm = ref(false)

const form = reactive({
  platform: 'openai' as PlatformValue,
  group_id: null as number | null,
  name: '',
  model: '',
  protocol: 'openai_responses' as DegradationProtocol,
  protocolTouched: false,
  reasoning_effort: '' as DegradationEffort | '',
  interval: '' as string | number,
  prompt_id: null as number | null,
  random_prompt: false,
  enabled: true,
})

const platformOptions = computed<{ value: PlatformValue; label: string }[]>(() => [
  { value: 'anthropic', label: t('degradationCheck.platforms.anthropic') },
  { value: 'openai', label: t('degradationCheck.platforms.openai') },
  { value: 'gemini', label: t('degradationCheck.platforms.gemini') },
  { value: 'grok', label: t('degradationCheck.platforms.grok') },
  { value: 'composite', label: t('degradationCheck.platforms.custom') },
])

const defaultProtocol = computed<DegradationProtocol>(() => (form.platform === 'anthropic' ? 'anthropic' : 'openai_responses'))

const protocolOptions = computed<{ value: DegradationProtocol; label: string }[]>(() => [
  { value: 'openai_responses', label: t('degradationCheck.protocols.openai_responses') },
  { value: 'openai_chat', label: t('degradationCheck.protocols.openai_chat') },
  { value: 'anthropic', label: t('degradationCheck.protocols.anthropic') },
])

const groupOptions = computed(() => {
  const options: { value: number; label: string; disabled?: boolean }[] = groups.value
    .filter((group) => group.platform === form.platform)
    .map((group) => ({ value: group.id, label: group.name }))
  const monitor = props.monitor
  if (monitor && monitor.group_platform === form.platform && !options.some((option) => option.value === monitor.group_id)) {
    options.push({ value: monitor.group_id, label: monitor.group_name || `#${monitor.group_id}`, disabled: true })
  }
  return options
})

const modelOptions = computed(() => {
  const models = new Set(modelCandidates.value)
  if (form.model) models.add(form.model)
  return [...models].map((model) => ({ value: model, label: model }))
})

const effortOptions = computed(() => [
  { value: '', label: t('admin.degradation.form.effortFollow', { effort: props.config.default_reasoning_effort }) },
  ...EFFORTS.map((effort) => ({ value: effort, label: t(`degradationCheck.efforts.${effort}`) })),
])

const defaultPromptName = computed(() => {
  const preset = props.prompts.find((prompt) => prompt.id === props.config.default_prompt_id) ?? props.prompts[0]
  return preset?.name ?? t('admin.degradation.config.emptyPresets')
})

const promptOptions = computed(() => [
  { value: '', label: t('admin.degradation.form.promptFollow', { name: defaultPromptName.value }) },
  ...props.prompts.map((prompt) => ({ value: String(prompt.id), label: prompt.name })),
])

// Select works with string values; '' means "follow the default preset".
const promptSelection = computed<string>({
  get: () => (form.prompt_id === null ? '' : String(form.prompt_id)),
  set: (value) => {
    const id = Number(value)
    form.prompt_id = value === '' || !Number.isFinite(id) ? null : id
  },
})

async function loadGroups() {
  if (groupsLoaded || groupsLoading.value) return
  groupsLoading.value = true
  try {
    groups.value = await adminAPI.groups.getAll()
    groupsLoaded = true
  } catch (err: unknown) {
    appStore.showError(extractApiErrorMessage(err, t('admin.degradation.form.groupLoadFailed')))
  } finally {
    groupsLoading.value = false
  }
}

async function loadModelCandidates(groupId: number | null) {
  const request = ++modelRequest
  if (!groupId) {
    modelCandidates.value = []
    return
  }
  try {
    const models = await adminAPI.groups.getModelsListCandidates(groupId)
    if (request === modelRequest) modelCandidates.value = models
  } catch {
    if (request === modelRequest) modelCandidates.value = []
  }
}

function selectPlatform(platform: PlatformValue) {
  if (form.platform === platform) return
  form.platform = platform
  form.group_id = null
  if (!form.protocolTouched) form.protocol = defaultProtocol.value
}

function selectProtocol(protocol: DegradationProtocol) {
  form.protocol = protocol
  form.protocolTouched = protocol !== defaultProtocol.value
}

function reset() {
  const monitor = props.monitor
  storage.value = null
  showReleaseConfirm.value = false
  if (monitor) {
    form.platform = (monitor.group_platform || 'openai') as PlatformValue
    form.group_id = monitor.group_id
    form.name = monitor.name
    form.model = monitor.model
    form.protocol = monitor.protocol
    form.protocolTouched = monitor.protocol !== defaultProtocol.value
    form.reasoning_effort = monitor.reasoning_effort
    form.interval = monitor.interval_minutes > 0 ? monitor.interval_minutes : ''
    form.prompt_id = monitor.prompt_id
    form.random_prompt = monitor.random_prompt
    form.enabled = monitor.enabled
  } else {
    form.platform = 'openai'
    form.group_id = null
    form.name = ''
    form.model = ''
    form.protocol = 'openai_responses'
    form.protocolTouched = false
    form.reasoning_effort = ''
    form.interval = ''
    form.prompt_id = null
    form.random_prompt = false
    form.enabled = true
  }
}

function parseInterval(): number | null {
  if (form.interval === '' || form.interval === null) return 0
  const value = Number(form.interval)
  if (!Number.isInteger(value) || value < 5 || value > 10080) return null
  return value
}

async function submit() {
  if (submitting.value) return
  if (!form.group_id) {
    appStore.showError(t('admin.degradation.form.groupRequired'))
    return
  }
  if (!form.model.trim()) {
    appStore.showError(t('admin.degradation.form.modelRequired'))
    return
  }
  const interval = parseInterval()
  if (interval === null) {
    appStore.showError(t('admin.degradation.form.intervalInvalid'))
    return
  }
  const payload: DegradationMonitorPayload = {
    name: form.name.trim(),
    group_id: form.group_id,
    model: form.model.trim(),
    reasoning_effort: form.reasoning_effort,
    protocol: form.protocol,
    interval_minutes: interval,
    prompt_id: form.prompt_id,
    random_prompt: form.random_prompt,
    enabled: form.enabled,
  }
  submitting.value = true
  try {
    if (props.monitor) {
      await adminAPI.degradation.updateMonitor(props.monitor.id, payload)
      appStore.showSuccess(t('admin.degradation.form.updated'))
    } else {
      await adminAPI.degradation.createMonitor(payload)
      appStore.showSuccess(t('admin.degradation.form.created'))
    }
    emit('saved')
    emit('close')
  } catch (err: unknown) {
    appStore.showError(extractApiErrorMessage(err, t('common.error')))
  } finally {
    submitting.value = false
  }
}

async function loadStorage() {
  if (!props.monitor || storageLoading.value) return
  storageLoading.value = true
  try {
    storage.value = await adminAPI.degradation.getStorage(props.monitor.id)
  } catch (err: unknown) {
    appStore.showError(extractApiErrorMessage(err, t('admin.degradation.form.statsFailed')))
  } finally {
    storageLoading.value = false
  }
}

async function releaseAll() {
  if (!props.monitor || releasing.value) return
  releasing.value = true
  try {
    const result = await adminAPI.degradation.releaseStorage(props.monitor.id)
    appStore.showSuccess(t('admin.degradation.form.released', { count: result.deleted }))
    showReleaseConfirm.value = false
    await loadStorage()
    emit('saved')
  } catch (err: unknown) {
    appStore.showError(extractApiErrorMessage(err, t('admin.degradation.form.releaseFailed')))
  } finally {
    releasing.value = false
  }
}

watch(() => [props.show, props.monitor] as const, ([show]) => {
  if (!show) return
  reset()
  void loadGroups()
  void loadModelCandidates(form.group_id)
}, { immediate: true })

watch(() => form.group_id, (groupId) => {
  if (props.show) void loadModelCandidates(groupId)
})
</script>

<style scoped>
.dg-platform-picker {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 0.5rem;
}

@media (min-width: 640px) {
  .dg-platform-picker { grid-template-columns: repeat(5, minmax(0, 1fr)); }
}

.dg-platform-option,
.dg-protocol-option {
  display: flex;
  min-height: 2.75rem;
  align-items: center;
  justify-content: center;
  gap: 0.5rem;
  padding: 0.5rem 0.75rem;
  border: 2px solid transparent;
  border-radius: 1rem;
  background: var(--md-sys-color-surface-container);
  font-size: 0.875rem;
  font-weight: 600;
  color: var(--md-sys-color-on-surface-variant);
  transition: border-color 160ms ease, background-color 160ms ease, color 160ms ease;
}

.dg-platform-option:hover,
.dg-protocol-option:hover {
  color: var(--md-sys-color-on-surface);
  background: color-mix(in srgb, var(--md-sys-color-surface-container) 70%, var(--md-sys-color-surface-container-high));
}

.dg-platform-option.is-active,
.dg-protocol-option.is-active {
  border-color: var(--md-sys-color-primary);
  background: var(--md-sys-color-surface);
  color: var(--md-sys-color-on-surface);
  box-shadow: 0 4px 12px -8px rgb(151 104 0 / 0.55);
}

.dg-protocol-picker {
  display: grid;
  gap: 0.5rem;
}

@media (min-width: 640px) {
  .dg-protocol-picker { grid-template-columns: repeat(3, minmax(0, 1fr)); }
}

.dg-protocol-default {
  padding: 0 0.4rem;
  border-radius: 999px;
  background: var(--md-sys-color-primary);
  font-size: 0.625rem;
  font-weight: 700;
  color: var(--md-sys-color-on-primary);
}

.dg-toggle-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 1rem;
}

.dg-storage {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 0.75rem;
  padding: 0.875rem 1rem;
  border-radius: 1rem;
  background: var(--md-sys-color-surface-container);
}

.dg-storage-title {
  display: flex;
  align-items: center;
  gap: 0.375rem;
  font-size: 0.875rem;
  font-weight: 700;
  color: var(--md-sys-color-on-surface);
}

.dg-storage-result {
  margin-top: 0.375rem;
  font-size: 0.8125rem;
  font-weight: 650;
  color: #7e5600;
}

.dg-storage-result.is-muted {
  font-weight: 500;
  color: var(--md-sys-color-on-surface-variant);
}

.dg-storage-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 0.5rem;
}
</style>
