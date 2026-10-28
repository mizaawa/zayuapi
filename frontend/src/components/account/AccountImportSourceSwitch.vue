<template>
  <div data-tour="account-form-type">
    <p :id="labelId" class="input-label">{{ t('admin.accounts.accountType') }}</p>
    <div class="account-source-switch" role="radiogroup" :aria-labelledby="labelId">
      <label
        v-for="option in options"
        :key="option.value"
        class="account-source-option"
        :class="{ 'is-active': modelValue === option.value, 'is-disabled': disabled }"
        :data-testid="option.testId"
      >
        <input
          type="radio"
          class="sr-only"
          :name="groupName"
          :value="option.value"
          :checked="modelValue === option.value"
          :disabled="disabled"
          @change="emit('update:modelValue', option.value)"
        />
        <Icon :name="option.icon" size="sm" />
        <span>{{ option.label }}</span>
      </label>
    </div>
    <p class="input-hint">
      {{ modelValue === 'provider' ? t('admin.providerImport.sourceProviderHint') : t('admin.providerImport.sourceApiHint') }}
    </p>
  </div>
</template>

<script setup lang="ts">
import { computed, useId } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import type { AccountImportSource } from './providerImport'
import '@/styles/provider-import.css'

withDefaults(defineProps<{
  modelValue: AccountImportSource
  disabled?: boolean
}>(), {
  disabled: false
})

const emit = defineEmits<{
  'update:modelValue': [value: AccountImportSource]
}>()

const { t } = useI18n()
const id = useId()
const labelId = `account-source-label-${id}`
const groupName = `account-source-${id}`

// Both sources create API-key accounts; a provider import only differs in
// where the server reads the key from.
const options = computed(() => [
  { value: 'api' as const, label: 'API Key', icon: 'key' as const, testId: 'account-type-api-key' },
  { value: 'provider' as const, label: t('admin.providerImport.sourceProvider'), icon: 'server' as const, testId: 'account-type-provider' }
])
</script>
