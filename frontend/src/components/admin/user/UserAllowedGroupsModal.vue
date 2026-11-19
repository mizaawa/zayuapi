<template>
  <BaseDialog :show="show" :title="t('admin.users.groupConfig')" width="wide" @close="$emit('close')">
    <div v-if="user" class="space-y-6">
      <!-- 用户信息头部 -->
      <div class="flex items-center gap-4 rounded-2xl bg-gradient-to-r from-primary-50 to-primary-100 p-5 dark:from-primary-900/30 dark:to-primary-800/20">
        <div class="flex h-14 w-14 items-center justify-center rounded-full bg-white shadow-sm dark:bg-dark-700">
          <span class="text-2xl font-semibold text-primary-600 dark:text-primary-400">{{ user.email.charAt(0).toUpperCase() }}</span>
        </div>
        <div class="flex-1">
          <p class="text-lg font-semibold text-gray-900 dark:text-white">{{ user.email }}</p>
          <p class="mt-1 text-sm text-gray-600 dark:text-gray-400">{{ t('admin.users.groupConfigHint', { email: user.email }) }}</p>
        </div>
      </div>

      <!-- 加载状态 -->
      <div v-if="loading" class="flex justify-center py-12">
        <svg class="h-10 w-10 animate-spin text-primary-500" fill="none" viewBox="0 0 24 24">
          <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
          <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
        </svg>
      </div>

      <div v-else class="space-y-6">
        <!-- 专属分组区域 -->
        <div v-if="exclusiveGroups.length > 0">
          <div class="mb-3 flex items-center gap-2">
            <div class="h-1.5 w-1.5 rounded-full bg-purple-500"></div>
            <h4 class="text-sm font-semibold text-gray-700 dark:text-gray-300">{{ t('admin.users.exclusiveGroups') }}</h4>
            <span class="text-xs text-gray-400">({{ exclusiveGroupConfigs.filter(c => c.isSelected).length }}/{{ exclusiveGroupConfigs.length }})</span>
          </div>
          <div class="grid gap-3">
            <div
              v-for="config in exclusiveGroupConfigs"
              :key="config.groupId"
              class="group relative overflow-hidden rounded-xl border-2 p-4 transition-all duration-200"
              :class="config.isSelected
                ? 'border-primary-400 bg-primary-50/50 shadow-sm dark:border-primary-500 dark:bg-primary-900/20'
                : 'border-gray-200 bg-white hover:border-gray-300 dark:border-dark-600 dark:bg-dark-800 dark:hover:border-dark-500'"
            >
              <div class="flex items-center gap-4">
                <!-- app-shell 会清空中性边框，因此用 ring 保证未选状态仍清晰可见。 -->
                <div class="flex-shrink-0">
                  <button
                    type="button"
                    role="checkbox"
                    :aria-checked="config.isSelected"
                    :aria-label="config.isSelected ? t('admin.users.deselectExclusiveGroup') : t('admin.users.selectExclusiveGroup')"
                    :data-test="`exclusive-group-checkbox-${config.groupId}`"
                    class="flex h-6 w-6 cursor-pointer items-center justify-center rounded-md ring-1 ring-inset transition-colors text-white focus:ring-primary-400 dark:focus:ring-offset-dark-800"
                    :class="config.isSelected ? 'control-selected bg-primary-500 ring-primary-600 dark:bg-primary-500 dark:ring-primary-300' : 'control-quiet bg-gray-100 ring-gray-400 hover:bg-primary-50 hover:ring-primary-400 dark:bg-dark-700 dark:ring-dark-400 dark:hover:bg-dark-600 dark:hover:ring-primary-400'"
                    @click.stop="toggleExclusiveGroup(config.groupId)"
                  >
                    <svg v-if="config.isSelected" class="h-full w-full" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="3">
                      <path stroke-linecap="round" stroke-linejoin="round" d="M5 13l4 4L19 7" />
                    </svg>
                  </button>
                </div>

                <!-- 分组信息 -->
                <div class="min-w-0 flex-1">
                  <div class="flex items-center gap-2">
                    <span class="text-base font-semibold text-gray-900 dark:text-white">{{ config.groupName }}</span>
                    <span class="inline-flex items-center rounded-full bg-purple-100 px-2 py-0.5 text-xs font-medium text-purple-700 dark:bg-purple-900/40 dark:text-purple-300">
                      {{ t('admin.groups.exclusive') }}
                    </span>
                  </div>
                  <div class="mt-1.5 flex items-center gap-3 text-sm">
                    <span class="inline-flex items-center gap-1 text-gray-500 dark:text-gray-400">
                      <PlatformIcon :platform="config.platform" size="xs" />
                      <span>{{ config.platform }}</span>
                    </span>
                    <span class="text-gray-300 dark:text-dark-500">•</span>
                    <span class="text-gray-500 dark:text-gray-400">
                      {{ t('admin.users.defaultRate') }}: <span class="font-medium text-gray-700 dark:text-gray-300">{{ config.defaultRate }}x</span>
                    </span>
                  </div>
                </div>

                <!-- 专属倍率输入 -->
                <div class="flex flex-shrink-0 items-center gap-3">
                  <label class="text-sm font-medium text-gray-600 dark:text-gray-400">{{ t('admin.users.customRate') }}</label>
                  <input
                    type="number"
                    step="0.001"
                    min="0.001"
                    :value="config.customRate ?? ''"
                    @input="updateCustomRate(config.groupId, ($event.target as HTMLInputElement).value)"
                    :placeholder="String(config.defaultRate)"
                    class="control-field hide-spinner w-24 rounded-lg border px-3 py-2 text-sm font-medium transition-colors border-gray-300 bg-white focus:border-primary-500 focus:ring-primary-500/20 dark:border-dark-500 dark:bg-dark-700 dark:focus:border-primary-500"
                  />
                </div>
              </div>
            </div>
          </div>
        </div>

        <!-- 公开分组区域 -->
        <div v-if="publicGroups.length > 0">
          <div class="mb-3 flex items-center gap-2">
            <div class="h-1.5 w-1.5 rounded-full bg-green-500"></div>
            <h4 class="text-sm font-semibold text-gray-700 dark:text-gray-300">{{ t('admin.users.publicGroups') }}</h4>
            <span class="text-xs text-gray-400">({{ enabledPublicGroupCount }}/{{ publicGroupConfigs.length }})</span>
          </div>
          <div class="grid gap-3">
            <div
              v-for="config in publicGroupConfigs"
              :key="config.groupId"
              class="relative overflow-hidden rounded-xl border-2 p-4 transition-colors"
              :class="config.isBlocked
                ? 'border-red-300 bg-red-50/70 dark:border-red-700/70 dark:bg-red-900/20'
                : 'border-green-200 bg-green-50/50 dark:border-green-800/50 dark:bg-green-900/10'"
            >
              <div class="flex flex-wrap items-center gap-4">
                <!-- 公开分组开关：勾选表示可用，点击后变为叉号并封禁该分组。 -->
                <div class="flex-shrink-0">
                  <button
                    type="button"
                    class="flex h-6 w-6 items-center justify-center rounded-md border-2 text-white transition-colors focus:outline-none focus:ring-2 focus:ring-offset-1"
                    role="checkbox"
                    :aria-checked="!config.isBlocked"
                    :class="config.isBlocked
                      ? 'border-red-500 bg-red-500 focus:ring-red-400 dark:border-red-500 dark:bg-red-600'
                      : 'border-green-400 bg-green-500 focus:ring-green-400 dark:border-green-600 dark:bg-green-600'"
                    :aria-label="config.isBlocked ? t('admin.users.enablePublicGroup') : t('admin.users.blockPublicGroup')"
                    @click.stop="togglePublicGroup(config.groupId)"
                  >
                    <svg v-if="config.isBlocked" class="h-full w-full" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="3">
                      <path stroke-linecap="round" stroke-linejoin="round" d="M6 6l12 12M18 6L6 18" />
                    </svg>
                    <svg v-else class="h-full w-full" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="3">
                      <path stroke-linecap="round" stroke-linejoin="round" d="M5 13l4 4L19 7" />
                    </svg>
                  </button>
                </div>

                <!-- 分组信息 -->
                <div class="min-w-0 flex-1">
                  <div class="flex items-center gap-2">
                    <span class="text-base font-semibold text-gray-900 dark:text-white">{{ config.groupName }}</span>
                    <span
                      v-if="config.isBlocked"
                      class="inline-flex items-center rounded-full bg-red-100 px-2 py-0.5 text-xs font-semibold text-red-700 dark:bg-red-900/40 dark:text-red-300"
                    >{{ t('admin.users.groupBlocked') }}</span>
                  </div>
                  <div class="mt-1.5 flex items-center gap-3 text-sm">
                    <span class="inline-flex items-center gap-1 text-gray-500 dark:text-gray-400">
                      <PlatformIcon :platform="config.platform" size="xs" />
                      <span>{{ config.platform }}</span>
                    </span>
                    <span class="text-gray-300 dark:text-dark-500">•</span>
                    <span class="text-gray-500 dark:text-gray-400">
                      {{ t('admin.users.defaultRate') }}: <span class="font-medium text-gray-700 dark:text-gray-300">{{ config.defaultRate }}x</span>
                    </span>
                  </div>
                </div>

                <!-- 专属倍率输入 -->
                <div class="flex flex-shrink-0 items-center gap-3">
                  <label class="text-sm font-medium text-gray-600 dark:text-gray-400">{{ t('admin.users.customRate') }}</label>
                  <input
                    type="number"
                    step="0.001"
                    min="0.001"
                    :value="config.customRate ?? ''"
                    @input="updateCustomRate(config.groupId, ($event.target as HTMLInputElement).value)"
                    :placeholder="String(config.defaultRate)"
                    class="control-field hide-spinner w-24 rounded-lg border px-3 py-2 text-sm font-medium transition-colors border-gray-300 bg-white focus:border-primary-500 focus:ring-primary-500/20 dark:border-dark-500 dark:bg-dark-700 dark:focus:border-primary-500"
                  />
                </div>
              </div>
            </div>
          </div>
        </div>

        <!-- 无分组提示 -->
        <div v-if="groups.length === 0" class="flex flex-col items-center justify-center py-12 text-center">
          <div class="mb-4 flex h-16 w-16 items-center justify-center rounded-full bg-gray-100 dark:bg-dark-700">
            <svg class="h-8 w-8 text-gray-400" fill="none" viewBox="0 0 24 24" stroke="currentColor">
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 11H5m14 0a2 2 0 012 2v6a2 2 0 01-2 2H5a2 2 0 01-2-2v-6a2 2 0 012-2m14 0V9a2 2 0 00-2-2M5 11V9a2 2 0 012-2m0 0V5a2 2 0 012-2h6a2 2 0 012 2v2M7 7h10" />
            </svg>
          </div>
          <p class="text-gray-500 dark:text-gray-400">{{ t('common.noGroupsAvailable') }}</p>
        </div>
      </div>
    </div>

    <template #footer>
      <div class="flex justify-end gap-3">
        <button @click="$emit('close')" class="btn btn-secondary px-5">{{ t('common.cancel') }}</button>
        <button @click="handleSave" :disabled="submitting" class="btn btn-primary px-6">
          <svg v-if="submitting" class="-ml-1 mr-2 h-4 w-4 animate-spin" fill="none" viewBox="0 0 24 24">
            <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
            <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
          </svg>
          {{ submitting ? t('common.saving') : t('common.save') }}
        </button>
      </div>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { ref, watch, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { adminAPI } from '@/api/admin'
import type { AdminUser, Group, GroupPlatform } from '@/types'
import BaseDialog from '@/components/common/BaseDialog.vue'
import PlatformIcon from '@/components/common/PlatformIcon.vue'

interface GroupRateConfig {
  groupId: number
  groupName: string
  platform: GroupPlatform
  isExclusive: boolean
  defaultRate: number
  customRate: number | null
  isSelected: boolean
  isBlocked: boolean
}

const props = defineProps<{ show: boolean; user: AdminUser | null }>()
const emit = defineEmits(['close', 'success'])
const { t } = useI18n()
const appStore = useAppStore()

const groups = ref<Group[]>([])
const groupConfigs = ref<GroupRateConfig[]>([])
const originalGroupRates = ref<Record<number, number>>({}) // 记录原始专属倍率，用于检测删除
const originalBlockedGroups = ref<number[]>([])
const loading = ref(false)
const submitting = ref(false)

// 分离专属分组和公开分组
const exclusiveGroups = computed(() => groups.value.filter((g) => g.is_exclusive))
const publicGroups = computed(() => groups.value.filter((g) => !g.is_exclusive))

const exclusiveGroupConfigs = computed(() => groupConfigs.value.filter((c) => c.isExclusive))
const publicGroupConfigs = computed(() => groupConfigs.value.filter((c) => !c.isExclusive))
const enabledPublicGroupCount = computed(() => publicGroupConfigs.value.filter((c) => !c.isBlocked).length)

watch(
  () => props.show,
  (v) => {
    if (v && props.user) {
      load()
    }
  }
)

const load = async () => {
  loading.value = true
  try {
    const res = await adminAPI.groups.list(1, 1000)
    // 只显示标准类型且活跃的分组
    groups.value = res.items.filter((g) => g.subscription_type === 'standard' && g.status === 'active')

    // 初始化配置
    const userAllowedGroups = props.user?.allowed_groups || []
    const userBlockedGroups = props.user?.blocked_groups || []
    const userGroupRates = props.user?.group_rates || {}

    // 保存原始专属倍率，用于检测删除操作
    originalGroupRates.value = { ...userGroupRates }
    originalBlockedGroups.value = [...userBlockedGroups]

    groupConfigs.value = groups.value.map((g) => ({
      groupId: g.id,
      groupName: g.name,
      platform: g.platform,
      isExclusive: g.is_exclusive,
      defaultRate: g.rate_multiplier,
      customRate: userGroupRates[g.id] ?? null,
      // 专属分组：检查是否在 allowed_groups 中
      // 公开分组默认可用；blocked_groups 中的公开分组显示为叉号。
      isSelected: g.is_exclusive ? userAllowedGroups.includes(g.id) : !userBlockedGroups.includes(g.id),
      isBlocked: !g.is_exclusive && userBlockedGroups.includes(g.id),
    }))
  } catch (error) {
    console.error('Failed to load groups:', error)
  } finally {
    loading.value = false
  }
}

const toggleExclusiveGroup = (groupId: number) => {
  const config = groupConfigs.value.find((c) => c.groupId === groupId)
  if (config && config.isExclusive) {
    config.isSelected = !config.isSelected
  }
}

const togglePublicGroup = (groupId: number) => {
  const config = groupConfigs.value.find((c) => c.groupId === groupId)
  if (config && !config.isExclusive) {
    config.isBlocked = !config.isBlocked
    config.isSelected = !config.isBlocked
  }
}

const updateCustomRate = (groupId: number, value: string) => {
  const config = groupConfigs.value.find((c) => c.groupId === groupId)
  if (config) {
    if (value === '' || value === null || value === undefined) {
      config.customRate = null
    } else {
      const numValue = parseFloat(value)
      config.customRate = isNaN(numValue) ? null : numValue
    }
  }
}

const handleSave = async () => {
  if (!props.user) return
  submitting.value = true

  try {
    // 构建 allowed_groups（仅包含专属分组中被勾选的）
    const allowedGroups = groupConfigs.value.filter((c) => c.isExclusive && c.isSelected).map((c) => c.groupId)
    const visiblePublicGroupIds = new Set(publicGroupConfigs.value.map((c) => c.groupId))
    const blockedGroups = [
      // Keep bans for groups that are temporarily inactive and therefore not
      // present in this active-only editor; re-enabling a group must not
      // silently restore access.
      ...originalBlockedGroups.value.filter((id) => !visiblePublicGroupIds.has(id)),
      ...publicGroupConfigs.value.filter((c) => c.isBlocked).map((c) => c.groupId),
    ]

    // 构建 group_rates
    // - 有新专属倍率: 设置为该值
    // - 原本有专属倍率但现在被清空: 设置为 null（表示删除）
    const groupRates: Record<number, number | null> = {}
    for (const c of groupConfigs.value) {
      const hadOriginalRate = originalGroupRates.value[c.groupId] !== undefined

      if (c.customRate !== null) {
        // 有专属倍率
        groupRates[c.groupId] = c.customRate
      } else if (hadOriginalRate) {
        // 原本有专属倍率，现在被清空，需要显式删除
        groupRates[c.groupId] = null
      }
    }

    await adminAPI.users.update(props.user.id, {
      allowed_groups: allowedGroups,
      blocked_groups: [...new Set(blockedGroups)],
      group_rates: Object.keys(groupRates).length > 0 ? groupRates : undefined,
    })

    appStore.showSuccess(t('admin.users.groupConfigUpdated'))
    emit('success')
    emit('close')
  } catch (error) {
    console.error('Failed to update user group config:', error)
    appStore.showError(t('admin.users.failedToUpdate'))
  } finally {
    submitting.value = false
  }
}
</script>

<style scoped>
/* 隐藏数字输入框的箭头按钮 */
.hide-spinner::-webkit-outer-spin-button,
.hide-spinner::-webkit-inner-spin-button {
  -webkit-appearance: none;
  margin: 0;
}
.hide-spinner {
  -moz-appearance: textfield;
}
</style>
