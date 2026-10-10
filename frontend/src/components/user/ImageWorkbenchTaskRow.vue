<template>
  <article class="image-task-row" :data-task-id="task.id">
    <a
      v-if="imageUrl && !imageFailed"
      :href="imageUrl"
      target="_blank"
      rel="noopener noreferrer"
      class="task-preview"
      :aria-label="t('imageWorkbench.preview')"
    >
      <img
        :src="imageUrl"
        :alt="task.workbench?.prompt || task.workbench?.model || ''"
        loading="lazy"
        @load="onImageLoad"
        @error="imageFailed = true"
      />
      <span v-if="image?.size_bytes" class="image-size">{{ formatBytes(image.size_bytes) }}</span>
    </a>
    <div
      v-else
      class="task-preview preview-placeholder"
      :class="{ 'is-running': task.status === 'processing' }"
    >
      <Icon
        :name="
          task.status === 'processing'
            ? 'refresh'
            : task.status === 'failed'
              ? 'exclamationTriangle'
              : 'grid'
        "
        size="lg"
        :class="{ 'animate-spin': task.status === 'processing' }"
      />
      <span>{{ imageFailed ? t('imageWorkbench.imageLoadError') : statusLabel }}</span>
      <button v-if="imageFailed" type="button" class="retry-image" @click="imageFailed = false">
        {{ t('imageWorkbench.retry') }}
      </button>
    </div>
    <div class="task-details">
      <div class="task-heading">
        <h3 :title="filename">{{ filename }}</h3>
        <span class="task-status" :class="`status-${task.status}`">{{ statusLabel }}</span>
      </div>
      <dl>
        <div>
          <dt>{{ t('imageWorkbench.createdAt') }}</dt>
          <dd>{{ date }}</dd>
        </div>
        <div>
          <dt>{{ t('imageWorkbench.duration') }}</dt>
          <dd>{{ duration }}</dd>
        </div>
        <div>
          <dt>{{ t('imageWorkbench.model') }}</dt>
          <dd>{{ task.workbench?.model || '-' }}</dd>
        </div>
        <div>
          <dt>{{ t('imageWorkbench.remaining') }}</dt>
          <dd :class="{ 'expires-soon': remainingSeconds < 60 && task.status === 'completed' }">
            {{ remaining }}
          </dd>
        </div>
      </dl>
      <div class="task-meta">
        <span v-if="keyName">{{ keyName }}</span
        ><span v-if="actualSize || task.workbench?.size">{{ actualSize || task.workbench?.size }}</span
        ><span v-if="task.status === 'processing' && task.workbench?.count">{{
          t('imageWorkbench.imageCount', { count: task.workbench.count })
        }}</span>
      </div>
      <p v-if="task.status === 'failed'" class="task-error" role="alert">{{ errorMessage }}</p>
    </div>
    <div class="task-actions">
      <button
        type="button"
        class="icon-action"
        :title="t('imageWorkbench.copyPrompt')"
        :aria-label="t('imageWorkbench.copyPrompt')"
        :disabled="!task.workbench?.prompt"
        @click="copyToClipboard(task.workbench?.prompt || '')"
      >
        <Icon name="copy" />
      </button>
      <button
        v-if="task.status !== 'processing'"
        type="button"
        class="icon-action"
        :title="t('imageWorkbench.delete')"
        :aria-label="t('imageWorkbench.delete')"
        @click="$emit('delete')"
      >
        <Icon name="trash" />
      </button>
      <button
        type="button"
        class="download-action"
        :disabled="!imageUrl || downloading"
        @click="$emit('download')"
      >
        <Icon
          :name="downloading ? 'refresh' : 'download'"
          :class="{ 'animate-spin': downloading }"
          size="sm"
        />{{ t('imageWorkbench.download') }}
      </button>
    </div>
  </article>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import { useClipboard } from '@/composables/useClipboard'
import { formatBytes } from '@/utils/format'
import { createImageFilename as imageFilename, safeImageUrl } from '@/utils/imageWorkbench'
import type { ImageTask } from '@/api/imageWorkbench'

const props = defineProps<{
  task: ImageTask
  imageIndex: number
  now: number
  keyName?: string
  downloading: boolean
}>()
defineEmits<{ download: []; delete: [] }>()
const { t, locale } = useI18n()
const { copyToClipboard } = useClipboard()
const imageFailed = ref(false)
const actualSize = ref('')
function onImageLoad(event: Event) {
  const image = event.target as HTMLImageElement
  if (image.naturalWidth && image.naturalHeight) {
    actualSize.value = `${image.naturalWidth}x${image.naturalHeight}`
  }
}
const image = computed(() => props.task.result?.data?.[props.imageIndex])
const errorMessage = computed(
  () =>
    (typeof props.task.error === 'string' ? props.task.error : props.task.error?.message) ||
    t('imageWorkbench.failed')
)
const imageUrl = computed(() =>
  props.task.expires_at <= props.now ? null : safeImageUrl(image.value?.url)
)
watch(imageUrl, () => {
  imageFailed.value = false
  actualSize.value = ''
})
const filename = computed(() =>
  imageFilename(props.task.id, props.imageIndex, props.task.created_at, image.value?.url)
)
const date = computed(() =>
  new Date(props.task.created_at * 1000).toLocaleString(locale.value === 'zh' ? 'zh-CN' : 'en-US', {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit'
  })
)
const duration = computed(() => {
  const seconds = Math.max(0, (props.task.completed_at || props.now) - props.task.created_at)
  return seconds < 60
    ? t('imageWorkbench.seconds', { count: seconds })
    : t('imageWorkbench.minutes', { count: Math.floor(seconds / 60) })
})
const remainingSeconds = computed(() => Math.max(0, props.task.expires_at - props.now))
const remaining = computed(() => {
  if (props.task.status === 'processing') return '-'
  if (!remainingSeconds.value) return t('imageWorkbench.expired')
  return remainingSeconds.value < 60
    ? t('imageWorkbench.underMinute')
    : t('imageWorkbench.minutes', { count: Math.floor(remainingSeconds.value / 60) })
})
const statusLabel = computed(() =>
  t(`imageWorkbench.${props.task.expires_at <= props.now ? 'expired' : props.task.status}`)
)
</script>

