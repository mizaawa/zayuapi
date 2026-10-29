<template>
  <AppLayout>
    <div class="redeem-page">
      <!-- Wallet: the balance a code tops up, with concurrency beside it. -->
      <section class="redeem-wallet">
        <div class="redeem-wallet-main">
          <p class="redeem-wallet-label">
            <span class="redeem-wallet-icon" aria-hidden="true"><Icon name="creditCard" size="sm" /></span>
            {{ t('redeem.currentBalance') }}
          </p>
          <p
            class="redeem-balance-value"
            :class="balanceToneClass(user?.balance)"
            data-testid="redeem-balance-value"
          >
            ${{ user?.balance?.toFixed(2) || '0.00' }}
          </p>
        </div>
        <dl class="redeem-wallet-facts">
          <div class="redeem-wallet-fact">
            <dt>{{ t('redeem.concurrency') }}</dt>
            <dd>{{ user?.concurrency || 0 }} <span>{{ t('redeem.requests') }}</span></dd>
          </div>
          <div class="redeem-wallet-fact">
            <dt>{{ t('redeem.recentActivity') }}</dt>
            <dd>{{ t('redeem.historyCount', { count: history.length }) }}</dd>
          </div>
        </dl>
      </section>

      <div class="redeem-grid">
        <!-- Redeem form; the outcome of the last attempt appears right under the button. -->
        <section class="redeem-card redeem-form-card">
          <header class="redeem-card-head">
            <span class="redeem-card-icon is-gift" aria-hidden="true"><Icon name="gift" size="md" /></span>
            <div class="min-w-0">
              <h2 class="redeem-card-title">{{ t('redeem.formTitle') }}</h2>
              <p class="redeem-card-subtitle">{{ t('redeem.formSubtitle') }}</p>
            </div>
          </header>

          <form @submit.prevent="handleRedeem" class="redeem-form">
            <label for="code" class="redeem-label">
              {{ t('redeem.redeemCodeLabel') }}
            </label>
            <div class="redeem-code-field" :class="{ 'is-disabled': submitting }">
              <Icon name="gift" size="md" class="redeem-code-icon" />
              <input
                id="code"
                v-model="redeemCode"
                type="text"
                required
                autocomplete="off"
                autocapitalize="off"
                spellcheck="false"
                :placeholder="t('redeem.redeemCodePlaceholder')"
                :disabled="submitting"
                class="redeem-code-input"
              />
              <button
                v-if="canPaste"
                type="button"
                class="redeem-paste"
                :disabled="submitting"
                @click="pasteCode"
              >
                <Icon name="clipboard" size="sm" />
                <span>{{ t('redeem.paste') }}</span>
              </button>
            </div>
            <p class="redeem-hint">
              <Icon name="infoCircle" size="xs" />
              {{ t('redeem.redeemCodeHint') }}
            </p>

            <button
              type="submit"
              :disabled="!redeemCode || submitting"
              class="btn btn-primary redeem-submit"
            >
              <svg
                v-if="submitting"
                class="h-5 w-5 animate-spin"
                fill="none"
                viewBox="0 0 24 24"
                aria-hidden="true"
              >
                <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
                <path
                  class="opacity-75"
                  fill="currentColor"
                  d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"
                ></path>
              </svg>
              <Icon v-else name="checkCircle" size="md" />
              {{ submitting ? t('redeem.redeeming') : t('redeem.redeemButton') }}
            </button>
          </form>

          <!-- Success Message -->
          <transition name="fade">
            <div v-if="redeemResult" class="redeem-result is-success" role="status">
              <span class="redeem-result-icon" aria-hidden="true"><Icon name="checkCircle" size="md" /></span>
              <div class="min-w-0 flex-1">
                <h3 class="redeem-result-title">{{ t('redeem.redeemSuccess') }}</h3>
                <p class="redeem-result-text">{{ redeemResult.message }}</p>
                <dl class="redeem-result-facts">
                  <div v-if="redeemResult.type === 'balance'">
                    <dt>{{ t('redeem.added') }}</dt>
                    <dd>${{ redeemResult.value.toFixed(2) }}</dd>
                  </div>
                  <div v-else-if="redeemResult.type === 'concurrency'">
                    <dt>{{ t('redeem.added') }}</dt>
                    <dd>{{ redeemResult.value }} {{ t('redeem.concurrentRequests') }}</dd>
                  </div>
                  <div v-else-if="redeemResult.type === 'subscription'" class="is-wide">
                    <dt>{{ t('redeem.subscriptionAssigned') }}</dt>
                    <dd>
                      <span v-if="redeemResult.group_name">{{ redeemResult.group_name }}</span>
                      <span v-if="redeemResult.validity_days"> · {{ t('redeem.subscriptionDays', { days: redeemResult.validity_days }) }}</span>
                    </dd>
                  </div>
                  <div v-if="redeemResult.new_balance !== undefined">
                    <dt>{{ t('redeem.newBalance') }}</dt>
                    <dd>${{ redeemResult.new_balance.toFixed(2) }}</dd>
                  </div>
                  <div v-if="redeemResult.new_concurrency !== undefined">
                    <dt>{{ t('redeem.newConcurrency') }}</dt>
                    <dd>{{ redeemResult.new_concurrency }} {{ t('redeem.requests') }}</dd>
                  </div>
                </dl>
              </div>
            </div>
          </transition>

          <!-- Error Message -->
          <transition name="fade">
            <div v-if="errorMessage" class="redeem-result is-error" role="alert">
              <span class="redeem-result-icon" aria-hidden="true"><Icon name="exclamationCircle" size="md" /></span>
              <div class="min-w-0 flex-1">
                <h3 class="redeem-result-title">{{ t('redeem.redeemFailed') }}</h3>
                <p class="redeem-result-text">{{ errorMessage }}</p>
              </div>
            </div>
          </transition>
        </section>

        <!-- Information Card -->
        <aside class="redeem-card redeem-rules">
          <h2 class="redeem-rules-title">
            <span class="redeem-card-icon is-info" aria-hidden="true"><Icon name="infoCircle" size="sm" /></span>
            {{ t('redeem.aboutCodes') }}
          </h2>
          <ul class="redeem-rules-list">
            <li><Icon name="checkCircle" size="sm" />{{ t('redeem.codeRule1') }}</li>
            <li><Icon name="sparkles" size="sm" />{{ t('redeem.codeRule2') }}</li>
            <li>
              <Icon name="chatBubble" size="sm" />
              <span class="min-w-0">
                {{ t('redeem.codeRule3') }}
                <span v-if="contactInfo" class="redeem-contact">{{ contactInfo }}</span>
              </span>
            </li>
            <li><Icon name="bolt" size="sm" />{{ t('redeem.codeRule4') }}</li>
          </ul>
        </aside>

        <!-- Recent Activity -->
        <section class="redeem-card redeem-history">
          <header class="redeem-history-head">
            <h2 class="redeem-card-title">{{ t('redeem.recentActivity') }}</h2>
            <span v-if="history.length > 0" class="redeem-history-count">{{ t('redeem.historyCount', { count: history.length }) }}</span>
          </header>

          <!-- Loading State -->
          <div v-if="loadingHistory" class="flex items-center justify-center py-10">
            <PageLoader compact />
          </div>

          <!-- History List -->
          <ol v-else-if="history.length > 0" class="redeem-history-list">
            <li v-for="item in history" :key="item.id" class="redeem-history-item">
              <span class="redeem-history-icon" :class="historyTone(item)" aria-hidden="true">
                <Icon v-if="isBalanceType(item.type)" name="dollar" size="sm" />
                <Icon v-else-if="isSubscriptionType(item.type)" name="badge" size="sm" />
                <Icon v-else name="bolt" size="sm" />
              </span>
              <div class="redeem-history-main">
                <p class="redeem-history-title">{{ getHistoryItemTitle(item) }}</p>
                <p class="redeem-history-meta">
                  <span>{{ formatDateTime(item.used_at) }}</span>
                  <span v-if="!isAdminAdjustment(item.type)" class="redeem-history-code">{{ item.code.slice(0, 8) }}...</span>
                  <span v-else>{{ t('redeem.adminAdjustment') }}</span>
                </p>
                <p v-if="item.notes" class="redeem-history-note" :title="item.notes">{{ item.notes }}</p>
              </div>
              <p class="redeem-history-value" :class="historyTone(item)">{{ formatHistoryValue(item) }}</p>
            </li>
          </ol>

          <!-- Empty State -->
          <div v-else class="redeem-empty">
            <span class="redeem-empty-icon" aria-hidden="true"><Icon name="clock" size="lg" /></span>
            <p>{{ t('redeem.historyWillAppear') }}</p>
          </div>
        </section>
      </div>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAuthStore } from '@/stores/auth'
