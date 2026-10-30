<template>
  <section class="profile-panel" data-testid="profile-balance-notify-panel">
    <header class="profile-panel-head">
      <span class="profile-panel-icon" aria-hidden="true">
        <Icon name="bell" size="md" />
      </span>
      <div class="min-w-0">
        <h2 class="profile-panel-title">{{ t('profile.balanceNotify.title') }}</h2>
        <p class="profile-panel-desc">{{ t('profile.balanceNotify.description') }}</p>
      </div>
    </header>

    <div class="profile-panel-body">
      <!-- Enable toggle -->
      <label class="profile-state notify-toggle">
        <span class="profile-state-title">{{ t('profile.balanceNotify.enabled') }}</span>
        <span class="notify-switch">
          <input type="checkbox" v-model="notifyEnabled" @change="handleToggle" class="sr-only peer" />
          <span class="notify-switch-track" aria-hidden="true"></span>
        </span>
      </label>

      <template v-if="notifyEnabled">
        <!-- Custom threshold with save button -->
        <div class="notify-section">
          <label for="balance-notify-threshold" class="notify-label">
            {{ t('profile.balanceNotify.threshold') }}
            <span class="notify-label-hint">{{ t('profile.balanceNotify.thresholdHint') }}</span>
          </label>
          <div class="notify-threshold">
            <div class="notify-money">
              <span aria-hidden="true">$</span>
              <input
                id="balance-notify-threshold"
                v-model.number="customThreshold"
                type="number"
                min="0"
                step="0.01"
                class="notify-money-input"
                :placeholder="systemDefaultThreshold > 0 ? `${t('profile.balanceNotify.systemDefault')} $${systemDefaultThreshold}` : t('profile.balanceNotify.thresholdPlaceholder')"
              />
            </div>
            <button
              @click="handleThresholdUpdate"
              :disabled="savingThreshold"
              class="btn btn-primary btn-sm whitespace-nowrap"
            >
              {{ savingThreshold ? t('common.saving') : t('common.save') }}
            </button>
          </div>
        </div>

        <!-- Email list with toggles -->
        <div class="notify-section">
          <p class="notify-label">{{ t('profile.balanceNotify.extraEmails') }}</p>
          <p class="notify-note">
            <Icon name="infoCircle" size="sm" class="shrink-0" />
            <span>{{ t('profile.balanceNotify.extraEmailsHint') }}</span>
          </p>

          <!-- Saved email entries -->
          <ul v-if="emailEntries.length > 0" class="notify-emails">
            <li v-for="(entry, idx) in emailEntries" :key="idx" class="notify-email">
              <div class="notify-email-main">
                <label class="notify-switch is-small">
                  <input type="checkbox" :checked="!entry.disabled" @change="handleEmailToggle(entry)" class="sr-only peer" :aria-label="entry.email" />
                  <span class="notify-switch-track" aria-hidden="true"></span>
                </label>
                <span class="notify-email-address" :class="{ 'is-off': entry.disabled }">{{ entry.email }}</span>
                <span v-if="entry.verified" class="profile-chip is-on">{{ t('profile.balanceNotify.verified') }}</span>
                <span v-else class="profile-chip is-warn">{{ t('profile.balanceNotify.unverified') }}</span>
              </div>
              <div class="notify-email-actions">
                <template v-if="!entry.verified">
                  <!-- Inline verify flow for saved unverified emails -->
                  <template v-if="verifyingEmail === entry.email">
                    <input
                      v-model="verifyCode"
                      type="text"
                      inputmode="numeric"
                      maxlength="6"
                      class="notify-code-input"
                      :placeholder="t('profile.balanceNotify.codePlaceholder')"
                    />
                    <button @click="verifySavedEmail(entry.email)" :disabled="!verifyCode || verifyCode.length !== 6 || verifyingSaved" class="profile-link-button">
                      {{ t('profile.balanceNotify.verify') }}
                    </button>
                    <span v-if="verifyCountdown > 0" class="notify-countdown">{{ verifyCountdown }}s</span>
                    <button v-else @click="sendCodeForSaved(entry.email)" :disabled="sendingSavedCode" class="profile-link-button is-muted">
                      {{ t('profile.balanceNotify.resend') }}
                    </button>
                    <button @click="verifyingEmail = ''" class="profile-link-button is-muted">
                      {{ t('common.cancel') }}
                    </button>
                  </template>
                  <template v-else>
                    <button @click="sendCodeForSaved(entry.email)" :disabled="sendingSavedCode" class="profile-link-button">
                      {{ t('profile.balanceNotify.verify') }}
                    </button>
                  </template>
                </template>
                <button @click="handleRemoveEmail(entry.email)" class="profile-link-button is-danger">
                  {{ t('profile.balanceNotify.removeEmail') }}
                </button>
              </div>
            </li>
          </ul>

          <!-- Pending (unverified) emails in verification flow -->
          <ul v-if="pendingEmails.length > 0" class="notify-emails">
            <li v-for="(pe, idx) in pendingEmails" :key="pe.email" class="notify-email is-pending">
              <div class="notify-email-main">
                <Icon name="mail" size="sm" class="shrink-0 text-primary-700" />
                <span class="notify-email-address">{{ pe.email }}</span>
              </div>
              <div v-if="!pe.codeSent" class="notify-email-actions">
                <button @click="sendCodeFor(idx)" :disabled="pe.sending" class="profile-link-button">
                  {{ t('profile.balanceNotify.sendCode') }}
                </button>
                <button @click="pendingEmails.splice(idx, 1)" class="profile-link-button is-danger">
                  {{ t('profile.balanceNotify.removeEmail') }}
                </button>
              </div>
              <div v-else class="notify-email-actions">
                <input
                  v-model="pe.code"
                  type="text"
                  inputmode="numeric"
                  maxlength="6"
                  class="notify-code-input"
                  :placeholder="t('profile.balanceNotify.codePlaceholder')"
                />
                <button @click="verifyPending(idx)" :disabled="!pe.code || pe.code.length !== 6 || pe.verifying" class="profile-link-button">
                  {{ t('profile.balanceNotify.verify') }}
                </button>
                <span v-if="pe.countdown > 0" class="notify-countdown">{{ pe.countdown }}s</span>
                <button v-else @click="sendCodeFor(idx)" :disabled="pe.sending" class="profile-link-button is-muted">
                  {{ t('profile.balanceNotify.resend') }}
                </button>
              </div>
            </li>
          </ul>

          <!-- Add new email input (hidden when at limit) -->
          <div v-if="canAddMore" class="notify-add">
            <input
              v-model="newEmail"
              type="email"
              class="input flex-1"
              :placeholder="t('profile.balanceNotify.emailPlaceholder')"
              @keyup.enter="addPendingEmail"
            />
            <button
              @click="addPendingEmail"
              :disabled="!newEmail"
              class="btn btn-secondary whitespace-nowrap"
            >
              {{ t('common.add') }}
            </button>
          </div>
          <p v-else class="notify-limit">
            {{ t('profile.balanceNotify.maxEmailsReached') }}
          </p>
        </div>
      </template>
    </div>
  </section>
