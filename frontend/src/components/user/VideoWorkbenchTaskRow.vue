<template>
  <article class="video-task-row" :data-task-id="task.id">
    <div class="task-preview" :class="`preview-${task.status}`">
      <Icon :name="task.status === 'processing' ? 'refresh' : task.status === 'queued' ? 'clock' : task.status === 'failed' ? 'exclamationTriangle' : 'play'" size="lg" :class="{ 'animate-spin': task.status === 'processing' }" />
      <span>{{ t(`videoWorkbench.${task.status}`) }}</span><small v-if="task.size_bytes">{{ formatBytes(task.size_bytes) }}</small>
    </div>
    <div class="task-details">
      <div class="task-heading"><h3 :title="task.workbench.prompt">{{ task.workbench.prompt }}</h3><span class="task-status" :class="`status-${task.status}`">{{ t(`videoWorkbench.${task.status}`) }}</span></div>
      <dl>
        <div><dt>{{ t('videoWorkbench.createdAt') }}</dt><dd>{{ date }}</dd></div>
        <div><dt>{{ t('videoWorkbench.elapsed') }}</dt><dd>{{ elapsed }}</dd></div>
        <div><dt>{{ t('videoWorkbench.model') }}</dt><dd :title="task.workbench.model">{{ task.workbench.model }}</dd></div>
        <div><dt>{{ t('videoWorkbench.remaining') }}</dt><dd>{{ remaining }}</dd></div>
      </dl>
      <p class="task-meta"><span v-if="keyName">{{ keyName }}</span><span>{{ task.workbench.ratio }}</span><span>{{ t('videoWorkbench.seconds', { count: task.workbench.duration }) }}</span></p>
      <p v-if="task.status === 'failed'" class="task-error" role="alert">{{ errorMessage }}</p>
    </div>
    <div class="task-actions">
      <button type="button" class="icon-action" :title="t('videoWorkbench.copyPrompt')" :aria-label="t('videoWorkbench.copyPrompt')" @click="copyToClipboard(task.workbench.prompt)"><Icon name="copy" size="sm" /></button>
      <button v-if="task.status !== 'processing'" type="button" class="icon-action" :title="t('videoWorkbench.delete')" :aria-label="t('videoWorkbench.delete')" @click="$emit('delete')"><Icon name="trash" size="sm" /></button>
      <button type="button" class="download-action" :disabled="task.status !== 'completed' || task.expires_at <= now || downloading" @click="$emit('download')"><Icon :name="downloading ? 'refresh' : 'download'" size="sm" :class="{ 'animate-spin': downloading }" />{{ t('videoWorkbench.download') }}</button>
    </div>
  </article>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import { useClipboard } from '@/composables/useClipboard'
import { formatBytes } from '@/utils/format'
import type { VideoTask } from '@/api/videoWorkbench'
const props = defineProps<{ task: VideoTask; now: number; keyName?: string; downloading: boolean }>()
defineEmits<{ download: []; delete: [] }>()
const { t, locale } = useI18n()
const { copyToClipboard } = useClipboard()
const date = computed(() => new Date(props.task.created_at * 1000).toLocaleString(locale.value === 'zh' ? 'zh-CN' : 'en-US', { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' }))
const elapsed = computed(() => props.task.started_at ? t('videoWorkbench.seconds', { count: Math.max(0, (props.task.completed_at || props.now) - props.task.started_at) }) : '-')
const remaining = computed(() => ['completed', 'failed'].includes(props.task.status) ? t('videoWorkbench.remainingMinutes', { count: Math.max(0, Math.ceil((props.task.expires_at - props.now) / 60)) }) : '-')
const errorMessage = computed(() => {
  const codes = ['generation_failed', 'request_rejected', 'model_unavailable', 'storage_failed', 'authorization_failed', 'interrupted', 'timeout']
  return t(`videoWorkbench.errors.${codes.includes(props.task.error_code || '') ? props.task.error_code : 'generation_failed'}`)
})
</script>

<style scoped>
.video-task-row { display: grid; grid-template-columns: 126px minmax(0,1fr) auto; align-items: center; gap: 22px; padding: 22px 0; border-bottom: 1px solid var(--md-sys-color-outline-variant); }
.task-preview { width: 126px; height: 94px; display: flex; flex-direction: column; align-items: center; justify-content: center; gap: 7px; border-radius: 8px; background: var(--md-sys-color-surface-container); color: var(--md-sys-color-on-surface-variant); font-size: 11px; }
.preview-completed { color: #23765c; background: color-mix(in srgb,#23765c 8%,var(--md-sys-color-surface)); } .preview-failed { color: var(--md-sys-color-error,#b3261e); }
.task-details { min-width: 0; } .task-heading { display: flex; align-items: flex-start; gap: 12px; margin-bottom: 12px; } h3 { min-width: 0; flex: 1; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-size: 14px; font-weight: 600; }
.task-status { flex-shrink: 0; padding: 2px 7px; border-radius: 6px; font-size: 10px; background: var(--md-sys-color-surface-container); } .status-completed { color: #23765c; } .status-failed,.task-error { color: var(--md-sys-color-error,#b3261e); } .status-processing { color: #257e9b; }
dl { display: grid; grid-template-columns: minmax(0,1fr) minmax(0,1fr); gap: 7px 18px; font-size: 11px; } dl > div { display: flex; gap: 8px; min-width: 0; } dt { flex-shrink: 0; color: var(--md-sys-color-on-surface-variant); } dd { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.task-meta { display: flex; flex-wrap: wrap; gap: 12px; margin-top: 10px; color: var(--md-sys-color-on-surface-variant); font-size: 10px; overflow-wrap: anywhere; } .task-error { margin-top: 10px; font-size: 12px; line-height: 1.6; }
.task-actions { display: flex; align-items: center; gap: 8px; } .icon-action { display: grid; place-items: center; width: 32px; height: 32px; border-radius: 8px; color: var(--md-sys-color-on-surface-variant); } .icon-action:hover { background: var(--md-sys-color-surface-container); }
.download-action { display: flex; align-items: center; justify-content: center; gap: 7px; padding: 0 15px; height: 36px; border-radius: 18px; background: var(--md-sys-color-surface-container-high); font-size: 12px; } .download-action:disabled { opacity: .4; cursor: not-allowed; }
@media (max-width: 1100px) { .video-task-row { grid-template-columns: 105px minmax(0,1fr); gap: 14px; } .task-preview { width: 105px; height: 90px; } .task-actions { grid-column: 2; justify-content: flex-end; } }
@media (max-width: 600px) { .video-task-row { grid-template-columns: 72px minmax(0,1fr); gap: 12px; padding: 18px 0; align-items: start; } .task-preview { width: 72px; height: 80px; } .task-heading { flex-wrap: wrap; gap: 6px; } h3 { font-size: 12px; flex-basis: 100%; } dl { grid-template-columns: minmax(0,1fr); gap: 6px; } .task-actions { grid-column: 1 / -1; } .task-preview small { font-size: 9px; } }
</style>
