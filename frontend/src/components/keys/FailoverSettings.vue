<template>
  <KeyFormSection
    :id="id" icon="swap" :title="t('keys.failover.enable')" :hint="t('keys.editor.failoverHint')"
    :enabled="modelValue.failover_enabled" :disabled="disabled" toggle-test="failover-toggle" data-test="failover-settings"
    @update:enabled="update({ failover_enabled: $event })"
  >
    <div class="grid min-w-0 grid-cols-1 gap-3" data-test="failover-controls">
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
              v-if="option" class="max-w-full"
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
            <span v-else class="text-[var(--md-sys-color-placeholder)]">{{ t('keys.selectGroup') }}</span>
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
      <div class="key-paired-fields grid grid-cols-1 gap-3">
        <div>
          <label :for="`${id}-attempts`" class="input-label">{{ t('keys.failover.maxRetries') }}</label>
          <input :id="`${id}-attempts`" :value="modelValue.failover_max_retries" :disabled="disabled"
            type="number" min="1" max="10" step="1" required class="input" data-test="failover-max-retries"
            @input="update({ failover_max_retries: ($event.target as HTMLInputElement).valueAsNumber })" />
        </div>
        <div>
          <label :for="`${id}-cooldown`" class="input-label">{{ t('keys.failover.cooldownSeconds') }}</label>
          <input :id="`${id}-cooldown`" :value="modelValue.failover_cooldown_seconds" :disabled="disabled"
            type="number" min="1" max="2147483647" step="1" required class="input" data-test="failover-cooldown"
            @input="update({ failover_cooldown_seconds: ($event.target as HTMLInputElement).valueAsNumber })" />
        </div>
      </div>
      <p class="key-editor-note text-xs leading-5">
        <Icon name="infoCircle" size="sm" aria-hidden="true" />{{ t('keys.failover.supportedPlatforms') }}
      </p>
    </div>
  </KeyFormSection>
</template>

<script setup lang="ts">
import { computed, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import KeyFormSection from './KeyFormSection.vue'
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