import { useAppStore } from '@/stores/app'
import { useSubscriptionStore } from '@/stores/subscriptions'
import { redeemAPI, authAPI, type RedeemHistoryItem } from '@/api'
import AppLayout from '@/components/layout/AppLayout.vue'
import Icon from '@/components/icons/Icon.vue'
import PageLoader from '@/components/common/PageLoader.vue'
import { formatDateTime } from '@/utils/format'

const { t } = useI18n()
const authStore = useAuthStore()
const appStore = useAppStore()
const subscriptionStore = useSubscriptionStore()

const user = computed(() => authStore.user)

function balanceToneClass(value: number | null | undefined): string {
  const amount = Number(value)
  if (!Number.isFinite(amount) || amount < 0) return 'balance-tone-negative'
  if (amount <= 10) return 'balance-tone-low'
  if (amount > 100) return 'balance-tone-premium'
  return 'balance-tone-default'
}

const redeemCode = ref('')
const submitting = ref(false)
const redeemResult = ref<{
  message: string
  type: string
  value: number
  new_balance?: number
  new_concurrency?: number
  group_name?: string
  validity_days?: number
} | null>(null)
const errorMessage = ref('')

// History data
const history = ref<RedeemHistoryItem[]>([])
const loadingHistory = ref(false)
const contactInfo = ref('')

