<template>
  <section
    class="plaza-group"
    :class="{ 'is-expanded': expanded }"
  >
    <!-- 分组摘要:价格明细默认收起,原生 button 同时支持鼠标与键盘操作。 -->
    <header>
      <button
        type="button"
        data-testid="group-toggle"
        class="plaza-group-toggle"
        :aria-expanded="expanded"
        :aria-controls="expanded ? pricingDetailsId : undefined"
        :aria-label="pricingToggleLabel"
        :title="pricingActionLabel"
        @click="toggleExpanded"
      >
        <span class="plaza-group-icon" aria-hidden="true">
          <PlatformIcon :platform="group.platform as GroupPlatform" size="md" />
        </span>

        <span class="plaza-group-main">
          <span class="plaza-group-titlebar">
            <span class="plaza-group-name">{{ group.name }}</span>
            <span
              v-if="group.is_exclusive"
              class="plaza-badge plaza-badge-exclusive"
            >
              <Icon name="shield" size="xs" class="h-3 w-3" />
              {{ t('modelPlaza.badges.exclusive') }}
            </span>
            <span
              v-if="group.subscription_type === 'subscription'"
              class="plaza-badge plaza-badge-subscription"
            >
              {{ t('modelPlaza.badges.subscription') }}
            </span>
          </span>
          <span v-if="group.description" class="plaza-group-description">
            {{ group.description }}
          </span>
          <span class="plaza-group-meta">
            <span class="inline-flex items-center gap-1">
              <Icon name="cube" size="xs" class="h-3.5 w-3.5" />
              {{ t('modelPlaza.detail.modelCount', { count: group.models.length }) }}
            </span>
            <span
              v-if="peakNote"
              class="inline-flex items-center gap-1 text-amber-700"
            >
              <Icon name="clock" size="xs" class="h-3 w-3" />
              {{ peakNote }}
            </span>
          </span>
          <!-- 收起时先露出前几个模型,不用展开也知道这个分组里有什么。 -->
          <span v-if="!expanded && previewModels.length > 0" class="plaza-group-preview">
            <span v-for="model in previewModels" :key="`${model.platform}:${model.name}`" class="plaza-model-chip">
              {{ model.name }}
            </span>
            <span v-if="hiddenModelCount > 0" class="plaza-model-more">
              {{ t('modelPlaza.detail.moreModels', { count: hiddenModelCount }) }}
            </span>
          </span>
        </span>

        <span class="plaza-group-side">
          <span class="plaza-rate" :title="t('modelPlaza.detail.effectiveRate')">
            <span v-if="hasCustomRate" class="plaza-rate-list">{{ group.rate_multiplier }}x</span>
            <span class="plaza-rate-value">{{ effectiveRate }}x</span>
          </span>
          <span class="plaza-group-action">
            <span class="hidden sm:inline">{{ pricingActionLabel }}</span>
            <Icon :name="expanded ? 'chevronUp' : 'chevronDown'" size="sm" aria-hidden="true" />
          </span>
        </span>
      </button>
    </header>

    <!-- 价格明细仅在用户展开后渲染,避免收起态占用长表格空间。 -->
    <div v-if="expanded" :id="pricingDetailsId" class="plaza-group-details" data-testid="group-pricing-details">
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
      <p v-else class="px-5 py-4 text-center text-sm text-gray-500 dark:text-dark-500">
        {{ t('modelPlaza.detail.noModels') }}
      </p>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import PlatformIcon from '@/components/common/PlatformIcon.vue'
import PlazaModelPricingTable from './PlazaModelPricingTable.vue'
import type { ModelPlazaGroup } from '@/api/modelPlaza'
import type { GroupPlatform } from '@/types'
import { hasPeakRate, formatPeakRateWindow, serverTimezoneLabel } from '@/utils/peak-rate'
import { useAppStore } from '@/stores/app'

const props = defineProps<{
  group: ModelPlazaGroup
}>()

// 收起态最多预览的模型数量
const PREVIEW_MODEL_LIMIT = 6

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

const effectiveRate = computed(() => props.group.user_rate_multiplier ?? props.group.rate_multiplier)
const hasCustomRate = computed(
  () => props.group.user_rate_multiplier != null && props.group.user_rate_multiplier !== props.group.rate_multiplier
)

