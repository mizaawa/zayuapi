<template>
  <section class="plaza-filters">
    <!-- 一级:平台 -->
    <div class="plaza-filter-row">
      <span class="plaza-filter-label">{{ t('modelPlaza.filters.platformLabel') }}</span>
      <div class="plaza-filter-options">
        <button
          v-for="p in ['all', ...platforms]"
          :key="`platform-${p}`"
          type="button"
          class="plaza-chip"
          :class="p === 'all' ? chipClass(platform === 'all') : platform === p ? 'chip-tinted-active' : 'chip-tinted'"
          :style="p === 'all' ? undefined : { '--chip-accent': platformAccentColor(p) }"
          :disabled="p !== 'all' && !platformEnabled(p)"
          :aria-pressed="platform === p"
          @click="$emit('update:platform', p)"
        >
          <PlatformIcon v-if="p !== 'all'" :platform="p as GroupPlatform" size="xs" />
          {{ p === 'all' ? t('modelPlaza.filters.all') : platformLabel(p) }}
        </button>
      </div>
    </div>

    <!-- 二级:分组(按所属平台着色,当前组合下无结果的置灰) -->
    <div class="plaza-filter-row">
      <span class="plaza-filter-label">{{ t('modelPlaza.filters.groupLabel') }}</span>
      <div class="plaza-filter-options">
        <button
          type="button"
          class="plaza-chip"
          :class="chipClass(groupId === 'all')"
          :aria-pressed="groupId === 'all'"
          @click="$emit('update:groupId', 'all')"
        >
          {{ t('modelPlaza.filters.all') }}
        </button>
        <button
          v-for="g in groups"
          :key="`group-${g.id}`"
          type="button"
          class="plaza-chip"
          :class="groupId === g.id ? 'chip-tinted-active' : 'chip-tinted'"
          :style="{ '--chip-accent': platformAccentColor(g.platform) }"
          :disabled="!groupEnabled(g)"
          :aria-pressed="groupId === g.id"
          @click="$emit('update:groupId', g.id)"
        >
          {{ g.name }}
        </button>
      </div>
    </div>

    <!-- 三级:倍率(当前组合下不存在的置灰) -->
    <div class="plaza-filter-row">
      <span class="plaza-filter-label">{{ t('modelPlaza.filters.rateLabel') }}</span>
      <div class="plaza-filter-options">
        <button
          type="button"
          class="plaza-chip"
          :class="chipClass(rate === 'all')"
          :aria-pressed="rate === 'all'"
          @click="$emit('update:rate', 'all')"
        >
          {{ t('modelPlaza.filters.all') }}
        </button>
        <button
          v-for="r in rates"
          :key="`rate-${r}`"
          type="button"
          class="plaza-chip plaza-chip-rate"
          :class="chipClass(rate === r)"
          :disabled="!rateEnabled(r)"
          :aria-pressed="rate === r"
          @click="$emit('update:rate', r)"
        >
          {{ r }}x
        </button>
      </div>
    </div>

    <!-- 四级:模型名搜索(纯前端过滤) -->
    <div class="plaza-filter-row">
      <span class="plaza-filter-label">{{ t('modelPlaza.filters.modelLabel') }}</span>
      <div class="plaza-search">
        <Icon
          name="search"
          size="sm"
          class="plaza-search-icon"
        />
        <input
          :value="search"
          type="text"
          :placeholder="t('modelPlaza.filters.searchPlaceholder')"
          class="input plaza-search-input"
          @input="$emit('update:search', ($event.target as HTMLInputElement).value)"
        />
        <button
          v-if="search"
          type="button"
          class="plaza-search-clear"
          :aria-label="t('modelPlaza.filters.clearSearch')"
          @click="$emit('update:search', '')"
        >
          <Icon name="x" size="xs" class="h-3.5 w-3.5" />
        </button>
      </div>
    </div>
  </section>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import PlatformIcon from '@/components/common/PlatformIcon.vue'
import { platformAccentColor, platformLabel } from '@/utils/platformColors'
import type { GroupPlatform } from '@/types'

const props = defineProps<{
  /** 数据中出现的平台(去重排序后)。 */
  platforms: string[]
  /** 全量分组(含平台与生效倍率),三个维度的置灰联动由此推导。 */
  groups: Array<{ id: number; name: string; platform: string; rate: number }>
  /** 全量生效倍率去重升序。 */
  rates: number[]
  platform: string
  groupId: number | 'all'
  rate: number | 'all'
  /** 模型名搜索词(纯前端过滤)。 */
  search: string
}>()

defineEmits<{
  'update:platform': [value: string]
  'update:groupId': [value: number | 'all']
  'update:rate': [value: number | 'all']
  'update:search': [value: string]
}>()

const { t } = useI18n()

/**
 * 三个维度互为约束(faceted):某选项可点 ⟺ 在「其他两维」当前选择下仍有分组命中。
 * 「全部」永远可点,作为解除本维约束的出口;可点项组合恒有结果,无需选择修正。
 */
