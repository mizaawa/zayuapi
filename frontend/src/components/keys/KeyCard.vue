<template>
  <article class="key-card" :class="`is-${apiKey.status}`" :data-test="`key-card-${apiKey.id}`">
    <header class="key-card-head">
      <div class="key-card-title">
        <div class="key-card-name-row">
          <h3 class="key-card-name" :title="apiKey.name">{{ apiKey.name }}</h3>
          <span
            v-if="hasIpRestriction"
            class="key-card-shield"
            :title="t('keys.ipRestrictionEnabled')"
            :aria-label="t('keys.ipRestrictionEnabled')"
            role="img"
          >
            <Icon name="shield" size="sm" />
          </span>
        </div>
        <p v-if="show('id') || show('last_used_at')" class="key-card-meta">
          <span v-if="show('id')" class="key-card-id" data-test="key-id">#{{ apiKey.id }}</span>
          <span
            v-if="show('last_used_at')"
            data-test="key-last-used"
            :title="apiKey.last_used_at ? formatDateTime(apiKey.last_used_at) : undefined"
          >
            {{ apiKey.last_used_at ? t('keys.lastUsedRelative', { time: formatRelativeTime(apiKey.last_used_at) }) : t('keys.neverUsed') }}
          </span>
        </p>
      </div>
      <span class="key-status" :class="`key-status-${apiKey.status}`" data-test="key-status">
        <span class="key-status-dot" aria-hidden="true"></span>
        {{ t('keys.status.' + apiKey.status) }}
      </span>
    </header>

    <!-- The key itself is the one thing people come here for: a tag that copies on click. -->
    <button
      type="button"
      class="key-secret"
      :class="{ 'is-copied': copied }"
      :aria-label="copied ? t('keys.copied') : t('keys.copyToClipboard')"
      data-test="key-copy"
      @click="emit('action', 'copy', apiKey)"
    >
      <code class="key-secret-text">{{ maskApiKey(apiKey.key) }}</code>
      <span class="key-secret-hint">
        <Icon :name="copied ? 'check' : 'clipboard'" size="sm" :stroke-width="copied ? 2 : 1.5" />
        {{ copied ? t('keys.copied') : t('keys.copyToClipboard') }}
      </span>
    </button>

    <!-- The title carries the full group name for when a narrow card cuts it short. -->
    <button
      type="button"
      class="key-card-group"
      :title="apiKey.group ? `${apiKey.group.name} · ${t('keys.clickToChangeGroup')}` : t('keys.clickToChangeGroup')"
      :data-test="`key-group-button-${apiKey.id}`"
      @click="emit('action', 'group', apiKey)"
    >
      <!-- 分组名按字数省略（悬停看全名），min-w-0 让徽章在窄卡片里继续收缩，不会把卡片撑宽 -->
      <GroupBadge
        v-if="apiKey.group"
        class="min-w-0 max-w-full"
        :name="apiKey.group.name"
        :max-name-length="GROUP_NAME_MAX_LENGTH"
        :platform="apiKey.group.platform"
        :subscription-type="apiKey.group.subscription_type"
        :rate-multiplier="apiKey.group.rate_multiplier"
        :user-rate-multiplier="userGroupRate"
        :peak-rate-enabled="apiKey.group.peak_rate_enabled"
        :peak-start="apiKey.group.peak_start"
        :peak-end="apiKey.group.peak_end"
        :peak-rate-multiplier="apiKey.group.peak_rate_multiplier"
      />
      <span v-else class="key-card-group-empty">{{ t('keys.noGroup') }}</span>
      <span v-if="apiKey.group && groupBlocked" class="key-card-blocked">{{ t('common.groupBlocked') }}</span>
      <span class="key-card-group-switch" aria-hidden="true">
        <Icon name="chevronDown" size="sm" />
      </span>
    </button>

    <dl v-if="show('usage') || show('current_concurrency')" class="key-card-stats">
      <template v-if="show('usage')">
        <div>
          <dt>{{ t('keys.today') }}</dt>
          <dd :title="exactCost(usage?.today_actual_cost)" data-test="key-today-cost">
            <span v-if="usage">{{ formatKeyCost(usage.today_actual_cost) }}</span>
            <span v-else class="key-card-pending" :class="{ 'is-loading': usagePending }">—</span>
          </dd>
        </div>
        <div>
          <dt>{{ t('keys.total') }}</dt>
          <dd :title="exactCost(usage?.total_actual_cost)" data-test="key-total-cost">
            <span v-if="usage">{{ formatKeyCost(usage.total_actual_cost) }}</span>
            <span v-else class="key-card-pending" :class="{ 'is-loading': usagePending }">—</span>
          </dd>
        </div>
      </template>
      <div v-if="show('current_concurrency')">
        <dt>{{ t('keys.currentConcurrency') }}</dt>
        <dd data-test="current-concurrency">
          <span class="key-live" :class="concurrency > 0 ? 'is-busy' : 'is-idle'">
            <span class="key-live-dot" aria-hidden="true"></span>{{ concurrency }}
          </span>
        </dd>
      </div>
    </dl>

    <template v-if="show('tokens')">
      <KeyTokenMix v-if="usage" :usage="usage" />
      <div v-else-if="usagePending" class="key-card-mix-placeholder" aria-hidden="true"></div>
    </template>

    <section v-if="showQuota || windows.length > 0" class="key-card-limits" data-test="key-limits">
      <div v-if="showQuota" class="key-limit">
        <div class="key-limit-row">
          <span class="key-limit-label">{{ t('keys.quota') }}</span>
          <span class="key-limit-value" :class="`is-${usageTone(apiKey.quota_used, apiKey.quota)}`">
            ${{ (apiKey.quota_used ?? 0).toFixed(2) }} / ${{ apiKey.quota.toFixed(2) }}
          </span>
        </div>
        <div class="key-meter">
          <div
            class="key-meter-fill"
            :class="`is-${usageTone(apiKey.quota_used, apiKey.quota)}`"
            :style="{ width: usagePercent(apiKey.quota_used, apiKey.quota) + '%' }"
          />
        </div>
      </div>
      <div v-for="window in windows" :key="window.key" class="key-limit" :data-test="`key-window-${window.key}`">
        <div class="key-limit-row">
          <span class="key-limit-label">{{ window.key }}</span>
          <span class="key-limit-value" :class="`is-${usageTone(window.used, window.limit)}`">
            ${{ window.used.toFixed(2) }} / ${{ window.limit.toFixed(2) }}
          </span>
        </div>
        <div class="key-meter key-meter-thin">
          <div
            class="key-meter-fill"
            :class="`is-${usageTone(window.used, window.limit)}`"
            :style="{ width: usagePercent(window.used, window.limit) + '%' }"
          />
        </div>
        <span v-if="resetCountdown(window.resetAt)" class="key-limit-reset">⟳ {{ resetCountdown(window.resetAt) }}</span>
      </div>
      <button
        v-if="windows.length > 0 && hasWindowUsage"
        type="button"
        class="key-limit-reset-button"
        :title="t('keys.resetRateLimitUsage')"
        data-test="key-reset-rate-limit"
        @click="emit('action', 'resetRateLimit', apiKey)"
      >
        <Icon name="refresh" size="xs" />
        {{ t('keys.resetUsage') }}
      </button>
    </section>

    <dl v-if="detailFields.length > 0" class="key-card-details">
      <div v-if="show('failover')">
        <dt>{{ t('keys.failover.group') }}</dt>
        <dd>
          <button
            type="button"
            :data-test="`failover-status-${apiKey.id}`"
            :disabled="!failoverSupported"
            :title="failoverSupported ? undefined : t('keys.failover.supportedPlatforms')"
            :class="[
              'key-failover inline-flex max-w-full items-center justify-center gap-1.5 rounded-full px-2.5 py-0.5 text-xs font-semibold transition-colors focus:outline-none focus-visible:ring-2 focus-visible:ring-primary-500 disabled:cursor-not-allowed',
              !apiKey.failover_enabled
                ? 'bg-gray-100 text-gray-500 hover:bg-gray-200 dark:bg-dark-700 dark:text-dark-400 dark:hover:bg-dark-600'
                : failoverActive
                  ? 'bg-red-50 text-red-700 hover:bg-red-100 dark:bg-red-900/25 dark:text-red-300 dark:hover:bg-red-900/40'
                  : 'bg-emerald-50 text-emerald-700 hover:bg-emerald-100 dark:bg-emerald-900/25 dark:text-emerald-300 dark:hover:bg-emerald-900/40'
            ]"
            @click="emit('action', 'failover', apiKey)"
          >{{ !apiKey.failover_enabled ? t('keys.failover.disabled') : failoverActive ? t('keys.failover.active') : t('keys.failover.healthy') }}</button>
        </dd>
      </div>
      <div v-if="show('expires_at')">
        <dt>{{ t('keys.expiresAt') }}</dt>
        <dd data-test="key-expiry">
          <template v-if="apiKey.expires_at">
            <span v-if="expiry === 'expired'" class="key-tag key-tag-danger">{{ t('keys.status.expired') }}</span>
            <span v-else-if="expiry === 'soon'" class="key-tag key-tag-warning">
              {{ t('keys.expiresSoon', { days: daysUntil(apiKey.expires_at, now) }) }}
            </span>
            <span class="key-card-detail-text">{{ formatDateTime(apiKey.expires_at) }}</span>
          </template>
          <span v-else class="key-card-detail-muted">{{ t('keys.noExpiration') }}</span>
        </dd>
      </div>
      <div v-if="show('created_at')">
        <dt>{{ t('keys.created') }}</dt>
        <dd class="key-card-detail-text">{{ formatDateTime(apiKey.created_at) }}</dd>
      </div>
      <div v-if="show('last_used_ip')">
        <dt>{{ t('keys.lastUsedIP') }}</dt>
        <dd data-test="last-used-ip" class="key-card-detail-text key-card-ip">{{ apiKey.last_used_ip || '-' }}</dd>
      </div>
    </dl>

    <footer class="key-card-actions">
      <button
        v-for="action in actions"
        :key="action.name"
        type="button"
        class="key-action"
        :class="`key-action-${action.tone}`"
        :disabled="action.name === 'toggle' && toggling"
        :title="action.label"
        :data-test="`key-action-${action.name}`"
        @click="emit('action', action.name, apiKey)"
      >
        <Icon :name="action.icon" size="sm" />
        <span class="key-action-label">{{ action.label }}</span>
      </button>
    </footer>
  </article>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { ApiKey } from '@/types'