</template>

<script setup lang="ts">
import { ref, computed, watch, onMounted, onUnmounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAuthStore } from '@/stores/auth'
import { useAppStore } from '@/stores/app'
import { userAPI } from '@/api'
import Icon from '@/components/icons/Icon.vue'
import { extractApiErrorMessage } from '@/utils/apiError'
import type { NotifyEmailEntry } from '@/types'

const maxTotalEmails = 3

interface PendingEmail {
  email: string
  codeSent: boolean
  code: string
  sending: boolean
  verifying: boolean
  countdown: number
  timer: ReturnType<typeof setInterval> | null
}

const props = defineProps<{
  enabled: boolean
  threshold: number | null
  extraEmails: NotifyEmailEntry[]
  systemDefaultThreshold: number
  userEmail: string
}>()

const { t } = useI18n()
const authStore = useAuthStore()
const appStore = useAppStore()

const notifyEnabled = ref(props.enabled)
const customThreshold = ref<number | null>(props.threshold)
const emailEntries = ref<NotifyEmailEntry[]>([...props.extraEmails])
const pendingEmails = ref<PendingEmail[]>([])
const newEmail = ref('')
const savingThreshold = ref(false)

// State for verifying saved unverified emails
const verifyingEmail = ref('')
const verifyCode = ref('')
const verifyingSaved = ref(false)
const sendingSavedCode = ref(false)
const verifyCountdown = ref(0)
let verifyTimer: ReturnType<typeof setInterval> | null = null
let disposed = false

