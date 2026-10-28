<template>
  <!-- Phones: two-column grid, with search and groups spanning both columns.
       From the sm breakpoint up: the original single wrapping row. -->
  <div class="account-filters" data-testid="account-filters">
    <SearchInput
      :model-value="searchQuery"
      :placeholder="t('admin.accounts.searchAccounts')"
      class="account-filters-search"
      @update:model-value="$emit('update:searchQuery', $event)"
      @search="$emit('change')"
    />
    <Select :model-value="filters.platform" class="account-filter" :options="pOpts" @update:model-value="updatePlatform" @change="$emit('change')" />
    <Select :model-value="filters.type" class="account-filter" :options="tOpts" @update:model-value="updateType" @change="$emit('change')" />
    <Select :model-value="filters.status" class="account-filter" :options="sOpts" @update:model-value="updateStatus" @change="$emit('change')" />
    <Select :model-value="filters.privacy_mode" class="account-filter" :options="privacyOpts" @update:model-value="updatePrivacyMode" @change="$emit('change')" />
    <Select
      :model-value="filters.group"
      class="account-filter account-filter-group"
      :options="gOpts"
      :dropdown-min-width="320"
      data-testid="account-group-filter"
      @update:model-value="updateGroup"
      @change="$emit('change')"
    >
      <template #selected="{ option }">
        <span class="account-group-value">
          <span class="account-group-value-name">{{ option?.label ?? t('common.selectOption') }}</span>
          <span v-if="option?.rate" class="account-group-rate" data-testid="account-group-selected-rate">{{ option.rate }}</span>
        </span>
      </template>
      <template #option="{ option, selected }">
        <span class="account-group-option" :title="option.label">
          <span v-if="option.platform" class="account-group-icon" :class="platformTone(option.platform)" aria-hidden="true">
            <PlatformIcon :platform="option.platform" size="md" />
          </span>
          <span class="account-group-name">{{ option.label }}</span>
          <span
            v-if="option.rate"
            class="account-group-rate"
            data-testid="account-group-rate"
            :title="`${t('admin.groups.columns.rateMultiplier')}: ${option.rate}`"
          >{{ option.rate }}</span>
          <Icon v-if="selected" name="check" size="sm" :stroke-width="2" class="account-group-check" />
        </span>
      </template>
    </Select>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'; import { useI18n } from 'vue-i18n'; import Select from '@/components/common/Select.vue'; import SearchInput from '@/components/common/SearchInput.vue'
import Icon from '@/components/icons/Icon.vue'
import PlatformIcon from '@/components/common/PlatformIcon.vue'
import type { AdminGroup } from '@/types'
const props = defineProps<{ searchQuery: string; filters: Record<string, any>; groups?: AdminGroup[] }>()
const emit = defineEmits(['update:searchQuery', 'update:filters', 'change']); const { t } = useI18n()
const updatePlatform = (value: string | number | boolean | null) => { emit('update:filters', { ...props.filters, platform: value }) }
const updateType = (value: string | number | boolean | null) => { emit('update:filters', { ...props.filters, type: value }) }
const updateStatus = (value: string | number | boolean | null) => { emit('update:filters', { ...props.filters, status: value }) }
const updatePrivacyMode = (value: string | number | boolean | null) => { emit('update:filters', { ...props.filters, privacy_mode: value }) }
const updateGroup = (value: string | number | boolean | null) => { emit('update:filters', { ...props.filters, group: value }) }
const pOpts = computed(() => [{ value: '', label: t('admin.accounts.allPlatforms') }, { value: 'anthropic', label: 'Anthropic' }, { value: 'openai', label: 'OpenAI' }, { value: 'gemini', label: 'Gemini' }, { value: 'grok', label: 'Grok' }, { value: 'custom', label: 'Custom' }])
const tOpts = computed(() => [{ value: '', label: t('admin.accounts.allTypes') }, { value: 'oauth', label: t('admin.accounts.oauthType') }, { value: 'setup-token', label: t('admin.accounts.setupToken') }, { value: 'apikey', label: t('admin.accounts.apiKey') }, { value: 'bedrock', label: 'AWS Bedrock' }])
const sOpts = computed(() => [{ value: '', label: t('admin.accounts.allStatus') }, { value: 'active', label: t('admin.accounts.status.active') }, { value: 'inactive', label: t('admin.accounts.status.inactive') }, { value: 'error', label: t('admin.accounts.status.error') }, { value: 'rate_limited', label: t('admin.accounts.status.rateLimited') }, { value: 'temp_unschedulable', label: t('admin.accounts.status.tempUnschedulable') }, { value: 'unschedulable', label: t('admin.accounts.status.unschedulable') }])
const privacyOpts = computed(() => [
  { value: '', label: t('admin.accounts.allPrivacyModes') },
  { value: '__unset__', label: t('admin.accounts.privacyUnset') },
  { value: 'training_off', label: 'Privacy' },
  { value: 'training_set_cf_blocked', label: 'CF' },
  { value: 'training_set_failed', label: 'Fail' }
])
// Group rates use the console's "0.5x" notation; toFixed trims float noise.
const formatGroupRate = (rate: unknown): string =>
  typeof rate === 'number' && Number.isFinite(rate) && rate >= 0 ? `${Number(rate.toFixed(4))}x` : ''
