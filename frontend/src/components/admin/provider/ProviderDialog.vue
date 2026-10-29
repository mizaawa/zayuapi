<template>
  <BaseDialog :show="show" :title="provider ? t('admin.upstream.editProvider') : t('admin.upstream.add')" width="normal" @close="emit('close')">
    <form id="provider-form" class="space-y-4 text-sm" @submit.prevent="save">
      <p class="-mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.upstream.formHint') }}</p>

      <label class="block">
        <span class="input-label">{{ t('admin.upstream.providerName') }} <span class="text-primary-600">*</span></span>
        <input v-model.trim="form.name" class="input provider-input" required :placeholder="t('admin.upstream.namePlaceholder')" />
      </label>
      <label class="provider-toggle-row">
        <span class="text-sm text-gray-700 dark:text-gray-300">{{ t('admin.upstream.suppressLowBalanceAlert') }}</span>
        <input v-model="form.suppress_low_balance_alert" type="checkbox" class="h-4 w-4 accent-primary-600" data-testid="provider-suppress-low-balance-alert" />
      </label>

      <fieldset>
        <legend class="input-label">{{ t('admin.upstream.providerType') }}</legend>
        <div class="grid grid-cols-2 gap-2">
          <label v-for="kind in kinds" :key="kind.value" class="provider-choice provider-platform" :class="form.site_type === kind.value ? 'provider-choice-active' : ''">
            <input v-model="form.site_type" type="radio" name="provider-kind" :value="kind.value" class="sr-only" />
            <span class="provider-choice-icon"><Icon name="server" size="sm" /></span>
            <span class="min-w-0 flex-1"><strong class="block font-semibold">{{ kind.label }}</strong><small class="block truncate text-[10px] text-gray-500 dark:text-gray-400">{{ kind.hint }}</small></span>
            <Icon :name="form.site_type === kind.value ? 'checkCircle' : 'chevronRight'" size="sm" class="shrink-0" />
          </label>
        </div>
      </fieldset>

      <label class="block">
        <span class="input-label">{{ t('admin.upstream.siteUrl') }}</span>
        <input v-model.trim="form.base_url" class="input provider-input font-mono" type="url" required placeholder="https://api.zayuapi.com" />
        <span class="input-hint block">{{ t('admin.upstream.siteUrlHint') }}</span>
      </label>

      <fieldset>
        <legend class="input-label">{{ t('admin.upstream.loginMode') }}</legend>
        <div class="grid grid-cols-2 gap-2">
          <label v-for="mode in modes" :key="mode.value" class="provider-choice provider-mode" :class="form.auth_mode === mode.value ? 'provider-choice-active' : ''">
            <input v-model="form.auth_mode" type="radio" name="provider-auth" :value="mode.value" class="sr-only" />
            <span class="provider-radio" aria-hidden="true"><span v-if="form.auth_mode === mode.value" class="provider-radio-dot" /></span>
            <Icon :name="mode.value === 'password' ? 'shield' : 'key'" size="sm" />
            <span class="truncate">{{ mode.label }}</span>
          </label>
        </div>
      </fieldset>

      <div v-if="form.auth_mode === 'token' && form.site_type === 'sub2api'" class="rounded-2xl bg-primary-50 p-3.5 text-xs leading-relaxed text-gray-700 dark:border-primary-700 dark:bg-primary-900/20 dark:text-gray-200">
        <p class="font-semibold">{{ t('admin.upstream.sub2TokenGuideTitle') }}</p>
        <p class="mt-1">{{ t('admin.upstream.sub2TokenGuide') }}</p>
        <a v-if="siteLink" :href="siteLink" target="_blank" rel="noopener noreferrer" class="mt-2 inline-flex items-center gap-1 font-semibold text-primary-700 underline-offset-2 hover:underline dark:text-primary-300">{{ t('admin.upstream.sub2TokenGuideLocation') }} <Icon name="externalLink" size="xs" /></a>
        <p v-else class="mt-2 font-semibold">{{ t('admin.upstream.sub2TokenGuideLocation') }}</p>
        <p class="mt-1 text-gray-500 dark:text-gray-400">{{ t('admin.upstream.sub2TokenGuideRefresh') }}</p>
      </div>

      <div v-if="form.auth_mode === 'token' && form.site_type === 'newapi'" class="rounded-2xl bg-primary-50 p-3.5 text-xs leading-relaxed text-gray-700 dark:border-primary-700 dark:bg-primary-900/20 dark:text-gray-200" data-testid="provider-newapi-token-guide">
        <p class="font-semibold">{{ t('admin.upstream.newapiTokenGuideTitle') }}</p>
        <p class="mt-1">{{ t('admin.upstream.newapiTokenGuide') }}</p>
        <p class="mt-1.5 flex flex-wrap gap-1.5"><code v-for="scope in newapiScopes" :key="scope" class="rounded-md bg-white/70 px-1.5 py-0.5 font-mono text-[11px] dark:bg-black/20">{{ scope }}</code></p>
        <p class="mt-1.5 text-gray-500 dark:text-gray-400">{{ t('admin.upstream.newapiTokenGuideWrite') }}</p>
        <p class="mt-1 text-gray-500 dark:text-gray-400">{{ t('admin.upstream.newapiTokenGuideLegacy') }}</p>
      </div>

      <div v-if="form.auth_mode === 'password'" class="grid gap-3 sm:grid-cols-2">
        <label class="block"><span class="input-label">{{ t('admin.upstream.loginAccount') }}</span><input v-model.trim="form.account" class="input provider-input" autocomplete="username" required :placeholder="form.site_type === 'sub2api' ? 'name@example.com' : t('admin.upstream.newapiAccountPlaceholder')" /></label>
        <label class="block"><span class="input-label">{{ t('admin.upstream.loginPassword') }}</span>
          <span class="provider-secret"><input v-model="form.password" class="input provider-input pr-10" :type="visible.password ? 'text' : 'password'" autocomplete="new-password" :required="secretRequired" :placeholder="provider && !secretRequired ? t('admin.upstream.keepSecret') : t('admin.upstream.passwordPlaceholder')" /><button type="button" :aria-label="visible.password ? t('admin.upstream.hideSecret') : t('admin.upstream.showSecret')" @click="visible.password = !visible.password"><Icon :name="visible.password ? 'eyeOff' : 'eye'" size="sm" /></button></span>
        </label>
      </div>

      <template v-else-if="form.site_type === 'sub2api'">
        <label class="block"><span class="input-label">Access Token</span>
          <span class="provider-secret"><input v-model.trim="form.access_token" class="input provider-input" :type="visible.access ? 'text' : 'password'" :required="secretRequired && !form.refresh_token" :placeholder="provider && !secretRequired ? t('admin.upstream.keepSecret') : t('admin.upstream.accessTokenPlaceholder')" autocomplete="off" /><button type="button" :aria-label="visible.access ? t('admin.upstream.hideSecret') : t('admin.upstream.showSecret')" @click="visible.access = !visible.access"><Icon :name="visible.access ? 'eyeOff' : 'eye'" size="sm" /></button></span>
          <span class="input-hint block">{{ t('admin.upstream.accessTokenHint') }}</span>
        </label>
        <div class="grid gap-3 sm:grid-cols-2">
          <label class="block"><span class="input-label">Refresh Token</span>
            <span class="provider-secret"><input v-model.trim="form.refresh_token" class="input provider-input" :type="visible.refresh ? 'text' : 'password'" :required="secretRequired && !form.access_token" :placeholder="t('admin.upstream.refreshTokenPlaceholder')" autocomplete="off" /><button type="button" :aria-label="visible.refresh ? t('admin.upstream.hideSecret') : t('admin.upstream.showSecret')" @click="visible.refresh = !visible.refresh"><Icon :name="visible.refresh ? 'eyeOff' : 'eye'" size="sm" /></button></span>
          </label>
          <label class="block"><span class="input-label">Token Type</span><input v-model.trim="form.token_type" class="input provider-input" placeholder="Bearer" /></label>
        </div>
        <p class="-mt-2 text-[11px] text-gray-500 dark:text-gray-400">{{ t('admin.upstream.tokenEitherHint') }}</p>
      </template>

      <div v-else class="grid gap-3 sm:grid-cols-2">
        <label class="block"><span class="input-label">{{ t('admin.upstream.systemAccessToken') }}</span>
          <span class="provider-secret"><input v-model.trim="form.system_access_token" class="input provider-input pr-10" :type="visible.system ? 'text' : 'password'" :required="secretRequired" :placeholder="provider && !secretRequired ? t('admin.upstream.keepSecret') : t('admin.upstream.systemTokenPlaceholder')" autocomplete="off" /><button type="button" :aria-label="visible.system ? t('admin.upstream.hideSecret') : t('admin.upstream.showSecret')" @click="visible.system = !visible.system"><Icon :name="visible.system ? 'eyeOff' : 'eye'" size="sm" /></button></span>
          <span class="input-hint block">{{ t('admin.upstream.systemTokenHint') }}</span>
        </label>
        <label class="block"><span class="input-label">{{ t('admin.upstream.userId') }} <small class="font-normal text-gray-400">{{ t('admin.upstream.userIdLegacyOnly') }}</small></span><input v-model.trim="form.user_id" class="input provider-input" type="text" maxlength="128" :required="userIdRequired" :placeholder="t('admin.upstream.userIdPlaceholder')" /><span class="input-hint block">{{ t('admin.upstream.userIdHint') }}</span></label>
      </div>

      <div class="grid gap-3 pt-1 sm:grid-cols-2">
        <label class="block"><span class="input-label">{{ t('admin.upstream.exchangeRate') }}</span><input v-model.number="form.exchange_rate" class="input provider-input" type="number" min="0.001" max="1000000" step="0.001" required /><span class="input-hint block">{{ t('admin.upstream.exchangeRateHint') }}</span></label>
        <label class="block"><span class="input-label">{{ t('admin.upstream.notes') }} <small class="font-normal text-gray-400">{{ t('admin.upstream.optional') }}</small></span><input v-model.trim="form.notes" class="input provider-input" :placeholder="t('admin.upstream.notesPlaceholder')" /></label>
      </div>

      <div class="provider-probe-bar">
        <button type="button" class="btn btn-secondary inline-flex items-center gap-2" :disabled="probing || saving" @click="testConnection"><Icon name="shield" size="sm" />{{ probing ? t('admin.upstream.probing') : t('admin.upstream.probe') }}</button>
        <span class="text-[11px] text-gray-500 dark:text-gray-400">{{ t('admin.upstream.probeHint') }}</span>
      </div>
      <p v-if="probeMessage" role="status" class="rounded-xl px-3 py-2.5 text-xs leading-relaxed" :class="probeOk ? 'bg-emerald-50 text-emerald-800' : 'bg-red-50 text-red-700'">{{ probeMessage }}</p>
    </form>
    <template #footer><div class="flex w-full justify-end gap-2"><button type="button" class="btn btn-secondary" @click="emit('close')">{{ t('common.cancel') }}</button><button type="submit" form="provider-form" class="btn btn-primary" :disabled="saving || probing">{{ saving ? t('admin.upstream.savingProvider') : provider ? t('common.save') : t('admin.upstream.loginAndAdd') }}</button></div></template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import { createProvider, probeProvider, updateProvider, type Provider, type ProviderInput } from '@/api/admin/upstream'