const canAddMore = computed(() => {
  return emailEntries.value.length + pendingEmails.value.length < maxTotalEmails
})

watch(() => props.enabled, (val) => { notifyEnabled.value = val })
watch(() => props.threshold, (val) => { customThreshold.value = val })
watch(() => props.extraEmails, (val) => { emailEntries.value = [...val] })

// When list is empty on mount, pre-fill the add input with user's email
onMounted(() => {
  if (emailEntries.value.length === 0 && props.userEmail) {
    newEmail.value = props.userEmail
  }
})

onUnmounted(() => {
  disposed = true
  for (const pe of pendingEmails.value) {
    if (pe.timer) clearInterval(pe.timer)
  }
  if (verifyTimer) clearInterval(verifyTimer)
})

const handleToggle = async () => {
  try {
    const updated = await userAPI.updateProfile({ balance_notify_enabled: notifyEnabled.value })
    authStore.user = updated
  } catch (err: unknown) {
    appStore.showError(extractApiErrorMessage(err, t('common.error')))
    notifyEnabled.value = !notifyEnabled.value
  }
}

const handleThresholdUpdate = async () => {
  savingThreshold.value = true
  try {
    const threshold = customThreshold.value && customThreshold.value > 0 ? customThreshold.value : 0
    const updated = await userAPI.updateProfile({ balance_notify_threshold: threshold })
    authStore.user = updated
    appStore.showSuccess(t('common.saved'))
  } catch (err: unknown) {
    appStore.showError(extractApiErrorMessage(err, t('common.error')))
  } finally {
    savingThreshold.value = false
  }
}

async function handleEmailToggle(entry: NotifyEmailEntry) {
  const newDisabled = !entry.disabled
  try {
    const updated = await userAPI.toggleNotifyEmail(entry.email, newDisabled)
    authStore.user = updated
    emailEntries.value = [...updated.balance_notify_extra_emails]
  } catch (err: unknown) {
    appStore.showError(extractApiErrorMessage(err, t('common.error')))
  }
}

function addPendingEmail() {
  const email = newEmail.value.trim()
  if (!email) return
  // Check duplicates
  const isDuplicate = emailEntries.value.some(e => e.email.toLowerCase() === email.toLowerCase())
    || pendingEmails.value.some(p => p.email.toLowerCase() === email.toLowerCase())
  if (isDuplicate) {
    appStore.showError(t('profile.balanceNotify.emailDuplicate'))
    return
  }
  pendingEmails.value.push({ email, codeSent: false, code: '', sending: false, verifying: false, countdown: 0, timer: null })
  newEmail.value = ''
}

async function sendCodeFor(idx: number) {
  const pe = pendingEmails.value[idx]
  if (!pe) return
  pe.sending = true
  try {
    await userAPI.sendNotifyEmailCode(pe.email)
    if (disposed || !pendingEmails.value.includes(pe)) return
    pe.codeSent = true
    pe.countdown = 60
    pe.timer = setInterval(() => {
      pe.countdown--
      if (pe.countdown <= 0 && pe.timer) {
        clearInterval(pe.timer)
        pe.timer = null
      }
    }, 1000)
    appStore.showSuccess(t('profile.balanceNotify.codeSent'))
  } catch (err: unknown) {
    appStore.showError(extractApiErrorMessage(err, t('common.error')))
  } finally {
    pe.sending = false
  }
}

