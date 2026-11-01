<template>
  <div class="interval-row"
       :class="isEmpty ? 'border-red-400 bg-red-50 dark:border-red-500 dark:bg-red-950/20' : 'border-gray-200 bg-white dark:border-dark-500 dark:bg-dark-700'">
    <!-- Fields wrap into as many columns as fit: two on phones, one line in wide dialogs. -->
    <div class="interval-fields">
      <!-- Token mode: context range + prices ($/MTok) -->
      <template v-if="mode === 'token'">
        <label class="interval-field">
          <span class="interval-label">{{ t('admin.channels.form.minTokens') }}</span>
          <input :value="interval.min_tokens" @input="emitField('min_tokens', toInt(($event.target as HTMLInputElement).value))"
            type="number" min="0" class="input text-xs" />
        </label>
        <label class="interval-field">
          <span class="interval-label">{{ t('admin.channels.form.maxTokens') }} <span class="interval-unit">{{ t('admin.channels.form.inclusive') }}</span></span>
          <input :value="interval.max_tokens ?? ''" @input="emitField('max_tokens', toIntOrNull(($event.target as HTMLInputElement).value))"
            type="number" min="0" class="input text-xs" :placeholder="'∞'" />
        </label>
        <label class="interval-field">
          <span class="interval-label">{{ t('admin.channels.form.inputPrice') }} <span v-if="isEmpty" class="text-red-500">*</span> <span class="interval-unit">$/M</span></span>
          <input :value="interval.input_price" @input="emitField('input_price', ($event.target as HTMLInputElement).value)"
            type="number" step="any" min="0" class="input text-xs" />
        </label>
        <label class="interval-field">
          <span class="interval-label">{{ t('admin.channels.form.outputPrice') }} <span v-if="isEmpty" class="text-red-500">*</span> <span class="interval-unit">$/M</span></span>
          <input :value="interval.output_price" @input="emitField('output_price', ($event.target as HTMLInputElement).value)"
            type="number" step="any" min="0" class="input text-xs" />
        </label>
        <label class="interval-field">
          <span class="interval-label">{{ t('admin.channels.form.cacheWritePriceShort') }} <span class="interval-unit">$/M</span></span>
          <input :value="interval.cache_write_price" @input="emitField('cache_write_price', ($event.target as HTMLInputElement).value)"
            type="number" step="any" min="0" class="input text-xs" />
        </label>
        <label class="interval-field">
          <span class="interval-label">{{ t('admin.channels.form.cacheReadPriceShort') }} <span class="interval-unit">$/M</span></span>
          <input :value="interval.cache_read_price" @input="emitField('cache_read_price', ($event.target as HTMLInputElement).value)"
            type="number" step="any" min="0" class="input text-xs" />
        </label>
      </template>

      <!-- Per-request / Image mode: tier label + context range + price -->
      <template v-else>
        <label class="interval-field">
          <span class="interval-label">
            {{ mode === 'image' || mode === 'video' ? t('admin.channels.form.resolution') : t('admin.channels.form.tierLabel') }}
          </span>
          <input :value="interval.tier_label" @input="emitField('tier_label', ($event.target as HTMLInputElement).value)"
            type="text" class="input text-xs" :placeholder="mode === 'image' ? '1K / 2K / 4K' : mode === 'video' ? '480p / 720p / 1080p / 4k' : ''" />
        </label>
        <label class="interval-field">
          <span class="interval-label">{{ t('admin.channels.form.minTokens') }}</span>
          <input :value="interval.min_tokens" @input="emitField('min_tokens', toInt(($event.target as HTMLInputElement).value))"
            type="number" min="0" class="input text-xs" />
        </label>
        <label class="interval-field">
          <span class="interval-label">{{ t('admin.channels.form.maxTokens') }} <span class="interval-unit">{{ t('admin.channels.form.inclusive') }}</span></span>
          <input :value="interval.max_tokens ?? ''" @input="emitField('max_tokens', toIntOrNull(($event.target as HTMLInputElement).value))"
            type="number" min="0" class="input text-xs" :placeholder="'∞'" />
        </label>
        <label class="interval-field">
          <span class="interval-label">{{ t('admin.channels.form.perRequestPrice') }} <span v-if="isEmpty" class="text-red-500">*</span> <span class="interval-unit">$</span></span>
          <input :value="interval.per_request_price" @input="emitField('per_request_price', ($event.target as HTMLInputElement).value)"
            type="number" step="any" min="0" class="input text-xs" />
        </label>
      </template>
    </div>

    <button type="button" @click="emit('remove')" class="interval-remove" :title="t('common.delete')" :aria-label="t('common.delete')">
      <Icon name="x" size="sm" />
    </button>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import type { IntervalFormEntry } from './types'
import type { BillingMode } from '@/api/admin/channels'

const { t } = useI18n()

const props = defineProps<{
  interval: IntervalFormEntry
  mode: BillingMode
}>()

const emit = defineEmits<{
  update: [interval: IntervalFormEntry]
  remove: []
}>()

// 检测所有价格字段是否都为空
const isEmpty = computed(() => {
  const iv = props.interval
  return (iv.input_price == null || iv.input_price === '') &&
    (iv.output_price == null || iv.output_price === '') &&
    (iv.cache_write_price == null || iv.cache_write_price === '') &&
    (iv.cache_read_price == null || iv.cache_read_price === '') &&
    (iv.per_request_price == null || iv.per_request_price === '')
})

function emitField(field: keyof IntervalFormEntry, value: string | number | null) {
  emit('update', { ...props.interval, [field]: value === '' ? null : value })
}

function toInt(val: string): number {
  const n = parseInt(val, 10)
  return isNaN(n) ? 0 : n
}

function toIntOrNull(val: string): number | null {
  if (val === '') return null
  const n = parseInt(val, 10)
  return isNaN(n) ? null : n
}
</script>

<style scoped>
.interval-row {
  @apply relative rounded-lg border p-2.5 pr-10;
}

.interval-fields {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(6.5rem, 1fr));
  gap: 0.5rem;
}

/* Labels of neighbouring fields can wrap to different heights; keep every
   input on the bottom line so a row of inputs stays aligned. */
.interval-field {
  @apply flex min-w-0 flex-col justify-end gap-0.5;
}

.interval-label {
  @apply text-xs leading-4 text-gray-400;
}

.interval-unit {
  @apply whitespace-nowrap text-gray-300 dark:text-gray-500;
}

.interval-remove {
  @apply absolute right-1 top-1 rounded-lg p-2 text-gray-400 transition-colors hover:bg-red-50 hover:text-red-500 dark:hover:bg-red-900/20;
}
</style>