import type { BatchApiKeyUsageStats } from '@/api/usage'
import GroupBadge from '@/components/common/GroupBadge.vue'
import Icon from '@/components/icons/Icon.vue'
import KeyTokenMix from './KeyTokenMix.vue'
import {
  daysUntil,
  expiryState,
  formatKeyCost,
  formatResetCountdown,
  isFailoverActive,
  rateLimitWindows,
  supportsFailover,
  usagePercent,
  usageTone,
  type KeyCardField
} from './keyCards'
import { formatDateTime, formatRelativeTime } from '@/utils/format'
import { maskApiKey } from '@/utils/maskApiKey'

export type KeyCardAction = 'copy' | 'group' | 'failover' | 'resetRateLimit' | 'use' | 'ccs' | 'toggle' | 'edit' | 'delete'

// 卡片里分组名最多显示的字数，超出用"…"省略（悬停看全名）
const GROUP_NAME_MAX_LENGTH = 20

const props = withDefaults(defineProps<{
  apiKey: ApiKey
  /** Absent while the batch usage request is in flight or after it failed. */
  usage?: BatchApiKeyUsageStats
  usagePending?: boolean
  hiddenFields: ReadonlySet<KeyCardField>
  /** Shared page clock in ms; the page advances it only when a card's text can change. */
  now: number
  copied?: boolean
  toggling?: boolean
  groupBlocked?: boolean
  userGroupRate?: number
  showCcsImport?: boolean
}>(), {
  usage: undefined,
  usagePending: false,
  copied: false,
  toggling: false,
  groupBlocked: false,
  userGroupRate: undefined,
  showCcsImport: true
})