async function verifyPending(idx: number) {
  const pe = pendingEmails.value[idx]
  if (!pe || !pe.code || pe.code.length !== 6) return
  pe.verifying = true
  try {
    await userAPI.verifyNotifyEmail(pe.email, pe.code)
    if (pe.timer) clearInterval(pe.timer)
    pendingEmails.value.splice(idx, 1)
    appStore.showSuccess(t('profile.balanceNotify.verifySuccess'))
    const updated = await userAPI.getProfile()
    authStore.user = updated
    emailEntries.value = [...updated.balance_notify_extra_emails]
  } catch (err: unknown) {
    appStore.showError(extractApiErrorMessage(err, t('common.error')))
  } finally {
    pe.verifying = false
  }
}

const handleRemoveEmail = async (email: string) => {
  try {
    await userAPI.removeNotifyEmail(email)
    appStore.showSuccess(t('profile.balanceNotify.removeSuccess'))
    const updated = await userAPI.getProfile()
    authStore.user = updated
    emailEntries.value = [...updated.balance_notify_extra_emails]
  } catch (err: unknown) {
    appStore.showError(extractApiErrorMessage(err, t('common.error')))
  }
}

// Verify saved unverified emails
async function sendCodeForSaved(email: string) {
  sendingSavedCode.value = true
  try {
    await userAPI.sendNotifyEmailCode(email)
    if (disposed) return
    verifyingEmail.value = email
    verifyCode.value = ''
    verifyCountdown.value = 60
    if (verifyTimer) clearInterval(verifyTimer)
    verifyTimer = setInterval(() => {
      verifyCountdown.value--
      if (verifyCountdown.value <= 0 && verifyTimer) {
        clearInterval(verifyTimer)
        verifyTimer = null
      }
    }, 1000)
    appStore.showSuccess(t('profile.balanceNotify.codeSent'))
  } catch (err: unknown) {
    appStore.showError(extractApiErrorMessage(err, t('common.error')))
  } finally {
    sendingSavedCode.value = false
  }
}

async function verifySavedEmail(email: string) {
  if (!verifyCode.value || verifyCode.value.length !== 6) return
  verifyingSaved.value = true
  try {
    await userAPI.verifyNotifyEmail(email, verifyCode.value)
    verifyingEmail.value = ''
    verifyCode.value = ''
    if (verifyTimer) { clearInterval(verifyTimer); verifyTimer = null }
    appStore.showSuccess(t('profile.balanceNotify.verifySuccess'))
    const updated = await userAPI.getProfile()
    authStore.user = updated
    emailEntries.value = [...updated.balance_notify_extra_emails]
  } catch (err: unknown) {
    appStore.showError(extractApiErrorMessage(err, t('common.error')))
  } finally {
    verifyingSaved.value = false
  }
}
</script>

<style scoped>
.notify-toggle {
  cursor: pointer;
}

.notify-switch {
  position: relative;
  display: inline-flex;
  flex-shrink: 0;
  cursor: pointer;
}

.notify-switch-track {
  position: relative;
  width: 2.75rem;
  height: 1.5rem;
  border-radius: 999px;
  background: #e3d9b8;
  transition: background-color 180ms ease;
}

.notify-switch-track::after {
  position: absolute;
  top: 0.1875rem;
  left: 0.1875rem;
  width: 1.125rem;
  height: 1.125rem;
  border-radius: 999px;
  background: #fff;
  box-shadow: 0 1px 2px rgb(63 52 32 / 0.3);
  content: '';
  transition: transform 200ms var(--theme-ease-out);
}

.peer:checked + .notify-switch-track {
  background: #e9b824;
}

.peer:checked + .notify-switch-track::after {
  transform: translateX(1.25rem);
}

.peer:focus-visible + .notify-switch-track {
  outline: 3px solid rgb(233 184 36 / 0.45);
  outline-offset: 2px;
}

.notify-switch.is-small .notify-switch-track {
  width: 2.25rem;
  height: 1.25rem;
}

.notify-switch.is-small .notify-switch-track::after {
  top: 0.125rem;
  left: 0.125rem;
  width: 1rem;
  height: 1rem;
}

