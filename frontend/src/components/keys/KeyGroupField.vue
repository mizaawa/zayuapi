<template>
  <button type="button" class="key-group-field select-trigger min-h-11" aria-haspopup="dialog" :aria-expanded="open">
    <span class="flex min-w-0 flex-1 flex-wrap items-center gap-1.5">
      <GroupBadge v-if="group" class="max-w-full" :name="group.name" :platform="group.platform"
        :subscription-type="group.subscription_type" :rate-multiplier="group.rate_multiplier" :user-rate-multiplier="userRate"
        :peak-rate-enabled="group.peak_rate_enabled" :peak-start="group.peak_start" :peak-end="group.peak_end"
        :peak-rate-multiplier="group.peak_rate_multiplier" />
      <span v-else class="text-[var(--md-sys-color-placeholder)]">{{ t('keys.selectGroup') }}</span>
      <span v-if="blocked" class="inline-flex shrink-0 items-center rounded-full bg-red-100 px-2 py-0.5 text-[10px] font-semibold text-red-700">
        {{ t('common.groupBlocked') }}
      </span>
    </span>
    <Icon name="chevronDown" size="sm" class="select-icon" aria-hidden="true" />
  </button>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import GroupBadge from '@/components/common/GroupBadge.vue'
import Icon from '@/components/icons/Icon.vue'
import type { Group } from '@/types'

defineProps<{ group?: Group; userRate?: number | null; open: boolean; blocked?: boolean }>()
const { t } = useI18n()
</script>

<style scoped>
.key-group-field:hover { background: color-mix(in srgb, var(--md-sys-color-surface-container) 82%, var(--md-sys-color-surface-container-high)); }
.key-group-field:is(:focus-visible, [aria-expanded='true']) { background: var(--md-sys-color-surface); }
.key-group-field .select-icon { color: var(--md-sys-color-on-surface-variant); }
</style>
