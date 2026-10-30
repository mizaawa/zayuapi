<template>
  <div :class="props.embedded ? '' : 'profile-panel'">
    <header v-if="!props.embedded" class="profile-panel-head">
      <div class="min-w-0">
        <h2 class="profile-panel-title">{{ t('profile.editProfile') }}</h2>
      </div>
    </header>
    <form class="profile-edit-form" @submit.prevent="handleUpdateProfile">
      <div>
        <label for="username" class="profile-box-title mb-2 block">
          {{ t('profile.username') }}
        </label>
        <input
          id="username"
          v-model="username"
          type="text"
          class="input"
          autocomplete="nickname"
          :placeholder="t('profile.enterUsername')"
        />
      </div>

      <div class="profile-actions">
        <button type="submit" :disabled="loading" class="btn btn-primary btn-sm">
          {{ loading ? t('profile.updating') : t('profile.updateProfile') }}
        </button>
      </div>
    </form>
  </div>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAuthStore } from '@/stores/auth'
import { useAppStore } from '@/stores/app'
import { userAPI } from '@/api'

const props = withDefaults(defineProps<{
  initialUsername: string
  embedded?: boolean
}>(), {
  embedded: false,
})

const { t } = useI18n()
const authStore = useAuthStore()
const appStore = useAppStore()

const username = ref(props.initialUsername)
let savedUsername = props.initialUsername
const loading = ref(false)

watch(() => props.initialUsername, (val) => {
  if (username.value === savedUsername) username.value = val
  savedUsername = val
})

const handleUpdateProfile = async () => {
  if (!username.value.trim()) {
    appStore.showError(t('profile.usernameRequired'))
    return
  }

  loading.value = true
  const submittedUsername = username.value
  try {
    const updatedUser = await userAPI.updateProfile({
      username: submittedUsername
    })
    if (username.value === submittedUsername) username.value = updatedUser.username
    savedUsername = updatedUser.username
    authStore.user = updatedUser
    appStore.showSuccess(t('profile.updateSuccess'))
  } catch (error: any) {
    appStore.showError(error.response?.data?.detail || t('profile.updateFailed'))
  } finally {
    loading.value = false
  }
}
</script>

<style scoped>
.profile-edit-form {
  display: flex;
  min-width: 0;
  flex-direction: column;
  gap: 0.875rem;
}

.profile-edit-form .input {
  background: var(--md-sys-color-surface);
}
</style>