.notify-switch.is-small .peer:checked + .notify-switch-track::after {
  transform: translateX(1rem);
}

.notify-section {
  display: flex;
  min-width: 0;
  flex-direction: column;
  gap: 0.5rem;
}

.notify-label {
  display: flex;
  flex-wrap: wrap;
  align-items: baseline;
  gap: 0.25rem 0.5rem;
  font-size: 0.875rem;
  font-weight: 700;
  color: #3f3420;
}

.notify-label-hint {
  font-size: 0.75rem;
  font-weight: 500;
  color: #6b6045;
}

.notify-threshold {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 0.5rem;
}

.notify-money {
  display: flex;
  min-width: 0;
  flex: 1 1 12rem;
  align-items: center;
  gap: 0.375rem;
  padding: 0 0.875rem;
  border-radius: 1rem;
  background: var(--md-sys-color-surface-container);
  font-weight: 700;
  color: #7e5600;
  box-shadow: inset 0 1px 2px rgb(63 52 32 / 0.06);
  transition: background-color 160ms ease, box-shadow 160ms ease;
}

.notify-money:focus-within {
  background: var(--md-sys-color-surface);
  box-shadow: inset 0 1px 2px rgb(63 52 32 / 0.04), 0 0 0 4px rgb(233 184 36 / 0.16), 0 0 0 1px rgb(201 154 18 / 0.5);
}

.notify-money-input {
  min-width: 0;
  flex: 1;
  height: 2.75rem;
  border: 0;
  background: transparent;
  font-size: 0.9375rem;
  font-weight: 600;
  font-variant-numeric: tabular-nums;
  color: #3f3420;
  outline: none;
}

.notify-note {
  display: flex;
  align-items: flex-start;
  gap: 0.375rem;
  padding: 0.625rem 0.75rem;
  border-radius: 0.875rem;
  background: #fff3bd;
  font-size: 0.75rem;
  line-height: 1.125rem;
  color: #65460b;
}

.notify-emails {
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
}

.notify-email {
  display: flex;
  min-width: 0;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 0.5rem 0.75rem;
  padding: 0.625rem 0.75rem;
  border-radius: 1rem;
  background: #fffbe8;
}

.notify-email.is-pending {
  background: #fff3bd;
}

.notify-email-main {
  display: flex;
  min-width: 0;
  flex: 1 1 14rem;
  align-items: center;
  gap: 0.625rem;
}

.notify-email-address {
  min-width: 0;
  font-size: 0.875rem;
  font-weight: 600;
  overflow-wrap: anywhere;
  color: #3f3420;
}

.notify-email-address.is-off {
  color: #9b8c62;
}

.notify-email-actions {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: flex-end;
  gap: 0.25rem;
  margin-left: auto;
}

.notify-code-input {
  width: 6.5rem;
  height: 2rem;
  padding: 0 0.625rem;
  border-radius: 0.75rem;
  background: var(--md-sys-color-surface);
  font-size: 0.8125rem;
  font-variant-numeric: tabular-nums;
  letter-spacing: 0.08em;
  color: #3f3420;
  box-shadow: inset 0 0 0 1px rgb(215 193 106 / 0.55);
  outline: none;
}

.notify-code-input:focus {
  box-shadow: inset 0 0 0 1px rgb(201 154 18 / 0.8), 0 0 0 3px rgb(233 184 36 / 0.18);
}

.notify-countdown {
  padding: 0 0.375rem;
  font-size: 0.75rem;
  font-variant-numeric: tabular-nums;
  color: #9b8c62;
}

.notify-add {
  display: flex;
  min-width: 0;
  flex-wrap: wrap;
  gap: 0.5rem;
}

.notify-add .input {
  min-width: 12rem;
}

.notify-limit {
  font-size: 0.75rem;
  color: #9b8c62;
}

@media (prefers-reduced-motion: reduce) {
  .notify-switch-track,
  .notify-switch-track::after {
    transition: none;
  }
}
</style>
