<template>
  <section class="profile-panel" data-testid="profile-totp-panel">
    <header class="profile-panel-head">
      <span class="profile-panel-icon is-garden" aria-hidden="true">
        <Icon name="shield" size="md" />
      </span>
      <div class="min-w-0">
        <h2 class="profile-panel-title">{{ t('profile.totp.title') }}</h2>
        <p class="profile-panel-desc">{{ t('profile.totp.description') }}</p>
      </div>
    </header>

    <div class="profile-panel-body">
      <!-- Loading state -->
      <div v-if="loading" class="profile-state" aria-busy="true">
        <div class="profile-state-main">
          <span class="profile-state-mark skeleton"></span>
          <span class="skeleton h-4 w-40 rounded-full"></span>
        </div>
      </div>

      <!-- Feature disabled globally -->
      <div v-else-if="status && !status.feature_enabled" class="profile-state">
        <div class="profile-state-main">
          <span class="profile-state-mark is-warn" aria-hidden="true">
            <Icon name="exclamationTriangle" size="md" />
          </span>
          <div class="min-w-0">
            <p class="profile-state-title">{{ t('profile.totp.featureDisabled') }}</p>
            <p class="profile-state-hint">{{ t('profile.totp.featureDisabledHint') }}</p>
          </div>
        </div>
      </div>

      <!-- 2FA Enabled -->
      <div v-else-if="status?.enabled" class="profile-state">
        <div class="profile-state-main">
          <span class="profile-state-mark is-on" aria-hidden="true">
            <Icon name="shield" size="md" />
          </span>
          <div class="min-w-0">
            <p class="profile-state-title">{{ t('profile.totp.enabled') }}</p>
            <p v-if="status.enabled_at" class="profile-state-hint">
              {{ t('profile.totp.enabledAt') }}: {{ formatDate(status.enabled_at) }}
            </p>
          </div>
        </div>
        <button
          type="button"
          class="btn btn-secondary btn-sm profile-danger-button"
          @click="showDisableDialog = true"
        >
          {{ t('profile.totp.disable') }}
        </button>
      </div>

      <!-- 2FA Not Enabled -->
      <div v-else class="profile-state">
        <div class="profile-state-main">
          <span class="profile-state-mark" aria-hidden="true">
            <Icon name="shield" size="md" />
          </span>
          <div class="min-w-0">
            <p class="profile-state-title">{{ t('profile.totp.notEnabled') }}</p>
            <p class="profile-state-hint">{{ t('profile.totp.notEnabledHint') }}</p>
          </div>
        </div>
        <button
          type="button"
          class="btn btn-primary btn-sm"
          @click="showSetupModal = true"
        >
          {{ t('profile.totp.enable') }}
        </button>
      </div>
    </div>

    <!-- Setup Modal -->
    <TotpSetupModal
      v-if="showSetupModal"
      @close="showSetupModal = false"
      @success="handleSetupSuccess"
    />

    <!-- Disable Dialog -->
    <TotpDisableDialog
      v-if="showDisableDialog"
      @close="showDisableDialog = false"
      @success="handleDisableSuccess"
    />
  </section>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { totpAPI } from '@/api'
import Icon from '@/components/icons/Icon.vue'
import type { TotpStatus } from '@/types'
import TotpSetupModal from './TotpSetupModal.vue'
import TotpDisableDialog from './TotpDisableDialog.vue'

const { t } = useI18n()

const loading = ref(true)
const status = ref<TotpStatus | null>(null)
const showSetupModal = ref(false)
const showDisableDialog = ref(false)

const loadStatus = async () => {
  loading.value = true
  try {
    status.value = await totpAPI.getStatus()
  } catch (error) {
    console.error('Failed to load TOTP status:', error)
  } finally {
    loading.value = false
  }
}

const handleSetupSuccess = () => {
  showSetupModal.value = false
  loadStatus()
}

const handleDisableSuccess = () => {
  showDisableDialog.value = false
  loadStatus()
}

const formatDate = (timestamp: number) => {
  // Backend returns Unix timestamp in seconds, convert to milliseconds
  const date = new Date(timestamp * 1000)
  return date.toLocaleDateString(undefined, {
    year: 'numeric',
    month: 'long',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit'
  })
}

onMounted(() => {
  loadStatus()
})
</script>

<style scoped>
.profile-danger-button {
  color: #a44f3b;
}

.profile-danger-button:hover:not(:disabled) {
  background: #fce7df;
}
</style>
