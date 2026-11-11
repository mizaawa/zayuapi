<template>
  <td v-if="!pricing" colspan="3" class="border-l border-gray-100 px-3 py-2.5 text-xs text-gray-400 dark:border-dark-700/60 dark:text-dark-500">
    -
  </td>
  <template v-else-if="mode === 'token'">
    <td v-for="field in tokenFields" :key="field" class="border-l border-gray-100 px-3 py-2.5 align-middle font-mono text-xs text-gray-500 dark:border-dark-700/60 dark:text-dark-400">
      <template v-if="intervals.length">
        <div v-for="(interval, index) in intervals" :key="index" class="pz-token-tier">
          <span class="font-sans text-gray-400 dark:text-dark-500">{{ pricingTierLabel(interval) }}</span>
          {{ amount(interval[field], 1_000_000) }}
        </div>
      </template>
      <template v-else>{{ amount(pricing[field], 1_000_000) }}</template>
      <div v-if="imageTokenPrice(field) != null" class="mt-1">
        <span class="font-sans text-gray-400 dark:text-dark-500">{{ t('modelPlaza.table.imageTokens') }}</span>
        {{ amount(imageTokenPrice(field), 1_000_000) }}
      </div>
    </td>
    <td class="px-3 py-2.5 align-middle font-mono text-xs text-gray-500 dark:text-dark-400">
      <template v-if="hasCache">
        <div v-for="(interval, index) in cacheRows" :key="index" :class="{ 'pz-token-tier': intervals.length }">
          <span v-if="intervals.length" class="block font-sans text-gray-400 dark:text-dark-500">{{ pricingTierLabel(interval) }}</span>
          <div>{{ t('modelPlaza.table.cacheWrite') }} {{ amount(interval.cache_write_price, 1_000_000) }}</div>
          <div>{{ t('modelPlaza.table.cacheRead') }} {{ amount(interval.cache_read_price, 1_000_000) }}</div>
        </div>
      </template>
      <span v-else>-</span>
    </td>
  </template>
  <td v-else colspan="3" class="border-l border-gray-100 px-3 py-2.5 align-middle font-mono text-xs text-gray-500 dark:border-dark-700/60 dark:text-dark-400">
    <div v-if="pricing.per_request_price != null">
      <span v-if="pricing.intervals?.length" class="font-sans">{{ t('modelPlaza.table.defaultPrice') }} </span>
      {{ amount(pricing.per_request_price, 1) }} {{ unit }}
    </div>
    <div v-for="(interval, index) in pricing.intervals ?? []" :key="index">
      <span class="font-sans">{{ pricingTierLabel(interval) }} </span>
      {{ amount(interval.per_request_price ?? pricing.per_request_price, 1) }} {{ unit }}
    </div>
    <span v-if="pricing.per_request_price == null && !pricing.intervals?.length">-</span>
  </td>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { PlazaOfficialPricing } from '@/api/modelPlaza'
import { formatScaled } from '@/utils/pricing'
import { pricingTierLabel, tokenPricingIntervals } from './plazaPricing'

const props = defineProps<{
  pricing: PlazaOfficialPricing | null
  currency: { symbol: string }
}>()
const { t } = useI18n()
const tokenFields = ['input_price', 'output_price'] as const
const mode = computed(() => props.pricing?.billing_mode || 'token')
const intervals = computed(() => tokenPricingIntervals(props.pricing))
const cacheRows = computed(() => intervals.value.length ? intervals.value : [{
  min_tokens: 0, max_tokens: null,
  cache_write_price: props.pricing?.cache_write_price,
  cache_read_price: props.pricing?.cache_read_price
}])
const hasCache = computed(() => cacheRows.value.some((row) => row.cache_write_price != null || row.cache_read_price != null))
const unit = computed(() => t(`modelPlaza.table.${mode.value === 'image' ? 'perUnitImage' : mode.value === 'video' ? 'perUnitSecond' : 'perUnitRequest'}`))

function amount(value: number | null | undefined, scale: number): string {
  return value == null ? '-' : formatScaled(value, scale, 2, props.currency.symbol)
}

function imageTokenPrice(field: typeof tokenFields[number]): number | null | undefined {
  return field === 'input_price' ? props.pricing?.image_input_price : props.pricing?.image_output_price
}
</script>

<style scoped>
.pz-token-tier { min-height: 3rem; }
.pz-token-tier + .pz-token-tier { margin-top: 0.375rem; }
</style>
