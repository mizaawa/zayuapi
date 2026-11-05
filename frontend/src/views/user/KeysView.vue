<template>
  <AppLayout>
    <div class="keys-page">
      <section class="keys-toolbar">
        <div class="keys-toolbar-main">
          <div class="keys-toolbar-filters">
            <SearchInput
              v-model="filterSearch"
              :placeholder="t('keys.searchPlaceholder')"
              class="keys-search"
              @search="onFilterChange"
            />
            <Select
              :model-value="filterGroupId"
              class="keys-filter-select"
              :options="groupFilterOptions"
              @update:model-value="onGroupFilterChange"
            />
            <Select
              :model-value="filterStatus"
              class="keys-filter-select"
              :options="statusFilterOptions"
              @update:model-value="onStatusFilterChange"
            />
          </div>

          <div class="keys-toolbar-actions">
            <button
              @click="loadApiKeys"
              :disabled="loading"
              class="btn btn-secondary keys-toolbar-button"
              :title="t('common.refresh')"
            >
              <Icon name="refresh" size="md" :class="loading ? 'animate-spin' : ''" />
            </button>
            <div class="relative" ref="fieldMenuRef" @keydown.esc="showFieldMenu = false">
              <button
                type="button"
                @click="showFieldMenu = !showFieldMenu"
                class="btn btn-secondary keys-toolbar-button"
                :title="t('keys.cardFields')"
                aria-haspopup="true"
                :aria-expanded="showFieldMenu"
                data-test="keys-field-menu-button"
              >
                <Icon name="grid" size="md" />
                <span class="hidden md:inline">{{ t('keys.cardFields') }}</span>
              </button>
              <div
                v-if="showFieldMenu"
                class="keys-field-menu absolute left-0 top-full z-50 mt-1.5 max-h-96 w-52 overflow-y-auto py-1.5 lg:left-auto lg:right-0"
                role="menu"
                data-test="keys-field-menu"
              >
                <button
                  v-for="field in cardFieldOptions"
                  :key="field.key"
                  type="button"
                  role="menuitemcheckbox"
                  :aria-checked="!hiddenFields.has(field.key)"
                  class="keys-field-option"
                  :data-test="`keys-field-${field.key}`"
                  @click="toggleField(field.key)"
                >
                  <span>{{ field.label }}</span>
                  <Icon
                    v-if="!hiddenFields.has(field.key)"
                    name="check"
                    size="sm"
                    class="text-primary-600"
                    :stroke-width="2"
                  />
                </button>
              </div>
            </div>
            <button @click="showCreateModal = true" class="btn btn-primary keys-toolbar-button" data-tour="keys-create-btn">
              <Icon name="plus" size="md" />
              {{ t('keys.createKey') }}
            </button>
          </div>
        </div>

        <div class="keys-toolbar-meta">
          <EndpointPopover
            v-if="publicSettings?.api_base_url || customEndpoints.length > 0"
            :api-base-url="publicSettings?.api_base_url || ''"
            :custom-endpoints="customEndpoints"
          />
          <div class="keys-list-controls">
            <span v-if="pagination.total > 0" class="keys-count">
              {{ t('keys.countSummary', { count: pagination.total }) }}
            </span>
            <div class="keys-sort">
              <Select
                :model-value="sortState.sort_by"
                class="keys-sort-select"
                :options="sortOptions"
                :searchable="false"
                :aria-label="t('keys.sortBy')"
                data-test="keys-sort-field"
                @update:model-value="onSortFieldChange"
              >
                <template #selected="{ option }">
                  <span class="keys-sort-value">
                    <Icon name="sort" size="sm" class="keys-sort-glyph" />
                    <span class="truncate">{{ option?.label }}</span>
                  </span>
                </template>
              </Select>
              <button
                type="button"
                class="keys-sort-order"
                :title="sortOrderLabel"
                :aria-label="`${t('keys.sortDirection')}: ${sortOrderLabel}`"
                data-test="keys-sort-order"
                @click="toggleSortOrder"
              >
                <Icon :name="sortState.sort_order === 'asc' ? 'arrowUp' : 'arrowDown'" size="sm" :stroke-width="2" />
              </button>
            </div>
          </div>
        </div>
      </section>

      <!-- Cards on every screen size: each row holds as many as fit the current width
           (window size, sidebar and browser zoom alike), never fewer than one. -->
      <section class="keys-results" :aria-busy="loading" :aria-label="t('keys.title')">
        <div v-if="loading && apiKeys.length === 0" class="keys-grid" data-test="keys-skeleton">
          <div v-for="i in SKELETON_CARD_COUNT" :key="i" class="keys-skeleton-card" aria-hidden="true">
            <div class="flex items-start justify-between gap-4">
              <div class="flex-1 space-y-2">
                <div class="keys-skeleton h-4 w-2/5"></div>
                <div class="keys-skeleton h-3 w-1/3"></div>
              </div>
              <div class="keys-skeleton h-6 w-14 rounded-full"></div>
            </div>
            <div class="keys-skeleton h-10 w-full"></div>
            <div class="keys-skeleton h-9 w-full"></div>
            <div class="grid grid-cols-3 gap-2">
              <div class="keys-skeleton h-12"></div>
              <div class="keys-skeleton h-12"></div>
              <div class="keys-skeleton h-12"></div>
            </div>
            <div class="keys-skeleton h-24 w-full"></div>
            <div class="keys-skeleton mt-auto h-10 w-full"></div>
          </div>
        </div>

        <div v-else-if="apiKeys.length === 0" class="keys-empty">
          <div v-if="loadFailed" class="keys-state" role="alert" data-test="keys-load-failed">
            <span class="keys-state-glyph is-error" aria-hidden="true">
              <Icon name="exclamationTriangle" size="lg" />
            </span>
            <strong class="keys-state-title">{{ t('keys.failedToLoad') }}</strong>
            <span class="keys-state-hint">{{ t('keys.loadFailedHint') }}</span>
            <button type="button" class="btn btn-secondary keys-toolbar-button" @click="loadApiKeys">
              <Icon name="refresh" size="sm" />
              {{ t('keys.reload') }}
            </button>
          </div>
          <div v-else-if="lastLoadFiltered" class="keys-state" data-test="keys-no-match">
            <span class="keys-state-glyph" aria-hidden="true">
              <Icon name="search" size="lg" />
            </span>
            <strong class="keys-state-title">{{ t('keys.noMatchTitle') }}</strong>
            <span class="keys-state-hint">{{ t('keys.noMatchHint') }}</span>
            <button type="button" class="btn btn-secondary keys-toolbar-button" data-test="keys-clear-filters" @click="clearFilters">
              {{ t('keys.clearFilters') }}
            </button>
          </div>
          <EmptyState
            v-else
            :title="t('keys.noKeysYet')"
            :description="t('keys.createFirstKey')"
            :action-text="t('keys.createKey')"
            @action="showCreateModal = true"
          />
        </div>

        <div v-else class="keys-grid" :class="{ 'is-refreshing': loading }" data-test="keys-grid">
          <KeyCard
            v-for="key in apiKeys"
            :key="key.id"
            :api-key="key"
            :usage="usageStats[key.id]"
            :usage-pending="usagePending"
            :hidden-fields="hiddenFields"
            :now="now"
            :toggling="togglingKeyIds.has(key.id)"
            :group-blocked="Boolean(key.group && (key.group.is_blocked_for_user === true || isBlockedGroupId(key.group.id)))"
            :user-group-rate="key.group ? userGroupRates[key.group.id] : undefined"
            @action="handleCardAction"
          />
        </div>
      </section>

      <Pagination
        v-if="pagination.total > 0"
        :page="pagination.page"
        :total="pagination.total"
        :page-size="pagination.page_size"
        @update:page="handlePageChange"
        @update:pageSize="handlePageSizeChange"
      />
    </div>

    <BaseDialog
      :show="showFailoverModal"
      :title="t('keys.failover.group')"
      width="normal"
      @close="closeFailoverModal"
    >
      <form id="failover-form" class="space-y-5" @submit.prevent="saveFailoverSettings">
        <FailoverSettings
          v-if="selectedFailoverKey"
          id="dialog-key-failover"
          v-model="failoverSettings"
          :primary-group="failoverPrimaryGroup"
          :groups="groups"
          :user-group-rates="userGroupRates"
          :disabled="savingFailover || releasingFailover"
        />
        <div v-if="selectedFailoverKey && isFailoverActive(selectedFailoverKey)" class="text-sm text-gray-600 dark:text-gray-300">
          {{ t('keys.failover.cooldownUntil') }}: {{ formatDateTime(selectedFailoverKey.failover_cooldown_until!) }}
        </div>
      </form>
      <template #footer>
        <div class="flex flex-wrap justify-end gap-3">
          <button
            type="button"
            class="btn btn-secondary"
            :disabled="savingFailover || releasingFailover || !selectedFailoverKey || !isFailoverActive(selectedFailoverKey)"
            data-test="release-failover-cooldown"
            @click="releaseFailoverCooldown"
          >
            {{ t('keys.failover.release') }}
          </button>
          <button
            type="submit"
            form="failover-form"
            class="btn btn-primary"
            :disabled="savingFailover || releasingFailover"
            data-test="update-failover-settings"
          >
            {{ t('keys.failover.update') }}
          </button>
        </div>
      </template>
    </BaseDialog>

    <!-- Create/Edit Modal -->
    <BaseDialog
      :show="showCreateModal || showEditModal"
      :title="showEditModal ? t('keys.editKey') : t('keys.createKey')"
      width="normal"
      @close="closeModals"
    >
      <form id="key-form" @submit.prevent="handleSubmit" class="space-y-5">
        <div>
          <label class="input-label">{{ t('keys.nameLabel') }}</label>
          <input
            v-model="formData.name"
            type="text"
            required
            class="input"
            :placeholder="t('keys.namePlaceholder')"
            data-tour="key-form-name"
          />
        </div>

        <div>
          <label class="input-label" for="key-form-group">{{ t('keys.groupLabel') }}</label>
          <button
            id="key-form-group"
            type="button"
            class="key-group-field"
            :class="{ 'is-open': groupPickerTarget?.kind === 'form' }"
            aria-haspopup="dialog"
            :aria-expanded="groupPickerTarget?.kind === 'form'"
            data-tour="key-form-group"
            @click="openFormGroupPicker"
          >
            <span class="key-group-field-value">
              <template v-if="editPrimaryGroup">
                <GroupBadge
                  class="max-w-full"
                  :name="editPrimaryGroup.name"
                  :platform="editPrimaryGroup.platform"
                  :subscription-type="editPrimaryGroup.subscription_type"
                  :rate-multiplier="editPrimaryGroup.rate_multiplier"
                  :user-rate-multiplier="userGroupRates[editPrimaryGroup.id]"
                  :peak-rate-enabled="editPrimaryGroup.peak_rate_enabled"
                  :peak-start="editPrimaryGroup.peak_start"
                  :peak-end="editPrimaryGroup.peak_end"
                  :peak-rate-multiplier="editPrimaryGroup.peak_rate_multiplier"
                />
                <span
                  v-if="editPrimaryGroup.is_blocked_for_user === true || isBlockedGroupId(editPrimaryGroup.id)"
                  class="inline-flex shrink-0 items-center rounded-full bg-red-100 px-2 py-0.5 text-[10px] font-semibold text-red-700"
                >{{ t('common.groupBlocked') }}</span>
              </template>
              <span v-else class="key-group-field-placeholder">{{ t('keys.selectGroup') }}</span>
            </span>
            <Icon name="chevronDown" size="md" class="key-group-field-icon" />
          </button>
        </div>

        <!-- Custom Key Section (only for create) -->
        <div v-if="!showEditModal" class="space-y-3">
          <div class="flex items-center justify-between">
            <label class="input-label mb-0">{{ t('keys.customKeyLabel') }}</label>
            <button
              type="button"
              @click="formData.use_custom_key = !formData.use_custom_key"
              :class="[
                'relative inline-flex h-5 w-9 flex-shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 ease-in-out focus:outline-none',
                formData.use_custom_key ? 'bg-primary-600' : 'bg-gray-200 dark:bg-dark-600'
              ]"
            >
              <span
                :class="[
                  'pointer-events-none inline-block h-4 w-4 transform rounded-full bg-white shadow ring-0 transition duration-200 ease-in-out',
                  formData.use_custom_key ? 'translate-x-4' : 'translate-x-0'
                ]"
              />
            </button>
          </div>
          <div v-if="formData.use_custom_key">
            <input
              v-model="formData.custom_key"
              type="text"
              class="input font-mono"
              :placeholder="t('keys.customKeyPlaceholder')"
              :class="{ 'border-red-500 dark:border-red-500': customKeyError }"
            />
            <p v-if="customKeyError" class="mt-1 text-sm text-red-500">{{ customKeyError }}</p>
            <p v-else class="input-hint">{{ t('keys.customKeyHint') }}</p>
          </div>
        </div>

        <div v-if="showEditModal">
          <label class="input-label">{{ t('keys.statusLabel') }}</label>
          <Select
            v-model="formData.status"
            :options="statusOptions"
            :placeholder="t('keys.selectStatus')"
          />
        </div>

        <FailoverSettings
          v-if="showEditModal && supportsFailover(editPrimaryGroup)"
          id="edit-key-failover"
          v-model="failoverSettings"
          :primary-group="editPrimaryGroup"
          :groups="groups"
          :user-group-rates="userGroupRates"
          :disabled="submitting"
        />

        <SystemPromptSettings
          v-if="showEditModal"
          id="edit-key-system-prompt"
          v-model="systemPromptSettings"
          :disabled="submitting"
        />

        <!-- IP Restriction Section -->
        <div class="space-y-3">
          <div class="flex items-center justify-between">
            <label class="input-label mb-0">{{ t('keys.ipRestriction') }}</label>
            <button
              type="button"
              @click="formData.enable_ip_restriction = !formData.enable_ip_restriction"
              :class="[
                'relative inline-flex h-5 w-9 flex-shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 ease-in-out focus:outline-none',
                formData.enable_ip_restriction ? 'bg-primary-600' : 'bg-gray-200 dark:bg-dark-600'
              ]"
            >
              <span
                :class="[
                  'pointer-events-none inline-block h-4 w-4 transform rounded-full bg-white shadow ring-0 transition duration-200 ease-in-out',
                  formData.enable_ip_restriction ? 'translate-x-4' : 'translate-x-0'
                ]"
              />
            </button>
          </div>

          <div v-if="formData.enable_ip_restriction" class="space-y-4 pt-2">
            <div>
              <label class="input-label">{{ t('keys.ipWhitelist') }}</label>
              <textarea
                v-model="formData.ip_whitelist"
                rows="3"
                class="input font-mono text-sm"
                :placeholder="t('keys.ipWhitelistPlaceholder')"
              />
              <p class="input-hint">{{ t('keys.ipWhitelistHint') }}</p>
            </div>

            <div>
              <label class="input-label">{{ t('keys.ipBlacklist') }}</label>
              <textarea
                v-model="formData.ip_blacklist"
                rows="3"
                class="input font-mono text-sm"
                :placeholder="t('keys.ipBlacklistPlaceholder')"
              />
              <p class="input-hint">{{ t('keys.ipBlacklistHint') }}</p>
            </div>
          </div>
        </div>

        <!-- Quota Limit Section -->
        <div class="space-y-3">
          <label class="input-label">{{ t('keys.quotaLimit') }}</label>
          <!-- Switch commented out - always show input, 0 = unlimited
          <div class="flex items-center justify-between">
            <label class="input-label mb-0">{{ t('keys.quotaLimit') }}</label>
            <button
              type="button"
              @click="formData.enable_quota = !formData.enable_quota"
              :class="[
                'relative inline-flex h-5 w-9 flex-shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 ease-in-out focus:outline-none',
                formData.enable_quota ? 'bg-primary-600' : 'bg-gray-200 dark:bg-dark-600'
              ]"
            >
              <span
                :class="[
                  'pointer-events-none inline-block h-4 w-4 transform rounded-full bg-white shadow ring-0 transition duration-200 ease-in-out',
                  formData.enable_quota ? 'translate-x-4' : 'translate-x-0'
                ]"
              />
            </button>
          </div>
          -->

          <div class="space-y-4">
            <div>
              <div class="relative">
                <span class="absolute left-3 top-1/2 -translate-y-1/2 text-gray-500">$</span>
                <input
                  v-model.number="formData.quota"
                  type="number"
                  step="0.01"
                  min="0"
                  class="input pl-7"
                  :placeholder="t('keys.quotaAmountPlaceholder')"
                />
              </div>
              <p class="input-hint">{{ t('keys.quotaAmountHint') }}</p>
            </div>

            <!-- Quota used display (only in edit mode) -->
            <div v-if="showEditModal && selectedKey && selectedKey.quota > 0">
              <label class="input-label">{{ t('keys.quotaUsed') }}</label>
              <div class="flex items-center gap-2">
                <div class="flex-1 rounded-lg bg-gray-100 px-3 py-2 dark:bg-dark-700">
                  <span class="font-medium text-gray-900 dark:text-white">
                    ${{ selectedKey.quota_used?.toFixed(4) || '0.0000' }}
                  </span>
                  <span class="mx-2 text-gray-400">/</span>
                  <span class="text-gray-500 dark:text-gray-400">
                    ${{ selectedKey.quota?.toFixed(2) || '0.00' }}
                  </span>
                </div>
                <button
                  type="button"
                  @click="confirmResetQuota"
                  class="btn btn-secondary text-sm"
                  :title="t('keys.resetQuotaUsed')"
                >
                  {{ t('keys.reset') }}
                </button>
              </div>
            </div>
          </div>
        </div>

        <!-- Rate Limit Section -->
        <div class="space-y-3">
          <div class="flex items-center justify-between">
            <label class="input-label mb-0">{{ t('keys.rateLimitSection') }}</label>
            <button
              type="button"
              @click="formData.enable_rate_limit = !formData.enable_rate_limit"
              :class="[
                'relative inline-flex h-5 w-9 flex-shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 ease-in-out focus:outline-none',
                formData.enable_rate_limit ? 'bg-primary-600' : 'bg-gray-200 dark:bg-dark-600'
              ]"
            >
              <span
                :class="[
                  'pointer-events-none inline-block h-4 w-4 transform rounded-full bg-white shadow ring-0 transition duration-200 ease-in-out',
                  formData.enable_rate_limit ? 'translate-x-4' : 'translate-x-0'
                ]"
              />
            </button>
          </div>

          <div v-if="formData.enable_rate_limit" class="space-y-4 pt-2">
            <p class="input-hint -mt-2">{{ t('keys.rateLimitHint') }}</p>
            <!-- 5-Hour Limit -->
            <div>
              <label class="input-label">{{ t('keys.rateLimit5h') }}</label>
              <div class="relative">
                <span class="absolute left-3 top-1/2 -translate-y-1/2 text-gray-500">$</span>
                <input
                  v-model.number="formData.rate_limit_5h"
                  type="number"
                  step="0.01"
                  min="0"
                  class="input pl-7"
                  :placeholder="'0'"
                />
              </div>
              <!-- Usage info (edit mode only) -->
              <div v-if="showEditModal && selectedKey && selectedKey.rate_limit_5h > 0" class="mt-2">
                <div class="flex items-center gap-2">
                  <div class="flex-1 rounded-lg bg-gray-100 px-3 py-2 dark:bg-dark-700 text-sm">
                    <span :class="[
                      'font-medium',
                      selectedKey.usage_5h >= selectedKey.rate_limit_5h ? 'text-red-500' :
                      selectedKey.usage_5h >= selectedKey.rate_limit_5h * 0.8 ? 'text-yellow-500' :
                      'text-gray-900 dark:text-white'
                    ]">
                      ${{ selectedKey.usage_5h?.toFixed(4) || '0.0000' }}
                    </span>
                    <span class="mx-2 text-gray-400">/</span>
                    <span class="text-gray-500 dark:text-gray-400">
                      ${{ selectedKey.rate_limit_5h?.toFixed(2) || '0.00' }}
                    </span>
                  </div>
                </div>
                <div class="mt-1 h-1.5 w-full overflow-hidden rounded-full bg-gray-200 dark:bg-dark-600">
                  <div
                    :class="[
                      'h-full rounded-full transition-all',
                      selectedKey.usage_5h >= selectedKey.rate_limit_5h ? 'bg-red-500' :
                      selectedKey.usage_5h >= selectedKey.rate_limit_5h * 0.8 ? 'bg-yellow-500' :
                      'bg-green-500'
                    ]"
                    :style="{ width: Math.min((selectedKey.usage_5h / selectedKey.rate_limit_5h) * 100, 100) + '%' }"
                  />
                </div>
              </div>
            </div>

            <!-- Daily Limit -->
            <div>
              <label class="input-label">{{ t('keys.rateLimit1d') }}</label>
              <div class="relative">
                <span class="absolute left-3 top-1/2 -translate-y-1/2 text-gray-500">$</span>
                <input
                  v-model.number="formData.rate_limit_1d"
                  type="number"
                  step="0.01"
                  min="0"
                  class="input pl-7"
                  :placeholder="'0'"
                />
              </div>
              <!-- Usage info (edit mode only) -->
              <div v-if="showEditModal && selectedKey && selectedKey.rate_limit_1d > 0" class="mt-2">
                <div class="flex items-center gap-2">
                  <div class="flex-1 rounded-lg bg-gray-100 px-3 py-2 dark:bg-dark-700 text-sm">
                    <span :class="[
                      'font-medium',
                      selectedKey.usage_1d >= selectedKey.rate_limit_1d ? 'text-red-500' :
                      selectedKey.usage_1d >= selectedKey.rate_limit_1d * 0.8 ? 'text-yellow-500' :
                      'text-gray-900 dark:text-white'
                    ]">
                      ${{ selectedKey.usage_1d?.toFixed(4) || '0.0000' }}
                    </span>
                    <span class="mx-2 text-gray-400">/</span>
                    <span class="text-gray-500 dark:text-gray-400">
                      ${{ selectedKey.rate_limit_1d?.toFixed(2) || '0.00' }}
                    </span>
                  </div>
                </div>
                <div class="mt-1 h-1.5 w-full overflow-hidden rounded-full bg-gray-200 dark:bg-dark-600">
                  <div
                    :class="[
                      'h-full rounded-full transition-all',
                      selectedKey.usage_1d >= selectedKey.rate_limit_1d ? 'bg-red-500' :
                      selectedKey.usage_1d >= selectedKey.rate_limit_1d * 0.8 ? 'bg-yellow-500' :
                      'bg-green-500'
                    ]"
                    :style="{ width: Math.min((selectedKey.usage_1d / selectedKey.rate_limit_1d) * 100, 100) + '%' }"
                  />
                </div>
              </div>
            </div>

            <!-- 7-Day Limit -->
            <div>
              <label class="input-label">{{ t('keys.rateLimit7d') }}</label>
              <div class="relative">
                <span class="absolute left-3 top-1/2 -translate-y-1/2 text-gray-500">$</span>
                <input
                  v-model.number="formData.rate_limit_7d"
                  type="number"
                  step="0.01"
                  min="0"
                  class="input pl-7"
                  :placeholder="'0'"
                />
              </div>
              <!-- Usage info (edit mode only) -->
              <div v-if="showEditModal && selectedKey && selectedKey.rate_limit_7d > 0" class="mt-2">
                <div class="flex items-center gap-2">
                  <div class="flex-1 rounded-lg bg-gray-100 px-3 py-2 dark:bg-dark-700 text-sm">
                    <span :class="[
                      'font-medium',
                      selectedKey.usage_7d >= selectedKey.rate_limit_7d ? 'text-red-500' :
                      selectedKey.usage_7d >= selectedKey.rate_limit_7d * 0.8 ? 'text-yellow-500' :
                      'text-gray-900 dark:text-white'
                    ]">
                      ${{ selectedKey.usage_7d?.toFixed(4) || '0.0000' }}
                    </span>
                    <span class="mx-2 text-gray-400">/</span>
                    <span class="text-gray-500 dark:text-gray-400">
                      ${{ selectedKey.rate_limit_7d?.toFixed(2) || '0.00' }}
                    </span>
                  </div>
                </div>
                <div class="mt-1 h-1.5 w-full overflow-hidden rounded-full bg-gray-200 dark:bg-dark-600">
                  <div
                    :class="[
                      'h-full rounded-full transition-all',
                      selectedKey.usage_7d >= selectedKey.rate_limit_7d ? 'bg-red-500' :
                      selectedKey.usage_7d >= selectedKey.rate_limit_7d * 0.8 ? 'bg-yellow-500' :
                      'bg-green-500'
                    ]"
                    :style="{ width: Math.min((selectedKey.usage_7d / selectedKey.rate_limit_7d) * 100, 100) + '%' }"
                  />
                </div>
              </div>
            </div>

            <!-- Reset Rate Limit button (edit mode only) -->
            <div v-if="showEditModal && selectedKey && (selectedKey.rate_limit_5h > 0 || selectedKey.rate_limit_1d > 0 || selectedKey.rate_limit_7d > 0)">
              <button
                type="button"
                @click="confirmResetRateLimit"
                class="btn btn-secondary text-sm"
              >
                {{ t('keys.resetRateLimitUsage') }}
              </button>
            </div>
          </div>
        </div>

        <!-- Expiration Section -->
        <div class="space-y-3">
          <div class="flex items-center justify-between">
            <label class="input-label mb-0">{{ t('keys.expiration') }}</label>
            <button
              type="button"
              @click="formData.enable_expiration = !formData.enable_expiration"
              :class="[
                'relative inline-flex h-5 w-9 flex-shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 ease-in-out focus:outline-none',
                formData.enable_expiration ? 'bg-primary-600' : 'bg-gray-200 dark:bg-dark-600'
              ]"
            >
              <span
                :class="[
                  'pointer-events-none inline-block h-4 w-4 transform rounded-full bg-white shadow ring-0 transition duration-200 ease-in-out',
                  formData.enable_expiration ? 'translate-x-4' : 'translate-x-0'
                ]"
              />
            </button>
          </div>

          <div v-if="formData.enable_expiration" class="space-y-4 pt-2">
            <!-- Quick select buttons (for both create and edit mode) -->
            <div class="flex flex-wrap gap-2">
              <button
                v-for="days in ['7', '30', '90']"
                :key="days"
                type="button"
                @click="setExpirationDays(parseInt(days))"
                :class="[
                  'rounded-lg px-3 py-1.5 text-sm transition-colors',
                  formData.expiration_preset === days
                    ? 'bg-primary-100 text-primary-700 dark:bg-primary-900/30 dark:text-primary-400'
                    : 'bg-gray-100 text-gray-600 hover:bg-gray-200 dark:bg-dark-700 dark:text-gray-400 dark:hover:bg-dark-600'
                ]"
              >
                {{ showEditModal ? t('keys.extendDays', { days }) : t('keys.expiresInDays', { days }) }}
              </button>
              <button
                type="button"
                @click="formData.expiration_preset = 'custom'"
                :class="[
                  'rounded-lg px-3 py-1.5 text-sm transition-colors',
                  formData.expiration_preset === 'custom'
                    ? 'bg-primary-100 text-primary-700 dark:bg-primary-900/30 dark:text-primary-400'
                    : 'bg-gray-100 text-gray-600 hover:bg-gray-200 dark:bg-dark-700 dark:text-gray-400 dark:hover:bg-dark-600'
                ]"
              >
                {{ t('keys.customDate') }}
              </button>
            </div>

            <!-- Date picker (always show for precise adjustment) -->
            <div>
              <label class="input-label">{{ t('keys.expirationDate') }}</label>
              <input
                v-model="formData.expiration_date"
                type="datetime-local"
                class="input"
              />
              <p class="input-hint">{{ t('keys.expirationDateHint') }}</p>
            </div>

            <!-- Current expiration display (only in edit mode) -->
            <div v-if="showEditModal && selectedKey?.expires_at" class="text-sm">
              <span class="text-gray-500 dark:text-gray-400">{{ t('keys.currentExpiration') }}: </span>
              <span class="font-medium text-gray-900 dark:text-white">
                {{ formatDateTime(selectedKey.expires_at) }}
              </span>
            </div>
          </div>
        </div>
      </form>
      <template #footer>
        <div class="flex justify-end gap-3">
          <button @click="closeModals" type="button" class="btn btn-secondary">
            {{ t('common.cancel') }}
          </button>
          <button
            form="key-form"
            type="submit"
            :disabled="submitting"
            class="btn btn-primary"
            data-tour="key-form-submit"
          >
            <svg
              v-if="submitting"
              class="-ml-1 mr-2 h-4 w-4 animate-spin"
              fill="none"
              viewBox="0 0 24 24"
            >
              <circle
                class="opacity-25"
                cx="12"
                cy="12"
                r="10"
                stroke="currentColor"
                stroke-width="4"
              ></circle>
              <path
                class="opacity-75"
                fill="currentColor"
                d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"
              ></path>
            </svg>
            {{
              submitting
                ? t('keys.saving')
                : showEditModal
                  ? t('common.update')
                  : t('common.create')
            }}
          </button>
        </div>
      </template>
    </BaseDialog>

    <BaseDialog
      :show="createdApiKey !== null"
      :title="t('keys.createdKeyTitle')"
      width="normal"
      @close="closeCreatedApiKey"
    >
      <div class="space-y-4">
        <p class="text-sm text-gray-600 dark:text-gray-300">{{ t('keys.createdKeyWarning') }}</p>
        <label class="input-label" for="created-api-key">{{ t('keys.apiKey') }}</label>
        <input
          id="created-api-key"
          class="input font-mono"
          type="text"
          :value="createdApiKey || ''"
          readonly
          data-test="created-api-key"
        />
      </div>
      <template #footer>
        <div class="flex justify-end gap-3">
          <button type="button" class="btn btn-secondary" @click="closeCreatedApiKey">
            {{ t('common.close') }}
          </button>
          <button type="button" class="btn btn-primary" data-test="copy-created-api-key" @click="copyCreatedApiKey">
            <Icon name="clipboard" size="sm" />
            {{ t('keys.copyCreatedKey') }}
          </button>
        </div>
      </template>
    </BaseDialog>

    <!-- Delete Confirmation Dialog -->
    <ConfirmDialog
      :show="showDeleteDialog"
      :title="t('keys.deleteKey')"
      :message="t('keys.deleteConfirmMessage', { name: selectedKey?.name })"
      :confirm-text="t('common.delete')"
      :cancel-text="t('common.cancel')"
      :danger="true"
      :loading="deleting"
      @confirm="handleDelete"
      @cancel="showDeleteDialog = false"
    />

    <!-- Reset Quota Confirmation Dialog -->
    <ConfirmDialog
      :show="showResetQuotaDialog"
      :title="t('keys.resetQuotaTitle')"
      :message="t('keys.resetQuotaConfirmMessage', { name: selectedKey?.name, used: selectedKey?.quota_used?.toFixed(4) })"
      :confirm-text="t('keys.reset')"
      :cancel-text="t('common.cancel')"
      :danger="true"
      @confirm="resetQuotaUsed"
      @cancel="showResetQuotaDialog = false"
    />

    <!-- Reset Rate Limit Confirmation Dialog -->
    <ConfirmDialog
      :show="showResetRateLimitDialog"
      :title="t('keys.resetRateLimitTitle')"
      :message="t('keys.resetRateLimitConfirmMessage', { name: selectedKey?.name })"
      :confirm-text="t('keys.reset')"
      :cancel-text="t('common.cancel')"
      :danger="true"
      @confirm="resetRateLimitUsage"
      @cancel="showResetRateLimitDialog = false"
    />

    <!-- One picker serves both the key form and the table's "change group" button. -->
    <KeyGroupPicker
      :show="groupPickerTarget !== null"
      :options="groupOptions"
      :selected-id="groupPickerSelectedId"
      :title="groupPickerTitle"
      :subtitle="groupPickerSubtitle"
      @select="handleGroupPicked"
      @close="closeGroupPicker"
    />
  </AppLayout>
