<template>
  <section
    class="plaza-group-section overflow-hidden rounded-2xl border shadow-card"
    :class="[platformBorderStrongClass(group.platform)]"
  >
    <!-- 分组摘要:价格明细默认收起,原生 button 同时支持鼠标与键盘操作。 -->
    <header :class="expanded ? 'border-b border-gray-100 dark:border-dark-700/60' : ''">
      <button
        type="button"
        data-testid="group-toggle"
        class="flex w-full cursor-pointer items-start gap-4 px-5 py-4 text-left transition-colors hover:bg-black/[0.025] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-primary-500 dark:hover:bg-white/[0.025] dark:focus-visible:ring-primary-400"
        :aria-expanded="expanded"
        :aria-controls="expanded ? pricingDetailsId : undefined"
        :aria-label="pricingToggleLabel"
        :title="pricingActionLabel"
        @click="toggleExpanded"
      >
        <span class="min-w-0 flex-1">
          <span class="flex flex-wrap items-center gap-2">
            <GroupBadge
              :name="group.name"
              :platform="group.platform as GroupPlatform"
              :subscription-type="(group.subscription_type || 'standard') as SubscriptionType"
              :rate-multiplier="group.rate_multiplier"
              :user-rate-multiplier="group.user_rate_multiplier ?? null"
              :peak-rate-enabled="group.peak_rate_enabled"
              :peak-start="group.peak_start"
              :peak-end="group.peak_end"
              :peak-rate-multiplier="group.peak_rate_multiplier"
              always-show-rate
            />
            <span
              v-if="group.is_exclusive"
              class="inline-flex items-center gap-1 rounded-md bg-purple-50 px-2 py-0.5 text-xs font-medium text-purple-600 dark:bg-purple-900/20 dark:text-purple-400"
            >
              <Icon name="shield" size="xs" class="h-3 w-3" />
              {{ t('modelPlaza.badges.exclusive') }}
            </span>
            <span
              v-if="group.subscription_type === 'subscription'"
              class="inline-flex items-center rounded-md bg-violet-50 px-2 py-0.5 text-xs font-medium text-violet-600 dark:bg-violet-900/20 dark:text-violet-400"
            >
              {{ t('modelPlaza.badges.subscription') }}
            </span>
          </span>
          <span class="mt-2 flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-gray-400 dark:text-dark-500">
            <span class="inline-flex items-center gap-1">
              <Icon name="cube" size="xs" class="h-3.5 w-3.5" />
              {{ t('modelPlaza.detail.modelCount', { count: group.models.length }) }}
            </span>
            <span
              v-if="peakNote"
              class="inline-flex items-center gap-1 text-amber-600 dark:text-amber-400"
            >
              <Icon name="clock" size="xs" class="h-3 w-3" />
              {{ peakNote }}
            </span>
          </span>
          <span v-if="group.description" class="mt-2 block text-sm text-gray-500 dark:text-dark-400">
            {{ group.description }}
          </span>
        </span>

        <span class="flex shrink-0 items-center gap-1.5 pt-0.5 text-xs font-medium text-primary-600 dark:text-primary-400">
          <span class="hidden sm:inline">
            {{ pricingActionLabel }}
          </span>
          <Icon :name="expanded ? 'chevronUp' : 'chevronDown'" size="sm" aria-hidden="true" />
        </span>
      </button>
    </header>

    <!-- 价格明细仅在用户展开后渲染,避免收起态占用长表格空间。 -->
    <div v-if="expanded" :id="pricingDetailsId" data-testid="group-pricing-details">
      <PlazaModelPricingTable
        v-if="group.models.length > 0"
        :models="group.models"
        :platform="group.platform"
        :rate-multiplier="group.rate_multiplier"
        :user-rate-multiplier="group.user_rate_multiplier ?? null"
        :image-rate-independent="group.image_rate_independent"
        :image-rate-multiplier="group.image_rate_multiplier"
        :video-rate-independent="group.video_rate_independent"
        :video-rate-multiplier="group.video_rate_multiplier"
      />
      <p v-else class="px-5 py-4 text-center text-sm text-gray-400 dark:text-dark-500">
        {{ t('modelPlaza.detail.noModels') }}
      </p>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import GroupBadge from '@/components/common/GroupBadge.vue'
import PlazaModelPricingTable from './PlazaModelPricingTable.vue'
import type { ModelPlazaGroup } from '@/api/modelPlaza'
import type { GroupPlatform, SubscriptionType } from '@/types'
import { platformBorderStrongClass } from '@/utils/platformColors'
import { hasPeakRate, formatPeakRateWindow, serverTimezoneLabel } from '@/utils/peak-rate'
import { useAppStore } from '@/stores/app'

const props = defineProps<{
  group: ModelPlazaGroup
}>()

const { t } = useI18n()
const appStore = useAppStore()
const expanded = ref(false)

const pricingDetailsId = computed(() => `model-plaza-group-${props.group.id}-pricing`)
const pricingActionLabel = computed(() =>
  expanded.value ? t('modelPlaza.detail.collapsePricing') : t('modelPlaza.detail.expandPricing')
)
const pricingToggleLabel = computed(() =>
  props.group.name ? `${props.group.name}: ${pricingActionLabel.value}` : pricingActionLabel.value
)

function toggleExpanded() {
  expanded.value = !expanded.value
}

const peakNote = computed(() => {
  if (!hasPeakRate(props.group)) return ''
  const window = formatPeakRateWindow(
    props.group,
    serverTimezoneLabel(appStore.cachedPublicSettings?.server_utc_offset)
  )
  return t('modelPlaza.detail.peakNote', {
    window,
    multiplier: props.group.peak_rate_multiplier
  })
})
</script>

<style scoped>
.plaza-group-section {
  background: color-mix(in srgb, var(--md-sys-color-surface) 64%, var(--md-sys-color-surface-container));
}
</style>
