<template>
  <div class="plaza-pricing-table overflow-x-auto">
    <table class="w-full min-w-[860px] table-fixed border-collapse text-sm tabular-nums">
      <colgroup>
        <col class="w-[22%]" />
        <col class="w-[10%]" />
        <col class="w-[10%]" />
        <col class="w-[14%]" />
        <col class="w-[10%]" />
        <col class="w-[10%]" />
        <col class="w-[14%]" />
        <col class="w-[10%]" />
      </colgroup>
      <thead>
        <tr
          class="text-xs font-semibold uppercase tracking-wider text-gray-500 dark:text-dark-400"
        >
          <th
            rowspan="2"
            class="border-r border-gray-100 py-2.5 pl-5 pr-4 text-left align-middle dark:border-dark-700/60"
          >
            {{ t('modelPlaza.table.model') }}
          </th>
          <th colspan="3" class="pz-bg pt-2 text-center">
            <div class="pz-title border-b pb-2 font-semibold">
              {{ t('modelPlaza.table.paidPrice') }}
              <span v-if="models.some((model) => isTokenMode(billingMode(model)))" class="pz-unit ml-1 normal-case font-normal">{{ t('modelPlaza.table.unitPerMillion') }}</span>
            </div>
          </th>
          <th
            colspan="3"
            class="border-l border-gray-100 pt-2 text-center dark:border-dark-700/60"
          >
            <div class="border-b border-gray-200 pb-2 text-gray-400 dark:border-dark-600 dark:text-dark-500">
              <span :title="t('modelPlaza.table.channelPriceHint')">{{ t('modelPlaza.table.officialPrice') }}</span>
              <span v-if="hasTokenOriginal" class="ml-1 normal-case font-normal text-gray-400 dark:text-dark-500">{{ t('modelPlaza.table.unitPerMillion') }}</span>
            </div>
          </th>
          <th
            rowspan="2"
            class="border-l border-gray-100 py-2.5 pl-3 pr-5 text-right align-middle dark:border-dark-700/60"
          >
            {{ t('modelPlaza.table.rate') }}
          </th>
        </tr>
        <tr
          class="border-b border-gray-200 text-left text-[11px] font-medium uppercase leading-4 tracking-wide text-gray-400 dark:border-dark-700 dark:text-dark-500"
        >
          <th class="pz-bg px-3 py-2 font-medium">{{ t('modelPlaza.table.input') }}</th>
          <th class="pz-bg px-3 py-2 font-medium">{{ t('modelPlaza.table.output') }}</th>
          <th class="pz-bg px-3 py-2 font-medium">{{ t('modelPlaza.table.cache') }}</th>
          <th class="border-l border-gray-100 px-3 py-2 font-medium dark:border-dark-700/60">
            {{ t('modelPlaza.table.input') }}
          </th>
          <th class="px-3 py-2 font-medium">{{ t('modelPlaza.table.output') }}</th>
          <th class="px-3 py-2 font-medium">{{ t('modelPlaza.table.cache') }}</th>
        </tr>
      </thead>
      <tbody>
        <tr
          v-for="m in sortedModels"
          :key="`${m.platform}:${m.name}`"
          :data-paid-heading="priceHeading('paidPrice', isTokenMode(billingMode(m)))"
          :data-original-heading="priceHeading('officialPrice', originalIsToken(m))"
          class="border-b border-gray-100 transition-colors last:border-b-0 hover:bg-gray-50/70 dark:border-dark-800 dark:hover:bg-dark-800/50"
        >
          <!-- 模型名 + 非 token 计费模式徽章 -->
          <td class="pz-model border-r border-gray-100 py-2.5 pl-5 pr-4 align-middle dark:border-dark-700/60">
            <div class="flex flex-wrap items-center gap-1.5">
              <span class="font-medium text-gray-900 dark:text-white">{{ m.name }}</span>
              <span
                v-if="platform && m.platform !== platform"
                class="pz-platform inline-flex items-center gap-1 rounded-md px-1.5 py-0.5 text-[10px] font-medium"
              >
                <PlatformIcon :platform="m.platform as GroupPlatform" size="xs" />
                <span>{{ platformLabel(m.platform) }}</span>
              </span>
              <span
                v-if="billingMode(m) !== BILLING_MODE_TOKEN"
                class="rounded-md bg-gray-100 px-1.5 py-0.5 text-[10px] font-medium text-gray-500 dark:bg-dark-700/70 dark:text-dark-300"
              >
                {{ billingModeLabel(m) }}
              </span>
            </div>
            <p v-if="m.pricing_source === 'group'" class="mt-1 text-[10px] text-gray-400 dark:text-dark-500">
              {{ t('modelPlaza.table.groupPriceSource') }}
            </p>
          </td>

          <!-- token 计费:输入 / 输出(阶梯内联)/ 缓存(写/读) -->
          <template v-if="isTokenMode(billingMode(m))">
            <td :data-col="t('modelPlaza.table.input')" class="pz-cell px-3 py-2.5 align-middle font-mono font-semibold text-gray-900 dark:text-gray-50">
              <template v-if="tokenIntervals(m).length">
                <div
                  v-for="(iv, idx) in tokenIntervals(m)"
                  :key="idx"
                  class="pz-token-tier flex flex-wrap content-start items-baseline gap-x-1 text-xs leading-5"
                >
                  <span class="pz-tier-label font-sans font-normal text-gray-400 dark:text-dark-500">{{ tierLabel(iv) }}</span>
                  <span class="whitespace-nowrap">{{ paidPerMillion(m, iv.input_price) }}</span>
                  <small v-if="iv.image_input_price != null" class="basis-full">{{ t('modelPlaza.table.imageTokens') }} {{ paidPerMillion(m, iv.image_input_price) }}</small>
                </div>
              </template>
              <template v-else>
                {{ paidPerMillion(m, m.pricing?.input_price) }}
                <small v-if="m.pricing?.image_input_price != null" class="block">{{ t('modelPlaza.table.imageTokens') }} {{ paidPerMillion(m, m.pricing.image_input_price) }}</small>
              </template>
            </td>
            <td :data-col="t('modelPlaza.table.output')" class="pz-cell px-3 py-2.5 align-middle font-mono font-semibold text-gray-900 dark:text-gray-50">
              <template v-if="tokenIntervals(m).length">
                <div
                  v-for="(iv, idx) in tokenIntervals(m)"
                  :key="idx"
                  class="pz-token-tier flex flex-wrap content-start items-baseline gap-x-1 text-xs leading-5"
                >
                  <span class="pz-tier-label font-sans font-normal text-gray-400 dark:text-dark-500">{{ tierLabel(iv) }}</span>
                  <span class="whitespace-nowrap">{{ paidPerMillion(m, iv.output_price) }}</span>
                  <small v-if="iv.image_output_price != null" class="basis-full">{{ t('modelPlaza.table.imageTokens') }} {{ paidPerMillion(m, iv.image_output_price) }}</small>
                </div>
              </template>
              <template v-else>
                {{ paidPerMillion(m, m.pricing?.output_price) }}
                <small v-if="m.pricing?.image_output_price != null" class="block">{{ t('modelPlaza.table.imageTokens') }} {{ paidPerMillion(m, m.pricing.image_output_price) }}</small>
              </template>
            </td>
            <td :data-col="t('modelPlaza.table.cache')" class="pz-cell pz-cache px-3 py-2.5 align-middle">
              <div
                v-if="tokenIntervals(m).length && hasCachePricing(m)"
                class="font-mono text-xs text-gray-800 dark:text-gray-200"
              >
                <div v-for="(iv, idx) in tokenIntervals(m)" :key="idx" class="pz-token-tier">
                  <span class="block font-sans font-normal text-gray-400 dark:text-dark-500">{{ tierLabel(iv) }}</span>
                  <div class="flex flex-wrap items-baseline gap-x-1">
                    <span class="font-sans font-normal text-gray-400 dark:text-dark-500">{{ t('modelPlaza.table.cacheWrite') }}</span>
                    <span class="whitespace-nowrap">{{ paidPerMillion(m, iv.cache_write_price) }}</span>
                  </div>
                  <div class="flex flex-wrap items-baseline gap-x-1">
                    <span class="font-sans font-normal text-gray-400 dark:text-dark-500">{{ t('modelPlaza.table.cacheRead') }}</span>
                    <span class="whitespace-nowrap">{{ paidPerMillion(m, iv.cache_read_price) }}</span>
                  </div>
                </div>
              </div>
              <div
                v-else-if="hasCachePricing(m)"
                class="space-y-0.5 font-mono text-xs text-gray-800 dark:text-gray-200"
              >
                <div>
                  <span class="mr-1 font-sans font-normal text-gray-400 dark:text-dark-500">{{ t('modelPlaza.table.cacheWrite') }}</span>
                  {{ paidPerMillion(m, m.pricing?.cache_write_price) }}
                </div>
                <div>
                  <span class="mr-1 font-sans font-normal text-gray-400 dark:text-dark-500">{{ t('modelPlaza.table.cacheRead') }}</span>
                  {{ paidPerMillion(m, m.pricing?.cache_read_price) }}
                </div>
              </div>
              <span v-else class="text-gray-400 dark:text-dark-500">-</span>
            </td>
          </template>

          <!-- Media and per-request prices retain their billing units. -->
          <template v-else>
            <td colspan="3" class="pz-cell px-3 py-2.5 align-middle">
              <div
                v-if="requestIntervals(m).length"
                class="flex flex-wrap items-center gap-1.5"
              >
                <span
                  v-for="(iv, idx) in requestIntervals(m)"
                  :key="idx"
                  class="inline-flex items-center gap-1 rounded-md bg-gray-100 px-2 py-0.5 font-mono text-xs text-gray-800 dark:bg-dark-700/60 dark:text-gray-200"
                >
                  <span class="font-sans text-gray-400 dark:text-dark-500">{{ tierLabel(iv) }}</span>
                  {{ paidRequestPrice(m, iv.per_request_price)
                  }}<span class="font-sans text-gray-400 dark:text-dark-500">{{ perUnitSuffix(m) }}</span>
                </span>
              </div>
              <template v-else-if="m.pricing?.per_request_price != null">
                <span class="font-mono font-semibold text-gray-900 dark:text-gray-50">
                  {{ paidRequestPrice(m, m.pricing.per_request_price) }}
                </span>
                <span class="ml-1 text-xs text-gray-400 dark:text-dark-500">{{ perUnitSuffix(m) }}</span>
              </template>
              <span v-else class="text-gray-400 dark:text-dark-500">-</span>
            </td>
          </template>

          <PlazaOriginalPriceCells :pricing="originalPricing(m)" :currency="displayCurrency(m, true)" />

          <!-- Independent media rates override group and user rates. -->
          <td
            :data-col="t('modelPlaza.table.rate')"
            class="pz-rate border-l border-gray-100 py-2.5 pl-3 pr-5 text-right align-middle font-mono text-xs dark:border-dark-700/60"
          >
            <span
              v-if="usesIndependentMediaRate(m)"
              class="font-bold text-gray-700 dark:text-gray-300"
              >{{ requestRate(m) }}x</span
            >
            <template v-else-if="hasCustomRate">
              <span class="mr-1 text-gray-400 line-through dark:text-dark-500">{{ rateMultiplier }}x</span>
              <span class="font-bold text-primary-600 dark:text-primary-400">{{ effectiveRate }}x</span>
            </template>
            <span v-else class="font-bold text-gray-700 dark:text-gray-300">{{ effectiveRate }}x</span>
          </td>
        </tr>
      </tbody>
    </table>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { formatScaled } from '@/utils/pricing'
