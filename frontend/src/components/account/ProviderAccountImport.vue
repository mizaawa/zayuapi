<template>
  <section class="provider-import" :aria-label="t('admin.providerImport.sourceProvider')" data-testid="provider-account-import">
    <div class="provider-import-field">
      <div class="provider-import-heading">
        <span class="input-label mb-0">{{ t('admin.providerImport.selectProvider') }}</span>
        <button
          type="button"
          class="provider-import-refresh"
          data-testid="provider-import-refresh-providers"
          :disabled="disabled || creating || providersLoading"
          @click="refreshProviders"
        >
          <Icon name="refresh" size="xs" :class="{ 'animate-spin': providersLoading }" />
          {{ t('admin.providerImport.refreshProviders') }}
        </button>
      </div>
      <ProviderImportPicker
        :model-value="providerId"
        :options="providerOptions"
        icon="server"
        :label="t('admin.providerImport.selectProvider')"
        :placeholder="t('admin.providerImport.providerPlaceholder')"
        :hint="t('admin.providerImport.providerHint')"
        :count-text="t('admin.providerImport.providerCount', { count: providers.length })"
        :loading="providersLoading"
        :loading-text="t('admin.providerImport.loading')"
        :disabled="disabled || creating"
        test-id="provider-import-provider-picker"
        @update:model-value="chooseProvider"
      />
      <p v-if="providersError" class="provider-import-note is-error" role="alert">
        <Icon name="exclamationCircle" size="sm" />
        <span>{{ providersError }}</span>
      </p>
      <p v-else-if="providerVanished" class="provider-import-note is-error" role="alert">
        <Icon name="exclamationCircle" size="sm" />
        <span>{{ t('admin.providerImport.providerMissing') }}</span>
      </p>
      <p v-else-if="providersLoaded && !providersLoading && !providers.length" class="provider-import-note is-info" data-testid="provider-import-empty">
        <Icon name="infoCircle" size="sm" />
        <span>
          {{ t('admin.providerImport.noProviders') }}
          <RouterLink to="/admin/upstream" target="_blank" rel="noopener noreferrer">{{ t('admin.providerImport.addProviderLink') }}</RouterLink>
        </span>
      </p>
    </div>

    <div class="provider-import-field">
      <div class="provider-import-heading">
        <span class="input-label mb-0">{{ t('admin.providerImport.selectKey') }}</span>
        <button
          type="button"
          class="provider-import-refresh"
          data-testid="provider-import-refresh-keys"
          :disabled="disabled || creating || !selectedProvider || keysLoading"
          @click="refreshKeys"
        >
          <Icon name="refresh" size="xs" :class="{ 'animate-spin': keysLoading }" />
          {{ t('admin.providerImport.refreshKeys') }}
        </button>
      </div>
      <ProviderImportPicker
        :model-value="keyId"
        :options="keyOptions"
        icon="key"
        cards
        :label="t('admin.providerImport.selectKey')"
        :placeholder="t('admin.providerImport.keyPlaceholder')"
        :hint="selectedProvider ? t('admin.providerImport.keyHint') : t('admin.providerImport.keyHintNoProvider')"
        :count-text="t('admin.providerImport.keyCount', { count: keyOptions.length })"
        :loading="keysLoading"
        :loading-text="t('admin.providerImport.loading')"
        :disabled="disabled || creating || !keyList"
        :action="createAction"
        :empty-text="t('admin.providerImport.noKeys')"
        test-id="provider-import-key-picker"
        @update:model-value="chooseKey"
        @action="openCreate"
      />
      <p
        v-if="notice"
        class="provider-import-note"
        :class="notice.tone === 'success' ? 'is-success' : 'is-info'"
        role="status"
        data-testid="provider-import-notice"
      >
        <Icon :name="notice.tone === 'success' ? 'checkCircle' : 'infoCircle'" size="sm" />
        <span>{{ notice.text }}</span>
      </p>
      <p v-if="keysError" class="provider-import-note is-error" role="alert">
        <Icon name="exclamationCircle" size="sm" />
        <span>{{ keysError }}</span>
      </p>
      <p v-else-if="keyProblem" class="provider-import-note is-error" role="alert">
        <Icon name="exclamationCircle" size="sm" />
        <span>{{ keyProblem }}</span>
      </p>
      <p v-else-if="keyList && !keyList.keys.length && !notice" class="provider-import-note">
        <Icon name="infoCircle" size="sm" />
        <span>{{ t('admin.providerImport.noKeys') }}</span>
      </p>
      <div v-if="selection" class="provider-import-endpoint" data-testid="provider-import-endpoint">
        <Icon name="link" size="sm" />
        <span>{{ t('admin.providerImport.boundEndpoint') }}<code>{{ selection.endpoint }}</code></span>
      </div>
      <p v-if="syncedAt" class="provider-import-synced">{{ t('admin.providerImport.lastSynced', { time: syncedAt }) }}</p>
    </div>

    <ProviderKeyCreateDialog
      :show="creating"
      :provider="selectedProvider ?? null"
      @close="closeCreate"
      @created="keyCreated"
    />
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import ProviderImportPicker from './ProviderImportPicker.vue'
import ProviderKeyCreateDialog from './ProviderKeyCreateDialog.vue'
import {
  listProviderKeys,
  listProviderOptions,
  type ProviderKey,
  type ProviderKeyCreated,
  type ProviderKeyList,
  type ProviderOption
} from '@/api/admin/upstream'
import {
  formatProviderMultiplier,
  formatProviderSyncedAt,
  providerAccountEndpoint,
  providerErrorText,
  providerPlatformLabel,
  type ProviderAccountSelection,
  type ProviderPickerOption
} from './providerImport'
import type { AccountPlatform } from '@/types'
import '@/styles/provider-import.css'