function platformEnabled(p: string): boolean {
  return props.groups.some(
    (g) =>
      g.platform === p &&
      (props.groupId === 'all' || g.id === props.groupId) &&
      (props.rate === 'all' || g.rate === props.rate)
  )
}

function groupEnabled(g: { platform: string; rate: number }): boolean {
  return (
    (props.platform === 'all' || g.platform === props.platform) &&
    (props.rate === 'all' || g.rate === props.rate)
  )
}

function rateEnabled(r: number): boolean {
  return props.groups.some(
    (g) =>
      g.rate === r &&
      (props.platform === 'all' || g.platform === props.platform) &&
      (props.groupId === 'all' || g.id === props.groupId)
  )
}

function chipClass(active: boolean): string {
  return active ? 'chip-neutral-active' : 'chip-neutral'
}
</script>

<style scoped>
.plaza-filters {
  display: flex;
  flex-direction: column;
  gap: 0.75rem;
  padding: 1rem 1.125rem;
  border-radius: 1.5rem;
  background: var(--md-sys-color-surface);
  box-shadow: var(--theme-highlight), var(--theme-elevation-1);
}

.plaza-filter-row {
  display: flex;
  flex-direction: column;
  gap: 0.375rem;
}

.plaza-filter-label {
  font-size: 0.75rem;
  font-weight: 600;
  color: var(--md-sys-color-on-surface-variant);
}

.plaza-filter-options {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 0.375rem;
}

.plaza-chip {
  display: inline-flex;
  align-items: center;
  gap: 0.375rem;
  padding: 0.3125rem 0.8125rem;
  border-radius: 999px;
  font-size: 0.8125rem;
  font-weight: 500;
  transition: background-color 160ms ease, color 160ms ease, box-shadow 160ms ease, opacity 160ms ease;
}

.plaza-chip:disabled {
  cursor: not-allowed;
  opacity: 0.4;
  filter: grayscale(1);
}

.plaza-chip-rate {
  font-variant-numeric: tabular-nums;
}

.chip-neutral {
  color: var(--md-sys-color-on-surface-variant);
  background: #faf6e8;
}

.chip-neutral:not(:disabled):hover {
  color: var(--md-sys-color-on-surface);
  background: #fff3bd;
}

.chip-neutral-active {
  font-weight: 650;
  color: var(--md-sys-color-on-primary);
  background: var(--md-sys-color-primary);
  box-shadow: inset 0 1px 0 rgb(255 255 255 / 0.4), 0 4px 10px -6px rgb(151 104 0 / 0.55);
}

/* 平台/分组 chip 的配色统一从 --chip-accent(平台主色)派生,新增平台无需扩展样式。
   激活态与非激活态在模板上互斥挂载,避免选择器优先级互相覆盖。 */
.chip-tinted {
  color: color-mix(in srgb, var(--chip-accent) 70%, #3f3420);
  background-color: color-mix(in srgb, var(--chip-accent) 10%, var(--md-sys-color-surface));
}

.chip-tinted:not(:disabled):hover {
  background-color: color-mix(in srgb, var(--chip-accent) 18%, var(--md-sys-color-surface));
}

.chip-tinted-active {
  font-weight: 650;
  color: color-mix(in srgb, var(--chip-accent) 72%, #3f3420);
  background-color: color-mix(in srgb, var(--chip-accent) 20%, var(--md-sys-color-surface));
  box-shadow: inset 0 0 0 1.5px color-mix(in srgb, var(--chip-accent) 60%, transparent);
}

.plaza-search {
  position: relative;
  width: 100%;
}

.plaza-search-icon {
  position: absolute;
  top: 50%;
  left: 0.875rem;
  transform: translateY(-50%);
  color: var(--md-sys-color-on-surface-variant);
  opacity: 0.7;
  pointer-events: none;
}

.plaza-search-input {
  padding-top: 0.5rem;
  padding-bottom: 0.5rem;
  padding-left: 2.375rem;
  padding-right: 2.25rem;
  border-radius: 999px;
}

.plaza-search-clear {
  position: absolute;
  top: 50%;
  right: 0.625rem;
  transform: translateY(-50%);
  padding: 0.25rem;
  border-radius: 999px;
  color: var(--md-sys-color-on-surface-variant);
}

.plaza-search-clear:hover {
  background: #fff3bd;
  color: var(--md-sys-color-on-surface);
}

@media (min-width: 640px) {
  .plaza-filters {
    padding: 1.125rem 1.375rem;
  }

  .plaza-filter-row {
    flex-direction: row;
    align-items: flex-start;
    gap: 0.75rem;
  }

  .plaza-filter-label {
    width: 2.5rem;
    flex-shrink: 0;
    padding-top: 0.375rem;
  }

  .plaza-search {
    width: 20rem;
  }
}
</style>
