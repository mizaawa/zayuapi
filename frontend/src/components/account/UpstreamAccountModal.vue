<template>
  <BaseDialog :show="show" :title="account ? t('admin.upstream.editProvider') : t('admin.upstream.add')" width="wide" @close="emit('close')">
    <form id="upstream-account-form" class="space-y-5" @submit.prevent="submit">
      <div class="rounded-xl border border-gray-200 p-4 dark:border-dark-600">
        <h4 class="mb-4 text-sm font-semibold text-gray-900 dark:text-white">{{ t('admin.upstream.connection') }}</h4>
        <div class="grid gap-4 sm:grid-cols-2">
          <label class="block sm:col-span-2"><span class="input-label">{{ t('admin.accounts.accountName') }}</span><input v-model.trim="name" class="input" required /></label>
          <label class="block sm:col-span-2"><span class="input-label">{{ t('admin.accounts.baseUrl') }}</span><input v-model.trim="baseUrl" class="input font-mono text-sm" type="url" placeholder="https://api.example.com/v1" required /></label>
          <label class="block sm:col-span-2"><span class="input-label">API Key</span><input v-model.trim="apiKey" class="input font-mono text-sm" type="password" autocomplete="new-password" :required="!account" :placeholder="account ? t('admin.upstream.keepKey') : 'sk-…'" /></label>
          <label class="block"><span class="input-label">{{ t('admin.accounts.concurrency') }}</span><input v-model.number="concurrency" class="input" type="number" min="1" max="10000" required /></label>
          <label class="block"><span class="input-label">{{ t('admin.accounts.priority') }}</span><input v-model.number="priority" class="input" type="number" min="1" max="10000" required /></label>
        </div>
      </div>

      <div class="rounded-xl border border-gray-200 p-4 dark:border-dark-600">
        <div class="mb-4 flex flex-wrap items-center justify-between gap-2">
          <h4 class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('admin.upstream.models') }}</h4>
          <button type="button" class="btn btn-secondary text-xs" :disabled="discovering || !baseUrl || (!apiKey && !account)" @click="discoverModels">{{ discovering ? t('admin.upstream.loading') : t('admin.upstream.discover') }}</button>
        </div>
        <label class="block"><span class="input-label">{{ t('admin.upstream.availableModels') }}</span><textarea v-model="modelLines" class="input min-h-28 font-mono text-sm" rows="4" :placeholder="t('admin.upstream.modelsPlaceholder')" /></label>
        <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.upstream.modelsHint') }}</p>
        <div class="mt-4 flex items-center justify-between gap-2">
          <span class="input-label mb-0">{{ t('admin.upstream.modelAliases') }}</span>
          <button type="button" class="text-xs font-medium text-primary-600 hover:text-primary-700" @click="aliases.push({ from: '', to: '' })">+ {{ t('admin.accounts.addMapping') }}</button>
        </div>
        <div v-for="(alias, index) in aliases" :key="index" class="mt-2 flex items-center gap-2">
          <input v-model.trim="alias.from" class="input min-w-0 flex-1 font-mono text-xs" :placeholder="t('admin.accounts.requestModel')" :aria-label="t('admin.accounts.requestModel')" />
          <span class="shrink-0 text-gray-400">→</span>
          <input v-model.trim="alias.to" class="input min-w-0 flex-1 font-mono text-xs" :placeholder="t('admin.accounts.actualModel')" :aria-label="t('admin.accounts.actualModel')" />
          <button type="button" class="shrink-0 px-1 text-red-500" :aria-label="t('common.delete')" @click="aliases.splice(index, 1)">×</button>
        </div>
      </div>

      <div class="rounded-xl border border-gray-200 p-4 dark:border-dark-600">
        <h4 class="mb-4 text-sm font-semibold text-gray-900 dark:text-white">{{ t('admin.upstream.routing') }}</h4>
        <GroupSelector v-model="groupIds" :groups="groups" platform="custom" />
        <div class="mt-4"><label class="input-label">{{ t('admin.accounts.proxy') }}</label><ProxySelector v-model="proxyId" :proxies="proxies" /></div>
      </div>
    </form>
    <template #footer><div class="flex w-full justify-end gap-2"><button class="btn btn-secondary" type="button" @click="emit('close')">{{ t('common.cancel') }}</button><button class="btn btn-primary" type="submit" form="upstream-account-form" :disabled="saving">{{ saving ? t('admin.upstream.savingProvider') : t('common.save') }}</button></div></template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import type { Account, AdminGroup, Proxy } from '@/types'