// Helper functions for history display
const isBalanceType = (type: string) => {
  return type === 'balance' || type === 'admin_balance'
}

const isSubscriptionType = (type: string) => {
  return type === 'subscription'
}

const isAdminAdjustment = (type: string) => {
  return type === 'admin_balance' || type === 'admin_concurrency'
}

// One tone per outcome: credit (green), debit (red), subscription (violet), concurrency (blue/amber).
const historyTone = (item: RedeemHistoryItem): string => {
  if (isBalanceType(item.type)) return item.value >= 0 ? 'is-credit' : 'is-debit'
  if (isSubscriptionType(item.type)) return 'is-subscription'
  return item.value >= 0 ? 'is-concurrency' : 'is-reduced'
}

// Only offered where the browser exposes clipboard reads; the user still confirms by pressing Redeem.
const canPaste = typeof navigator !== 'undefined' && typeof navigator.clipboard?.readText === 'function'

const pasteCode = async () => {
  try {
    const text = (await navigator.clipboard.readText()).trim()
    if (text) redeemCode.value = text.slice(0, 128)
  } catch {
    appStore.showWarning(t('redeem.pasteFailed'))
  }
}

const getHistoryItemTitle = (item: RedeemHistoryItem) => {
  if (item.type === 'balance') {
    return t('redeem.balanceAddedRedeem')
  } else if (item.type === 'admin_balance') {
    return item.value >= 0 ? t('redeem.balanceAddedAdmin') : t('redeem.balanceDeductedAdmin')
  } else if (item.type === 'concurrency') {
    return t('redeem.concurrencyAddedRedeem')
  } else if (item.type === 'admin_concurrency') {
    return item.value >= 0 ? t('redeem.concurrencyAddedAdmin') : t('redeem.concurrencyReducedAdmin')
  } else if (item.type === 'subscription') {
    return t('redeem.subscriptionAssigned')
  }
  return t('common.unknown')
}