const props = defineProps<{ show: boolean; provider?: Provider | null }>()
const emit = defineEmits<{ close: []; saved: [] }>()
const { t } = useI18n()
const appStore = useAppStore()
const kinds = computed(() => [
  { value: 'sub2api', label: 'Sub2API', hint: t('admin.upstream.sub2apiHint') },
  { value: 'newapi', label: 'New API', hint: t('admin.upstream.newapiHint') }
] as const)
const modes = computed(() => [
  { value: 'password', label: t('admin.upstream.passwordLogin') },
  { value: 'token', label: form.site_type === 'newapi' ? t('admin.upstream.newapiKey') : t('admin.upstream.sub2apiKey') }
] as const)
const form = reactive<ProviderInput>({ name: '', site_type: 'sub2api', base_url: '', auth_mode: 'password', account: '', password: '', access_token: '', refresh_token: '', token_type: 'Bearer', user_id: '', system_access_token: '', exchange_rate: 1, notes: '', suppress_low_balance_alert: false })
const visible = reactive({ password: false, access: false, refresh: false, system: false })
const siteLink = computed(() => {
  try {
    const url = new URL(form.base_url)
    return url.protocol === 'https:' && !url.username && !url.password ? url.origin : ''
  } catch { return '' }
})
const probing = ref(false)
const saving = ref(false)
const probeMessage = ref('')
const probeOk = ref(false)
const secretRequired = computed(() => !props.provider || props.provider.site_type !== form.site_type || props.provider.auth_mode !== form.auth_mode || props.provider.base_url.replace(/\/$/, '') !== form.base_url.replace(/\/$/, '') || (form.auth_mode === 'password' && props.provider.account !== form.account) || (form.site_type === 'newapi' && form.auth_mode === 'token' && (props.provider.user_id ?? '') !== form.user_id))
// New API scopes needed to read the balance, usage and keys and to import a key.
const newapiScopes = ['profile:read', 'usage:read', 'api_key:read', 'api_key:reveal']
// A personal access token (nap_…) identifies its account; only a legacy system
// access token is sent with the user ID. A kept token keeps its saved ID.
const userIdRequired = computed(() => {
  const token = form.system_access_token?.trim() ?? ''
  return token !== '' && !token.startsWith('nap_')
})
watch(() => [props.show, props.provider] as const, ([show]) => {
  if (!show) return
  const provider = props.provider
  Object.assign(form, { name: provider?.name ?? '', site_type: provider?.site_type ?? 'sub2api', base_url: provider?.base_url ?? '', auth_mode: provider?.auth_mode ?? 'password', account: provider?.account ?? '', password: '', access_token: '', refresh_token: '', token_type: provider?.token_type ?? 'Bearer', user_id: provider?.user_id ?? '', system_access_token: '', exchange_rate: provider?.exchange_rate ?? 1, notes: provider?.notes ?? '', suppress_low_balance_alert: provider?.suppress_low_balance_alert ?? false })
  Object.assign(visible, { password: false, access: false, refresh: false, system: false })
  probeMessage.value = ''
  probeOk.value = false
}, { immediate: true })
function payload(): ProviderInput {
  return { id: props.provider?.id, name: form.name.trim(), site_type: form.site_type, base_url: form.base_url.trim(), auth_mode: form.auth_mode,
    account: form.auth_mode === 'password' ? form.account?.trim() : undefined,
    password: form.auth_mode === 'password' ? form.password : undefined,
    access_token: form.site_type === 'sub2api' && form.auth_mode === 'token' ? form.access_token?.trim() : undefined,
    refresh_token: form.site_type === 'sub2api' && form.auth_mode === 'token' ? form.refresh_token?.trim() : undefined,
    token_type: form.site_type === 'sub2api' && form.auth_mode === 'token' ? form.token_type?.trim() || 'Bearer' : undefined,
    user_id: form.site_type === 'newapi' && form.auth_mode === 'token' ? form.user_id?.trim() : undefined,
    system_access_token: form.site_type === 'newapi' && form.auth_mode === 'token' ? form.system_access_token?.trim() : undefined,
    exchange_rate: form.exchange_rate, notes: form.notes?.trim(), suppress_low_balance_alert: form.suppress_low_balance_alert }
}
function failureReason(error: unknown, fallback: string): string {
  if (typeof error === 'object' && error !== null) {
    const err = error as { message?: unknown; reason?: unknown; response?: { data?: { message?: unknown; reason?: unknown } } }
    const reason = err.response?.data?.message ?? err.response?.data?.reason ?? err.message ?? err.reason
    if (typeof reason === 'string' && reason.trim()) return reason.trim().slice(0, 350)
  }
  return fallback
}
async function testConnection() {
  probing.value = true
  probeMessage.value = ''
  try {
    const result = await probeProvider(payload())
    probeOk.value = result.status === 'connected'
    probeMessage.value = probeOk.value ? t('admin.upstream.probeSuccess') : result.last_error || t('admin.upstream.probeFailed')
    if (probeOk.value) appStore.showSuccess(probeMessage.value)
    else appStore.showError(probeMessage.value)
  } catch (error) {
    probeOk.value = false
    probeMessage.value = failureReason(error, t('admin.upstream.probeFailed'))
    appStore.showError(probeMessage.value)
  } finally { probing.value = false }
}
async function save() {
  saving.value = true
  probeMessage.value = ''
  try {
    const result = props.provider
      ? await updateProvider(props.provider.id, payload())
      : await createProvider(payload())
    if (result.status === 'disconnected') {
      appStore.showWarning(`${t('admin.upstream.providerSavedDisconnected')}: ${result.last_error || t('admin.upstream.probeFailed')}`)
    } else {
      appStore.showSuccess(t('admin.upstream.providerSaved'))
    }
    emit('saved')
    emit('close')
  } catch (error) {
    probeOk.value = false
    probeMessage.value = failureReason(error, t('admin.upstream.providerSaveFailed'))
    appStore.showError(probeMessage.value)
  } finally { saving.value = false }
}
</script>