const emit = defineEmits<{
  action: [name: KeyCardAction, key: ApiKey]
}>()

const { t } = useI18n()

const show = (field: KeyCardField) => !props.hiddenFields.has(field)

const hasIpRestriction = computed(() =>
  (props.apiKey.ip_whitelist?.length ?? 0) > 0 || (props.apiKey.ip_blacklist?.length ?? 0) > 0
)
const concurrency = computed(() => props.apiKey.current_concurrency ?? 0)
const exactCost = (amount: number | undefined) => (amount === undefined ? undefined : `$${amount.toFixed(4)}`)

const showQuota = computed(() => show('usage') && props.apiKey.quota > 0)
const windows = computed(() => (show('rate_limit') ? rateLimitWindows(props.apiKey) : []))
const hasWindowUsage = computed(() =>
  props.apiKey.usage_5h > 0 || props.apiKey.usage_1d > 0 || props.apiKey.usage_7d > 0
)
const resetCountdown = (resetAt: string | null) => formatResetCountdown(resetAt, props.now, t('keys.resetNow'))

const failoverSupported = computed(() => supportsFailover(props.apiKey.group))
const failoverActive = computed(() => isFailoverActive(props.apiKey, props.now))
const expiry = computed(() => (props.apiKey.expires_at ? expiryState(props.apiKey.expires_at, props.now) : null))

