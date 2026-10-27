<template>
  <div class="card min-w-0 overflow-visible p-0">
    <div class="border-b border-gray-100 px-4 py-4 dark:border-dark-700">
      <div class="flex items-center justify-between gap-3">
        <div>
          <h3 class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('admin.operations.userRanking') }}</h3>
          <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.operations.userRankingHint') }}</p>
        </div>
        <span class="shrink-0 rounded-full bg-primary-50 px-2.5 py-1 text-xs font-medium text-primary-600 dark:bg-primary-900/20 dark:text-primary-400">
          {{ search.trim() ? t('admin.operations.searchResults') : t('admin.operations.top25') }}
        </span>
      </div>
      <div class="relative mt-4">
        <Icon name="search" size="sm" class="pointer-events-none absolute left-3 top-2.5 text-gray-400" />
        <input :value="search" type="search" class="w-full rounded-lg border border-gray-200 bg-white py-2 pl-9 pr-3 text-sm text-gray-900 outline-none transition-colors placeholder:text-gray-400 focus:border-primary-400 focus:ring-2 focus:ring-primary-500/10 dark:border-dark-600 dark:bg-dark-800 dark:text-white"
          :placeholder="t('admin.operations.searchEmail')" :aria-label="t('admin.operations.searchEmail')" role="combobox" aria-autocomplete="list"
          aria-controls="operations-user-suggestions" :aria-expanded="suggestionsOpen" data-testid="user-search"
          @input="$emit('update:search', ($event.target as HTMLInputElement).value)" @focus="$emit('focus-search')" @blur="$emit('blur-search')" @keydown.esc="$emit('dismiss-suggestions')" />
        <div v-if="suggestionsOpen && search.trim()" id="operations-user-suggestions" role="listbox" class="absolute left-0 right-0 top-full z-20 mt-1 max-h-56 overflow-y-auto rounded-lg border border-gray-200 bg-white py-1 shadow-lg dark:border-dark-600 dark:bg-dark-800" data-testid="user-suggestions">
          <p v-if="suggestionsLoading" class="flex items-center gap-2 px-3 py-2 text-xs text-gray-500" role="status"><LoadingSpinner size="sm" />{{ t('admin.operations.loading') }}</p>
          <button v-for="user in suggestions" :key="user.id" type="button" role="option" class="block w-full truncate px-3 py-2 text-left text-sm text-gray-700 hover:bg-primary-50 focus:bg-primary-50 dark:text-gray-200 dark:hover:bg-dark-700 dark:focus:bg-dark-700" :aria-selected="false" @mousedown.prevent @click="$emit('select-suggestion', user)">
            {{ user.email }}
          </button>
          <p v-if="!suggestionsLoading && !suggestions.length" class="px-3 py-2 text-xs text-gray-500">{{ t('admin.operations.noMatchingEmails') }}</p>
        </div>
      </div>
    </div>
    <div class="flex items-center justify-between border-b border-gray-100 bg-gray-50/80 px-4 py-2.5 text-xs font-medium text-gray-500 dark:border-dark-700 dark:bg-dark-800/40 dark:text-gray-400">
      <span>{{ t('admin.operations.users') }}</span><span>{{ t('admin.operations.userCost') }}</span>
    </div>
    <div class="relative max-h-[440px] min-h-40 overflow-y-auto overscroll-contain" data-testid="users-scroll-region" :aria-busy="loading">
      <div v-if="loading" class="absolute inset-0 z-10 flex items-center justify-center bg-white/75 dark:bg-dark-800/75" role="status" data-testid="users-loading"><LoadingSpinner size="md" /><span class="sr-only">{{ t('admin.operations.loading') }}</span></div>
      <button v-for="(user, index) in users" :key="user.user_id" type="button" class="flex w-full items-center gap-3 border-b border-gray-100 px-4 py-3 text-left transition-colors last:border-b-0 dark:border-dark-700/70" :class="[selectedUserId === user.user_id ? 'bg-primary-50/80 dark:bg-primary-900/20' : 'hover:bg-gray-50 dark:hover:bg-dark-800/50', loading && selectedUserId === user.user_id ? 'relative z-20' : '']" :aria-pressed="selectedUserId === user.user_id" :data-user="user.user_id" @click="$emit('select-user', user.user_id)">
        <span class="w-6 shrink-0 text-center text-xs font-semibold tabular-nums text-gray-400">{{ index + 1 }}</span>
        <span class="flex h-8 w-8 shrink-0 items-center justify-center rounded-full bg-primary-50 text-primary-600 dark:bg-primary-900/25 dark:text-primary-400"><Icon name="user" size="sm" /></span>
        <span class="min-w-0 flex-1 truncate text-sm font-medium text-gray-800 dark:text-gray-200" :title="user.email">{{ user.email }}</span>
        <span class="shrink-0 text-right text-sm font-semibold tabular-nums text-gray-900 dark:text-white">{{ formatMoney(user.user_cost) }}</span>
        <LoadingSpinner v-if="loading && selectedUserId === user.user_id" size="sm" data-testid="selected-user-loading" />
        <Icon name="chevronRight" size="xs" class="shrink-0 text-gray-400" />
      </button>
      <div v-if="error" class="px-4 py-8 text-center text-sm text-gray-500 dark:text-gray-400">
        <p>{{ t('admin.operations.failedToLoadUsers') }}</p>
        <button type="button" class="mt-2 font-medium text-primary-600 hover:underline dark:text-primary-400" @click="$emit('retry')">{{ t('admin.operations.retry') }}</button>
      </div>
      <p v-else-if="!users.length && !loading" class="px-4 py-12 text-center text-sm text-gray-400">{{ t(search.trim() ? 'admin.operations.noMatchingUsers' : 'admin.operations.noUsers') }}</p>
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
</script>
