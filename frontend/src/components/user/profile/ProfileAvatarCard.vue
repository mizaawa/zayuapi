<template>
  <div :class="props.embedded ? 'profile-avatar' : 'profile-panel'">
    <header v-if="!props.embedded" class="profile-panel-head">
      <span class="profile-panel-icon" aria-hidden="true">
        <Icon name="user" size="md" />
      </span>
      <div class="min-w-0">
        <h2 class="profile-panel-title">{{ t('profile.avatar.title') }}</h2>
        <p class="profile-panel-desc">{{ t('profile.avatar.description') }}</p>
      </div>
    </header>

    <div class="profile-avatar-row">
      <div class="profile-avatar-preview">
        <img
          v-if="avatarPreviewUrl"
          data-testid="profile-avatar-preview"
          :src="avatarPreviewUrl"
          :alt="displayName"
        >
        <span v-else aria-hidden="true">{{ avatarInitial }}</span>
        <span v-if="avatarDraft" class="profile-avatar-draft">{{ t('profile.avatar.unsaved') }}</span>
      </div>

      <div class="min-w-0 flex-1">
        <p class="profile-box-title">
          {{ props.embedded ? t('profile.avatar.title') : displayName }}
        </p>
        <p class="profile-box-hint">{{ t('profile.avatar.uploadHint') }}</p>
      </div>
    </div>

    <div class="profile-avatar-actions">
      <label class="btn btn-secondary btn-sm cursor-pointer">
        <input
          data-testid="profile-avatar-file-input"
          type="file"
          accept="image/*"
          class="hidden"
          @change="handleAvatarFileChange"
        >
        <Icon name="upload" size="sm" />
        {{ t('profile.avatar.uploadAction') }}
      </label>

      <button
        data-testid="profile-avatar-save"
        type="button"
        class="btn btn-primary btn-sm"
        :disabled="avatarSaving || !avatarDraft"
        @click="handleAvatarSave"
      >
        {{ t('common.save') }}
      </button>

      <button
        data-testid="profile-avatar-delete"
        type="button"
        class="profile-link-button is-danger"
        :disabled="avatarSaving"
        @click="handleAvatarDelete"
      >
        <Icon name="trash" size="sm" />
        {{ t('common.delete') }}
      </button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { userAPI } from '@/api'
import Icon from '@/components/icons/Icon.vue'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'
import type { User } from '@/types'
import { extractApiErrorMessage } from '@/utils/apiError'

const props = withDefaults(defineProps<{
  user: User | null
  embedded?: boolean
}>(), {
  embedded: false,
})

const { t } = useI18n()
const authStore = useAuthStore()
const appStore = useAppStore()

const targetAvatarUploadBytes = 20 * 1024
const avatarScaleSteps = [1, 0.92, 0.84, 0.76, 0.68, 0.6, 0.52, 0.44, 0.36]
const avatarQualitySteps = [0.92, 0.84, 0.76, 0.68, 0.6, 0.52, 0.44, 0.36]
const avatarDraft = ref('')
const avatarSaving = ref(false)

const displayName = computed(() => props.user?.username?.trim() || props.user?.email?.trim() || t('profile.user'))
const avatarInitial = computed(() => Array.from(displayName.value)[0]?.toUpperCase() || 'U')
const avatarPreviewUrl = computed(() => avatarDraft.value.trim() || props.user?.avatar_url?.trim() || '')

watch(
  () => props.user?.avatar_url,
  () => {
    avatarDraft.value = ''
  }
)

function normalizeUploadedAvatar(value: string): string | null {
  const normalized = value.trim()
  if (!normalized) {
    return null
  }

  if (!/^data:image\/[a-zA-Z0-9.+-]+;base64,/i.test(normalized)) {
    appStore.showError(t('profile.avatar.uploadRequired'))
    return null
  }

  return normalized
}

function readFileAsDataURL(file: File): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader()
    reader.onload = () => resolve(typeof reader.result === 'string' ? reader.result : '')
    reader.onerror = () => reject(reader.error ?? new Error('avatar_read_failed'))
    reader.readAsDataURL(file)
  })
}

function loadImage(dataURL: string): Promise<HTMLImageElement> {
  return new Promise((resolve, reject) => {
    const image = new Image()
    image.onload = () => resolve(image)
    image.onerror = () => reject(new Error(t('profile.avatar.readFailed')))
    image.src = dataURL
  })
}

function canvasToBlob(canvas: HTMLCanvasElement, type: string, quality: number): Promise<Blob> {
  return new Promise((resolve, reject) => {
    canvas.toBlob((blob) => {
      if (!blob) {
        reject(new Error(t('profile.avatar.compressFailed')))
        return
      }
      resolve(blob)
    }, type, quality)
  })
}