<style scoped>
.provider-input { border: 1px solid color-mix(in srgb, var(--md-sys-color-primary) 26%, transparent); border-radius: 0.875rem; background: var(--md-sys-color-surface); padding: 0.65rem 0.9rem; box-shadow: inset 0 1px 2px rgb(63 52 32 / 0.04); }
.provider-input:hover:not(:focus) { border-color: color-mix(in srgb, var(--md-sys-color-primary) 45%, transparent); }
.provider-input:focus { border-color: var(--md-sys-color-primary); box-shadow: 0 0 0 4px rgb(233 184 36 / 0.16); }
.provider-choice { display: flex; align-items: center; min-width: 0; gap: 0.5rem; cursor: pointer; border: 1px solid color-mix(in srgb, var(--md-sys-color-primary) 22%, transparent); border-radius: 1rem; background: var(--md-sys-color-surface); color: var(--md-sys-color-on-surface); transition: border-color 0.15s, background 0.15s, box-shadow 0.2s, transform 0.2s var(--theme-ease-out); }
.provider-choice:focus-within { outline: 2px solid var(--md-sys-color-primary); outline-offset: 2px; }
.provider-choice:hover { border-color: var(--md-sys-color-primary); transform: translateY(-1px); }
.provider-choice-active { border-color: var(--md-sys-color-primary); background: linear-gradient(135deg, #fffbe8, #fff3bd); color: #7e5600; box-shadow: inset 0 0 0 1px color-mix(in srgb, var(--md-sys-color-primary) 30%, transparent), 0 8px 18px -12px rgb(151 104 0 / 0.55); }
.provider-platform { min-height: 3.6rem; padding: 0.6rem 0.7rem; font-size: 0.8rem; }
.provider-choice-icon { display: flex; flex-shrink: 0; align-items: center; justify-content: center; width: 2rem; height: 2rem; border-radius: 0.65rem; background: color-mix(in srgb, var(--md-sys-color-primary) 16%, var(--md-sys-color-surface)); color: #7e5600; }
.provider-choice-active .provider-choice-icon { background: var(--md-sys-color-primary); color: var(--md-sys-color-on-primary); }
.provider-mode { min-height: 2.75rem; padding: 0.5rem 0.75rem; font-size: 0.78rem; font-weight: 600; }
.provider-radio { display: inline-flex; flex-shrink: 0; align-items: center; justify-content: center; width: 0.9rem; height: 0.9rem; border: 1.5px solid currentColor; border-radius: 50%; }
.provider-radio-dot { width: 0.45rem; height: 0.45rem; border-radius: 50%; background: currentColor; }
.provider-secret { position: relative; display: block; }
.provider-secret .provider-input { width: 100%; min-width: 0; padding-inline-end: 3rem; box-sizing: border-box; }
.provider-secret button { position: absolute; right: 0.55rem; top: 50%; display: flex; align-items: center; justify-content: center; width: 1.85rem; height: 1.85rem; transform: translateY(-50%); border-radius: 0.55rem; background: transparent; color: var(--md-sys-color-on-surface-variant); transition: background 0.15s, color 0.15s; }
.provider-secret button:hover { background: var(--md-sys-color-surface-container); color: #7e5600; }
.provider-toggle-row { display: flex; align-items: center; justify-content: space-between; gap: 0.75rem; padding: 0.7rem 0.9rem; border-radius: 1rem; background: color-mix(in srgb, var(--md-sys-color-surface-container) 55%, var(--md-sys-color-surface)); cursor: pointer; }
.provider-probe-bar { display: flex; flex-wrap: wrap; align-items: center; gap: 0.6rem; padding: 0.75rem; border-radius: 1rem; background: linear-gradient(135deg, rgb(228 240 223 / 0.7), rgb(255 251 232 / 0.9)); }
</style>
