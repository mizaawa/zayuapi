<template>
  <BaseDialog :show="show" :title="provider ? t('admin.upstream.editProvider') : t('admin.upstream.add')" width="normal" @close="emit('close')">
    <form id="provider-form" class="space-y-5" @submit.prevent="save">
      <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.upstream.formHint') }}</p>
      <label class="block"><span class="input-label">{{ t('admin.upstream.providerName') }}</span><input v-model.trim="form.name" class="input" required :placeholder="t('admin.upstream.namePlaceholder')" /></label>
      <fieldset>
        <legend class="input-label">{{ t('admin.upstream.providerType') }}</legend>
        <div class="grid grid-cols-2 gap-3">
          <label v-for="kind in kinds" :key="kind.value" class="flex cursor-pointer items-center gap-2 rounded-xl border p-3 text-sm transition-colors" :class="form.site_type === kind.value ? 'border-primary-500 bg-primary-50 dark:bg-primary-900/20' : 'border-gray-200 dark:border-dark-600'">
            <input v-model="form.site_type" type="radio" :value="kind.value" class="accent-primary-600" /><span><strong class="block text-gray-900 dark:text-white">{{ kind.label }}</strong><small class="text-gray-500 dark:text-gray-400">{{ kind.hint }}</small></span>
          </label>
        </div>
      </fieldset>
      <label class="block"><span class="input-label">{{ t('admin.upstream.siteUrl') }}</span><input v-model.trim="form.base_url" class="input font-mono text-sm" type="url" required placeholder="https://provider.example.com" /><span class="input-hint">{{ t('admin.upstream.siteUrlHint') }}</span></label>
      <fieldset>
        <legend class="input-label">{{ t('admin.upstream.loginMode') }}</legend>
        <div class="grid grid-cols-2 gap-3">
          <label v-for="mode in modes" :key="mode.value" class="flex cursor-pointer items-center gap-2 rounded-xl border px-3 py-2.5 text-xs font-medium transition-colors" :class="form.auth_mode === mode.value ? 'border-primary-500 bg-primary-50 text-primary-700 dark:bg-primary-900/20 dark:text-primary-300' : 'border-gray-200 text-gray-600 dark:border-dark-600 dark:text-gray-300'">
            <input v-model="form.auth_mode" type="radio" :value="mode.value" class="accent-primary-600" />{{ mode.label }}
          </label>
        </div>
      </fieldset>
      <div v-if="form.auth_mode === 'password'" class="grid gap-4 sm:grid-cols-2">
        <label class="block"><span class="input-label">{{ t('admin.upstream.loginAccount') }}</span><input v-model.trim="form.account" class="input" autocomplete="username" required placeholder="name@example.com" /></label>
        <label class="block"><span class="input-label">{{ t('admin.upstream.loginPassword') }}</span><input v-model="form.password" class="input" type="password" autocomplete="new-password" :required="!provider" :placeholder="provider ? t('admin.upstream.keepSecret') : t('admin.upstream.passwordPlaceholder')" /></label>
      </div>
      <label v-else class="block"><span class="input-label">{{ t('admin.upstream.siteKey') }}</span><input v-model.trim="form.api_key" class="input font-mono text-sm" type="password" autocomplete="off" :required="!provider" :placeholder="provider ? t('admin.upstream.keepSecret') : 'sk-…'" /></label>
      <div class="grid gap-4 sm:grid-cols-2">
      <label class="block"><span class="input-label">{{ t('admin.upstream.exchangeRate') }}</span><input v-model.number="form.exchange_rate" class="input" type="number" min="0.001" max="1000000" step="0.001" required /><span class="input-hint">{{ t('admin.upstream.exchangeRateHint') }}</span></label>
        <label class="block"><span class="input-label">{{ t('admin.upstream.notes') }}</span><input v-model.trim="form.notes" class="input" :placeholder="t('admin.upstream.notesPlaceholder')" /></label>
      </div>
      <div class="border-t border-gray-100 pt-4 dark:border-dark-700">
        <button type="button" class="btn btn-secondary inline-flex items-center gap-2" :disabled="probing || saving" @click="testConnection"><Icon name="refresh" size="sm" :class="{ 'animate-spin': probing }" />{{ probing ? t('admin.upstream.probing') : t('admin.upstream.probe') }}</button>
        <p v-if="probeMessage" role="status" class="mt-2 text-xs" :class="probeOk ? 'text-emerald-600 dark:text-emerald-400' : 'text-red-600 dark:text-red-400'">{{ probeMessage }}</p>
      </div>
    </form>
    <template #footer><div class="flex w-full justify-end gap-2"><button type="button" class="btn btn-secondary" @click="emit('close')">{{ t('common.cancel') }}</button><button type="submit" form="provider-form" class="btn btn-primary" :disabled="saving || probing">{{ saving ? t('admin.upstream.savingProvider') : provider ? t('common.save') : t('admin.upstream.add') }}</button></div></template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import { createProvider, probeProvider, updateProvider, type Provider, type ProviderInput } from '@/api/admin/upstream'