const detailFields = computed(() =>
  (['failover', 'expires_at', 'created_at', 'last_used_ip'] as const).filter((field) => show(field))
)

type CardActionButton = {
  name: Extract<KeyCardAction, 'use' | 'ccs' | 'toggle' | 'edit' | 'delete'>
  icon: 'terminal' | 'upload' | 'ban' | 'checkCircle' | 'edit' | 'trash'
  tone: 'use' | 'neutral' | 'warning' | 'success' | 'danger'
  label: string
}

const actions = computed<CardActionButton[]>(() => {
  const list: CardActionButton[] = [{ name: 'use', icon: 'terminal', tone: 'use', label: t('keys.useKey') }]
  if (props.showCcsImport) {
    list.push({ name: 'ccs', icon: 'upload', tone: 'neutral', label: t('keys.importToCcSwitch') })
  }
  list.push(
    props.apiKey.status === 'active'
      ? { name: 'toggle', icon: 'ban', tone: 'warning', label: t('keys.disable') }
      : { name: 'toggle', icon: 'checkCircle', tone: 'success', label: t('keys.enable') },
    { name: 'edit', icon: 'edit', tone: 'neutral', label: t('common.edit') },
    { name: 'delete', icon: 'trash', tone: 'danger', label: t('common.delete') }
  )
  return list
})
</script>

<style scoped>
.key-card {
  position: relative;
  display: flex;
  min-width: 0;
  flex-direction: column;
  gap: 0.75rem;
  padding: 1rem;
  border-radius: 1.25rem;
  background: var(--md-sys-color-surface);
  color: var(--md-sys-color-on-surface);
  box-shadow: var(--theme-highlight), var(--theme-elevation-1);
  transition: box-shadow 200ms ease;
  /* Long pages skip layout and paint for cards far off screen. */
  content-visibility: auto;
  contain-intrinsic-size: auto 30rem;
}

.key-card:hover {
  box-shadow: var(--theme-highlight), var(--theme-elevation-2);
}

