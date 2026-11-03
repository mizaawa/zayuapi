<template>
  <BaseDialog
    :show="show"
    :title="editing ? t('admin.degradation.preset.editTitle') : t('admin.degradation.preset.createTitle')"
    width="normal"
    @close="emit('close')"
  >
    <form id="degradation-prompt-form" class="space-y-4" @submit.prevent="submit">
      <div>
        <label class="input-label" for="degradation-prompt-name">{{ t('admin.degradation.preset.name') }}</label>
        <input
          id="degradation-prompt-name"
          v-model="name"
          type="text"
          maxlength="100"
          class="input"
          :placeholder="t('admin.degradation.preset.namePlaceholder')"
        />
      </div>
      <div>
        <label class="input-label" for="degradation-prompt-text">{{ t('admin.degradation.preset.prompt') }} <span class="text-red-500">*</span></label>
        <textarea
          id="degradation-prompt-text"
          v-model="text"
          rows="9"
          class="input min-h-40 resize-y leading-relaxed"
          :placeholder="t('admin.degradation.preset.promptPlaceholder')"
          data-testid="degradation-prompt-text"
        ></textarea>
        <p class="input-hint text-right" :class="{ 'text-red-600': tooLong }">{{ t('admin.degradation.preset.bytes', { count: bytes }) }}</p>
      </div>
    </form>

    <template #footer>
      <div class="flex flex-wrap items-center justify-between gap-3">
        <button
          v-if="editing"
          type="button"
          class="btn btn-danger"
          :disabled="deleting || submitting"
          data-testid="degradation-prompt-delete"
          @click="showDeleteConfirm = true"
        >
          <Icon name="trash" size="sm" />
          {{ t('common.delete') }}
        </button>
        <span v-else></span>
        <div class="flex gap-3">
          <button type="button" class="btn btn-secondary" @click="emit('close')">{{ t('common.cancel') }}</button>
          <button type="submit" form="degradation-prompt-form" class="btn btn-primary" :disabled="submitting || tooLong" data-testid="degradation-prompt-submit">
            {{ submitting ? t('common.submitting') : t('common.confirm') }}
          </button>
        </div>
      </div>
    </template>
  </BaseDialog>

  <ConfirmDialog
    :show="showDeleteConfirm"
    :title="t('admin.degradation.preset.deleteTitle')"
    :message="t('admin.degradation.preset.deleteConfirm', { name: prompt?.name ?? '' })"
    :confirm-text="t('common.delete')"
    :loading="deleting"
    danger
    @confirm="remove"
    @cancel="showDeleteConfirm = false"
  />
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import type { DegradationPrompt } from '@/api/admin/degradation'
import BaseDialog from '@/components/common/BaseDialog.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import { useAppStore } from '@/stores/app'
import { extractApiErrorMessage } from '@/utils/apiError'

const PROMPT_BYTE_LIMIT = 16000

const props = defineProps<{
  show: boolean
  prompt: DegradationPrompt | null
}>()

const emit = defineEmits<{
  (e: 'close'): void
  (e: 'saved', prompt: DegradationPrompt): void
  (e: 'deleted', id: number): void
}>()

const { t } = useI18n()
const appStore = useAppStore()
const name = ref('')
const text = ref('')
const submitting = ref(false)
const deleting = ref(false)
const showDeleteConfirm = ref(false)

const editing = computed(() => props.prompt !== null)
const bytes = computed(() => new TextEncoder().encode(text.value.trim()).length)
const tooLong = computed(() => bytes.value > PROMPT_BYTE_LIMIT)

watch(() => [props.show, props.prompt] as const, ([show, prompt]) => {
  if (!show) return
  name.value = prompt?.name ?? ''
  text.value = prompt?.prompt ?? ''
  showDeleteConfirm.value = false
}, { immediate: true })

async function submit() {
  if (submitting.value) return
  if (!text.value.trim()) {
    appStore.showError(t('admin.degradation.preset.required'))
    return
  }
  if (tooLong.value) {
    appStore.showError(t('admin.degradation.preset.tooLong'))
    return
  }
  submitting.value = true
  try {
    const payload = { name: name.value.trim(), prompt: text.value.trim() }
    const saved = props.prompt
      ? await adminAPI.degradation.updatePrompt(props.prompt.id, payload)
      : await adminAPI.degradation.createPrompt(payload)
    appStore.showSuccess(props.prompt ? t('admin.degradation.preset.updated') : t('admin.degradation.preset.created'))
    emit('saved', saved)
    emit('close')
  } catch (err: unknown) {
    appStore.showError(extractApiErrorMessage(err, t('admin.degradation.preset.failed')))
  } finally {
    submitting.value = false
  }
}

async function remove() {
  if (!props.prompt || deleting.value) return
  deleting.value = true
  const id = props.prompt.id
  try {
    await adminAPI.degradation.deletePrompt(id)
    appStore.showSuccess(t('admin.degradation.preset.deleted'))
    showDeleteConfirm.value = false
    emit('deleted', id)
    emit('close')
  } catch (err: unknown) {
    appStore.showError(extractApiErrorMessage(err, t('admin.degradation.preset.deleteFailed')))
  } finally {
    deleting.value = false
  }
}
</script>
