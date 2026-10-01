<template>
  <div class="space-y-3" data-test="failover-settings">
    <div class="flex items-center justify-between gap-3">
      <label :for="`${id}-enabled`" class="input-label mb-0">{{ t('keys.failover.enable') }}</label>
      <button
        :id="`${id}-enabled`"
        type="button"
        role="switch"
        :aria-checked="modelValue.failover_enabled"
        :aria-label="t('keys.failover.enable')"
        :disabled="disabled"
        data-test="failover-toggle"
        :class="[
          'relative inline-flex h-5 w-9 flex-shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors focus:outline-none focus-visible:ring-2 focus-visible:ring-primary-500 disabled:cursor-not-allowed disabled:opacity-50',
          modelValue.failover_enabled ? 'bg-primary-600' : 'bg-gray-200 dark:bg-dark-600'
        ]"
        @click="update({ failover_enabled: !modelValue.failover_enabled })"
      >
        <span
          :class="[
            'pointer-events-none inline-block h-4 w-4 rounded-full bg-white shadow transition-transform',
            modelValue.failover_enabled ? 'translate-x-4' : 'translate-x-0'
          ]"
        />
      </button>
    </div>

    <div
      v-if="modelValue.failover_enabled"
      class="failover-controls space-y-4 rounded-lg border p-4"
      data-test="failover-controls"
    >
      <p class="failover-hint text-xs leading-5">
        {{ t('keys.failover.supportedPlatforms') }}
      </p>
      <div>
        <label :for="`${id}-group`" class="input-label">{{ t('keys.failover.group') }}</label>
        <Select
          :id="`${id}-group`"
          :model-value="modelValue.failover_group_id"
          :options="groupOptions"
          :disabled="disabled"
          centered
          :placeholder="t('keys.selectGroup')"
          :searchable="true"
          :search-placeholder="t('keys.searchGroup')"
          :aria-label="t('keys.failover.group')"
          data-test="failover-group"
          @update:model-value="update({ failover_group_id: $event as number | null })"
        >
          <template #selected="{ option }">
            <GroupBadge
              v-if="option"
              :name="(option as GroupOption).label"
              :platform="(option as GroupOption).platform"
              :subscription-type="(option as GroupOption).subscriptionType"
              :rate-multiplier="(option as GroupOption).rate"
              :user-rate-multiplier="(option as GroupOption).userRate"
              :peak-rate-enabled="(option as GroupOption).peakRateEnabled"
              :peak-start="(option as GroupOption).peakStart"
              :peak-end="(option as GroupOption).peakEnd"
              :peak-rate-multiplier="(option as GroupOption).peakRateMultiplier"
            />
            <span v-else class="text-gray-400">{{ t('keys.selectGroup') }}</span>
          </template>
          <template #option="{ option, selected }">
            <GroupOptionItem
              :name="(option as GroupOption).label"
              :platform="(option as GroupOption).platform"
              :subscription-type="(option as GroupOption).subscriptionType"
              :rate-multiplier="(option as GroupOption).rate"
              :user-rate-multiplier="(option as GroupOption).userRate"
              :peak-rate-enabled="(option as GroupOption).peakRateEnabled"
              :peak-start="(option as GroupOption).peakStart"
              :peak-end="(option as GroupOption).peakEnd"
              :peak-rate-multiplier="(option as GroupOption).peakRateMultiplier"
              :description="(option as GroupOption).description"
              :selected="selected"
            />
          </template>
        </Select>
      </div>

      <div>
        <label :for="`${id}-attempts`" class="input-label">{{ t('keys.failover.maxRetries') }}</label>
        <input
          :id="`${id}-attempts`"
          :value="modelValue.failover_max_retries"
          :disabled="disabled"
          type="number"
          min="1"
          max="10"
          step="1"
          required
          class="input"
          data-test="failover-max-retries"
          @input="update({ failover_max_retries: ($event.target as HTMLInputElement).valueAsNumber })"
        />
      </div>

      <div>
        <label :for="`${id}-cooldown`" class="input-label">{{ t('keys.failover.cooldownSeconds') }}</label>
        <input
          :id="`${id}-cooldown`"
          :value="modelValue.failover_cooldown_seconds"
          :disabled="disabled"
          type="number"
          min="1"
          max="2147483647"
          step="1"
          required
          class="input"
          data-test="failover-cooldown"
          @input="update({ failover_cooldown_seconds: ($event.target as HTMLInputElement).valueAsNumber })"
        />
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Select from '@/components/common/Select.vue'
import GroupBadge from '@/components/common/GroupBadge.vue'
import GroupOptionItem from '@/components/common/GroupOptionItem.vue'
import type { ApiKeyFailoverSettings, Group } from '@/types'

const props = withDefaults(defineProps<{
  id: string
  modelValue: ApiKeyFailoverSettings
  primaryGroup?: Group
  groups: Group[]
  userGroupRates: Record<number, number>
  disabled?: boolean
}>(), { disabled: false })

const emit = defineEmits<{
  'update:modelValue': [settings: ApiKeyFailoverSettings]
}>()
const { t } = useI18n()

const eligibleGroups = computed(() => props.groups.filter((group) =>
  props.primaryGroup?.platform !== 'custom' && props.primaryGroup?.platform !== 'composite' &&
  props.primaryGroup && group.platform === props.primaryGroup.platform &&
  group.id !== props.primaryGroup.id && group.status === 'active' && !group.is_blocked_for_user
))

const groupOptions = computed(() => eligibleGroups.value.map((group) => ({
  value: group.id,
  label: group.name,
  description: group.description,
  platform: group.platform,
  subscriptionType: group.subscription_type,
  rate: group.rate_multiplier,
  userRate: props.userGroupRates[group.id] ?? null,
  peakRateEnabled: group.peak_rate_enabled,
  peakStart: group.peak_start,
  peakEnd: group.peak_end,
  peakRateMultiplier: group.peak_rate_multiplier
})))
type GroupOption = typeof groupOptions.value[number]

const update = (changes: Partial<ApiKeyFailoverSettings>) => {
  emit('update:modelValue', { ...props.modelValue, ...changes })
}

watch(() => props.primaryGroup?.id, (id, previousId) => {
  if (previousId === undefined || id === previousId || props.modelValue.failover_group_id === null) return
  if (!eligibleGroups.value.some((group) => group.id === props.modelValue.failover_group_id)) {
    update({ failover_enabled: false, failover_group_id: null })
  }
})
</script>

<style scoped>
.failover-controls {
  border-color: color-mix(in srgb, var(--md-sys-color-outline) 65%, var(--md-sys-color-surface));
  background: var(--md-sys-color-surface-container);
  color: var(--md-sys-color-on-surface);
}

.failover-hint {
  color: var(--md-sys-color-on-surface-variant);
}

.failover-controls .input-label {
  color: var(--md-sys-color-on-surface);
}

.failover-controls .input,
.failover-controls :deep(.select-trigger) {
  background: var(--md-sys-color-surface);
}
</style>