/* Keys that cannot serve requests carry a thin strip in their status colour along the top. */
.key-card::before {
  content: '';
  position: absolute;
  top: 0;
  right: 1.25rem;
  left: 1.25rem;
  height: 3px;
  border-radius: 0 0 3px 3px;
  background: transparent;
}

.key-card.is-inactive::before {
  background: #cdbf95;
}

.key-card.is-quota_exhausted::before {
  background: #f59e0b;
}

.key-card.is-expired::before {
  background: #ef4444;
}

/* ---------- Header ---------- */
.key-card-head {
  display: flex;
  align-items: flex-start;
  gap: 0.75rem;
}

.key-card-title {
  min-width: 0;
  flex: 1;
}

.key-card-name-row {
  display: flex;
  min-width: 0;
  align-items: flex-start;
  gap: 0.375rem;
}

.key-card-name {
  display: -webkit-box;
  min-width: 0;
  overflow: hidden;
  -webkit-box-orient: vertical;
  -webkit-line-clamp: 2;
  font-size: 0.9375rem;
  font-weight: 650;
  line-height: 1.35;
  overflow-wrap: anywhere;
  color: var(--md-sys-color-on-surface);
}

.key-card-shield {
  display: inline-flex;
  flex-shrink: 0;
  margin-top: 0.125rem;
  color: #0284c7;
}

.key-card-meta {
  display: flex;
  flex-wrap: wrap;
  gap: 0 0.5rem;
  margin-top: 0.125rem;
  font-size: 0.75rem;
  color: var(--md-sys-color-on-surface-variant);
}

.key-card-id {
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  font-variant-numeric: tabular-nums;
}

.key-status {
  display: inline-flex;
  flex-shrink: 0;
  align-items: center;
  gap: 0.375rem;
  padding: 0.1875rem 0.625rem;
  border-radius: 999px;
  font-size: 0.75rem;
  font-weight: 600;
  white-space: nowrap;
}

.key-status-dot {
  width: 0.375rem;
  height: 0.375rem;
  border-radius: 999px;
  background: currentColor;
}

.key-status-active {
  background: #e4f0df;
  color: #2e5038;
}

.key-status-inactive {
  background: #f3efe2;
  color: #716744;
}

.key-status-quota_exhausted {
  background: #fef3c7;
  color: #92400e;
}

.key-status-expired {
  background: #fee2e2;
  color: #b91c1c;
}

/* ---------- Key and group ---------- */
.key-secret {
  display: flex;
  width: 100%;
  min-width: 0;
  align-items: center;
  justify-content: space-between;
  gap: 0.5rem;
  padding: 0.625rem 0.75rem;
  border-radius: 0.875rem;
  color: #7e5600;
  background: #fffbe8;
  box-shadow: inset 0 0 0 1px rgb(215 193 106 / 0.55);
  transition: background-color 160ms ease, color 160ms ease, box-shadow 160ms ease;
}

.key-secret:hover {
  background: #fff3bd;
  box-shadow: inset 0 0 0 1px rgb(201 154 18 / 0.6);
}

.key-secret.is-copied {
  color: #2e5038;
  background: #e4f0df;
  box-shadow: inset 0 0 0 1px rgb(92 152 101 / 0.45);
}

.key-secret-text {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  font-size: 0.8125rem;
  letter-spacing: 0.01em;
  color: var(--md-sys-color-on-surface);
}

.key-secret-hint {
  display: inline-flex;
  flex-shrink: 0;
  align-items: center;
  gap: 0.25rem;
  font-size: 0.75rem;
  font-weight: 600;
  white-space: nowrap;
}

.key-card-group {
  display: flex;
  width: 100%;
  min-width: 0;
  align-items: center;
  gap: 0.5rem;
  padding: 0.5rem 0.625rem;
  border-radius: 0.875rem;
  background: #fffbe8;
  text-align: left;
  transition: background-color 160ms ease;
}

.key-card-group:hover {
  background: #fff3bd;
}

.key-card-group-empty {
  font-size: 0.875rem;
  color: var(--md-sys-color-on-surface-variant);
}