const props = withDefaults(defineProps<{
  platform: AccountPlatform | string
  disabled?: boolean
}>(), {
  disabled: false
})

const emit = defineEmits<{
  select: [selection: ProviderAccountSelection | null]
}>()

const { t } = useI18n()

const providers = ref<ProviderOption[]>([])
const providersLoading = ref(false)
const providersLoaded = ref(false)
const providersError = ref('')
const providerVanished = ref(false)
const providerId = ref('')
const keyList = ref<ProviderKeyList | null>(null)
const keysLoading = ref(false)
const keysError = ref('')
const keyId = ref('')
const creating = ref(false)
const notice = ref<{ tone: 'success' | 'info'; text: string } | null>(null)
// Responses for a superseded provider or refresh must not overwrite newer state.
let providerRequest = 0
let keyRequest = 0

const selectedProvider = computed(() => providers.value.find((provider) => provider.id === providerId.value))
const selectedKey = computed(() => keyList.value?.keys.find((key) => key.id === keyId.value))
const siteTypeLabel = (kind: string) => (kind === 'newapi' ? 'New API' : 'Sub2API')
const unnamed = (name: string) => name || t('admin.providerImport.unnamedKey')

const providerOptions = computed<ProviderPickerOption[]>(() => providers.value.map((provider) => {
  const disconnected = provider.status === 'disconnected'
  return {
    id: provider.id,
    name: provider.name,
    detail: provider.base_url,
    badge: disconnected
      ? `${siteTypeLabel(provider.site_type)} · ${t('admin.providerImport.providerDisconnected')}`
      : siteTypeLabel(provider.site_type),
    badgeTone: disconnected ? 'warning' : 'honey'
  }
}))

function keyStatusLabel(key: ProviderKey): string {
  if (key.available) return t('admin.providerImport.keyStatus.available')
  switch (key.status) {
    case 'disabled':
      return t('admin.providerImport.keyStatus.disabled')
    case 'expired':
      return t('admin.providerImport.keyStatus.expired')
    case 'quota_exhausted':
      return t('admin.providerImport.keyStatus.quotaExhausted')
    default:
      return t('admin.providerImport.keyStatus.unavailable')
  }
}

function keyDetail(key: ProviderKey): string {
  const group = key.group_name || t('admin.providerImport.defaultGroup')
  const platform = providerPlatformLabel(key.group_platform)
  const multiplier = t('admin.providerImport.multiplier', { value: formatProviderMultiplier(key.group_multiplier) })
  return [group, platform, multiplier].filter(Boolean).join(' · ')
}

const keyOptions = computed<ProviderPickerOption[]>(() => (keyList.value?.keys ?? []).map((key) => ({
  id: key.id,
  name: unnamed(key.name),
  detail: keyDetail(key),
  badge: keyStatusLabel(key),
  badgeTone: key.available ? 'success' : 'neutral',
  available: key.available
})))