const formatHistoryValue = (item: RedeemHistoryItem) => {
  if (isBalanceType(item.type)) {
    const sign = item.value >= 0 ? '+' : '-'
    return `${sign}$${Math.abs(item.value).toFixed(2)}`
  } else if (isSubscriptionType(item.type)) {
    // 订阅类型显示有效天数和分组名称
    const days = item.validity_days || Math.round(item.value)
    const groupName = item.group?.name || ''
    return groupName ? `${days}${t('redeem.days')} - ${groupName}` : `${days}${t('redeem.days')}`
  } else {
    const sign = item.value >= 0 ? '+' : ''
    return `${sign}${item.value} ${t('redeem.requests')}`
  }
}

const fetchHistory = async () => {
  loadingHistory.value = true
  try {
    history.value = await redeemAPI.getHistory()
  } catch (error) {
    console.error('Failed to fetch history:', error)
  } finally {
    loadingHistory.value = false
  }
}

const handleRedeem = async () => {
  if (!redeemCode.value.trim()) {
    appStore.showError(t('redeem.pleaseEnterCode'))
    return
  }

  submitting.value = true
  errorMessage.value = ''
  redeemResult.value = null

  try {
    let result: Awaited<ReturnType<typeof redeemAPI.redeem>>
    try {
      result = await redeemAPI.redeem(redeemCode.value.trim())
    } catch (error: any) {
      errorMessage.value = error.response?.data?.detail || t('redeem.failedToRedeem')
      appStore.showError(t('redeem.redeemFailed'))
      return
    }

    // The redeem request is the transaction boundary. Report success before
    // refreshing related page data so a refresh failure cannot misreport it.
    redeemResult.value = result
    redeemCode.value = ''
    appStore.showSuccess(t('redeem.codeRedeemSuccess'))

    try {
      await authStore.refreshUser()
    } catch (error) {
      console.error('Failed to refresh user after redeem:', error)
    }

    if (result.type === 'subscription') {
      try {
        await subscriptionStore.fetchActiveSubscriptions(true)
      } catch (error) {
        console.error('Failed to refresh subscriptions after redeem:', error)
        appStore.showWarning(t('redeem.subscriptionRefreshFailed'))
      }
    }

    await fetchHistory()
  } finally {
    submitting.value = false
  }
}

onMounted(async () => {
  fetchHistory()
  try {
    const settings = await authAPI.getPublicSettings()
    contactInfo.value = settings.contact_info || ''
  } catch (error) {
    console.error('Failed to load contact info:', error)
  }
})
</script>

<style scoped>
.redeem-page {
  display: flex;
  max-width: 72rem;
  flex-direction: column;
  gap: 1rem;
  margin: 0 auto;
}

