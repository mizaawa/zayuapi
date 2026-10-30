<template>
  <section class="profile-panel" data-testid="profile-passkey-panel">
    <header class="profile-panel-head">
      <span class="profile-panel-icon is-plum" aria-hidden="true">
        <Icon name="fingerprint" size="md" />
      </span>
      <div class="min-w-0">
        <h2 class="profile-panel-title">{{ t('profile.passkey.title') }}</h2>
        <p class="profile-panel-desc">{{ t('profile.passkey.description') }}</p>
        <div v-if="enabled && supported && !showAddForm" class="profile-panel-head-actions">
          <button
            type="button"
            class="btn btn-primary btn-sm"
            :disabled="busy"
            @click="showAddForm = true"
          >
            <Icon name="plus" size="sm" />
            {{ t('profile.passkey.add') }}
          </button>
        </div>
      </div>
    </header>

    <div class="profile-panel-body">
      <div v-if="!enabled" class="profile-state">
        <div class="profile-state-main">
          <span class="profile-state-mark" aria-hidden="true"><Icon name="fingerprint" size="md" /></span>
          <p class="profile-state-hint">{{ t('profile.passkey.featureDisabled') }}</p>
        </div>
      </div>
      <div v-if="enabled && !supported" class="profile-state">
        <div class="profile-state-main">
          <span class="profile-state-mark is-warn" aria-hidden="true"><Icon name="exclamationTriangle" size="md" /></span>
          <p class="profile-state-hint">{{ t('profile.passkey.unsupported') }}</p>
        </div>
      </div>

      <form
        v-if="enabled && supported && showAddForm"
        class="profile-box passkey-form"
        @submit.prevent="addPasskey"
      >
        <div class="grid gap-3 sm:grid-cols-2">
          <div>
            <label for="passkey-name" class="input-label">{{ t('profile.passkey.name') }}</label>
            <input
              id="passkey-name"
              v-model="newName"
              class="input"
              maxlength="100"
              :placeholder="t('profile.passkey.namePlaceholder')"
              autofocus
            />
          </div>
          <div>
            <label for="passkey-add-password" class="input-label">{{
              t('profile.currentPassword')
            }}</label>
            <input
              id="passkey-add-password"
              v-model="newPassword"
              type="password"
              autocomplete="current-password"
              class="input"
              :placeholder="t('profile.passkey.passwordPlaceholder')"
            />
          </div>
        </div>
        <div class="profile-actions">
          <button type="button" class="btn btn-secondary btn-sm" :disabled="busy" @click="cancelAdd">
            {{ t('common.cancel') }}
          </button>
          <button type="submit" class="btn btn-primary btn-sm" :disabled="busy || newPassword.length === 0">
            {{ busy ? t('common.processing') : t('profile.passkey.continue') }}
          </button>
        </div>
      </form>

      <template v-if="enabled">
        <div v-if="loading" class="passkey-list" aria-busy="true">
          <div class="passkey-item">
            <span class="passkey-mark skeleton"></span>
            <span class="skeleton h-4 w-40 rounded-full"></span>
          </div>
        </div>

        <p
          v-else-if="credentials.length === 0"
          class="passkey-empty"
        >
          {{ t('profile.passkey.empty') }}
        </p>

        <ul v-else class="passkey-list">
          <li
            v-for="credential in credentials"
            :key="credential.id"
            class="passkey-item"
          >
            <span class="passkey-mark" aria-hidden="true"><Icon name="key" size="sm" /></span>
            <div class="min-w-0 flex-1">
              <div class="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1">
                <p class="passkey-name">{{ credential.name }}</p>
                <span v-if="credential.backup" class="profile-chip is-on">
                  {{ t('profile.passkey.synced') }}
                </span>
              </div>
              <p class="passkey-meta">
                <span>{{ t('profile.passkey.createdAt', { date: formatDate(credential.created_at) }) }}</span>
                <span v-if="credential.last_used_at">{{ t('profile.passkey.lastUsed', { date: formatDate(credential.last_used_at) }) }}</span>
              </p>
            </div>
            <div class="passkey-actions">
              <button
                type="button"
                class="profile-link-button"
                :disabled="busy"
                @click="renamePasskey(credential)"
              >
                {{ t('common.edit') }}
              </button>
              <button
                type="button"
                class="profile-link-button is-danger"
                :disabled="busy"
                @click="deletePasskey(credential)"
              >
                {{ t('common.delete') }}
              </button>
            </div>
          </li>
        </ul>
      </template>
    </div>

    <!-- 删除确认：吊销凭据需验证当前密码，防止被窃会话静默移除 Passkey -->
    <Teleport to="body">
      <div v-if="deleteTarget" class="modal-overlay" @click.self="closeDeleteDialog">
        <div
          class="modal-content passkey-delete-dialog"
          role="dialog"
          aria-modal="true"
          aria-labelledby="passkey-delete-title"
        >
          <div class="modal-header">
            <h3 id="passkey-delete-title" class="modal-title">
              {{ t('profile.passkey.deleteTitle') }}
            </h3>
          </div>
          <form @submit.prevent="confirmDelete">
            <div class="modal-body space-y-4">
              <p class="text-sm leading-6 text-gray-600">
                {{ t('profile.passkey.deleteConfirm', { name: deleteTarget.name }) }}
              </p>
              <div>
                <label for="passkey-delete-password" class="input-label">{{
                  t('profile.currentPassword')
                }}</label>
                <input
                  id="passkey-delete-password"
                  v-model="deletePassword"
                  type="password"
                  autocomplete="current-password"
                  class="input"
                  :placeholder="t('profile.passkey.passwordPlaceholder')"
                  autofocus
                />
              </div>
            </div>
            <div class="modal-footer">
              <button type="button" class="btn btn-secondary" :disabled="busy" @click="closeDeleteDialog">
                {{ t('common.cancel') }}
              </button>
              <button
                type="submit"
                class="btn btn-danger"
                :disabled="busy || deletePassword.length === 0"
              >
                {{ busy ? t('common.processing') : t('common.delete') }}
              </button>
            </div>
          </form>
        </div>
      </div>
    </Teleport>
  </section>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { passkeyAPI, type PasskeyCredentialSummary } from '@/api'