<style scoped>
.image-task-row {
  display: grid;
  grid-template-columns: 148px minmax(0, 1fr) auto;
  align-items: center;
  gap: 24px;
  padding: 22px 0;
  border-bottom: 1px solid color-mix(in srgb, var(--md-sys-color-outline) 45%, transparent);
  color: var(--md-sys-color-on-surface);
}
.task-preview {
  position: relative;
  display: flex;
  align-items: center;
  justify-content: center;
  width: 148px;
  height: 122px;
  background: var(--md-sys-color-surface-container);
  border-radius: 20px;
  overflow: hidden;
}
.task-preview img {
  width: 100%;
  height: 100%;
  object-fit: cover;
  object-position: center;
}
.image-size {
  position: absolute;
  right: 6px;
  bottom: 6px;
  padding: 2px 6px;
  font-size: 11px;
  color: var(--md-sys-color-surface);
  background: color-mix(in srgb, var(--md-sys-color-on-surface) 85%, transparent);
  border-radius: 16px;
}
.preview-placeholder {
  flex-direction: column;
  gap: 10px;
  color: var(--md-sys-color-on-surface-variant);
  font-size: 12px;
  text-align: center;
  padding: 8px;
}
.is-running {
  color: var(--md-sys-color-on-surface);
  background: var(--md-sys-color-surface-container-high);
}
.task-details {
  min-width: 0;
}
.task-heading {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px;
  margin-bottom: 12px;
}
h3 {
  font-size: 14px;
  font-weight: 600;
  overflow-wrap: anywhere;
}
.task-status {
  flex-shrink: 0;
  padding: 3px 7px;
  border-radius: 16px;
  background: var(--md-sys-color-surface-container);
  color: var(--md-sys-color-on-surface-variant);
  font-size: 11px;
}
.status-completed {
  color: var(--md-sys-color-secondary);
  background: color-mix(in srgb, var(--md-sys-color-secondary) 10%, var(--md-sys-color-surface));
}
.status-processing {
  color: var(--md-sys-color-on-surface);
  background: var(--md-sys-color-surface-container-high);
}
.status-failed {
  color: #a43b3b;
  background: #fbeaea;
}
dl {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(0, 0.8fr);
  gap: 9px 20px;
  font-size: 12px;
}
dl > div {
  display: flex;
  align-items: baseline;
  gap: 10px;
  min-width: 0;
}
dt {
  color: var(--md-sys-color-on-surface-variant);
  flex-shrink: 0;
}
dd {
  overflow-wrap: anywhere;
}
.task-meta {
  display: flex;
  flex-wrap: wrap;
  gap: 12px;
  font-size: 11px;
  color: var(--md-sys-color-on-surface-variant);
  margin-top: 12px;
}
.task-error,
.expires-soon {
  color: #b64848;
}
.task-error {
  margin-top: 8px;
  font-size: 12px;
  overflow-wrap: anywhere;
}
.task-actions {
  display: flex;
  gap: 4px;
  align-items: center;
}
.icon-action {
  width: 36px;
  height: 36px;
  display: grid;
  place-items: center;
  color: var(--md-sys-color-on-surface-variant);
  border-radius: 16px;
}
.icon-action:hover:not(:disabled) {
  background: var(--md-sys-color-surface-container);
  color: var(--md-sys-color-on-surface);
}
.download-action {
  display: flex;
  gap: 7px;
  align-items: center;
  min-height: 36px;
  padding: 0 12px;
  border-radius: 16px;
  background: var(--md-sys-color-primary);
  color: var(--md-sys-color-on-primary);
  font-size: 12px;
  margin-left: 8px;
  white-space: nowrap;
}
.download-action:hover:not(:disabled) {
  background: var(--md-sys-color-surface-container-high);
}
button:disabled {
  opacity: 0.4;
  cursor: not-allowed;
}
.retry-image {
  color: var(--md-sys-color-on-surface);
  text-decoration: underline;
}
@media (max-width: 1200px) {
  .image-task-row {
    grid-template-columns: 128px minmax(0, 1fr);
    gap: 18px;
  }
  .task-preview {
    width: 128px;
    height: 112px;
  }
  .task-actions {
    grid-column: 2;
    justify-content: flex-end;
    margin-top: -6px;
  }
}
@media (max-width: 600px) {
  .image-task-row {
    grid-template-columns: 88px minmax(0, 1fr);
    gap: 14px;
    align-items: start;
  }
  .task-preview {
    width: 88px;
    height: 100px;
  }
  dl {
    grid-template-columns: minmax(0, 1fr);
    gap: 6px;
  }
  .task-heading {
    margin-bottom: 8px;
  }
  h3 {
    font-size: 12px;
  }
  .task-actions {
    grid-column: 1 / -1;
    margin: 0;
  }
  .task-meta {
    gap: 6px;
  }
}
</style>