const platformTones: Record<string, string> = {
  anthropic: 'tone-anthropic', openai: 'tone-openai', gemini: 'tone-gemini', grok: 'tone-grok',
  custom: 'tone-custom', composite: 'tone-custom', antigravity: 'tone-antigravity'
}
const platformTone = (platform: string) => platformTones[platform] ?? 'tone-custom'
const gOpts = computed(() => [
  { value: '', label: t('admin.accounts.allGroups') },
  { value: 'ungrouped', label: t('admin.accounts.ungroupedGroup') },
  ...(props.groups || []).map(g => ({ value: String(g.id), label: g.name, platform: g.platform, rate: formatGroupRate(g.rate_multiplier) }))
])
</script>

<style scoped>
.account-filters {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 0.75rem;
  width: 100%;
}

.account-filters-search,
.account-filter-group {
  grid-column: 1 / -1;
}

.account-filter {
  min-width: 0;
}

@media (min-width: 640px) {
  .account-filters {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    width: auto;
  }

  .account-filters-search {
    width: 16rem;
  }

  .account-filter {
    width: 10rem;
  }

  /* The desktop trigger is too narrow for a tag; the list still shows it. */
  .account-group-value .account-group-rate {
    display: none;
  }
}

@media (max-width: 639.98px) {
  /* Half-width selects: trim the side padding so labels such as
     "全部Privacy状态" still fit on a 360px screen. */
  .account-filter :deep(.select-trigger) {
    gap: 0.375rem;
    padding-inline: 0.75rem;
  }
}

/* Trigger: the selected group's name and rate. */
.account-group-value {
  display: flex;
  min-width: 0;
  align-items: center;
  gap: 0.5rem;
}

.account-group-value-name {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

/* List rows: platform icon, name (up to two lines), rate tag, checkmark.
   The width cap bounds the auto-sized menu: 22rem on wide screens, and on
   phones the trigger's width (viewport minus page and row padding), so long
   names wrap instead of widening the menu. */
.account-group-option {
  display: flex;
  flex: 1 1 auto;
  min-width: 0;
  max-width: min(22rem, calc(100vw - 4rem));
  align-items: center;
  gap: 0.625rem;
}

.account-group-icon {
  display: inline-flex;
  flex-shrink: 0;
  align-items: center;
  justify-content: center;
  width: 1.75rem;
  height: 1.75rem;
  border-radius: 0.625rem;
}

.account-group-name {
  display: -webkit-box;
  flex: 1 1 auto;
  min-width: 0;
  overflow: hidden;
  -webkit-box-orient: vertical;
  -webkit-line-clamp: 2;
  line-clamp: 2;
  line-height: 1.35;
  overflow-wrap: anywhere;
  white-space: normal;
}

.account-group-rate {
  display: inline-flex;
  flex-shrink: 0;
  align-items: center;
  padding: 0.0625rem 0.5rem;
  border: 1px solid rgb(233 184 36 / 0.45);
  border-radius: 999px;
  background: #fff3bd;
  color: #7e5600;
  font-size: 0.6875rem;
  font-weight: 700;
  line-height: 1.125rem;
  font-variant-numeric: tabular-nums;
  white-space: nowrap;
}

.account-group-check {
  flex-shrink: 0;
  color: #976800;
}

.tone-anthropic {
  background: #fdecdf;
  color: #c2410c;
}

.tone-openai {
  background: #e4f0df;
  color: #2e6b45;
}

.tone-gemini {
  background: #e0f0fb;
  color: #1f5f8b;
}

.tone-grok {
  background: #ececec;
  color: #27272a;
}

.tone-antigravity {
  background: #f1e8fb;
  color: #6b3fa0;
}

.tone-custom {
  background: #faf6e8;
  color: #5b5238;
}
</style>
