<template>
  <div class="card flex min-w-0 flex-col overflow-visible p-0">
    <div class="ops-panel-head">
      <div class="flex w-full items-center justify-between gap-3">
        <div class="flex min-w-0 items-center gap-3">
          <span class="ops-panel-icon bg-blue-100 text-blue-600 dark:bg-blue-900/30 dark:text-blue-400"><Icon name="users" size="sm" :stroke-width="2" /></span>
          <div class="min-w-0">
            <h3 class="ops-panel-title">{{ t('admin.operations.userRanking') }}</h3>
            <p class="ops-panel-hint">{{ t('admin.operations.userRankingHint') }}</p>
          </div>
        </div>
        <span class="ops-count shrink-0">
          {{ search.trim() ? t('admin.operations.searchResults') : t('admin.operations.top25') }}
        </span>
      </div>
      <div class="relative w-full">
        <Icon name="search" size="sm" class="pointer-events-none absolute left-3.5 top-1/2 -translate-y-1/2 text-gray-400" />
        <input :value="search" type="search" class="input ops-search"
          :placeholder="t('admin.operations.searchEmail')" :aria-label="t('admin.operations.searchEmail')" role="combobox" aria-autocomplete="list"
          aria-controls="operations-user-suggestions" :aria-expanded="suggestionsOpen" data-testid="user-search"
          @input="$emit('update:search', ($event.target as HTMLInputElement).value)" @focus="$emit('focus-search')" @blur="$emit('blur-search')" @keydown.esc="$emit('dismiss-suggestions')" />
        <div v-if="suggestionsOpen && search.trim()" id="operations-user-suggestions" role="listbox" class="absolute left-0 right-0 top-full z-20 mt-1 max-h-56 overflow-y-auto ops-popover py-1" data-testid="user-suggestions">
          <p v-if="suggestionsLoading" class="flex items-center gap-2 px-3 py-2 text-xs text-gray-500" role="status"><LoadingSpinner size="sm" />{{ t('admin.operations.loading') }}</p>
          <button v-for="user in suggestions" :key="user.id" type="button" role="option" class="block w-full truncate px-3 py-2 text-left text-sm text-gray-700 hover:bg-primary-50 focus:bg-primary-50 dark:text-gray-200 dark:hover:bg-dark-700 dark:focus:bg-dark-700" :aria-selected="false" @mousedown.prevent @click="$emit('select-suggestion', user)">
            {{ user.email }}
          </button>
          <p v-if="!suggestionsLoading && !suggestions.length" class="px-3 py-2 text-xs text-gray-500">{{ t('admin.operations.noMatchingEmails') }}</p>
        </div>
      </div>
    </div>
    <div class="ops-table-head flex items-center justify-between px-4 py-2.5 sm:px-5">
      <span>{{ t('admin.operations.users') }}</span><span class="pr-6">{{ t('admin.operations.userCost') }}</span>
    </div>
    <div class="relative max-h-[440px] min-h-40 overflow-y-auto overscroll-contain" data-testid="users-scroll-region" :aria-busy="loading">
      <div v-if="loading" class="absolute inset-0 z-10 flex items-center justify-center bg-white/75 dark:bg-dark-800/75" role="status" data-testid="users-loading"><LoadingSpinner size="md" /><span class="sr-only">{{ t('admin.operations.loading') }}</span></div>
      <button v-for="(user, index) in users" :key="user.user_id" type="button" class="ops-row flex w-full items-center gap-3 px-4 py-2.5 text-left sm:px-5" :class="[selectedUserId === user.user_id ? 'ops-row-active' : 'ops-row-hover', loading && selectedUserId === user.user_id ? 'relative z-20' : '']" :aria-pressed="selectedUserId === user.user_id" :data-user="user.user_id" @click="$emit('select-user', user.user_id)">
        <span class="ops-rank" :class="search.trim() ? '' : rankClass(index)">{{ index + 1 }}</span>
        <span class="min-w-0 flex-1 truncate text-sm font-medium text-gray-800 dark:text-gray-200" :title="user.email">{{ user.email }}</span>
        <span class="shrink-0 text-right text-sm font-semibold tabular-nums text-gray-900 dark:text-white">{{ formatMoney(user.user_cost) }}</span>
        <LoadingSpinner v-if="loading && selectedUserId === user.user_id" size="sm" data-testid="selected-user-loading" />
        <Icon name="chevronRight" size="xs" class="shrink-0 text-gray-400" />
      </button>
      <div v-if="error" class="px-4 py-8 text-center text-sm text-gray-500 dark:text-gray-400">
        <p>{{ t('admin.operations.failedToLoadUsers') }}</p>
        <button type="button" class="mt-2 font-medium text-primary-600 hover:underline dark:text-primary-400" @click="$emit('retry')">{{ t('admin.operations.retry') }}</button>
      </div>
      <div v-else-if="!users.length && !loading" class="ops-empty">
        <Icon name="users" size="lg" class="text-primary-300" />
        <p>{{ t(search.trim() ? 'admin.operations.noMatchingUsers' : 'admin.operations.noUsers') }}</p>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type { OperationsUserSpendStat } from '@/api/admin/dashboard'
import type { SimpleUser } from '@/api/admin/usage'
import Icon from '@/components/icons/Icon.vue'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import { formatMoney } from '@/views/admin/operationsUtils'

defineProps<{
  users: OperationsUserSpendStat[]
  selectedUserId: number | null
  search: string
  suggestions: SimpleUser[]
  suggestionsOpen: boolean
  suggestionsLoading: boolean
  loading: boolean
  error: boolean
}>()
defineEmits<{
  'update:search': [value: string]
  'focus-search': []
  'blur-search': []
  'dismiss-suggestions': []
  'select-suggestion': [user: SimpleUser]
  'select-user': [userId: number]
  retry: []
}>()
const { t } = useI18n()
const rankClass = (index: number) => ['ops-rank-gold', 'ops-rank-silver', 'ops-rank-bronze'][index] ?? ''
</script>
