<template>
  <div class="space-y-5">
    <!-- 页头(独立形态下展示标题;后台形态 AppHeader 已有页面标题) -->
    <header v-if="!embedded" class="plaza-intro">
      <div class="min-w-0">
        <h1 class="plaza-intro-title">{{ t('modelPlaza.title') }}</h1>
        <p class="plaza-intro-description">{{ t('modelPlaza.description') }}</p>
      </div>
      <p v-if="!loading && !error && orderedGroups.length > 0" class="plaza-intro-summary">
        {{ t('modelPlaza.summary', { groups: orderedGroups.length, models: modelTotal }) }}
      </p>
    </header>

    <!-- 全局价格说明(管理员配置,Markdown) -->
    <div
      v-if="descriptionHtml"
      class="plaza-description"
      v-html="descriptionHtml"
    ></div>

    <!-- 未登录提示 -->
    <p
      v-if="!isAuthenticated"
      class="flex items-center gap-1.5 text-xs text-gray-500 dark:text-dark-500"
    >
      <Icon name="infoCircle" size="xs" class="h-3.5 w-3.5" />
      {{ t('modelPlaza.anonymousHint') }}
    </p>

    <!-- 加载/错误/空 -->
    <div v-if="loading" class="flex min-h-[240px] items-center justify-center">
      <div class="h-8 w-8 animate-spin rounded-full border-2 border-primary-600/25 border-t-primary-600 dark:border-primary-400/25 dark:border-t-primary-400"></div>
    </div>
    <div
      v-else-if="error"
      class="rounded-2xl bg-red-50 px-5 py-8 text-center text-sm text-red-700 dark:bg-red-500/10 dark:text-red-300"
    >
      {{ t('modelPlaza.loadFailed') }}
    </div>
    <template v-else>
      <!-- 筛选区:平台 → 分组 → 倍率 -->
      <PlazaFilterBar
        :platforms="platforms"
        :groups="groupOptions"
        :rates="rates"
        :platform="selectedPlatform"
        :group-id="selectedGroupId"
        :rate="selectedRate"
        :search="searchQuery"
        @update:platform="selectedPlatform = $event"
        @update:group-id="selectedGroupId = $event"
        @update:rate="selectedRate = $event"
        @update:search="searchQuery = $event"
      />

      <!-- 分组分节的模型清单(按分组管理里的排序展示) -->
      <div v-if="filteredGroups.length > 0" class="space-y-4">
        <PlazaGroupSection v-for="g in filteredGroups" :key="g.id" :group="g" />
      </div>
      <div
        v-else
        class="plaza-empty"
      >
        {{ searchActive ? t('modelPlaza.noSearchResult') : t('modelPlaza.empty') }}
      </div>
    </template>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { marked } from 'marked'
import DOMPurify from 'dompurify'
import Icon from '@/components/icons/Icon.vue'
import PlazaFilterBar from './PlazaFilterBar.vue'
import PlazaGroupSection from './PlazaGroupSection.vue'
import type { ModelPlazaGroup, ModelPlazaResponse } from '@/api/modelPlaza'
import { useAuthStore } from '@/stores/auth'

const props = defineProps<{
  response: ModelPlazaResponse | null
  loading: boolean
  error?: boolean
  /** 后台内嵌形态(AppLayout 内):隐藏页头。 */
  embedded?: boolean
}>()

const { t } = useI18n()
const authStore = useAuthStore()
const isAuthenticated = computed(() => authStore.isAuthenticated)

const selectedPlatform = ref<string>('all')
const selectedGroupId = ref<number | 'all'>('all')
const selectedRate = ref<number | 'all'>('all')
const searchQuery = ref('')

const searchActive = computed(() => searchQuery.value.trim() !== '')

const descriptionHtml = computed(() => {
  const md = props.response?.description?.trim()
  if (!md) return ''
  return DOMPurify.sanitize(marked.parse(md) as string)
})

/** 生效倍率 = 用户专属倍率 ?? 分组默认倍率。 */
function effectiveRate(g: ModelPlazaGroup): number {
  return g.user_rate_multiplier ?? g.rate_multiplier
}

/**
 * 分组按管理员在分组管理里设定的排序展示;同序(或旧接口未返回排序)时保持服务端顺序。
 */
const orderedGroups = computed(() =>
  (props.response?.groups ?? [])
    .map((group, index) => ({ group, index }))
    .sort((a, b) => (a.group.sort_order ?? 0) - (b.group.sort_order ?? 0) || a.index - b.index)
    .map(({ group }) => group)
)