</template>

<script setup lang="ts">
import { ref, reactive, computed, watch, onMounted, onUnmounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { useOnboardingStore } from '@/stores/onboarding'
import { useClipboard } from '@/composables/useClipboard'
import { getPersistedPageSize } from '@/composables/usePersistedPageSize'

const { t } = useI18n()
import { keysAPI, authAPI, usageAPI, userGroupsAPI } from '@/api'
import AppLayout from '@/components/layout/AppLayout.vue'
import Pagination from '@/components/common/Pagination.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import Select from '@/components/common/Select.vue'
import SearchInput from '@/components/common/SearchInput.vue'
import Icon from '@/components/icons/Icon.vue'
import FailoverSettings from '@/components/keys/FailoverSettings.vue'
import SystemPromptSettings from '@/components/keys/SystemPromptSettings.vue'
import EndpointPopover from '@/components/keys/EndpointPopover.vue'
import KeyCard, { type KeyCardAction } from '@/components/keys/KeyCard.vue'
import KeyGroupPicker from '@/components/keys/KeyGroupPicker.vue'
import type { KeyGroupOption } from '@/components/keys/keyGroupFilters'
import {
  KEY_CARD_FIELDS,
  KEY_SORT_FIELDS,
  isFailoverActive as isKeyFailoverActive,
  isKeySortField,
  loadHiddenKeyCardFields,
  loadKeySort,
  nextKeyClockTick,
  saveHiddenKeyCardFields,
  saveKeySort,
  supportsFailover,
  type KeyCardField,
  type KeySort
} from '@/components/keys/keyCards'
import GroupBadge from '@/components/common/GroupBadge.vue'
import type { ApiKey, ApiKeyFailoverSettings, ApiKeySystemPromptSettings, CustomEndpoint, Group, PublicSettings, UpdateApiKeyRequest } from '@/types'
import type { BatchApiKeyUsageStats } from '@/api/usage'
import { formatDateTime } from '@/utils/format'
import { fetchUsageWithRetry } from '@/utils/usageRequest'

// Helper to format date for datetime-local input
const formatDateTimeLocal = (isoDate: string): string => {
  const date = new Date(isoDate)
  const pad = (n: number) => n.toString().padStart(2, '0')
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}`
}

// 首次加载时占位的卡片数
const SKELETON_CARD_COUNT = 6
const appStore = useAppStore()
const onboardingStore = useOnboardingStore()
const { copyToClipboard: clipboardCopy } = useClipboard()

// ---------- Card fields ----------
const cardFieldLabels: Record<KeyCardField, () => string> = {
  id: () => t('keys.id'),
  usage: () => t('keys.usage'),
  current_concurrency: () => t('keys.currentConcurrency'),
  tokens: () => t('keys.tokenMix'),
  rate_limit: () => t('keys.rateLimitColumn'),
  failover: () => t('keys.failover.group'),
  expires_at: () => t('keys.expiresAt'),
  last_used_at: () => t('keys.lastUsedAt'),
  last_used_ip: () => t('keys.lastUsedIP'),
  created_at: () => t('keys.created')
}

const cardFieldOptions = computed(() =>
  KEY_CARD_FIELDS.map((key) => ({ key, label: cardFieldLabels[key]() }))
)

const hiddenFields = reactive(new Set<KeyCardField>())

const loadCardFields = () => {
  hiddenFields.clear()
  for (const field of loadHiddenKeyCardFields()) hiddenFields.add(field)
}

const toggleField = (field: KeyCardField) => {
  if (hiddenFields.has(field)) {
    hiddenFields.delete(field)
  } else {
    hiddenFields.add(field)
  }
  saveHiddenKeyCardFields(hiddenFields)
}

// ---------- Sorting ----------
const sortState = ref<KeySort>(loadKeySort())

const sortOptions = computed(() => {
  const labels: Record<keyof typeof KEY_SORT_FIELDS, string> = {
    created_at: t('keys.created'),
    name: t('common.name'),
    last_used_at: t('keys.lastUsedAt'),
    expires_at: t('keys.expiresAt'),
    current_concurrency: t('keys.currentConcurrency'),
    status: t('common.status'),
    id: t('keys.id')
  }
  return Object.entries(labels).map(([value, label]) => ({ value, label }))
})

const sortOrderLabel = computed(() =>
  sortState.value.sort_order === 'asc' ? t('keys.sortAsc') : t('keys.sortDesc')
)

const applySort = (sort: KeySort) => {
  sortState.value = sort
  saveKeySort(sort)
  pagination.value.page = 1
  loadApiKeys()
}

// A new field starts in the direction people usually want for it (A→Z, newest first, soonest expiry first).
const onSortFieldChange = (value: string | number | boolean | null) => {
  if (!isKeySortField(value) || value === sortState.value.sort_by) return
  applySort({ sort_by: value, sort_order: KEY_SORT_FIELDS[value] })
}

const toggleSortOrder = () => {
  applySort({ ...sortState.value, sort_order: sortState.value.sort_order === 'asc' ? 'desc' : 'asc' })
}

const apiKeys = ref<ApiKey[]>([])
const groups = ref<Group[]>([])
const loading = ref(false)
// Pick the empty-state message: the last list request failed, or it ran with filters.
const loadFailed = ref(false)
const lastLoadFiltered = ref(false)
const submitting = ref(false)
const deleting = ref(false)
const togglingKeyIds = reactive(new Set<number>())
let failoverRefreshTimer: ReturnType<typeof setInterval> | null = null
let failoverRefreshController: AbortController | null = null
const usageStats = ref<Record<string, BatchApiKeyUsageStats>>({})
const usagePending = ref(false)
const userGroupRates = ref<Record<number, number>>({})
const blockedGroupIds = computed(() => new Set(
  groups.value.filter((group) => group.is_blocked_for_user === true).map((group) => group.id)
))
const isBlockedGroupId = (groupId: number) => blockedGroupIds.value.has(groupId)

const pagination = ref({
  page: 1,
  page_size: getPersistedPageSize(),
  total: 0,
  pages: 0
})

// Filter state
const filterSearch = ref('')
const filterStatus = ref('')
const filterGroupId = ref<string | number>('')

const showCreateModal = ref(false)
const showEditModal = ref(false)
const showFailoverModal = ref(false)
const selectedFailoverKey = ref<ApiKey | null>(null)
const savingFailover = ref(false)
const releasingFailover = ref(false)
const showDeleteDialog = ref(false)
const showResetQuotaDialog = ref(false)
const showResetRateLimitDialog = ref(false)
const showFieldMenu = ref(false)
const selectedKey = ref<ApiKey | null>(null)
const createdApiKey = ref<string | null>(null)
const publicSettings = ref<PublicSettings | null>(null)
// Keep authenticated relay URLs local to this page, outside public app config.
const customEndpoints = ref<CustomEndpoint[]>([])
const fieldMenuRef = ref<HTMLElement | null>(null)
let abortController: AbortController | null = null

// ---------- Clock ----------
// Cards read one shared clock. Instead of re-rendering every card each second,
// it only moves when some card's text can change: on the minute for countdowns
// and "last used" times, and right when a cooldown, expiry or reset passes.
const now = ref(Date.now())
let clockTimer: ReturnType<typeof setTimeout> | null = null

const scheduleClockTick = () => {
  if (clockTimer) clearTimeout(clockTimer)
  const current = Date.now()
  const next = nextKeyClockTick(apiKeys.value, current)
  clockTimer = setTimeout(() => {
    clockTimer = null
    // Timers can fire a hair early; never let the clock fall short of the moment it waited for.
    now.value = Math.max(Date.now(), next)
    scheduleClockTick()
  }, next - current)
}

const stopClock = () => {
  if (clockTimer) clearTimeout(clockTimer)
  clockTimer = null
}

watch(apiKeys, () => {
  now.value = Date.now()
  scheduleClockTick()
})

const isFailoverActive = (key: ApiKey) => isKeyFailoverActive(key, now.value)

const formData = ref({
  name: '',
  group_id: null as number | null,
  status: 'active' as 'active' | 'inactive',
  use_custom_key: false,
  custom_key: '',
  enable_ip_restriction: false,
  ip_whitelist: '',
  ip_blacklist: '',
  // Quota settings (empty = unlimited)
  enable_quota: false,
  quota: null as number | null,
  // Rate limit settings
  enable_rate_limit: false,
  rate_limit_5h: null as number | null,
  rate_limit_1d: null as number | null,
  rate_limit_7d: null as number | null,
  enable_expiration: false,
  expiration_preset: '30' as '7' | '30' | '90' | 'custom',
  expiration_date: ''
})

const getFailoverSettings = (key?: ApiKey): ApiKeyFailoverSettings => ({
  failover_enabled: key?.failover_enabled ?? false,
  failover_group_id: key?.failover_group_id ?? null,
  failover_max_retries: key?.failover_max_retries ?? 3,
  failover_cooldown_seconds: key?.failover_cooldown_seconds ?? 300
})
const failoverSettings = ref<ApiKeyFailoverSettings>(getFailoverSettings())
const getSystemPromptSettings = (key?: ApiKey): ApiKeySystemPromptSettings => ({
  custom_system_prompt_enabled: key?.custom_system_prompt_enabled ?? false,
  custom_system_prompt_force: key?.custom_system_prompt_force ?? false,
  custom_system_prompt: key?.custom_system_prompt ?? ''
})
const systemPromptSettings = ref<ApiKeySystemPromptSettings>(getSystemPromptSettings())
const editPrimaryGroup = computed(() =>
  groups.value.find((group) => group.id === formData.value.group_id) ??
  (selectedKey.value?.group_id === formData.value.group_id ? selectedKey.value.group : undefined)
)
const failoverPrimaryGroup = computed(() =>
  groups.value.find((group) => group.id === selectedFailoverKey.value?.group_id) ?? selectedFailoverKey.value?.group
)

const validateFailoverSettings = (primaryGroup: Group | undefined): boolean => {
  if (!failoverSettings.value.failover_enabled) return true
  const settings = failoverSettings.value
  const fallbackGroup = groups.value.find((group) => group.id === settings.failover_group_id)
  if (!primaryGroup || !supportsFailover(primaryGroup) || !fallbackGroup || fallbackGroup.id === primaryGroup.id ||
    fallbackGroup.platform !== primaryGroup.platform || fallbackGroup.status !== 'active' ||
    fallbackGroup.is_blocked_for_user) {
    appStore.showError(t('keys.failover.groupRequired'))
    return false
  }
  if (!Number.isInteger(settings.failover_max_retries) || settings.failover_max_retries < 1 || settings.failover_max_retries > 10) {
    appStore.showError(t('keys.failover.invalidMaxRetries'))
    return false
  }
  if (!Number.isInteger(settings.failover_cooldown_seconds) || settings.failover_cooldown_seconds < 1 || settings.failover_cooldown_seconds > 2147483647) {
    appStore.showError(t('keys.failover.invalidCooldown'))
    return false
  }
  return true
}

watch(() => formData.value.group_id, (groupId, previousGroupId) => {
  if (!showEditModal.value || groupId === previousGroupId) return
  if (!supportsFailover(editPrimaryGroup.value)) failoverSettings.value = getFailoverSettings()
})

// 自定义Key验证
const customKeyError = computed(() => {
  if (!formData.value.use_custom_key || !formData.value.custom_key) {
    return ''
  }
  const key = formData.value.custom_key
  if (key.length < 16) {
    return t('keys.customKeyTooShort')
  }
  // 检查字符：只允许字母、数字、下划线、连字符
  if (!/^[a-zA-Z0-9_-]+$/.test(key)) {
    return t('keys.customKeyInvalidChars')
  }
  return ''
})

const statusOptions = computed(() => [
  { value: 'active', label: t('common.active') },
  { value: 'inactive', label: t('common.inactive') }
])

const shouldSubmitEditStatus = (key: ApiKey, status: 'active' | 'inactive') => {
  if (key.status === 'quota_exhausted' || key.status === 'expired') {
    return status === 'active'
  }
  return true
}

// Filter dropdown options
const groupFilterOptions = computed(() => [
  { value: '', label: t('keys.allGroups') },
  { value: 0, label: t('keys.noGroup') },
  ...groups.value.map((g) => ({ value: g.id, label: g.name }))
])

const statusFilterOptions = computed(() => [
  { value: '', label: t('keys.allStatus') },
  { value: 'active', label: t('keys.status.active') },
  { value: 'inactive', label: t('keys.status.inactive') },
  { value: 'quota_exhausted', label: t('keys.status.quota_exhausted') },
  { value: 'expired', label: t('keys.status.expired') }
])

const onFilterChange = () => {
  pagination.value.page = 1
  loadApiKeys()
}

const onGroupFilterChange = (value: string | number | boolean | null) => {
  filterGroupId.value = value as string | number
  onFilterChange()
}

const onStatusFilterChange = (value: string | number | boolean | null) => {
  filterStatus.value = value as string
  onFilterChange()
}

// Convert groups to picker options with rate multiplier and subscription type
const groupOptions = computed<KeyGroupOption[]>(() =>
  groups.value.map((group) => ({
    value: group.id,
    label: group.name,
    description: group.description,
    rate: group.rate_multiplier,
    userRate: userGroupRates.value[group.id] ?? null,
    peakRateEnabled: group.peak_rate_enabled,
    peakStart: group.peak_start,
    peakEnd: group.peak_end,
    peakRateMultiplier: group.peak_rate_multiplier,
    subscriptionType: group.subscription_type,
    platform: group.platform,
    blocked: group.is_blocked_for_user === true || isBlockedGroupId(group.id)
  }))
)

// The group picker either fills the key form or rebinds a key from the table.
type GroupPickerTarget = { kind: 'form' } | { kind: 'key'; keyId: number }
const groupPickerTarget = ref<GroupPickerTarget | null>(null)

const groupPickerKey = computed(() => {
  const target = groupPickerTarget.value
  if (target?.kind !== 'key') return null
  return apiKeys.value.find((key) => key.id === target.keyId) ?? null
})

const groupPickerSelectedId = computed(() => {
  const target = groupPickerTarget.value
  if (!target) return null
  return target.kind === 'form' ? formData.value.group_id : groupPickerKey.value?.group_id ?? null
})

const groupPickerTitle = computed(() =>
  groupPickerTarget.value?.kind === 'key' ? t('keys.groupPicker.changeTitle') : t('keys.selectGroup')
)

const groupPickerSubtitle = computed(() =>
  groupPickerKey.value ? t('keys.groupPicker.forKey', { name: groupPickerKey.value.name }) : ''
)

const openFormGroupPicker = () => {
  groupPickerTarget.value = { kind: 'form' }
}

const openKeyGroupPicker = (key: ApiKey) => {
  groupPickerTarget.value = { kind: 'key', keyId: key.id }
}

const closeGroupPicker = () => {
  groupPickerTarget.value = null
}

const handleGroupPicked = (option: KeyGroupOption) => {
  const target = groupPickerTarget.value
  const key = groupPickerKey.value
  closeGroupPicker()
  if (target?.kind === 'form') {
    formData.value.group_id = option.value
  } else if (key) {
    void changeGroup(key, option.value)
  }
}

const copyCreatedApiKey = async () => {
  if (createdApiKey.value) await clipboardCopy(createdApiKey.value, t('keys.copied'))
}

const closeCreatedApiKey = () => {
  createdApiKey.value = null
}

const isAbortError = (error: unknown) => {
  if (!error || typeof error !== 'object') return false
  const { name, code } = error as { name?: string; code?: string }
  return name === 'AbortError' || code === 'ERR_CANCELED'
}

const loadApiKeys = async () => {
  failoverRefreshController?.abort()
  abortController?.abort()
  const controller = new AbortController()
  abortController = controller
  const { signal } = controller
  const isCurrentRequest = () => abortController === controller && !signal.aborted
  loading.value = true
  try {
    // Build filters
    const filters: {
      search?: string
      status?: string
      group_id?: number | string
      sort_by?: string
      sort_order?: 'asc' | 'desc'
    } = {}
    if (filterSearch.value) filters.search = filterSearch.value
    if (filterStatus.value) filters.status = filterStatus.value
    if (filterGroupId.value !== '') filters.group_id = filterGroupId.value
    filters.sort_by = sortState.value.sort_by
    filters.sort_order = sortState.value.sort_order

    const { page, page_size } = pagination.value
    const response = await fetchUsageWithRetry(
      () => keysAPI.list(page, page_size, filters, { signal }),
      signal,
    )
    if (!isCurrentRequest()) return
    // Deleting the last key on the last page leaves that page empty: show the one before it instead.
    if (response.items.length === 0 && response.total > 0 && page > 1) {
      pagination.value.page = Math.max(1, Math.min(page - 1, response.pages || 1))
      void loadApiKeys()
      return
    }
    loadFailed.value = false
    lastLoadFiltered.value = filters.search !== undefined || filters.status !== undefined || filters.group_id !== undefined
    apiKeys.value = response.items
    pagination.value.total = response.total
    pagination.value.pages = response.pages

    // Load usage stats for all API keys in the list
    if (response.items.length > 0) {
      const keyIds = response.items.map((k) => k.id)
      usagePending.value = true
      try {
        const usageResponse = await usageAPI.getDashboardApiKeysUsage(keyIds, { signal })
        if (!isCurrentRequest()) return
        usageStats.value = usageResponse.stats
      } catch (e) {
        if (isCurrentRequest() && !isAbortError(e)) {
          console.error('Failed to load usage stats:', e)
        }
      } finally {
        if (abortController === controller) usagePending.value = false
      }
    }
  } catch (error) {
    if (!isCurrentRequest() || isAbortError(error)) {
      return
    }
    loadFailed.value = true
    appStore.showError(t('keys.failedToLoad'))
  } finally {
    if (abortController === controller) {
      loading.value = false
    }
  }
}

const loadGroups = async () => {
  try {
    groups.value = await userGroupsAPI.getAvailable({ includeBlocked: true })
  } catch (error) {
    console.error('Failed to load groups:', error)
  }
}

const loadUserGroupRates = async () => {
  try {
    userGroupRates.value = await userGroupsAPI.getUserGroupRates()
  } catch (error) {
    console.error('Failed to load user group rates:', error)
  }
}

const loadPublicSettings = async () => {
  try {
    publicSettings.value = await authAPI.getPublicSettings()
  } catch (error) {
    console.error('Failed to load public settings:', error)
  }
}

const loadUserEndpointSettings = async () => {
  try {
    const settings = await authAPI.getUserEndpointSettings()
    customEndpoints.value = settings.custom_endpoints || []
  } catch (error) {
    console.error('Failed to load API endpoint settings:', error)
  }
}

const handlePageChange = (page: number) => {
  pagination.value.page = page
  loadApiKeys()
}

const handlePageSizeChange = (pageSize: number) => {
  pagination.value.page_size = pageSize
  pagination.value.page = 1
  loadApiKeys()
}

const editKey = (key: ApiKey) => {
  selectedKey.value = key
  failoverSettings.value = getFailoverSettings(key)
  systemPromptSettings.value = getSystemPromptSettings(key)
  const hasIPRestriction = (key.ip_whitelist?.length > 0) || (key.ip_blacklist?.length > 0)
  const hasExpiration = !!key.expires_at
  formData.value = {
    name: key.name,
    group_id: key.group_id,
    status: key.status === 'quota_exhausted' || key.status === 'expired' ? 'inactive' : key.status,
    use_custom_key: false,
    custom_key: '',
    enable_ip_restriction: hasIPRestriction,
    ip_whitelist: (key.ip_whitelist || []).join('\n'),
    ip_blacklist: (key.ip_blacklist || []).join('\n'),
    enable_quota: key.quota > 0,
    quota: key.quota > 0 ? key.quota : null,
    enable_rate_limit: (key.rate_limit_5h > 0) || (key.rate_limit_1d > 0) || (key.rate_limit_7d > 0),
    rate_limit_5h: key.rate_limit_5h || null,
    rate_limit_1d: key.rate_limit_1d || null,
    rate_limit_7d: key.rate_limit_7d || null,
    enable_expiration: hasExpiration,
    expiration_preset: 'custom',
    expiration_date: key.expires_at ? formatDateTimeLocal(key.expires_at) : ''
  }
  showEditModal.value = true
}

const openFailoverModal = (key: ApiKey) => {
  selectedFailoverKey.value = key
  failoverSettings.value = getFailoverSettings(key)
  showFailoverModal.value = true
}

const closeFailoverModal = () => {
  showFailoverModal.value = false
  selectedFailoverKey.value = null
}

const replaceApiKey = (key: ApiKey) => {
  apiKeys.value = apiKeys.value.map((item) => item.id === key.id ? key : item)
  if (selectedKey.value?.id === key.id) selectedKey.value = key
  if (selectedFailoverKey.value?.id === key.id) selectedFailoverKey.value = key
}

const saveFailoverSettings = async () => {
  if (!selectedFailoverKey.value || savingFailover.value || releasingFailover.value) return
  if (!validateFailoverSettings(failoverPrimaryGroup.value)) return
  const keyId = selectedFailoverKey.value.id
  failoverRefreshController?.abort()
  savingFailover.value = true
  try {
    const key = await keysAPI.update(keyId, { ...failoverSettings.value })
    replaceApiKey(key)
    appStore.showSuccess(t('keys.keyUpdatedSuccess'))
    closeFailoverModal()
  } catch (error: any) {
    appStore.showError(error.response?.data?.detail || error.message || t('keys.failedToSave'))
  } finally {
    savingFailover.value = false
  }
}

const releaseFailoverCooldown = async () => {
  if (!selectedFailoverKey.value || savingFailover.value || releasingFailover.value) return
  const keyId = selectedFailoverKey.value.id
  failoverRefreshController?.abort()
  releasingFailover.value = true
  try {
    const key = await keysAPI.update(keyId, { release_failover_cooldown: true })
    replaceApiKey(key)
    appStore.showSuccess(t('keys.failover.released'))
  } catch (error: any) {
    appStore.showError(error.response?.data?.detail || error.message || t('keys.failover.failedToRelease'))
  } finally {
    releasingFailover.value = false
  }
}

const refreshFailoverStatuses = async () => {
  if (loading.value || submitting.value || savingFailover.value || releasingFailover.value ||
    failoverRefreshController || !apiKeys.value.some((key) => key.failover_enabled)) return
  const controller = new AbortController()
  failoverRefreshController = controller
  try {
    const response = await keysAPI.list(pagination.value.page, pagination.value.page_size, {
      search: filterSearch.value || undefined,
      status: filterStatus.value || undefined,
      group_id: filterGroupId.value === '' ? undefined : filterGroupId.value,
      ...sortState.value
    }, { signal: controller.signal })
    if (controller.signal.aborted) return
    for (const key of response.items) {
      const existing = apiKeys.value.find((item) => item.id === key.id)
      if (existing) replaceApiKey({
        ...existing,
        ...getFailoverSettings(key),
        failover_group: key.failover_group,
        failover_cooldown_until: key.failover_cooldown_until
      })
    }
  } catch (error) {
    if (!isAbortError(error)) console.error('Failed to refresh API key failover status:', error)
  } finally {
    if (failoverRefreshController === controller) failoverRefreshController = null
  }
}

const toggleKeyStatus = async (key: ApiKey) => {
  if (togglingKeyIds.has(key.id)) return
  const newStatus = key.status === 'active' ? 'inactive' : 'active'
  togglingKeyIds.add(key.id)
  try {
    const updated = await keysAPI.toggleStatus(key.id, newStatus)
    const current = apiKeys.value.find((item) => item.id === key.id)
    if (current) replaceApiKey({ ...current, status: updated?.status ?? newStatus })
    appStore.showSuccess(
      newStatus === 'active' ? t('keys.keyEnabledSuccess') : t('keys.keyDisabledSuccess')
    )
    loadApiKeys()
  } catch (error) {
    appStore.showError(t('keys.failedToUpdateStatus'))
  } finally {
    togglingKeyIds.delete(key.id)
  }
}

const changeGroup = async (key: ApiKey, newGroupId: number | null) => {
  if (key.group_id === newGroupId) return

  try {
    await keysAPI.update(key.id, { group_id: newGroupId })
    const newGroup = groups.value.find((group) => group.id === newGroupId)
    const current = apiKeys.value.find((item) => item.id === key.id)
    if (current && newGroup) replaceApiKey({ ...current, group_id: newGroup.id, group: newGroup })
    const groupName = newGroup?.name
    appStore.showSuccess(
      groupName ? t('keys.groupChangedDetail', { key: key.name, group: groupName }) : t('keys.groupChangedSuccess'),
      3500,
      groupName ? { title: t('keys.groupChangedSuccess') } : undefined
    )
    loadApiKeys()
  } catch (error) {
    const typedError = error as any
    const detail = typedError?.response?.data?.detail || typedError?.message
    appStore.showError(
      (typedError?.code === 'GROUP_BLOCKED' || typedError?.reason === 'GROUP_BLOCKED')
        ? t('keys.groupBlockedToast')
        : detail || t('keys.failedToChangeGroup')
    )
  }
}

const closeFieldMenuOnOutsideClick = (event: MouseEvent) => {
  const target = event.target as Node | null
  if (fieldMenuRef.value && target && !fieldMenuRef.value.contains(target)) {
    showFieldMenu.value = false
  }
}

const clearFilters = () => {
  filterSearch.value = ''
  filterStatus.value = ''
  filterGroupId.value = ''
  onFilterChange()
}

const cardActionHandlers: Record<KeyCardAction, (key: ApiKey) => void> = {
  group: (key) => openKeyGroupPicker(key),
  failover: (key) => openFailoverModal(key),
  resetRateLimit: (key) => confirmResetRateLimitFromCard(key),
  toggle: (key) => void toggleKeyStatus(key),
  edit: (key) => editKey(key),
  delete: (key) => confirmDelete(key)
}

const handleCardAction = (name: KeyCardAction, key: ApiKey) => {
  cardActionHandlers[name](key)
}

const confirmDelete = (key: ApiKey) => {
  selectedKey.value = key
  showDeleteDialog.value = true
}

const handleSubmit = async () => {
  // Validate group_id is required
  if (formData.value.group_id === null) {
    appStore.showError(t('keys.groupRequired'))
    return
  }
  if (showEditModal.value && !validateFailoverSettings(editPrimaryGroup.value)) return
  if (showEditModal.value && systemPromptSettings.value.custom_system_prompt_enabled) {
    const prompt = systemPromptSettings.value.custom_system_prompt
    if (!prompt.trim()) {
      appStore.showError(t('keys.customSystemPrompt.required'))
      return
    }
    if (new TextEncoder().encode(prompt).length > 32768) {
      appStore.showError(t('keys.customSystemPrompt.tooLong'))
      return
    }
  }

  // Validate custom key if enabled
  if (!showEditModal.value && formData.value.use_custom_key) {
    if (!formData.value.custom_key) {
      appStore.showError(t('keys.customKeyRequired'))
      return
    }
    if (customKeyError.value) {
      appStore.showError(customKeyError.value)
      return
    }
  }

  // Parse IP lists only if IP restriction is enabled
  const parseIPList = (text: string): string[] =>
    text.split('\n').map(ip => ip.trim()).filter(ip => ip.length > 0)
  const ipWhitelist = formData.value.enable_ip_restriction ? parseIPList(formData.value.ip_whitelist) : []
  const ipBlacklist = formData.value.enable_ip_restriction ? parseIPList(formData.value.ip_blacklist) : []

  // Calculate quota value (null/empty/0 = unlimited, stored as 0)
  const quota = formData.value.quota && formData.value.quota > 0 ? formData.value.quota : 0

  // Calculate expiration
  let expiresInDays: number | undefined
  let expiresAt: string | null | undefined
  if (formData.value.enable_expiration && formData.value.expiration_date) {
    if (!showEditModal.value) {
      // Create mode: calculate days from date
      const expDate = new Date(formData.value.expiration_date)
      const now = new Date()
      const diffDays = Math.ceil((expDate.getTime() - now.getTime()) / (1000 * 60 * 60 * 24))
      expiresInDays = diffDays > 0 ? diffDays : 1
    } else {
      // Edit mode: use custom date directly
      expiresAt = new Date(formData.value.expiration_date).toISOString()
    }
  } else if (showEditModal.value) {
    // Edit mode: if expiration disabled or date cleared, send empty string to clear
    expiresAt = ''
  }

  // Calculate rate limit values (send 0 when toggle is off)
  const rateLimitData = formData.value.enable_rate_limit ? {
    rate_limit_5h: formData.value.rate_limit_5h && formData.value.rate_limit_5h > 0 ? formData.value.rate_limit_5h : 0,
    rate_limit_1d: formData.value.rate_limit_1d && formData.value.rate_limit_1d > 0 ? formData.value.rate_limit_1d : 0,
    rate_limit_7d: formData.value.rate_limit_7d && formData.value.rate_limit_7d > 0 ? formData.value.rate_limit_7d : 0,
  } : { rate_limit_5h: 0, rate_limit_1d: 0, rate_limit_7d: 0 }

  failoverRefreshController?.abort()
  submitting.value = true
  try {
    if (showEditModal.value && selectedKey.value) {
      const updates: UpdateApiKeyRequest = {
        ...failoverSettings.value,
        ...systemPromptSettings.value,
        name: formData.value.name,
        group_id: formData.value.group_id,
        ip_whitelist: ipWhitelist,
        ip_blacklist: ipBlacklist,
        quota: quota,
        expires_at: expiresAt,
        rate_limit_5h: rateLimitData.rate_limit_5h,
        rate_limit_1d: rateLimitData.rate_limit_1d,
        rate_limit_7d: rateLimitData.rate_limit_7d,
      }
      if (shouldSubmitEditStatus(selectedKey.value, formData.value.status)) {
        updates.status = formData.value.status
      }
      await keysAPI.update(selectedKey.value.id, updates)
      appStore.showSuccess(t('keys.keyUpdatedSuccess'))
    } else {
      const customKey = formData.value.use_custom_key ? formData.value.custom_key : undefined
      const createdKey = await keysAPI.create(
        formData.value.name,
        formData.value.group_id,
        customKey,
        ipWhitelist,
        ipBlacklist,
        quota,
        expiresInDays,
        rateLimitData
      )
      createdApiKey.value = createdKey.key
      appStore.showSuccess(t('keys.keyCreatedSuccess'))
      // Only advance tour if active, on submit step, and creation succeeded
      if (onboardingStore.isCurrentStep('[data-tour="key-form-submit"]')) {
        onboardingStore.nextStep(500)
      }
    }
    closeModals()
    loadApiKeys()
  } catch (error: any) {
    const errorMsg = (error?.code === 'GROUP_BLOCKED' || error?.reason === 'GROUP_BLOCKED')
      ? t('keys.groupBlockedToast')
      : error.response?.data?.detail || error.message || t('keys.failedToSave')
    appStore.showError(errorMsg)
    // Don't advance tour on error
  } finally {
    submitting.value = false
  }
}

/**
 * 处理删除 API Key 的操作
 * 优化：错误处理改进，优先显示后端返回的具体错误消息（如权限不足等），
 * 若后端未返回消息则显示默认的国际化文本
 */
const handleDelete = async () => {
  if (!selectedKey.value || deleting.value) return

  deleting.value = true
  const deletedId = selectedKey.value.id
  try {
    await keysAPI.delete(deletedId)
    apiKeys.value = apiKeys.value.filter((key) => key.id !== deletedId)
    appStore.showSuccess(t('keys.keyDeletedSuccess'))
    showDeleteDialog.value = false
    loadApiKeys()
  } catch (error: any) {
    // 优先使用后端返回的错误消息，提供更具体的错误信息给用户
    const errorMsg = error?.message || t('keys.failedToDelete')
    appStore.showError(errorMsg)
  } finally {
    deleting.value = false
  }
}

const closeModals = () => {
  if (groupPickerTarget.value?.kind === 'form') closeGroupPicker()
  showCreateModal.value = false
  showEditModal.value = false
  selectedKey.value = null
  failoverSettings.value = getFailoverSettings()
  systemPromptSettings.value = getSystemPromptSettings()
  formData.value = {
    name: '',
    group_id: null,
    status: 'active',
    use_custom_key: false,
    custom_key: '',
    enable_ip_restriction: false,
    ip_whitelist: '',
    ip_blacklist: '',
    enable_quota: false,
    quota: null,
    enable_rate_limit: false,
    rate_limit_5h: null,
    rate_limit_1d: null,
    rate_limit_7d: null,
    enable_expiration: false,
    expiration_preset: '30',
    expiration_date: ''
  }
}

// Show reset quota confirmation dialog
const confirmResetQuota = () => {
  showResetQuotaDialog.value = true
}

// Set expiration date based on quick select days
const setExpirationDays = (days: number) => {
  formData.value.expiration_preset = days.toString() as '7' | '30' | '90'
  const expDate = new Date()
  expDate.setDate(expDate.getDate() + days)
  formData.value.expiration_date = formatDateTimeLocal(expDate.toISOString())
}

// Reset quota used for an API key
const resetQuotaUsed = async () => {
  if (!selectedKey.value) return
  showResetQuotaDialog.value = false
  try {
    await keysAPI.update(selectedKey.value.id, { reset_quota: true })
    appStore.showSuccess(t('keys.quotaResetSuccess'))
    // Update local state
    if (selectedKey.value) {
      selectedKey.value.quota_used = 0
    }
  } catch (error: any) {
    const errorMsg = error.response?.data?.detail || t('keys.failedToResetQuota')
    appStore.showError(errorMsg)
  }
}

// Show reset rate limit confirmation dialog (from edit modal)
const confirmResetRateLimit = () => {
  showResetRateLimitDialog.value = true
}

// Show reset rate limit confirmation dialog (from a key card)
const confirmResetRateLimitFromCard = (key: ApiKey) => {
  selectedKey.value = key
  showResetRateLimitDialog.value = true
}

// Reset rate limit usage for an API key
const resetRateLimitUsage = async () => {
  if (!selectedKey.value) return
  showResetRateLimitDialog.value = false
  try {
    await keysAPI.update(selectedKey.value.id, { reset_rate_limit_usage: true })
    appStore.showSuccess(t('keys.rateLimitResetSuccess'))
    // Refresh key data
    await loadApiKeys()
    // Update the editing key with fresh data
    const refreshedKey = apiKeys.value.find(k => k.id === selectedKey.value!.id)
    if (refreshedKey) {
      selectedKey.value = refreshedKey
    }
  } catch (error: any) {
    const errorMsg = error.response?.data?.detail || t('keys.failedToResetRateLimit')
    appStore.showError(errorMsg)
  }
}

onMounted(() => {
  loadCardFields()
  loadApiKeys()
  loadGroups()
  loadUserGroupRates()
  loadPublicSettings()
  loadUserEndpointSettings()
  document.addEventListener('click', closeFieldMenuOnOutsideClick)
  failoverRefreshTimer = setInterval(() => { void refreshFailoverStatuses() }, 15000)
})

onUnmounted(() => {
  document.removeEventListener('click', closeFieldMenuOnOutsideClick)
  stopClock()
  if (failoverRefreshTimer) clearInterval(failoverRefreshTimer)
  failoverRefreshController?.abort()
  abortController?.abort()
})
</script>

<style scoped>
.keys-page {
  display: flex;
  flex-direction: column;
  gap: 1.25rem;
}

/* ---------- Toolbar ---------- */
.keys-toolbar {
  display: flex;
  flex-direction: column;
  gap: 0.875rem;
  padding: 1rem;
  border-radius: 1.5rem;
  background: var(--md-sys-color-surface);
  box-shadow: var(--theme-highlight), var(--theme-elevation-1);
}

.keys-toolbar-main {
  display: flex;
  flex-direction: column;
  gap: 0.75rem;
}

.keys-toolbar-filters {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 0.625rem;
}

.keys-search {
  grid-column: 1 / -1;
}

.keys-list-controls {
  display: flex;
  min-width: 0;
  flex: 1 1 100%;
  align-items: center;
  justify-content: space-between;
  gap: 0.625rem;
}

.keys-sort {
  display: flex;
  min-width: 0;
  gap: 0.375rem;
}

.keys-sort-select {
  width: 10.5rem;
  min-width: 0;
  flex: 0 1 auto;
}

.keys-sort-value {
  display: flex;
  min-width: 0;
  align-items: center;
  gap: 0.375rem;
}

.keys-sort-glyph {
  flex-shrink: 0;
  color: var(--md-sys-color-on-surface-variant);
}

/* Same footprint and surface as the select it sits beside. */
.keys-sort-order {
  display: inline-flex;
  width: 2.75rem;
  flex-shrink: 0;
  align-items: center;
  justify-content: center;
  border-radius: 0.75rem;
  background: var(--md-sys-color-surface-container);
  color: var(--md-sys-color-on-surface);
  transition: background-color 160ms ease;
}

.keys-sort-order:hover {
  background: var(--md-sys-color-surface-container-high);
}

.keys-sort-order:focus-visible {
  outline: none;
  box-shadow: 0 0 0 2px rgb(233 184 36 / 0.45);
}

.keys-toolbar-actions {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 0.5rem;
}

.keys-toolbar-button {
  padding: 0.5625rem 0.875rem;
  border-radius: 0.875rem;
  white-space: nowrap;
}

.keys-toolbar-meta {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 0.5rem 1rem;
}

.keys-toolbar-meta:empty {
  display: none;
}

.keys-count {
  flex-shrink: 0;
  padding: 0.1875rem 0.625rem;
  white-space: nowrap;
  border-radius: 999px;
  background: #faf6e8;
  font-size: 0.75rem;
  font-variant-numeric: tabular-nums;
  color: var(--md-sys-color-on-surface-variant);
}

.keys-field-menu {
  border-radius: 1rem;
  background: var(--md-sys-color-surface);
  box-shadow: var(--theme-highlight), var(--theme-elevation-3);
}

.keys-field-option {
  display: flex;
  width: calc(100% - 0.75rem);
  margin: 0 0.375rem;
  align-items: center;
  justify-content: space-between;
  padding: 0.5rem 0.75rem;
  border-radius: 0.75rem;
  font-size: 0.8125rem;
  text-align: left;
  color: var(--md-sys-color-on-surface);
}

.keys-field-option:hover {
  background: color-mix(in srgb, var(--md-sys-color-primary) 14%, var(--md-sys-color-surface));
}

/* ---------- Card grid ---------- */
/* Columns follow the space actually available, so the count per row tracks the
   window, the sidebar and the browser zoom; a card never gets narrower than 20rem
   unless the screen itself is. */
.keys-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(min(100%, 20rem), 1fr));
  gap: 0.875rem;
  transition: opacity 160ms ease;
}

/* Reloads keep the current cards in place; they only fade if the answer takes a moment. */
.keys-grid.is-refreshing {
  opacity: 0.6;
  transition-delay: 200ms;
}

.keys-skeleton-card {
  display: flex;
  min-height: 26rem;
  flex-direction: column;
  gap: 0.75rem;
  padding: 1rem;
  border-radius: 1.25rem;
  background: var(--md-sys-color-surface);
  box-shadow: var(--theme-highlight), var(--theme-elevation-1);
}

.keys-skeleton {
  border-radius: 0.75rem;
  background: #f4ecd2;
  animation: keys-skeleton-pulse 1.4s ease-in-out infinite;
}

.keys-empty {
  border-radius: 1.5rem;
  background: var(--md-sys-color-surface);
  box-shadow: var(--theme-highlight), var(--theme-elevation-1);
}

.keys-state {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 0.5rem;
  padding: 3rem 1.5rem;
  text-align: center;
}

.keys-state-glyph {
  display: inline-flex;
  width: 3rem;
  height: 3rem;
  align-items: center;
  justify-content: center;
  margin-bottom: 0.25rem;
  border-radius: 999px;
  background: #fff3bd;
  color: #7e5600;
}

.keys-state-glyph.is-error {
  background: #fee2e2;
  color: #b91c1c;
}

.keys-state-title {
  font-size: 1rem;
  font-weight: 650;
  color: var(--md-sys-color-on-surface);
}

.keys-state-hint {
  max-width: 26rem;
  margin-bottom: 0.5rem;
  font-size: 0.875rem;
  color: var(--md-sys-color-on-surface-variant);
}

/* ---------- Key form: group field that opens the group picker ---------- */
.key-group-field {
  display: flex;
  width: 100%;
  min-height: 3rem;
  align-items: center;
  gap: 0.75rem;
  padding: 0.5rem 0.875rem 0.5rem 1rem;
  border-radius: 1rem;
  text-align: left;
  background: var(--md-sys-color-surface-container);
  box-shadow: inset 0 1px 2px rgb(63 52 32 / 0.06);
  transition: background-color 160ms ease, box-shadow 160ms ease;
}

.key-group-field:hover {
  background: color-mix(in srgb, var(--md-sys-color-surface-container) 82%, var(--md-sys-color-surface-container-high));
}

.key-group-field:focus-visible,
.key-group-field.is-open {
  outline: none;
  background: var(--md-sys-color-surface);
  box-shadow: inset 0 0 0 1.5px rgb(233 184 36 / 0.75), 0 0 0 4px rgb(233 184 36 / 0.14);
}

.key-group-field-value {
  display: flex;
  min-width: 0;
  flex: 1;
  flex-wrap: wrap;
  align-items: center;
  gap: 0.375rem;
}

.key-group-field-placeholder {
  font-size: 0.875rem;
  color: var(--md-sys-color-placeholder);
}

.key-group-field-icon {
  flex-shrink: 0;
  color: var(--md-sys-color-on-surface-variant);
}

@keyframes keys-skeleton-pulse {
  0%,
  100% { opacity: 1; }
  50% { opacity: 0.5; }
}

@media (prefers-reduced-motion: reduce) {
  .keys-skeleton {
    animation: none;
  }

  .keys-grid,
  .keys-grid.is-refreshing {
    transition: none;
  }
}

@media (min-width: 640px) {
  .keys-toolbar-filters {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
  }

  .keys-search {
    width: 16rem;
  }

  .keys-filter-select {
    width: 10rem;
  }
}

@media (min-width: 768px) {
  .keys-toolbar {
    padding: 1.125rem 1.25rem;
  }

  .keys-list-controls {
    flex: 0 0 auto;
    margin-left: auto;
  }

  .keys-grid {
    gap: 1rem;
  }
}

/* Filters and actions share a row when they fit; otherwise the actions wrap
   below and stay right-aligned instead of squeezing their labels. */
@media (min-width: 1024px) {
  .keys-toolbar-main {
    flex-direction: row;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
  }

  .keys-toolbar-filters {
    flex: 1 1 auto;
    min-width: 0;
  }

  .keys-toolbar-actions {
    flex: 0 0 auto;
    flex-wrap: nowrap;
    justify-content: flex-end;
    margin-left: auto;
  }
}
</style>
