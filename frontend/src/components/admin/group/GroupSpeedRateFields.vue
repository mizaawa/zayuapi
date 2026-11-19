<template>
  <div class="mt-4 border-t border-primary-200/70 pt-4 dark:border-primary-800/40">
    <div class="grid gap-3 sm:grid-cols-2">
      <label class="block">
        <span class="input-label">{{ t('admin.groups.fastRate.multiplier') }}</span>
        <input v-model.number="fast" type="number" min="0.1" max="100" step="0.0001" required class="input" :data-testid="`${prefix}-fast-rate-multiplier`" />
      </label>
      <label class="block">
        <span class="input-label">{{ t('admin.groups.fastRate.ultraMultiplier') }}</span>
        <input v-model.number="ultra" type="number" min="0.1" max="100" step="0.0001" required class="input" :data-testid="`${prefix}-ultrafast-rate-multiplier`" />
      </label>
    </div>
    <p class="input-hint">{{ t('admin.groups.fastRate.hint') }}</p>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'

defineProps<{ prefix: 'create' | 'edit' }>()
const fast = defineModel<number | ''>('fast', { required: true })
const ultra = defineModel<number | ''>('ultra', { required: true })
const { t } = useI18n()
</script>
