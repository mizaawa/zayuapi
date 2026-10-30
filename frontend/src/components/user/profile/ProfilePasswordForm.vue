<template>
  <section :class="props.embedded ? '' : 'profile-panel'" data-testid="profile-password-panel">
    <header v-if="!props.embedded" class="profile-panel-head">
      <span class="profile-panel-icon is-coral" aria-hidden="true">
        <Icon name="lock" size="md" />
      </span>
      <div class="min-w-0">
        <h2 class="profile-panel-title">{{ t('profile.changePassword') }}</h2>
        <p class="profile-panel-desc">{{ t('profile.passwordDescription') }}</p>
      </div>
    </header>

    <form class="profile-password-form" @submit.prevent="handleChangePassword">
      <div class="profile-password-current">
        <label for="old_password" class="input-label">
          {{ t('profile.currentPassword') }}
        </label>
        <input
          id="old_password"
          v-model="form.old_password"
          type="password"
          required
          autocomplete="current-password"
          class="input"
        />
      </div>

      <div>
        <label for="new_password" class="input-label">
          {{ t('profile.newPassword') }}
        </label>
        <input
          id="new_password"
          v-model="form.new_password"
          type="password"
          required
          autocomplete="new-password"
          class="input"
        />
        <p class="input-hint">
          {{ t('profile.passwordHint') }}
        </p>
      </div>

      <div>
        <label for="confirm_password" class="input-label">
          {{ t('profile.confirmNewPassword') }}
        </label>
        <input
          id="confirm_password"
          v-model="form.confirm_password"
          type="password"
          required
          autocomplete="new-password"
          class="input"
        />
      </div>

      <div class="profile-actions profile-password-submit">
        <button type="submit" :disabled="loading" class="btn btn-primary">
          {{ loading ? t('profile.changingPassword') : t('profile.changePasswordButton') }}
        </button>
      </div>
    </form>
  </section>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { userAPI } from '@/api'
import Icon from '@/components/icons/Icon.vue'

const { t } = useI18n()
const appStore = useAppStore()
const props = withDefaults(defineProps<{
  embedded?: boolean
}>(), {
  embedded: false,
})

const loading = ref(false)
const form = ref({
  old_password: '',
  new_password: '',
  confirm_password: ''
})

const handleChangePassword = async () => {
  if (form.value.new_password !== form.value.confirm_password) {
    appStore.showError(t('profile.passwordsNotMatch'))
    return
  }

  if (form.value.new_password.length < 8) {
    appStore.showError(t('profile.passwordTooShort'))
    return
  }

  loading.value = true
  try {
    await userAPI.changePassword(form.value.old_password, form.value.new_password)
    form.value = { old_password: '', new_password: '', confirm_password: '' }
    appStore.showSuccess(t('profile.passwordChangeSuccess'))
  } catch (error: any) {
    appStore.showError(error.response?.data?.detail || t('profile.passwordChangeFailed'))
  } finally {
    loading.value = false
  }
}
</script>

<style scoped>
.profile-password-form {
  display: grid;
  min-width: 0;
  grid-template-columns: minmax(0, 1fr);
  gap: 1rem;
}

@container profile (min-width: 40rem) {
  .profile-password-form {
    grid-template-columns: repeat(2, minmax(0, 1fr));
    align-items: start;
  }

  .profile-password-current,
  .profile-password-submit {
    grid-column: 1 / -1;
  }

  .profile-password-current {
    max-width: calc(50% - 0.5rem);
  }
}
</style>