const previewModels = computed(() => props.group.models.slice(0, PREVIEW_MODEL_LIMIT))
const hiddenModelCount = computed(() => Math.max(props.group.models.length - PREVIEW_MODEL_LIMIT, 0))

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
.plaza-group {
  overflow: hidden;
  border-radius: 1.5rem;
  background: var(--md-sys-color-surface);
  box-shadow: var(--theme-highlight), var(--theme-elevation-1);
  transition: box-shadow 220ms var(--theme-ease-out);
}

.plaza-group.is-expanded {
  box-shadow: var(--theme-highlight), var(--theme-elevation-2);
}

.plaza-group-toggle {
  display: flex;
  width: 100%;
  cursor: pointer;
  align-items: flex-start;
  gap: 0.875rem;
  padding: 1rem 1.125rem;
  text-align: left;
  transition: background-color 160ms ease;
}

.plaza-group-toggle:hover {
  background: #fffdf6;
}

.plaza-group-toggle:focus-visible {
  outline: none;
  box-shadow: inset 0 0 0 2px color-mix(in srgb, var(--md-sys-color-primary) 70%, transparent);
}

.plaza-group-icon {
  display: inline-flex;
  width: 2.75rem;
  height: 2.75rem;
  flex-shrink: 0;
  align-items: center;
  justify-content: center;
  border-radius: 0.875rem;
  color: #7e5600;
  background: #fff3bd;
}

.plaza-group-main {
  display: flex;
  min-width: 0;
  flex: 1;
  flex-direction: column;
  gap: 0.375rem;
}

.plaza-group-titlebar {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 0.375rem 0.5rem;
}

.plaza-group-name {
  font-size: 1rem;
  font-weight: 700;
  line-height: 1.375rem;
  color: var(--md-sys-color-on-surface);
  overflow-wrap: anywhere;
}

.plaza-badge {
  display: inline-flex;
  align-items: center;
  gap: 0.25rem;
  padding: 0.0625rem 0.5rem;
  border-radius: 999px;
  font-size: 0.6875rem;
  font-weight: 600;
  line-height: 1.125rem;
}

.plaza-badge-exclusive {
  background: #fff3bd;
  color: #7e5600;
}

.plaza-badge-subscription {
  background: #e4f0df;
  color: #2e5038;
}

.plaza-group-description {
  font-size: 0.8125rem;
  line-height: 1.25rem;
  color: var(--md-sys-color-on-surface-variant);
}

.plaza-group-meta {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 0.25rem 0.875rem;
  font-size: 0.75rem;
  color: var(--md-sys-color-on-surface-variant);
}

.plaza-group-preview {
  display: flex;
  flex-wrap: wrap;
  gap: 0.375rem;
  margin-top: 0.125rem;
}

.plaza-model-chip {
  display: inline-block;
  max-width: 100%;
  overflow: hidden;
  padding: 0.1875rem 0.625rem;
  border-radius: 999px;
  background: #faf6e8;
  font-size: 0.75rem;
  white-space: nowrap;
  text-overflow: ellipsis;
  color: #48402d;
}

.plaza-model-more {
  display: inline-flex;
  align-items: center;
  padding: 0.1875rem 0.5625rem;
  border-radius: 999px;
  background: #faf6e8;
  font-size: 0.75rem;
  font-weight: 600;
  color: var(--md-sys-color-on-surface-variant);
}

.plaza-group-side {
  display: flex;
  flex-shrink: 0;
  flex-direction: column;
  align-items: flex-end;
  gap: 0.5rem;
}

/* The rate is what people compare across groups, so it reads as a column down the right. */
.plaza-rate {
  display: inline-flex;
  align-items: baseline;
  gap: 0.375rem;
  padding: 0.25rem 0.75rem;
  border-radius: 0.875rem;
  background: #fff7d6;
}

.plaza-rate-list {
  font-size: 0.8125rem;
  text-decoration: line-through;
  color: var(--md-sys-color-on-surface-variant);
}

.plaza-rate-value {
  font-size: 1.25rem;
  font-weight: 750;
  line-height: 1.75rem;
  color: #3f3420;
}

.plaza-group-action {
  display: inline-flex;
  align-items: center;
  gap: 0.25rem;
  font-size: 0.75rem;
  font-weight: 600;
  color: #7e5600;
}

.plaza-group-details {
  box-shadow: inset 0 1px 0 rgb(215 193 106 / 0.35);
}

@media (min-width: 640px) {
  .plaza-group-toggle {
    gap: 1rem;
    padding: 1.125rem 1.375rem;
  }
}
</style>
