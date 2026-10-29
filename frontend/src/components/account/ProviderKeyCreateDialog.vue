<template>
  <BaseDialog
    :show="show"
    :title="t('admin.providerImport.createTitle')"
    width="normal"
    :z-index="70"
    @close="close"
  >
    <form id="provider-key-create-form" class="provider-key-create" @submit.prevent="create">
      <p class="provider-key-create-subtitle">
        <Icon name="server" size="sm" />
        <span>{{ t('admin.providerImport.createSubtitle', { provider: provider?.name ?? '' }) }}</span>
      </p>

      <div class="provider-import-field">
        <div class="provider-import-heading">
          <span class="input-label mb-0">{{ t('admin.providerImport.selectGroup') }}</span>
          <button
            v-if="!creating && !uncertain"
            type="button"
            class="provider-import-refresh"
            data-testid="provider-key-refresh-groups"
            :disabled="loading"
            @click="loadGroups"
          >
            <Icon name="refresh" size="xs" :class="{ 'animate-spin': loading }" />
            {{ t('admin.providerImport.refreshGroups') }}
          </button>
        </div>
        <ProviderImportPicker
          :model-value="groupId"
          :options="groupOptions"
          icon="key"
          :label="t('admin.providerImport.selectGroup')"
          :placeholder="t('admin.providerImport.groupPlaceholder')"
          :hint="t('admin.providerImport.groupHint')"
          :count-text="t('admin.providerImport.groupCount', { count: groups.length })"
          :loading="loading"
          :loading-text="t('admin.providerImport.loading')"
          :disabled="creating || uncertain"
          test-id="provider-key-group-picker"
          @update:model-value="groupId = $event"
        />
        <p v-if="loaded && !loading && !error && !groups.length" class="provider-import-note is-info">
          <Icon name="infoCircle" size="sm" />
          <span>{{ t('admin.providerImport.noGroups') }}</span>
        </p>
      </div>

      <div class="provider-key-name-preview">
        <Icon name="key" size="md" />
        <span>
          {{ t('admin.providerImport.keyName') }}
          <strong data-testid="provider-key-name-preview">{{ selectedGroup?.key_name ?? t('admin.providerImport.keyNamePending') }}</strong>
        </span>
      </div>
      <p class="provider-import-note">
        <Icon name="infoCircle" size="sm" />
        <span>{{ t('admin.providerImport.createNote') }}</span>
      </p>
      <p v-if="error" class="provider-import-note is-error" role="alert" data-testid="provider-key-create-error">
        <Icon name="exclamationCircle" size="sm" />
        <span>{{ error }}</span>
      </p>
    </form>

    <template #footer>
      <div class="flex justify-end gap-3">
        <button type="button" class="btn btn-secondary" :disabled="creating" @click="close">{{ t('common.cancel') }}</button>
        <button
          type="submit"
          form="provider-key-create-form"
          class="btn btn-primary"
          data-testid="provider-key-create-submit"
          :disabled="loading || creating || uncertain || !selectedGroup"
        >
          <Icon v-if="!creating" name="plus" size="sm" />
          {{ creating ? t('admin.providerImport.creating') : t('admin.providerImport.create') }}
        </button>
      </div>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import ProviderImportPicker from './ProviderImportPicker.vue'
import {
  createProviderKey,
  listProviderKeyGroups,
  type ProviderKeyCreated,
  type ProviderKeyGroup,
  type ProviderOption
} from '@/api/admin/upstream'
import {
  formatProviderMultiplier,
  providerErrorReason,
  providerErrorStatus,
  providerErrorText,
  providerImportErrorMessage,
  providerPlatformLabel,
  type ProviderPickerOption
} from './providerImport'
import '@/styles/provider-import.css'

const props = defineProps<{
  show: boolean
  provider: ProviderOption | null
}>()

const emit = defineEmits<{
  /** `refresh` is true when an uncertain creation requires a key list reload. */
  close: [refresh: boolean]
  created: [result: ProviderKeyCreated]
}>()

const { t } = useI18n()

const groups = ref<ProviderKeyGroup[]>([])
const groupId = ref('')
const loading = ref(false)
const loaded = ref(false)
const creating = ref(false)
const uncertain = ref(false)
const error = ref('')
let request = 0

const selectedGroup = computed(() => groups.value.find((group) => group.id === groupId.value))

const groupOptions = computed<ProviderPickerOption[]>(() => groups.value.map((group) => {
  const multiplier = formatProviderMultiplier(group.multiplier)
  return {
    id: group.id,
    name: group.name,
    detail: [providerPlatformLabel(group.platform), t('admin.providerImport.groupMultiplier', { value: multiplier })]
      .filter(Boolean)
      .join(' · '),
    badge: multiplier,
    badgeTone: 'honey'
  }
}))

async function loadGroups() {
  const provider = props.provider
  if (!provider) return
  const current = ++request
  loading.value = true
  error.value = ''
  try {
    const result = await listProviderKeyGroups(provider.id)
    if (current !== request) return
    groups.value = result.groups ?? []
    loaded.value = true
    if (!groups.value.some((group) => group.id === groupId.value)) groupId.value = ''
  } catch (failure) {
    if (current !== request) return
    groups.value = []
    groupId.value = ''
    error.value = providerImportErrorMessage(failure, t) || providerErrorText(failure, t('admin.providerImport.loadGroupsFailed'))
  } finally {
    if (current === request) loading.value = false
  }
}

watch(
  () => [props.show, props.provider?.id] as const,
  ([show]) => {
    request++
    groups.value = []
    groupId.value = ''
    loaded.value = false
    loading.value = false
    creating.value = false
    uncertain.value = false
    error.value = ''
    if (show) void loadGroups()
  },
  { immediate: true }
)

async function create() {
  const provider = props.provider
  const group = selectedGroup.value
  if (!provider || !group || loading.value || creating.value || uncertain.value) return
  creating.value = true
  error.value = ''
  try {
    const result = await createProviderKey(provider.id, group.id)
    emit('created', result)
  } catch (failure) {
    // A timeout or an unconfirmed upstream write may still have created the
    // key. Require a list refresh instead of offering a duplicate creation.
    if (providerErrorReason(failure) === 'PROVIDER_KEY_CREATE_UNCERTAIN' || providerErrorStatus(failure) === 0) {
      uncertain.value = true
      error.value = t('admin.providerImport.createUncertain')
    } else {
      const message = providerImportErrorMessage(failure, t).replace(/[。.]+$/u, '')
      error.value = t('admin.providerImport.createFailed', {
        message: message || providerErrorText(failure, t('admin.providerImport.createKeyFailed'))
      })
    }
  } finally {
    creating.value = false
  }
}

function close() {
  if (creating.value) return
  emit('close', uncertain.value)
}
</script>