/* ---------- Wallet ---------- */
.redeem-wallet {
  display: flex;
  flex-wrap: wrap;
  align-items: flex-end;
  justify-content: space-between;
  gap: 1rem 1.5rem;
  padding: 1.25rem;
  border-radius: 1.5rem;
  background:
    radial-gradient(28rem 14rem at 100% 0%, rgb(255 231 138 / 0.65), transparent 70%),
    linear-gradient(150deg, #fff3bd, #fff7d6 55%, #fffbe8);
  box-shadow: var(--theme-highlight), var(--theme-elevation-1);
}

.redeem-wallet-main {
  min-width: 0;
}

.redeem-wallet-label {
  display: inline-flex;
  align-items: center;
  gap: 0.5rem;
  font-size: 0.8125rem;
  font-weight: 600;
  color: #6b6045;
}

.redeem-wallet-icon {
  display: inline-flex;
  width: 1.75rem;
  height: 1.75rem;
  align-items: center;
  justify-content: center;
  border-radius: 0.625rem;
  background: rgb(255 254 248 / 0.8);
  color: #7e5600;
}

.redeem-balance-value {
  margin-top: 0.375rem;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: clamp(2.25rem, 1.5rem + 2.6vw, 3.25rem);
  font-weight: 750;
  line-height: 1.05;
  letter-spacing: -0.02em;
  color: #3f3420;
}

.redeem-wallet-facts {
  display: flex;
  flex-wrap: wrap;
  gap: 0.5rem;
}

.redeem-wallet-fact {
  min-width: 7.5rem;
  padding: 0.625rem 0.875rem;
  border-radius: 1rem;
  background: rgb(255 254 248 / 0.78);
  box-shadow: inset 0 1px 0 rgb(255 255 255 / 0.8);
}

.redeem-wallet-fact dt {
  font-size: 0.6875rem;
  font-weight: 600;
  color: #6b6045;
}

.redeem-wallet-fact dd {
  margin-top: 0.125rem;
  font-size: 1.125rem;
  font-weight: 700;
  font-variant-numeric: tabular-nums;
  color: #3f3420;
}

.redeem-wallet-fact dd span {
  font-size: 0.75rem;
  font-weight: 500;
  color: #6b6045;
}

/* ---------- Layout ---------- */
.redeem-grid {
  display: grid;
  grid-template-columns: minmax(0, 1fr);
  grid-template-areas:
    'form'
    'history'
    'rules';
  gap: 1rem;
}

.redeem-form-card { grid-area: form; }
.redeem-history { grid-area: history; }
.redeem-rules { grid-area: rules; }

.redeem-card {
  min-width: 0;
  padding: 1.125rem;
  border-radius: 1.5rem;
  background: var(--md-sys-color-surface);
  box-shadow: var(--theme-highlight), var(--theme-elevation-1);
}

.redeem-card-head {
  display: flex;
  align-items: flex-start;
  gap: 0.75rem;
  margin-bottom: 1.125rem;
}

.redeem-card-icon {
  display: inline-flex;
  width: 2.5rem;
  height: 2.5rem;
  flex-shrink: 0;
  align-items: center;
  justify-content: center;
  border-radius: 0.875rem;
}

.redeem-card-icon.is-gift {
  background: #fff3bd;
  color: #7e5600;
}

.redeem-card-icon.is-info {
  width: 1.875rem;
  height: 1.875rem;
  border-radius: 0.625rem;
  background: #e4f0df;
  color: #386344;
}

.redeem-card-title {
  font-size: 1rem;
  font-weight: 700;
  color: var(--md-sys-color-on-surface);
}

.redeem-card-subtitle {
  margin-top: 0.125rem;
  font-size: 0.8125rem;
  line-height: 1.25rem;
  color: var(--md-sys-color-on-surface-variant);
}

/* ---------- Form ---------- */
.redeem-form {
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
}

.redeem-label {
  font-size: 0.8125rem;
  font-weight: 600;
  color: var(--md-sys-color-on-surface-variant);
}

.redeem-code-field {
  display: flex;
  align-items: center;
  gap: 0.625rem;
  padding: 0.375rem 0.375rem 0.375rem 1rem;
  border-radius: 1.125rem;
  background: var(--md-sys-color-surface-container);
  box-shadow: inset 0 1px 2px rgb(63 52 32 / 0.06);
  transition: background-color 160ms ease, box-shadow 160ms ease;
}

.redeem-code-field:focus-within {
  background: var(--md-sys-color-surface);
  box-shadow: inset 0 1px 2px rgb(63 52 32 / 0.04), 0 0 0 4px rgb(233 184 36 / 0.18), 0 0 0 1px rgb(201 154 18 / 0.55);
}

.redeem-code-field.is-disabled {
  opacity: 0.7;
}

.redeem-code-icon {
  flex-shrink: 0;
  color: #9b8c62;
}

.redeem-code-input {
  min-width: 0;
  flex: 1;
  height: 2.75rem;
  border: 0;
  background: transparent;
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  font-size: 1.0625rem;
  font-weight: 600;
  letter-spacing: 0.06em;
  color: var(--md-sys-color-on-surface);
  outline: none;
}

.redeem-code-input::placeholder {
  font-family: system-ui, -apple-system, 'Segoe UI', sans-serif;
  font-size: 0.9375rem;
  font-weight: 400;
  letter-spacing: 0;
}

.redeem-paste {
  display: inline-flex;
  flex-shrink: 0;
  align-items: center;
  gap: 0.375rem;
  height: 2.5rem;
  padding: 0 0.875rem;
  border-radius: 0.875rem;
  background: var(--md-sys-color-surface);
  font-size: 0.8125rem;
  font-weight: 600;
  color: #7e5600;
  box-shadow: 0 1px 2px rgb(63 52 32 / 0.08);
  transition: background-color 160ms ease;
}

.redeem-paste:hover:not(:disabled) {
  background: #fff3bd;
}

.redeem-hint {
  display: flex;
  align-items: center;
  gap: 0.375rem;
  font-size: 0.75rem;
  color: var(--md-sys-color-on-surface-variant);
}

.redeem-submit {
  width: 100%;
  min-height: 3rem;
  margin-top: 0.5rem;
  border-radius: 1rem;
  font-size: 0.9375rem;
}

/* ---------- Result ---------- */
.redeem-result {
  display: flex;
  align-items: flex-start;
  gap: 0.75rem;
  margin-top: 1rem;
  padding: 0.875rem 1rem;
  border-radius: 1.125rem;
}

.redeem-result.is-success {
  background: #f2f8ef;
  color: #2e5038;
}

.redeem-result.is-error {
  background: #fef2f2;
  color: #991b1b;
}

.redeem-result-icon {
  display: inline-flex;
  width: 2.25rem;
  height: 2.25rem;
  flex-shrink: 0;
  align-items: center;
  justify-content: center;
  border-radius: 0.75rem;
  background: rgb(255 255 255 / 0.7);
}

.redeem-result-title {
  font-size: 0.875rem;
  font-weight: 700;
}

.redeem-result-text {
  margin-top: 0.125rem;
  font-size: 0.8125rem;
  overflow-wrap: anywhere;
  opacity: 0.9;
}

.redeem-result-facts {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 0.5rem;
  margin-top: 0.625rem;
}

.redeem-result-facts > div {
  min-width: 0;
  padding: 0.5rem 0.625rem;
  border-radius: 0.75rem;
  background: rgb(255 255 255 / 0.65);
}

.redeem-result-facts > div.is-wide {
  grid-column: 1 / -1;
}

.redeem-result-facts dt {
  font-size: 0.6875rem;
  opacity: 0.8;
}

.redeem-result-facts dd {
  font-size: 0.9375rem;
  font-weight: 700;
  font-variant-numeric: tabular-nums;
  overflow-wrap: anywhere;
}

/* ---------- Rules ---------- */
.redeem-rules-title {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  margin-bottom: 0.75rem;
  font-size: 0.9375rem;
  font-weight: 700;
  color: var(--md-sys-color-on-surface);
}

.redeem-rules-list {
  display: grid;
  gap: 0.5rem;
}

.redeem-rules-list li {
  display: flex;
  align-items: flex-start;
  gap: 0.625rem;
  padding: 0.625rem 0.75rem;
  border-radius: 0.875rem;
  background: #fffbe8;
  font-size: 0.8125rem;
  line-height: 1.25rem;
  color: #5b5238;
}

.redeem-rules-list li > :first-child {
  flex-shrink: 0;
  margin-top: 0.125rem;
  color: #976800;
}

.redeem-contact {
  display: inline-block;
  margin-left: 0.25rem;
  padding: 0 0.5rem;
  border-radius: 0.5rem;
  background: #fff3bd;
  font-size: 0.75rem;
  font-weight: 600;
  color: #65460b;
  overflow-wrap: anywhere;
}

/* ---------- History ---------- */
.redeem-history-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 0.75rem;
  margin-bottom: 0.875rem;
}

