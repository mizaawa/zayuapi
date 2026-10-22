<template>
  <section class="card" data-test="payment-permissions">
    <div class="border-b border-gray-100 px-4 py-4 dark:border-dark-700 sm:px-6">
      <h2 class="text-base font-semibold text-gray-900 dark:text-white sm:text-lg">
        {{ t('admin.settings.payment.permissions.title') }}
      </h2>
    </div>
    <div class="space-y-4 p-4 sm:p-6">
      <div class="flex items-start justify-between gap-4">
        <div class="min-w-0">
          <label id="exclusive-payment-label" class="font-medium text-gray-900 dark:text-white">
            {{ t('admin.settings.payment.permissions.exclusive') }}
          </label>
          <p id="exclusive-payment-hint" class="mt-1 text-xs leading-5 text-gray-500 dark:text-gray-400">
            {{ t('admin.settings.payment.permissions.exclusiveHint') }}
          </p>
        </div>
        <Toggle
          class="mt-0.5 shrink-0"
          :model-value="enabled"
          :disabled="configLoading || saving || !configLoaded"
          aria-labelledby="exclusive-payment-label"
          aria-describedby="exclusive-payment-hint"
          data-test="exclusive-payment-toggle"
          @update:model-value="updateEnabled"
        />
      </div>
      <div v-if="configError" class="flex flex-col items-start gap-3 sm:flex-row sm:items-center sm:justify-between" role="alert">
        <p class="text-sm text-red-600 dark:text-red-400">{{ configError }}</p>
        <button type="button" class="btn btn-secondary btn-sm" :disabled="configLoading" @click="loadConfig">
          <Icon name="refresh" size="sm" />
          {{ t('common.refresh') }}
        </button>
      </div>
      <template v-if="enabled && configLoaded">
        <div class="flex min-w-0 items-center gap-2 sm:gap-3">
          <SearchInput
            class="min-w-0 flex-1"
            v-model="search"
            :placeholder="t('admin.settings.payment.permissions.search')"
            @search="applySearch"
            @keydown.enter.prevent="applySearch(search)"
          />
          <button
            type="button"
            class="btn btn-secondary shrink-0"
            :disabled="usersLoading"
            :title="t('common.refresh')"
            :aria-label="t('common.refresh')"
            @click="loadUsers"
          >
            <Icon name="refresh" size="md" :class="{ 'animate-spin': usersLoading }" />
          </button>
        </div>
        <p v-if="usersError" class="text-sm text-red-600 dark:text-red-400" role="alert">{{ usersError }}</p>
        <DataTable :columns="columns" :data="users" :loading="usersLoading">
          <template #cell-email="{ row }">
            <span class="break-all">{{ row.email }}</span>
          </template>
          <template #cell-notes="{ row }">
            <span class="whitespace-pre-wrap break-words">{{ row.notes || '-' }}</span>
          </template>
          <template #cell-balance="{ row }">{{ formatCurrency(row.balance) }}</template>
          <template #cell-actions="{ row }">
            <button
              type="button"
              class="btn btn-danger btn-sm w-full sm:w-auto"
              :disabled="removingUserId !== null || saving"
              :aria-label="`${t('admin.settings.payment.permissions.remove')} ${row.email}`"
              @click="removePermission(row.id)"
            >
              {{ t('admin.settings.payment.permissions.remove') }}
            </button>
          </template>
          <template #empty>
            <p class="text-sm text-gray-500 dark:text-gray-400">
              {{ t(appliedSearch ? 'admin.settings.payment.permissions.noResults' : 'admin.settings.payment.permissions.empty') }}
            </p>
          </template>
        </DataTable>
        <Pagination
          v-if="total > pageSize"
          :total="total"
          :page="page"
          :page-size="pageSize"
          :show-page-size-selector="false"
          @update:page="changePage"
          @click.prevent
        />
      </template>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useEventListener } from '@vueuse/core'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import { useAppStore } from '@/stores/app'
import type { AdminUser } from '@/types'
import type { Column } from '@/components/common/types'
import DataTable from '@/components/common/DataTable.vue'
import Pagination from '@/components/common/Pagination.vue'
import SearchInput from '@/components/common/SearchInput.vue'
import Toggle from '@/components/common/Toggle.vue'
import Icon from '@/components/icons/Icon.vue'
import { extractApiErrorMessage } from '@/utils/apiError'
import { formatCurrency } from '@/utils/format'

