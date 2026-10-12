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
import { useI18n } from 'vue-i18n'
const model = defineModel<string>({ default: '' })
const { t } = useI18n()
</script>