import { Icon } from '@/components/icons'
import { useAppStore } from '@/stores/app'

const props = defineProps<{ enabled: boolean }>()

const { t } = useI18n()
const appStore = useAppStore()
const supported = passkeyAPI.isSupported()
const loading = ref(false)
const busy = ref(false)
const showAddForm = ref(false)
const newName = ref('')
const newPassword = ref('')
const deleteTarget = ref<PasskeyCredentialSummary | null>(null)
const deletePassword = ref('')
const credentials = ref<PasskeyCredentialSummary[]>([])

// apiClient 拦截器把错误规范化为 { code, reason, message }；
// 透出后端消息（如密码错误），否则回退到通用文案。
function extractErrorMessage(error: unknown, fallback: string): string {
  const message = (error as { message?: string }).message
  return typeof message === 'string' && message.length > 0 ? message : fallback
}

async function loadCredentials(): Promise<void> {
  if (!props.enabled) {
    credentials.value = []
    return
  }
  loading.value = true
  try {
    credentials.value = await passkeyAPI.list()
  } catch (error) {
    // 字符串错误码在 reason 字段（code 是数字状态码）；
    // 设置变更竞态下后端仍可能返回 PASSKEY_DISABLED，静默处理
    const reason = (error as { reason?: string }).reason
    if (reason !== 'PASSKEY_DISABLED') {
      appStore.showError(t('profile.passkey.loadFailed'))
    }
  } finally {
    loading.value = false
  }
}

async function addPasskey(): Promise<void> {
  if (newPassword.value.length === 0) return
  busy.value = true
  try {
    await passkeyAPI.register(newName.value.trim(), newPassword.value)
    appStore.showSuccess(t('profile.passkey.added'))
    cancelAdd()
    await loadCredentials()
  } catch (error) {
    if (!(error instanceof DOMException && error.name === 'NotAllowedError')) {
      appStore.showError(extractErrorMessage(error, t('profile.passkey.addFailed')))
    }
  } finally {
    busy.value = false
  }
}

function cancelAdd(): void {
  showAddForm.value = false
  newName.value = ''
  newPassword.value = ''
}

async function renamePasskey(credential: PasskeyCredentialSummary): Promise<void> {
  const name = window.prompt(t('profile.passkey.renamePrompt'), credential.name)?.trim()
  if (!name || name === credential.name) return
  busy.value = true
  try {
    await passkeyAPI.rename(credential.id, name)
    credential.name = name
    appStore.showSuccess(t('profile.passkey.renamed'))
  } catch {
    appStore.showError(t('profile.passkey.renameFailed'))
  } finally {
    busy.value = false
  }
}

function deletePasskey(credential: PasskeyCredentialSummary): void {
  deleteTarget.value = credential
  deletePassword.value = ''
}

function closeDeleteDialog(): void {
  deleteTarget.value = null
  deletePassword.value = ''
}

async function confirmDelete(): Promise<void> {
  const credential = deleteTarget.value
  if (!credential || deletePassword.value.length === 0) return
  busy.value = true
  try {
    await passkeyAPI.remove(credential.id, deletePassword.value)
    credentials.value = credentials.value.filter((item) => item.id !== credential.id)
    appStore.showSuccess(t('profile.passkey.deleted'))
    closeDeleteDialog()
  } catch (error) {
    // 密码错误等失败保持对话框打开，允许重试
    appStore.showError(extractErrorMessage(error, t('profile.passkey.deleteFailed')))
  } finally {
    busy.value = false
  }
}

function formatDate(value: string): string {
  return new Intl.DateTimeFormat(undefined, {
    year: 'numeric',
    month: 'short',
    day: 'numeric'
  }).format(new Date(value))
}

watch(
  () => props.enabled,
  () => {
    void loadCredentials()
  },
  { immediate: true }
)
</script>

<style scoped>
.passkey-form {
  display: flex;
  flex-direction: column;
  gap: 0.875rem;
}

.passkey-form .input {
  background: var(--md-sys-color-surface);
}

.passkey-empty {
  padding: 1rem;
  border-radius: 1.125rem;
  background: #fffbe8;
  font-size: 0.8125rem;
  color: #6b6045;
}

.passkey-list {
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
}

.passkey-item {
  display: flex;
  min-width: 0;
  flex-wrap: wrap;
  align-items: center;
  gap: 0.75rem;
  padding: 0.875rem 1rem;
  border-radius: 1.125rem;
  background: #fffbe8;
}

.passkey-mark {
  display: inline-flex;
  width: 2.25rem;
  height: 2.25rem;
  flex-shrink: 0;
  align-items: center;
  justify-content: center;
  border-radius: 0.75rem;
  background: #eee8f5;
  color: #6e5d84;
}

.passkey-name {
  min-width: 0;
  font-size: 0.875rem;
  font-weight: 700;
  overflow-wrap: anywhere;
  color: #3f3420;
}

.passkey-meta {
  display: flex;
  flex-wrap: wrap;
  gap: 0 0.75rem;
  margin-top: 0.125rem;
  font-size: 0.75rem;
  color: #6b6045;
}

.passkey-actions {
  display: flex;
  flex-shrink: 0;
  gap: 0.25rem;
  margin-left: auto;
}

.passkey-delete-dialog {
  max-width: 28rem;
}
</style>