.key-card-blocked {
  display: inline-flex;
  flex-shrink: 0;
  align-items: center;
  padding: 0.125rem 0.5rem;
  border-radius: 999px;
  background: #fee2e2;
  font-size: 0.625rem;
  font-weight: 600;
  color: #b91c1c;
}

.key-card-group-switch {
  display: inline-flex;
  flex-shrink: 0;
  margin-left: auto;
  color: var(--md-sys-color-on-surface-variant);
  opacity: 0.75;
  transition: opacity 160ms ease;
}

.key-card-group:hover .key-card-group-switch {
  opacity: 1;
}

/* ---------- Usage ---------- */
/* One equal column per visible tile: three by default, fewer when fields are hidden. */
.key-card-stats {
  display: grid;
  grid-auto-columns: minmax(0, 1fr);
  grid-auto-flow: column;
  gap: 0.5rem;
}

.key-card-stats > div {
  display: flex;
  min-width: 0;
  flex-direction: column;
  align-items: flex-start;
  gap: 0.25rem;
  padding: 0.5rem 0.625rem;
  border-radius: 0.875rem;
  background: #fffbe8;
}

.key-card-stats dt {
  max-width: 100%;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 0.6875rem;
  color: var(--md-sys-color-on-surface-variant);
}

.key-card-stats dd {
  max-width: 100%;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 0.8125rem;
  font-weight: 650;
  font-variant-numeric: tabular-nums;
  color: var(--md-sys-color-on-surface);
}

.key-card-pending {
  color: var(--md-sys-color-on-surface-variant);
}

.key-card-pending.is-loading {
  animation: key-card-pulse 1.2s ease-in-out infinite;
}

.key-live {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 0.375rem;
  padding: 0 0.5rem;
  border-radius: 999px;
  font-size: 0.8125rem;
  font-weight: 700;
  font-variant-numeric: tabular-nums;
}

.key-live-dot {
  width: 0.375rem;
  height: 0.375rem;
  flex-shrink: 0;
  border-radius: 999px;
  background: currentColor;
}

.key-live.is-idle {
  background: #faf6e8;
  color: #9b8c62;
}

.key-live.is-busy {
  background: #e4f0df;
  color: #2e5038;
}

.key-live.is-busy .key-live-dot {
  box-shadow: 0 0 0 3px rgb(92 152 101 / 0.22);
}

/* Same footprint as the token mix, so the card does not jump when usage arrives. */
.key-card-mix-placeholder {
  height: 6.25rem;
  border-radius: 0.875rem;
  background: #fffbe8;
  animation: key-card-pulse 1.2s ease-in-out infinite;
}

/* ---------- Quota and rate limits ---------- */
.key-card-limits {
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
  padding: 0.625rem 0.75rem;
  border-radius: 0.875rem;
  background: #fffbe8;
}

.key-limit {
  display: flex;
  flex-direction: column;
  gap: 0.25rem;
}

.key-limit-row {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: 0.5rem;
  font-size: 0.75rem;
}

.key-limit-label {
  color: var(--md-sys-color-on-surface-variant);
}

.key-limit-value {
  font-weight: 600;
  font-variant-numeric: tabular-nums;
  color: var(--md-sys-color-on-surface);
}

.key-limit-value.is-warning {
  color: #b45309;
}

.key-limit-value.is-danger {
  color: #dc2626;
}

.key-limit-reset {
  align-self: flex-end;
  font-size: 0.625rem;
  font-variant-numeric: tabular-nums;
  color: var(--md-sys-color-on-surface-variant);
}

.key-limit-reset-button {
  display: inline-flex;
  align-self: flex-start;
  align-items: center;
  gap: 0.25rem;
  padding: 0.125rem 0.5rem;
  border-radius: 0.5rem;
  font-size: 0.75rem;
  color: var(--md-sys-color-on-surface-variant);
  transition: background-color 150ms ease, color 150ms ease;
}