const createAction = computed(() => (keyList.value && !keysError.value
  ? { name: t('admin.providerImport.createFromProvider'), detail: t('admin.providerImport.createFromProviderHint') }
  : null))

const keyProblem = computed(() => {
  if (!keyId.value || !keyList.value || keysLoading.value) return ''
  if (!selectedKey.value) return t('admin.providerImport.keyMissing')
  if (!selectedKey.value.available) return t('admin.providerImport.keyUnavailable', { name: unnamed(selectedKey.value.name) })
  return ''
})

const selection = computed<ProviderAccountSelection | null>(() => {
  const provider = selectedProvider.value
  const key = selectedKey.value
  if (!provider || !key?.available || !keyList.value || keysLoading.value || keysError.value) return null
  return {
    providerId: provider.id,
    providerName: provider.name,
    keyId: key.id,
    keyName: unnamed(key.name),
    groupName: key.group_name,
    groupMultiplier: key.group_multiplier,
    endpoint: providerAccountEndpoint(keyList.value.endpoint || provider.base_url, props.platform)
  }
})

const syncedAt = computed(() => formatProviderSyncedAt(keyList.value?.synced_at))

watch(selection, (value, previous) => {
  if (JSON.stringify(value) !== JSON.stringify(previous)) emit('select', value)
})

function resetKeys() {
  keyRequest++
  keyList.value = null
  keyId.value = ''
  keysError.value = ''
  keysLoading.value = false
}

async function loadProviders() {
  const request = ++providerRequest
  providersLoading.value = true
  providersError.value = ''
  try {
    const list = await listProviderOptions()
    if (request !== providerRequest) return
    providers.value = list
    providersLoaded.value = true
    if (providerId.value && !list.some((provider) => provider.id === providerId.value)) {
      providerVanished.value = true
      providerId.value = ''
      resetKeys()
    } else if (!providerId.value && list.length === 1) {
      // A single provider is the obvious choice.
      chooseProvider(list[0].id)
    }
  } catch (error) {
    if (request === providerRequest) providersError.value = providerErrorText(error, t('admin.providerImport.loadProvidersFailed'))
  } finally {
    if (request === providerRequest) providersLoading.value = false
  }
}

async function loadKeys(preferredKeyId = '') {
  const id = providerId.value
  if (!id) return
  const request = ++keyRequest
  keysLoading.value = true
  keysError.value = ''
  try {
    const list = await listProviderKeys(id)
    if (request !== keyRequest || id !== providerId.value) return
    keyList.value = { ...list, keys: list.keys ?? [] }
    const preferred = preferredKeyId ? keyList.value.keys.find((key) => key.id === preferredKeyId && key.available) : undefined
    if (preferred) keyId.value = preferred.id
  } catch (error) {
    if (request !== keyRequest) return
    keyList.value = null
    keysError.value = providerErrorText(error, t('admin.providerImport.loadKeysFailed'))
  } finally {
    if (request === keyRequest) keysLoading.value = false
  }
}

function chooseProvider(id: string) {
  if (id === providerId.value) return
  providerId.value = id
  providerVanished.value = false
  notice.value = null
  resetKeys()
  void loadKeys()
}

function chooseKey(id: string) {
  const key = keyList.value?.keys.find((candidate) => candidate.id === id)
  if (!key?.available) return
  keyId.value = key.id
  notice.value = null
}

async function refreshProviders() {
  const previous = providerId.value
  notice.value = null
  await loadProviders()
  if (previous && previous === providerId.value) await loadKeys()
}

function refreshKeys() {
  notice.value = null
  void loadKeys()
}

function openCreate() {
  if (!selectedProvider.value || !keyList.value || keysLoading.value || props.disabled) return
  notice.value = null
  creating.value = true
}

function closeCreate(refresh: boolean) {
  creating.value = false
  // After an uncertain creation the list is the only source of truth.
  if (refresh) void loadKeys()
}

async function keyCreated(result: ProviderKeyCreated) {
  creating.value = false
  if (result.provider_id !== providerId.value) return
  await loadKeys(result.key_id)
  const created = result.key_id ? selectedKey.value : undefined
  notice.value = created && created.id === result.key_id
    ? { tone: 'success', text: t('admin.providerImport.createdSelected', { name: unnamed(created.name) }) }
    : { tone: 'info', text: t('admin.providerImport.createdPending', { name: result.name }) }
}

onMounted(() => {
  void loadProviders()
})
</script>