.redeem-history-count {
  padding: 0.125rem 0.625rem;
  border-radius: 999px;
  background: #faf6e8;
  font-size: 0.75rem;
  font-variant-numeric: tabular-nums;
  color: var(--md-sys-color-on-surface-variant);
}

.redeem-history-list {
  display: flex;
  flex-direction: column;
}

.redeem-history-item {
  display: flex;
  align-items: flex-start;
  gap: 0.75rem;
  padding: 0.75rem 0;
}

.redeem-history-item + .redeem-history-item {
  box-shadow: inset 0 1px 0 rgb(215 193 106 / 0.28);
}

.redeem-history-icon {
  display: inline-flex;
  width: 2.25rem;
  height: 2.25rem;
  flex-shrink: 0;
  align-items: center;
  justify-content: center;
  border-radius: 0.75rem;
}

.redeem-history-main {
  min-width: 0;
  flex: 1;
}

.redeem-history-title {
  font-size: 0.875rem;
  font-weight: 600;
  color: var(--md-sys-color-on-surface);
}

.redeem-history-meta {
  display: flex;
  flex-wrap: wrap;
  gap: 0 0.625rem;
  margin-top: 0.125rem;
  font-size: 0.75rem;
  color: #9b8c62;
}

.redeem-history-code {
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
}