.key-limit-reset-button:hover {
  background: #fff3bd;
  color: #7e5600;
}

.key-meter {
  height: 0.375rem;
  width: 100%;
  overflow: hidden;
  border-radius: 999px;
  background: #eee5c9;
}

.key-meter-thin {
  height: 0.25rem;
}

.key-meter-fill {
  height: 100%;
  border-radius: inherit;
  background: #e9b824;
  transition: width 300ms ease;
}

.key-meter-thin .key-meter-fill {
  background: #10b981;
}

.key-meter .key-meter-fill.is-warning {
  background: #f59e0b;
}

.key-meter .key-meter-fill.is-danger {
  background: #ef4444;
}

/* ---------- Details ---------- */
.key-card-details {
  display: flex;
  flex-direction: column;
  gap: 0.375rem;
  font-size: 0.75rem;
}

.key-card-details > div {
  display: flex;
  min-width: 0;
  align-items: center;
  justify-content: space-between;
  gap: 0.75rem;
}

.key-card-details dt {
  flex-shrink: 0;
  color: var(--md-sys-color-on-surface-variant);
}

.key-card-details dd {
  display: flex;
  min-width: 0;
  flex-wrap: wrap;
  align-items: center;
  justify-content: flex-end;
  gap: 0.25rem 0.375rem;
  text-align: right;
}

.key-card-detail-text {
  font-variant-numeric: tabular-nums;
  color: var(--md-sys-color-on-surface);
}

.key-card-detail-muted {
  color: var(--md-sys-color-on-surface-variant);
}

/* IPv6 addresses wrap inside the card instead of widening it. */
.key-card-ip {
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  overflow-wrap: anywhere;
}

.key-tag {
  display: inline-flex;
  align-items: center;
  padding: 0.0625rem 0.5rem;
  border-radius: 999px;
  font-size: 0.6875rem;
  font-weight: 600;
  line-height: 1.125rem;
  white-space: nowrap;
}

.key-tag-warning {
  background: #fef3c7;
  color: #92400e;
}

.key-tag-danger {
  background: #fee2e2;
  color: #b91c1c;
}

/* ---------- Actions ---------- */
.key-card-actions {
  display: flex;
  justify-content: space-between;
  gap: 0.25rem;
  margin-top: auto;
  padding-top: 0.625rem;
  box-shadow: inset 0 1px 0 rgb(215 193 106 / 0.3);
}

/* Buttons start from their label width and share what is left, so a longer
   label ("导入到 CCS", "Import to CCS") keeps its text on narrow cards. */
.key-action {
  display: inline-flex;
  min-width: 0;
  flex: 1 1 auto;
  flex-direction: column;
  align-items: center;
  gap: 0.125rem;
  padding: 0.375rem 0.25rem;
  border-radius: 0.75rem;
  color: var(--md-sys-color-on-surface-variant);
  transition: background-color 150ms ease, color 150ms ease;
}

.key-action:disabled {
  cursor: progress;
  opacity: 0.55;
}

.key-action-label {
  max-width: 100%;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 0.75rem;
}

/* "Use key" is the most common next step, so it keeps a quiet tint at rest. */
.key-action-use {
  background: #f2f8ef;
  color: #386344;
}

.key-action-use:hover {
  background: #e4f0df;
  color: #284331;
}

.key-action-neutral:hover {
  background: #fff3bd;
  color: #7e5600;
}

.key-action-warning:hover {
  background: #fef3c7;
  color: #92400e;
}

.key-action-success:hover {
  background: #e4f0df;
  color: #2e5038;
}

.key-action-danger:hover {
  background: #fee2e2;
  color: #b91c1c;
}

@keyframes key-card-pulse {
  0%,
  100% { opacity: 1; }
  50% { opacity: 0.45; }
}

@media (prefers-reduced-motion: reduce) {
  .key-card-pending.is-loading,
  .key-card-mix-placeholder {
    animation: none;
  }
}
</style>
