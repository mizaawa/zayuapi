<template>
  <!-- Header row: selection summary on the left, "update filtered results" on
       the right. Selection actions get a row of their own below it: one
       wrapping line on wide screens, two columns on phones. Labels never wrap,
       so a narrow bar wraps whole buttons instead of squeezing them. -->
  <section
    class="account-bulk-bar bg-primary-50 dark:bg-primary-900/20"
    :aria-busy="busy"
    data-testid="account-bulk-actions"
  >
    <div class="account-bulk-summary">
      <span v-if="allResultsSelected" class="account-bulk-count">
        {{ t('admin.accounts.bulkActions.selectedAll', { count: selectedIds.length }) }}
      </span>
      <span v-else-if="hasSelection" class="account-bulk-count">
        {{ t('admin.accounts.bulkActions.selected', { count: selectedIds.length }) }}
      </span>
      <span v-else class="account-bulk-count">
        {{ t('admin.accounts.bulkEdit.title') }}
      </span>
      <template v-if="hasSelection">
        <button type="button" class="account-bulk-link" @click="$emit('select-page')">
          {{ t('admin.accounts.bulkActions.selectCurrentPage') }}
        </button>
      </template>
      <template v-if="!allResultsSelected && totalResults > selectedIds.length">
        <span v-if="hasSelection" class="account-bulk-dot" aria-hidden="true">•</span>
        <button
          type="button"
          class="account-bulk-link"
          :disabled="selectingAll"
          @click="$emit('select-all-results')"
        >
          {{
            selectingAll
              ? t('admin.accounts.bulkActions.selectingAll')
              : t('admin.accounts.bulkActions.selectAllResults', { count: totalResults })
          }}
        </button>
      </template>
      <template v-if="hasSelection">
        <span class="account-bulk-dot" aria-hidden="true">•</span>
        <button type="button" class="account-bulk-link" :disabled="busy" @click="$emit('clear')">
          {{ t('admin.accounts.bulkActions.clear') }}
        </button>
      </template>
    </div>

    <button
      type="button"
      class="btn btn-sm account-bulk-action account-bulk-filtered"
      :class="hasSelection ? 'btn-secondary' : 'btn-primary'"
      :disabled="busy"
      data-testid="account-bulk-edit-filtered"
      @click="$emit('edit-filtered')"
    >
      <Icon name="filter" size="sm" />
      <span>{{ t('admin.accounts.bulkEdit.submit') }}</span>
    </button>

    <div v-if="hasSelection" class="account-bulk-actions">
      <button
        v-for="action in selectionActions"
        :key="action.key"
        type="button"
        class="btn btn-sm account-bulk-action"
        :class="action.buttonClass"
        :disabled="busy"
        :aria-busy="busyAction === action.key"
        :data-testid="`account-bulk-${action.key}`"
        @click="action.run()"
      >
        <Icon
          :name="busyAction === action.key ? 'refresh' : action.icon"
          size="sm"
          :class="busyAction === action.key ? 'animate-spin' : action.iconClass"
        />
        <span>{{ busyAction === action.key ? t('common.processing') : action.label }}</span>
      </button>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'

/** Bulk actions the parent can report as running. */
export type AccountBulkActionKey =
  | 'edit'
  | 'enable-scheduling'
  | 'disable-scheduling'
  | 'reset-status'
  | 'refresh-token'
  | 'probe-upstream-billing'
  | 'delete'

const props = withDefaults(defineProps<{
  selectedIds: number[]
  totalResults: number
  selectingAll: boolean
  allResultsSelected: boolean
  /** The bulk action still waiting for the server; every action is disabled meanwhile. */
  busyAction?: AccountBulkActionKey | null
}>(), {
  busyAction: null
})

const emit = defineEmits([
  'delete',
  'edit-selected',
  'edit-filtered',
  'clear',
  'select-page',
  'select-all-results',
  'toggle-schedulable',
  'reset-status',
  'refresh-token',
  'probe-upstream-billing'
])

const { t } = useI18n()

const hasSelection = computed(() => props.selectedIds.length > 0)
const busy = computed(() => props.busyAction != null)