const props = defineProps<{ active: boolean }>()
const { t } = useI18n()
const appStore = useAppStore()
const pageSize = 10
const enabled = ref(false)
const configLoaded = ref(false)
const configLoading = ref(false)
const configError = ref('')
const saving = ref(false)
const usersLoading = ref(false)
const usersError = ref('')
const removingUserId = ref<number | null>(null)
const search = ref('')
const appliedSearch = ref('')
const page = ref(1)
const total = ref(0)
const users = ref<AdminUser[]>([])
let listRequest = 0
let listController: AbortController | undefined
let configRequest = 0

const columns = computed<Column[]>(() => [
  { key: 'email', label: t('admin.settings.payment.permissions.email') },
  { key: 'notes', label: t('admin.users.notes') },
  { key: 'balance', label: t('admin.users.columns.balance') },
  { key: 'concurrency', label: t('admin.settings.payment.permissions.concurrency') },
  { key: 'actions', label: t('admin.users.columns.actions') },
])

function cancelListRequest() {
  listRequest++
  listController?.abort()
  usersLoading.value = false
}

async function loadConfig() {
  const request = ++configRequest
  configLoading.value = true
  configError.value = ''
  try {
    const { data } = await adminAPI.payment.getConfig()
    if (request !== configRequest) return
    enabled.value = data.payment_exclusive_enabled === true
    configLoaded.value = true
    if (enabled.value) await loadUsers()
    else cancelListRequest()
  } catch (error) {
    if (request !== configRequest) return
    configLoaded.value = false
    configError.value = extractApiErrorMessage(error, t('admin.settings.payment.permissions.loadFailed'))
  } finally {
    if (request === configRequest) configLoading.value = false
  }
}

async function updateEnabled(value: boolean) {
  if (saving.value || configLoading.value || !configLoaded.value) return
  saving.value = true
  try {
    await adminAPI.payment.updateConfig({ payment_exclusive_enabled: value })
    enabled.value = value
    page.value = 1
    if (value) await loadUsers()
    else cancelListRequest()
    window.dispatchEvent(new Event('payment-access-changed'))
    appStore.showSuccess(t('admin.settings.payment.permissions.updated'))
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('admin.settings.payment.permissions.updateFailed')))
  } finally {
    saving.value = false
  }
}

async function loadUsers() {
  if (!enabled.value || !props.active) return
  const request = ++listRequest
  listController?.abort()
  listController = new AbortController()
  usersLoading.value = true
  usersError.value = ''
  try {
    const result = await adminAPI.users.list(page.value, pageSize, {
      exclusive_recharge_enabled: true,
      email: appliedSearch.value,
    }, { signal: listController.signal })
    if (request !== listRequest) return
    total.value = result.total
    const lastPage = Math.max(1, Math.ceil(result.total / pageSize))
    if (page.value > lastPage) {
      page.value = lastPage
      await loadUsers()
      return
    }
    users.value = result.items
  } catch (error) {
    if (request !== listRequest) return
    users.value = []
    total.value = 0
    usersError.value = extractApiErrorMessage(error, t('admin.settings.payment.permissions.loadFailed'))
  } finally {
    if (request === listRequest) usersLoading.value = false
  }
}

function applySearch(value: string) {
  appliedSearch.value = value.trim()
  page.value = 1
  void loadUsers()
}

function changePage(value: number) {
  page.value = value
  void loadUsers()
}

async function removePermission(userId: number) {
  if (removingUserId.value !== null || saving.value) return
  removingUserId.value = userId
  try {
    await adminAPI.users.update(userId, { exclusive_recharge_enabled: false })
    window.dispatchEvent(new Event('payment-access-changed'))
    await loadUsers()
    appStore.showSuccess(t('admin.settings.payment.permissions.removed'))
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('admin.settings.payment.permissions.updateFailed')))
  } finally {
    removingUserId.value = null
  }
}

watch(() => props.active, (active) => {
  if (active) void loadConfig()
  else {
    configRequest++
    cancelListRequest()
  }
}, { immediate: true })

function refreshPermissions() {
  if (props.active && !saving.value && removingUserId.value === null) void loadConfig()
}

useEventListener(window, 'focus', refreshPermissions)
useEventListener(window, 'payment-access-changed', refreshPermissions)
useEventListener(window, 'payment-mode-changed', refreshPermissions)

onBeforeUnmount(() => {
  configRequest++
  cancelListRequest()
})
</script>
