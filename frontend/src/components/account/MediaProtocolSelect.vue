<template>
  <div>
    <label class="input-label" for="account-media-protocol">{{ t('videoWorkbench.protocol') }}</label>
    <select id="account-media-protocol" v-model="model" class="input">
      <option value="">{{ t('videoWorkbench.protocolCompatible') }}</option>
      <option value="sora">OpenAI Sora</option>
      <option value="8yes">8yes</option>
    </select>
    <p class="input-hint">{{ t('videoWorkbench.protocolHint') }}</p>
  </div>
</template>
<script setup lang="ts">
import { watch } from 'vue'
import { useI18n } from 'vue-i18n'
const model = defineModel<string>({ default: '' })
const { t } = useI18n()
// The retired OpenAI video protocol behaves exactly like the default one, so an
// account still storing it shows the default option instead of a blank select.
watch(
  model,
  (value) => {
    if (value === 'openai') model.value = ''
  },
  { immediate: true, flush: 'sync' }
)
</script>
