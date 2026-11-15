<template>
  <div>
    <label class="input-label">{{ t('videoWorkbench.protocolSelect') }}</label>
    <Select
      id="account-media-protocol"
      v-model="model"
      :options="options"
      :searchable="false"
      :aria-label="t('videoWorkbench.protocolSelect')"
      aria-describedby="account-media-protocol-hint"
    />
    <p id="account-media-protocol-hint" class="input-hint">{{ t('videoWorkbench.protocolHint') }}</p>
  </div>
</template>
<script setup lang="ts">
import { computed, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Select from '@/components/common/Select.vue'
const model = defineModel<string>({ default: '' })
const { t } = useI18n()
const options = computed(() => [
  { value: '', label: t('videoWorkbench.protocolCompatible') },
  { value: 'sora', label: 'OpenAI Sora' },
  { value: '8yes', label: '8yes' }
])
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