const modelTotal = computed(() =>
  orderedGroups.value.reduce((sum, group) => sum + group.models.length, 0)
)

const platforms = computed(() =>
  [...new Set(orderedGroups.value.map((g) => g.platform).filter(Boolean))].sort()
)

const groupOptions = computed(() =>
  orderedGroups.value.map((g) => ({
    id: g.id,
    name: g.name,
    platform: g.platform,
    rate: effectiveRate(g)
  }))
)

/** 全量生效倍率;当前组合下不可用的项由 FilterBar 置灰而非隐藏。 */
const rates = computed(() =>
  [...new Set(orderedGroups.value.map(effectiveRate))].sort((a, b) => a - b)
)

/** 数据刷新后选中的倍率可能不复存在,重置为全部。 */
watch(rates, (list) => {
  if (selectedRate.value !== 'all' && !list.includes(selectedRate.value)) {
    selectedRate.value = 'all'
  }
})

const filteredGroups = computed(() => {
  let groups = orderedGroups.value
  if (selectedPlatform.value !== 'all') {
    groups = groups.filter((g) => g.platform === selectedPlatform.value)
  }
  if (selectedGroupId.value !== 'all') {
    groups = groups.filter((g) => g.id === selectedGroupId.value)
  }
  if (selectedRate.value !== 'all') {
    groups = groups.filter((g) => effectiveRate(g) === selectedRate.value)
  }
  // 模型名搜索:分组内只留命中的模型,整组无命中则隐藏该分组。
  const q = searchQuery.value.trim().toLowerCase()
  if (q) {
    groups = groups
      .map((g) => ({ ...g, models: g.models.filter((m) => m.name.toLowerCase().includes(q)) }))
      .filter((g) => g.models.length > 0)
  }
  return groups
})
</script>

<style scoped>
.plaza-intro {
  display: flex;
  flex-wrap: wrap;
  align-items: flex-end;
  justify-content: space-between;
  gap: 0.75rem 1.5rem;
}

.plaza-intro-title {
  font-size: 1.75rem;
  font-weight: 700;
  line-height: 2.25rem;
  letter-spacing: -0.01em;
  color: var(--md-sys-color-on-surface);
}

.plaza-intro-description {
  margin-top: 0.25rem;
  font-size: 0.875rem;
  color: var(--md-sys-color-on-surface-variant);
}

.plaza-intro-summary {
  padding: 0.3125rem 0.875rem;
  border-radius: 999px;
  background: #fff3bd;
  font-size: 0.8125rem;
  font-weight: 600;
  color: #7e5600;
}

.plaza-description {
  padding: 1rem 1.25rem;
  border-radius: 1.25rem;
  background: var(--md-sys-color-surface);
  box-shadow: var(--theme-highlight), var(--theme-elevation-1);
  font-size: 0.875rem;
  line-height: 1.7;
  overflow-wrap: anywhere;
}

.plaza-empty {
  padding: 3rem 1.25rem;
  border-radius: 1.5rem;
  background: var(--md-sys-color-surface);
  text-align: center;
  font-size: 0.875rem;
  color: var(--md-sys-color-on-surface-variant);
  box-shadow: var(--theme-highlight), var(--theme-elevation-1);
}

.plaza-description :deep(h1),
.plaza-description :deep(h2),
.plaza-description :deep(h3) {
  @apply mb-2 mt-3 font-semibold text-gray-900 first:mt-0 dark:text-white;
}

.plaza-description :deep(p) {
  @apply mb-2 text-gray-700 last:mb-0 dark:text-dark-200;
}

.plaza-description :deep(a) {
  @apply text-primary-700 underline underline-offset-4 hover:text-primary-800 dark:text-primary-300;
}

.plaza-description :deep(ul) {
  @apply mb-2 list-disc pl-5;
}

.plaza-description :deep(ol) {
  @apply mb-2 list-decimal pl-5;
}

.plaza-description :deep(li) {
  @apply mb-0.5 text-gray-700 dark:text-dark-200;
}

.plaza-description :deep(code) {
  @apply rounded bg-gray-100 px-1.5 py-0.5 font-mono text-xs dark:bg-dark-800;
}

.plaza-description :deep(blockquote) {
  @apply my-2 border-l-4 border-primary-300 pl-3 text-gray-600 dark:border-dark-600 dark:text-dark-300;
}
</style>