async function compressAvatarFile(file: File): Promise<File> {
  const sourceDataURL = await readFileAsDataURL(file)
  const image = await loadImage(sourceDataURL)
  const canvas = document.createElement('canvas')
  const ctx = canvas.getContext('2d')
  if (!ctx) {
    throw new Error(t('profile.avatar.compressFailed'))
  }

  for (const scale of avatarScaleSteps) {
    const width = Math.max(1, Math.round(image.naturalWidth * scale))
    const height = Math.max(1, Math.round(image.naturalHeight * scale))
    canvas.width = width
    canvas.height = height
    ctx.clearRect(0, 0, width, height)
    ctx.drawImage(image, 0, 0, width, height)

    for (const quality of avatarQualitySteps) {
      const blob = await canvasToBlob(canvas, 'image/webp', quality)
      if (blob.size <= targetAvatarUploadBytes) {
        const fileName = file.name.replace(/\.[^.]+$/, '') || 'avatar'
        return new File([blob], `${fileName}.webp`, { type: 'image/webp' })
      }
    }
  }

  throw new Error(t('profile.avatar.compressTooLarge'))
}

async function prepareAvatarUpload(file: File): Promise<File> {
  if (!file.type.startsWith('image/')) {
    throw new Error(t('profile.avatar.invalidType'))
  }
  if (file.type === 'image/gif') {
    if (file.size > targetAvatarUploadBytes) {
      throw new Error(t('profile.avatar.gifTooLarge'))
    }
    return file
  }
  if (file.size <= targetAvatarUploadBytes) {
    return file
  }
  return compressAvatarFile(file)
}

async function handleAvatarFileChange(event: Event) {
  const input = event.target as HTMLInputElement | null
  const file = input?.files?.[0]
  if (input) {
    input.value = ''
  }
  if (!file) {
    return
  }

  try {
    const preparedFile = await prepareAvatarUpload(file)
    const dataURL = await readFileAsDataURL(preparedFile)
    const normalized = normalizeUploadedAvatar(dataURL)
    if (!normalized) {
      return
    }
    avatarDraft.value = normalized
  } catch (error: unknown) {
    appStore.showError(extractApiErrorMessage(error, t('common.error')))
  }
}

async function handleAvatarSave() {
  const normalized = normalizeUploadedAvatar(avatarDraft.value)
  if (!normalized) {
    return
  }

  avatarSaving.value = true
  try {
    const updated = await userAPI.updateProfile({ avatar_url: normalized })
    authStore.user = updated
    avatarDraft.value = updated.avatar_url?.trim() || ''
    appStore.showSuccess(t('profile.avatar.saveSuccess'))
  } catch (error: unknown) {
    appStore.showError(extractApiErrorMessage(error, t('common.error')))
  } finally {
    avatarSaving.value = false
  }
}

async function handleAvatarDelete() {
  if (avatarSaving.value) {
    return
  }
  if (!avatarDraft.value.trim() && !props.user?.avatar_url?.trim()) {
    appStore.showError(t('profile.avatar.emptyDeleteHint'))
    return
  }

  avatarSaving.value = true
  try {
    const updated = await userAPI.updateProfile({ avatar_url: '' })
    authStore.user = updated
    avatarDraft.value = ''
    appStore.showSuccess(t('profile.avatar.deleteSuccess'))
  } catch (error: unknown) {
    appStore.showError(extractApiErrorMessage(error, t('common.error')))
  } finally {
    avatarSaving.value = false
  }
}
</script>

<style scoped>
.profile-avatar {
  display: flex;
  min-width: 0;
  flex-direction: column;
  gap: 0.875rem;
}

.profile-panel .profile-avatar-row,
.profile-panel .profile-avatar-actions {
  grid-column: -2 / -1;
}

.profile-avatar-row {
  display: flex;
  min-width: 0;
  align-items: center;
  gap: 0.875rem;
}

.profile-avatar-preview {
  position: relative;
  display: flex;
  width: 4rem;
  height: 4rem;
  flex-shrink: 0;
  align-items: center;
  justify-content: center;
  border-radius: 1.25rem;
  background: linear-gradient(145deg, #f0c43d, #c99a12);
  font-size: 1.375rem;
  font-weight: 800;
  color: #fffef8;
  box-shadow: 0 0 0 3px #fffef8, 0 10px 20px -12px rgb(126 86 0 / 0.6);
}

.profile-avatar-preview img {
  width: 100%;
  height: 100%;
  border-radius: inherit;
  object-fit: cover;
}

/* An unsaved pick gets a small tag so it is not mistaken for the saved avatar. */
.profile-avatar-draft {
  position: absolute;
  right: -0.375rem;
  bottom: -0.375rem;
  padding: 0 0.375rem;
  border-radius: 999px;
  background: #e9b824;
  font-size: 0.625rem;
  font-weight: 750;
  line-height: 1.125rem;
  color: #3f3420;
  box-shadow: 0 0 0 2px #fffef8;
}

.profile-avatar-actions {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 0.5rem;
}
</style>