import PlatformIcon from '@/components/common/PlatformIcon.vue'
import PlazaOriginalPriceCells from './PlazaOriginalPriceCells.vue'
import { isTokenMode } from '@/constants/channel'
import { pricingTierLabel as tierLabel, tokenPricingIntervals } from './plazaPricing'
import { platformLabel } from '@/utils/platformColors'
import type { GroupPlatform } from '@/types'
import {
  BILLING_MODE_TOKEN,
  BILLING_MODE_IMAGE,
  BILLING_MODE_VIDEO,
  type BillingMode
} from '@/constants/channel'
import type { PlazaModel, PlazaOfficialPricing } from '@/api/modelPlaza'
import type { UserPricingInterval } from '@/api/channels'

const props = defineProps<{
  models: PlazaModel[]
  /** 分组平台;Composite 分组里与分组平台不同的模型会标出各自平台。 */
  platform?: string
  /** 分组默认倍率。 */
  rateMultiplier: number
  /** 用户专属倍率;与默认不同,实付价按此计算并划线展示原倍率。 */
  userRateMultiplier?: number | null
  /** 生图独立倍率:true 时图片计费模型的实付倍率取 imageRateMultiplier,不取分组/专属倍率。 */
  imageRateIndependent?: boolean
  imageRateMultiplier?: number | null
  videoRateIndependent?: boolean
  videoRateMultiplier?: number | null
}>()

