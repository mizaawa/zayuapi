<template>
  <KeyFormSection
    :id="id" icon="chatBubble" :title="t('keys.customSystemPrompt.enable')" :hint="t('keys.editor.promptHint')"
    :enabled="modelValue.custom_system_prompt_enabled" :disabled="disabled" toggle-test="system-prompt-toggle"
    data-test="system-prompt-settings" @update:enabled="toggleEnabled"
  >
    <div class="system-prompt-controls space-y-4" data-test="system-prompt-controls">
      <div class="flex items-center justify-between gap-3">
        <label :for="`${id}-force`" class="input-label mb-0">{{ t('keys.customSystemPrompt.force') }}</label>
        <Toggle
          :id="`${id}-force`" :model-value="modelValue.custom_system_prompt_force" :disabled="disabled"
          :aria-label="t('keys.customSystemPrompt.force')" :aria-describedby="`${id}-hint`" data-test="system-prompt-force-toggle"
          @update:model-value="update({ custom_system_prompt_force: $event })"
        />
      </div>
      <div>
        <label :for="`${id}-prompt`" class="input-label">{{ t('keys.customSystemPrompt.label') }}</label>
        <textarea
          :id="`${id}-prompt`"
          ref="promptInput"
          :value="modelValue.custom_system_prompt"
          :disabled="disabled"
          :placeholder="t('keys.customSystemPrompt.placeholder')"
          :aria-describedby="`${id}-hint`"
          rows="5"
          maxlength="32768"
          required
          class="input min-h-32 resize-y"
          data-test="system-prompt-input"
          @input="update({ custom_system_prompt: ($event.target as HTMLTextAreaElement).value })"
        />
      </div>
      <p :id="`${id}-hint`" class="key-editor-note text-xs leading-5">
        <Icon name="infoCircle" size="sm" aria-hidden="true" />{{ t('keys.customSystemPrompt.hint') }}
      </p>
    </div>
  </KeyFormSection>
</template>

<script setup lang="ts">
import { nextTick, ref } from 'vue'
import KeyFormSection from './KeyFormSection.vue'
import Toggle from '@/components/common/Toggle.vue'
import Icon from '@/components/icons/Icon.vue'
import { useI18n } from 'vue-i18n'
import type { ApiKeySystemPromptSettings } from '@/types'

const props = withDefaults(defineProps<{
  id: string
  modelValue: ApiKeySystemPromptSettings
  disabled?: boolean
}>(), { disabled: false })
const emit = defineEmits<{
  'update:modelValue': [settings: ApiKeySystemPromptSettings]
}>()
const { t } = useI18n()
const promptInput = ref<HTMLTextAreaElement | null>(null)

const update = (changes: Partial<ApiKeySystemPromptSettings>) => {
  emit('update:modelValue', { ...props.modelValue, ...changes })
}

const toggleEnabled = async (enabled: boolean) => {
  update({ custom_system_prompt_enabled: enabled })
  if (enabled) {
    await nextTick()
    promptInput.value?.focus()
  }
}
</script>