const props = defineProps<{ show: boolean; provider?: Provider | null }>()
const emit = defineEmits<{ close: []; saved: [] }>()
const { t } = useI18n()
const kinds = computed(() => [
  { value: 'sub2api', label: 'Sub2API', hint: t('admin.upstream.sub2apiHint') },
  { value: 'newapi', label: 'New API', hint: t('admin.upstream.newapiHint') }
] as const)
const modes = computed(() => [
  { value: 'password', label: t('admin.upstream.passwordLogin') },
  { value: 'api_key', label: form.site_type === 'newapi' ? t('admin.upstream.newapiKey') : t('admin.upstream.sub2apiKey') }
] as const)
const form = reactive<ProviderInput>({ name: '', site_type: 'sub2api', base_url: '', auth_mode: 'password', account: '', password: '', api_key: '', exchange_rate: 1, notes: '' })
const probing = ref(false)
const saving = ref(false)
const probeMessage = ref('')
const probeOk = ref(false)
watch(() => [props.show, props.provider] as const, ([show]) => {
  if (!show) return
  const provider = props.provider
  Object.assign(form, { name: provider?.name ?? '', site_type: provider?.site_type ?? 'sub2api', base_url: provider?.base_url ?? '', auth_mode: provider?.auth_mode ?? 'password', account: provider?.account ?? '', password: '', api_key: '', exchange_rate: provider?.exchange_rate ?? 1, notes: provider?.notes ?? '' })
  probeMessage.value = ''
  probeOk.value = false
}, { immediate: true })
function payload(): ProviderInput {
  return { id: props.provider?.id, name: form.name.trim(), site_type: form.site_type, base_url: form.base_url.trim(), auth_mode: form.auth_mode,
    account: form.auth_mode === 'password' ? form.account?.trim() : undefined, password: form.auth_mode === 'password' ? form.password : undefined,
    api_key: form.auth_mode === 'api_key' ? form.api_key?.trim() : undefined, exchange_rate: form.exchange_rate, notes: form.notes?.trim() }
}
async function testConnection() {
  probing.value = true
  probeMessage.value = ''
  try {
    const result = await probeProvider(payload())
    probeOk.value = result.status === 'connected'
    probeMessage.value = result.status === 'connected' ? t('admin.upstream.probeSuccess') : result.last_error || t('admin.upstream.probeFailed')
  } catch (error) { probeOk.value = false; probeMessage.value = t('admin.upstream.probeFailed'); console.error('Provider probe failed:', error) }
  finally { probing.value = false }
}
async function save() {
  saving.value = true
  try {
    if (props.provider) await updateProvider(props.provider.id, payload())
    else await createProvider(payload())
    emit('saved')
    emit('close')
  } catch (error) { probeOk.value = false; probeMessage.value = t('admin.upstream.providerSaveFailed'); console.error('Provider save failed:', error) }
  finally { saving.value = false }
}
</script>