const { t } = useI18n()

const PER_MILLION = 1_000_000

/**
 * 展示顺序:
 * 1. token 计费的排在前,按图/按次计费的沉到末尾——不同计费模式的单价量纲不同;
 * 2. 组内按渠道原价输出价从高到低,无渠道原价的排最后;
 * 3. 同价按名称降序(新版本号在前,如 gpt-5.6 先于 gpt-5.5)。
 */
const sortedModels = computed(() => {
  return [...props.models].sort((a, b) => {
    const ta = isTokenMode(billingMode(a))
    const tb = isTokenMode(billingMode(b))
    if (ta !== tb) return ta ? -1 : 1
    const pa = originalPricing(a)?.output_price ?? null
    const pb = originalPricing(b)?.output_price ?? null
    if (pa != null && pb != null && pa !== pb) return pb - pa
    if (pa != null && pb == null) return -1
    if (pa == null && pb != null) return 1
    return b.name.localeCompare(a.name)
  })
})

const effectiveRate = computed(() => props.userRateMultiplier ?? props.rateMultiplier)

/**
 * 渠道原价展示:优先渠道配置的定价;渠道没配时直接展示参考价(全局定价)
 * 本身,不再提示「未配置」。旧响应没有 reference_pricing 时退回实付基价。
 */