type SelectionAction = {
  key: AccountBulkActionKey
  icon: 'edit' | 'play' | 'ban' | 'sync' | 'key' | 'trendingUp' | 'trash'
  label: string
  buttonClass: string
  iconClass?: string
  run: () => void
}

// Most-used first, the destructive one last.
const selectionActions = computed<SelectionAction[]>(() => [
  {
    key: 'edit',
    icon: 'edit',
    label: t('admin.accounts.bulkActions.edit'),
    buttonClass: 'btn-primary',
    run: () => emit('edit-selected')
  },
  {
    key: 'enable-scheduling',
    icon: 'play',
    label: t('admin.accounts.bulkActions.enableScheduling'),
    buttonClass: 'btn-secondary',
    iconClass: 'text-emerald-600 dark:text-emerald-400',
    run: () => emit('toggle-schedulable', true)
  },
  {
    key: 'disable-scheduling',
    icon: 'ban',
    label: t('admin.accounts.bulkActions.disableScheduling'),
    buttonClass: 'btn-secondary',
    iconClass: 'text-amber-600 dark:text-amber-400',
    run: () => emit('toggle-schedulable', false)
  },
  {
    key: 'reset-status',
    icon: 'sync',
    label: t('admin.accounts.bulkActions.resetStatus'),
    buttonClass: 'btn-secondary',
    iconClass: 'text-sky-600 dark:text-sky-400',
    run: () => emit('reset-status')
  },
  {
    key: 'refresh-token',
    icon: 'key',
    label: t('admin.accounts.bulkActions.refreshToken'),
    buttonClass: 'btn-secondary',
    iconClass: 'text-violet-600 dark:text-violet-400',
    run: () => emit('refresh-token')
  },
  {
    key: 'probe-upstream-billing',
    icon: 'trendingUp',
    label: t('admin.accounts.bulkActions.probeUpstreamBilling'),
    buttonClass: 'btn-secondary',
    iconClass: 'text-teal-600 dark:text-teal-400',
    run: () => emit('probe-upstream-billing')
  },
  {
    key: 'delete',
    icon: 'trash',
    label: t('admin.accounts.bulkActions.delete'),
    buttonClass: 'btn-secondary account-bulk-action-danger',
    run: () => emit('delete')
  }
])
</script>

<style scoped>
.account-bulk-bar {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  align-items: center;
  gap: 0.75rem 1rem;
  margin-bottom: 1rem;
  padding: 0.75rem;
  border-radius: 0.75rem;
}

.account-bulk-summary {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 0.25rem 0.5rem;
  min-width: 0;
}

.account-bulk-count {
  @apply text-sm font-medium text-primary-900 dark:text-primary-100;
}

.account-bulk-link {
  @apply whitespace-nowrap text-xs font-medium text-primary-700 hover:text-primary-800 disabled:cursor-not-allowed disabled:opacity-60 dark:text-primary-300 dark:hover:text-primary-200;
}

.account-bulk-dot {
  @apply text-gray-300 dark:text-primary-800;
}

.account-bulk-actions {
  display: flex;
  grid-column: 1 / -1;
  flex-wrap: wrap;
  align-items: center;
  gap: 0.5rem;
}

.account-bulk-action {
  flex-shrink: 0;
  gap: 0.375rem;
  border: 1px solid transparent;
  white-space: nowrap;
}

.account-bulk-filtered {
  justify-self: end;
}

.account-bulk-action-danger {
  @apply text-red-700 hover:bg-red-50 dark:text-red-300 dark:hover:bg-red-900/30;
}

/* Phones: the primary action spans the row, the rest pair up below it. */
@media (max-width: 639px) {
  .account-bulk-actions {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }

  .account-bulk-actions > :first-child {
    grid-column: 1 / -1;
  }

  .account-bulk-action {
    min-width: 0;
    min-height: 2.5rem;
    font-size: 0.8125rem;
  }

  .account-bulk-actions > .account-bulk-action {
    width: 100%;
  }

  .account-bulk-action > span {
    overflow: hidden;
    text-overflow: ellipsis;
  }
}
</style>
