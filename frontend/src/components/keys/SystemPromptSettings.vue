<template>
  <div class="space-y-3" data-test="system-prompt-settings">
    <div class="flex items-center justify-between gap-3">
      <label :for="`${id}-enabled`" class="input-label mb-0">{{ t('keys.customSystemPrompt.enable') }}</label>
      <button
        :id="`${id}-enabled`"
        type="button"
        role="switch"
        :aria-checked="modelValue.custom_system_prompt_enabled"
        :aria-label="t('keys.customSystemPrompt.enable')"
        :aria-controls="`${id}-controls`"
        :disabled="disabled"
        data-test="system-prompt-toggle"
        :class="[
          'relative inline-flex h-5 w-9 flex-shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors focus:outline-none focus-visible:ring-2 focus-visible:ring-primary-500 disabled:cursor-not-allowed disabled:opacity-50',
          modelValue.custom_system_prompt_enabled ? 'bg-primary-600' : 'bg-gray-200 dark:bg-dark-600'
        ]"
        @click="toggleEnabled"
      >
        <span
          :class="[
            'pointer-events-none inline-block h-4 w-4 rounded-full bg-white shadow transition-transform',
            modelValue.custom_system_prompt_enabled ? 'translate-x-4' : 'translate-x-0'
          ]"
        />
      </button>
    </div>

    <div
      v-if="modelValue.custom_system_prompt_enabled"
      :id="`${id}-controls`"
      class="system-prompt-controls space-y-4 rounded-lg border p-4 shadow-sm"
      data-test="system-prompt-controls"
    >
      <p :id="`${id}-hint`" class="system-prompt-hint text-xs leading-5">
        {{ t('keys.customSystemPrompt.hint') }}
      </p>
      <div class="flex items-center justify-between gap-3">
        <label :for="`${id}-force`" class="input-label mb-0">{{ t('keys.customSystemPrompt.force') }}</label>
        <button
          :id="`${id}-force`"
          type="button"
          role="switch"
          :aria-checked="modelValue.custom_system_prompt_force"
          :aria-label="t('keys.customSystemPrompt.force')"
          :aria-describedby="`${id}-hint`"
          :disabled="disabled"
          data-test="system-prompt-force-toggle"
          :class="[
            'relative inline-flex h-5 w-9 flex-shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors focus:outline-none focus-visible:ring-2 focus-visible:ring-primary-500 disabled:cursor-not-allowed disabled:opacity-50',
            modelValue.custom_system_prompt_force ? 'bg-primary-600' : 'bg-gray-200 dark:bg-dark-600'
          ]"
          @click="update({ custom_system_prompt_force: !modelValue.custom_system_prompt_force })"
        >
          <span
            :class="[
              'pointer-events-none inline-block h-4 w-4 rounded-full bg-white shadow transition-transform',
              modelValue.custom_system_prompt_force ? 'translate-x-4' : 'translate-x-0'
            ]"
          />
        </button>
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
    </div>
  </div>
</template>

<script setup lang="ts">
import { nextTick, ref } from 'vue'
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

const toggleEnabled = async () => {
  const enabled = !props.modelValue.custom_system_prompt_enabled
  update({ custom_system_prompt_enabled: enabled })
  if (enabled) {
    await nextTick()
    promptInput.value?.focus()
  }
}
</script>

<style scoped>
.system-prompt-controls {
  border-color: color-mix(in srgb, var(--md-sys-color-outline) 65%, var(--md-sys-color-surface));
  background: var(--md-sys-color-surface-container);
  color: var(--md-sys-color-on-surface);
}

.system-prompt-hint {
  color: var(--md-sys-color-on-surface-variant);
}

.system-prompt-controls .input-label {
  color: var(--md-sys-color-on-surface);
}

.system-prompt-controls .input {
  background: var(--md-sys-color-surface);
}
</style>