function originalPricing(m: PlazaModel): PlazaOfficialPricing | null {
  if (m.official_pricing) return m.official_pricing
  if (m.reference_pricing) return m.reference_pricing
  if (m.pricing_source === 'reference' && m.pricing) return m.pricing
  return null
}

/** 原价区是否有 token 计价的模型,决定表头是否标「/百万 Token」。 */
function originalIsToken(model: PlazaModel): boolean {
  const pricing = originalPricing(model)
  return pricing != null && isTokenMode(pricing.billing_mode || BILLING_MODE_TOKEN)
}
const hasTokenOriginal = computed(() => props.models.some(originalIsToken))

function priceHeading(key: 'paidPrice' | 'officialPrice', token: boolean): string {
  const heading = t(`modelPlaza.table.${key}`)
  return token ? `${heading} ${t('modelPlaza.table.unitPerMillion')}` : heading
}
const hasCustomRate = computed(
  () => props.userRateMultiplier != null && props.userRateMultiplier !== props.rateMultiplier
)

function billingMode(m: PlazaModel): BillingMode {
  return (m.pricing?.billing_mode || BILLING_MODE_TOKEN) as BillingMode
}

function billingModeLabel(m: PlazaModel): string {
  switch (billingMode(m)) {
    case 'token_peak': return t('admin.channels.billingMode.tokenPeak')
    case BILLING_MODE_IMAGE: return t('modelPlaza.table.perImage')
    case BILLING_MODE_VIDEO: return t('modelPlaza.table.perVideoSecond')
    default: return t('modelPlaza.table.perRequest')
  }
}

/** 价格统一保底 2 位小数,更长的有效小数原样保留。 */
const MIN_DECIMALS = 2

function displayCurrency(m: PlazaModel, officialPrice: boolean): { symbol: string } {
  const settings = m.pricing_currency
  const cny = officialPrice ? settings?.official_cny : settings?.discounted_cny
  return { symbol: cny ? '¥' : '$' }
}

/** Price numbers and rate multipliers stay unchanged; only the symbol follows channel settings. */
function paidPerMillion(m: PlazaModel, value: number | null | undefined): string {
  if (value == null) return '-'
  const currency = displayCurrency(m, false)
  return formatScaled(value * effectiveRate.value, PER_MILLION, MIN_DECIMALS, currency.symbol)
}

/** Media rates use the same independent multiplier as billing. */
function usesIndependentMediaRate(m: PlazaModel): boolean {
  return (billingMode(m) === BILLING_MODE_IMAGE && props.imageRateIndependent === true)
    || (billingMode(m) === BILLING_MODE_VIDEO && props.videoRateIndependent === true)
}

/** Effective multiplier for non-token prices. */
function requestRate(m: PlazaModel): number {
  if (billingMode(m) === BILLING_MODE_IMAGE && props.imageRateIndependent) {
    return props.imageRateMultiplier ?? 1
  }
  if (billingMode(m) === BILLING_MODE_VIDEO && props.videoRateIndependent) {
    return props.videoRateMultiplier ?? 1
  }
  return effectiveRate.value
}

