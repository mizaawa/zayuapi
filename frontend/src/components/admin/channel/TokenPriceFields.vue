<template>
  <div class="grid min-w-0 grid-cols-2 gap-2 lg:grid-cols-3">
    <label v-for="(label, key) in tokenFields" :key="key" class="min-w-0 text-xs text-gray-500 dark:text-gray-400">
      {{ t(`admin.channels.form.${label}`) }}
      <input :value="value[key]" :data-price="key" type="number" step="any" min="0" class="input mt-0.5 w-full min-w-0 text-sm"
        :placeholder="t('admin.channels.form.pricePlaceholder')" @input="emit('change', key, ($event.target as HTMLInputElement).value || null)" />
    </label>
  </div>
</template>
<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { tokenFields, type TokenField, type TokenPrices } from '@/utils/tokenPriceWindows'
defineProps<{ value: TokenPrices<string | number | null> }>()
const emit = defineEmits<{ change: [field: TokenField, value: string | null] }>()
const { t } = useI18n()
</script>