import { useAppStore } from '@/stores/app'
import BaseDialog from '@/components/common/BaseDialog.vue'
import GroupSelector from '@/components/common/GroupSelector.vue'
import ProxySelector from '@/components/common/ProxySelector.vue'

const props = defineProps<{ show: boolean; account?: Account | null; groups: AdminGroup[]; proxies: Proxy[] }>()
const emit = defineEmits<{ (event: 'close'): void; (event: 'saved'): void }>()
const { t } = useI18n()
const appStore = useAppStore()
const name = ref('')
const baseUrl = ref('')
const apiKey = ref('')
const concurrency = ref(10)
const priority = ref(50)
const groupIds = ref<number[]>([])
const proxyId = ref<number | null>(null)
const modelLines = ref('')
const aliases = ref<{ from: string; to: string }[]>([])
const saving = ref(false)
const discovering = ref(false)

function reset() {
  const account = props.account
  name.value = account?.name ?? ''
  baseUrl.value = String(account?.credentials?.base_url ?? '')
  apiKey.value = ''
  concurrency.value = account?.concurrency || 10
  priority.value = account?.priority || 50
  groupIds.value = [...(account?.group_ids ?? [])]
  proxyId.value = account?.proxy_id ?? null
  const mapping = account?.credentials?.model_mapping
  const entries = mapping && typeof mapping === 'object' && !Array.isArray(mapping) ? Object.entries(mapping as Record<string, unknown>) : []
  modelLines.value = entries.filter(([from, to]) => from === to).map(([from]) => from).join('\n')
  aliases.value = entries.filter(([from, to]) => typeof to === 'string' && from !== to).map(([from, to]) => ({ from, to: String(to) }))
}
watch(() => [props.show, props.account] as const, ([show]) => { if (show) reset() }, { immediate: true })

function buildMapping(): Record<string, string> | null {
  const mapping: Record<string, string> = {}
  for (const raw of modelLines.value.split(/\r?\n|,/)) {
    const model = raw.trim()
    if (model) mapping[model] = model
  }
  for (const alias of aliases.value) {
    if (!alias.from.trim() || !alias.to.trim()) return null
    mapping[alias.from.trim()] = alias.to.trim()
  }
  return mapping
}

async function discoverModels() {
  discovering.value = true
  try {
    const result = props.account
      ? await adminAPI.accounts.syncUpstreamModels(props.account.id, { base_url: baseUrl.value, ...(apiKey.value ? { api_key: apiKey.value } : {}) })
      : await adminAPI.accounts.syncUpstreamModelsPreview({ platform: 'custom', type: 'apikey', base_url: baseUrl.value, api_key: apiKey.value })
    modelLines.value = result.models.join('\n')
    appStore.showSuccess(t('admin.upstream.discovered', { count: result.models.length }))
  } catch (error) {
    console.error('Upstream model discovery failed:', error)
    appStore.showError(t('admin.upstream.discoverFailed'))
  } finally { discovering.value = false }
}

async function submit() {
  const modelMapping = buildMapping()
  if (!modelMapping || !Number.isInteger(concurrency.value) || concurrency.value < 1 || !Number.isInteger(priority.value) || priority.value < 1) {
    appStore.showError(t('admin.upstream.invalidProvider'))
    return
  }
  saving.value = true
  try {
    const credentials: Record<string, unknown> = { base_url: baseUrl.value.trim(), model_mapping: modelMapping }
    if (apiKey.value) credentials.api_key = apiKey.value
    if (props.account) {
      await adminAPI.accounts.update(props.account.id, { name: name.value.trim(), credentials, concurrency: concurrency.value, priority: priority.value, proxy_id: proxyId.value ?? 0, group_ids: groupIds.value })
    } else {
      await adminAPI.accounts.create({ name: name.value.trim(), platform: 'custom', type: 'apikey', credentials, concurrency: concurrency.value, priority: priority.value, proxy_id: proxyId.value, group_ids: groupIds.value })
    }
    appStore.showSuccess(t('admin.upstream.providerSaved'))
    emit('saved')
    emit('close')
  } catch (error) {
    console.error('Failed to save upstream account:', error)
    appStore.showError(t('admin.upstream.providerSaveFailed'))
  } finally { saving.value = false }
}
</script>