/** Preserve each non-token price's unit when applying its multiplier. */
function paidRequestPrice(m: PlazaModel, value: number | null | undefined): string {
  if (value == null) return '-'
  const currency = displayCurrency(m, false)
  return formatScaled(value * requestRate(m), 1, MIN_DECIMALS, currency.symbol)
}

/** Media units must not fall back to the per-request suffix. */
function perUnitSuffix(m: PlazaModel): string {
  switch (billingMode(m)) {
    case BILLING_MODE_IMAGE: return t('modelPlaza.table.perUnitImage')
    case BILLING_MODE_VIDEO: return t('modelPlaza.table.perUnitSecond')
    default: return t('modelPlaza.table.perUnitRequest')
  }
}

function hasCachePricing(m: PlazaModel): boolean {
  const intervals = tokenIntervals(m)
  if (intervals.length) {
    return intervals.some((iv) => iv.cache_write_price != null || iv.cache_read_price != null)
  }
  return m.pricing?.cache_write_price != null || m.pricing?.cache_read_price != null
}

function tokenIntervals(m: PlazaModel): UserPricingInterval[] {
  return tokenPricingIntervals(m.pricing, t('admin.channels.form.fallbackPrices'))
}

/** 按次/按图模式的阶梯定价(仅保留配了按次价的档位)。 */
function requestIntervals(m: PlazaModel): UserPricingInterval[] {
  return (m.pricing?.intervals ?? []).filter((iv) => iv.per_request_price != null)
}

</script>

<style scoped>
/* 实付分区使用主题蜂蜜色,与全站配色一致;平台身份只由模型旁的小图标表达。 */
.plaza-pricing-table {
  --pz-title: #7e5600;
  --pz-bg: rgb(255 243 189 / 0.42);
  --pz-bg-hover: rgb(255 231 138 / 0.42);
}

.pz-bg,
.pz-cell {
  background-color: var(--pz-bg);
}

.pz-cell {
  transition: background-color 150ms cubic-bezier(0.4, 0, 0.2, 1);
}

.pz-token-tier {
  min-height: 3rem;
}

.pz-tier-label {
  max-width: 100%;
  overflow-wrap: anywhere;
}

.pz-token-tier + .pz-token-tier {
  margin-top: 0.375rem;
}

tbody tr:hover .pz-cell {
  background-color: var(--pz-bg-hover);
}

.pz-title {
  color: var(--pz-title);
  border-color: rgb(201 154 18 / 0.35);
}

.pz-unit {
  color: #976800;
  opacity: 0.75;
}

.pz-platform {
  background: #faf6e8;
  color: #5b5238;
}

/* Keep the same prices and accessible table headers when rows stack on a phone. */
@media (max-width: 639px) {
  table, tbody { display: block; width: 100%; min-width: 0; }
  colgroup { display: none; }
  thead { position: absolute; width: 1px; height: 1px; overflow: hidden; clip-path: inset(50%); }
  tbody tr { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); }
  tbody tr::before, tbody tr::after {
    grid-column: 1 / -1;
    padding: .625rem .875rem .25rem;
    font-size: .75rem;
    font-weight: 600;
  }
  tbody tr::before { order: 1; content: attr(data-paid-heading); background: var(--pz-bg); }
  tbody tr::after { order: 3; content: attr(data-original-heading); }
  tbody :deep(td) {
    order: 4;
    min-width: 0;
    padding: .375rem .875rem .625rem;
    border: 0;
    overflow-wrap: anywhere;
  }
  tbody :deep(td[data-col])::before {
    display: block;
    margin-bottom: .25rem;
    color: var(--md-sys-color-on-surface-variant);
    font: 500 .75rem/1.25 ui-sans-serif, system-ui, sans-serif;
    content: attr(data-col);
  }
  tbody td.pz-model { order: 0; grid-column: 1 / -1; padding-top: .875rem; }
  tbody td.pz-cell { order: 2; }
  tbody :deep(td[colspan]), tbody :deep(.pz-cache) { grid-column: 1 / -1; }
  tbody td.pz-rate { order: 5; grid-column: 1 / -1; display: flex; justify-content: end; align-items: baseline; gap: .375rem; }
}
</style>
