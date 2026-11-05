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
          v-if="previewSrc"
          data-testid="profile-avatar-preview"
          :src="previewSrc"
          :alt="displayName"
          referrerpolicy="no-referrer"
          @error="handlePreviewError"
        >
        <span v-else data-testid="profile-avatar-initial" aria-hidden="true">{{ avatarInitial }}</span>
        <span
          v-if="avatarBusy"
          data-testid="profile-avatar-saving"
          class="profile-avatar-draft"
        >{{ t('profile.avatar.saving') }}</span>
      </div>

      <div class="min-w-0 flex-1">
        <p class="profile-box-title">
          {{ props.embedded ? t('profile.avatar.title') : displayName }}
        </p>
        <p class="profile-box-hint">{{ t('profile.avatar.uploadHint') }}</p>
      </div>
    </div>

    <div class="profile-avatar-actions">
      <label
        class="btn btn-secondary btn-sm"
        :class="avatarBusy ? 'cursor-not-allowed opacity-60' : 'cursor-pointer'"
      >
        <input
          data-testid="profile-avatar-file-input"
          type="file"
          accept="image/*"
          class="hidden"
          :disabled="avatarBusy"
          @change="handleAvatarFileChange"
        >
        <Icon name="upload" size="sm" />
        {{ t('profile.avatar.uploadAction') }}
      </label>

      <button
        data-testid="profile-avatar-delete"
        type="button"
        class="profile-link-button is-danger"
        :disabled="avatarBusy"
        @click="handleAvatarDelete"
      >
        <Icon name="trash" size="sm" />
        {{ t('common.delete') }}
      </button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { userAPI } from '@/api'
import Icon from '@/components/icons/Icon.vue'
import { useAvatarImage } from '@/composables/useAvatarImage'
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
// An avatar is shown at 88px at most; starting from a smaller canvas keeps
// 48MP phone photos under mobile canvas limits and leaves room for quality.
const maxAvatarDimension = 512
const avatarScaleSteps = [1, 0.92, 0.84, 0.76, 0.68, 0.6, 0.52, 0.44, 0.36]
const avatarQualitySteps = [0.92, 0.84, 0.76, 0.68, 0.6, 0.52, 0.44, 0.36]
// Formats every browser can show; anything else is redrawn before upload.
const passThroughAvatarTypes = new Set(['image/png', 'image/jpeg', 'image/webp'])

// The picked image is shown here only while it is being saved.
const pendingAvatar = ref('')
const avatarBusy = ref(false)

const displayName = computed(() => props.user?.username?.trim() || props.user?.email?.trim() || t('profile.user'))
const avatarInitial = computed(() => Array.from(displayName.value)[0]?.toUpperCase() || 'U')
const { src: savedAvatarSrc, onError: onSavedAvatarError } = useAvatarImage(() => props.user?.avatar_url)
const previewSrc = computed(() => pendingAvatar.value || savedAvatarSrc.value)

function handlePreviewError() {
  if (!pendingAvatar.value) {
    onSavedAvatarError()
  }
}

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

function drawAvatar(ctx: CanvasRenderingContext2D, image: HTMLImageElement, width: number, height: number, type: string) {
  ctx.clearRect(0, 0, width, height)
  if (type === 'image/jpeg') {
    // JPEG has no transparency; paint white like the server-side compressor.
    ctx.fillStyle = '#ffffff'
    ctx.fillRect(0, 0, width, height)
  }
  ctx.drawImage(image, 0, 0, width, height)
}

async function compressAvatarFile(file: File): Promise<File> {
  const sourceDataURL = await readFileAsDataURL(file)
  const image = await loadImage(sourceDataURL)
  const canvas = document.createElement('canvas')
  const ctx = canvas.getContext('2d')
  if (!ctx) {
    throw new Error(t('profile.avatar.compressFailed'))
  }

  // SVGs without an intrinsic size report 0; draw them at the avatar size.
  const sourceWidth = image.naturalWidth || maxAvatarDimension
  const sourceHeight = image.naturalHeight || maxAvatarDimension
  const baseScale = Math.min(1, maxAvatarDimension / Math.max(sourceWidth, sourceHeight))
  const fileName = file.name.replace(/\.[^.]+$/, '') || 'avatar'
  let outputType = 'image/webp'

  for (const scale of avatarScaleSteps) {
    const width = Math.max(1, Math.round(sourceWidth * baseScale * scale))
    const height = Math.max(1, Math.round(sourceHeight * baseScale * scale))
    canvas.width = width
    canvas.height = height
    drawAvatar(ctx, image, width, height, outputType)

    for (const quality of avatarQualitySteps) {
      let blob = await canvasToBlob(canvas, outputType, quality)
      if (blob.type !== outputType) {
        // Safari cannot encode WebP and quietly returns a PNG instead, which
        // ignores the quality and is rarely under 20KB. Every browser encodes JPEG.
        outputType = 'image/jpeg'
        drawAvatar(ctx, image, width, height, outputType)
        blob = await canvasToBlob(canvas, outputType, quality)
      }
      if (blob.size <= targetAvatarUploadBytes) {
        const extension = outputType === 'image/jpeg' ? 'jpg' : 'webp'
        return new File([blob], `${fileName}.${extension}`, { type: outputType })
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
  if (file.size <= targetAvatarUploadBytes && passThroughAvatarTypes.has(file.type)) {
    return file
  }
  return compressAvatarFile(file)
}

// Picking an image saves it right away: a separate save step was easy to miss,
// and an unsaved pick never reaches the header or the profile card.
async function handleAvatarFileChange(event: Event) {
  const input = event.target as HTMLInputElement | null
  const file = input?.files?.[0]
  if (input) {
    input.value = ''
  }
  if (!file || avatarBusy.value) {
    return
  }

  avatarBusy.value = true
  try {
    const preparedFile = await prepareAvatarUpload(file)
    const dataURL = await readFileAsDataURL(preparedFile)
    const normalized = normalizeUploadedAvatar(dataURL)
    if (!normalized) {
      return
    }
    pendingAvatar.value = normalized
    const updated = await userAPI.updateProfile({ avatar_url: normalized })
    authStore.user = updated
    appStore.showSuccess(t('profile.avatar.saveSuccess'))
  } catch (error: unknown) {
    appStore.showError(extractApiErrorMessage(error, t('common.error')))
  } finally {
    pendingAvatar.value = ''
    avatarBusy.value = false
  }
}

async function handleAvatarDelete() {
  if (avatarBusy.value) {
    return
  }
  if (!props.user?.avatar_url?.trim()) {
    appStore.showError(t('profile.avatar.emptyDeleteHint'))
    return
  }

  avatarBusy.value = true
  try {
    const updated = await userAPI.updateProfile({ avatar_url: '' })
    authStore.user = updated
    appStore.showSuccess(t('profile.avatar.deleteSuccess'))
  } catch (error: unknown) {
    appStore.showError(extractApiErrorMessage(error, t('common.error')))
  } finally {
    avatarBusy.value = false
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

/* A pick that is still being saved gets a small tag. */
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