.redeem-history-note {
  margin-top: 0.25rem;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 0.75rem;
  font-style: italic;
  color: var(--md-sys-color-on-surface-variant);
}

/* Long subscription names wrap instead of squeezing the title column. */
.redeem-history-value {
  max-width: 45%;
  flex-shrink: 0;
  text-align: right;
  font-size: 0.875rem;
  font-weight: 700;
  font-variant-numeric: tabular-nums;
  overflow-wrap: anywhere;
}

.redeem-history-icon.is-credit { background: #e4f0df; color: #386344; }
.redeem-history-icon.is-debit { background: #fee2e2; color: #b91c1c; }
.redeem-history-icon.is-subscription { background: #ede9fe; color: #6d28d9; }
.redeem-history-icon.is-concurrency { background: #e0f2fe; color: #0369a1; }
.redeem-history-icon.is-reduced { background: #ffedd5; color: #c2410c; }

.redeem-history-value.is-credit { color: #2e5038; }
.redeem-history-value.is-debit { color: #b91c1c; }
.redeem-history-value.is-subscription { color: #6d28d9; }
.redeem-history-value.is-concurrency { color: #0369a1; }
.redeem-history-value.is-reduced { color: #c2410c; }

.redeem-empty {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 0.75rem;
  padding: 2rem 1rem;
  text-align: center;
  font-size: 0.875rem;
  color: var(--md-sys-color-on-surface-variant);
}

.redeem-empty-icon {
  display: inline-flex;
  width: 3.5rem;
  height: 3.5rem;
  align-items: center;
  justify-content: center;
  border-radius: 1.25rem;
  background: #faf6e8;
  color: #9b8c62;
}

.fade-enter-active,
.fade-leave-active {
  transition: opacity 0.3s ease, transform 0.3s ease;
}

.fade-enter-from,
.fade-leave-to {
  opacity: 0;
  transform: translateY(-8px);
}

@media (min-width: 768px) {
  .redeem-wallet {
    padding: 1.5rem 1.75rem;
  }

  .redeem-card {
    padding: 1.5rem;
  }
}

/* Desktop: the form and its rules on the left, the activity timeline beside them. */
@media (min-width: 1024px) {
  .redeem-page {
    gap: 1.25rem;
  }

  .redeem-grid {
    grid-template-columns: minmax(0, 1.08fr) minmax(0, 0.92fr);
    grid-template-areas:
      'form history'
      'rules history';
    align-items: start;
    gap: 1.25rem;
  }

  .redeem-history {
    position: sticky;
    top: 5.5rem;
  }

  .redeem-history-list {
    max-height: calc(100vh - 14rem);
    overflow-y: auto;
    margin-right: -0.5rem;
    padding-right: 0.5rem;
  }
}

@media (prefers-reduced-motion: reduce) {
  .fade-enter-active,
  .fade-leave-active {
    transition: none;
  }
}
</style>
