<template>
  <AppLayout>
    <div class="leaderboard-page mx-auto w-full max-w-6xl space-y-5">
      <section class="leaderboard-hero overflow-hidden rounded-3xl border">
        <div class="hero-content flex flex-wrap items-end justify-between gap-5 px-5 py-6 sm:px-7 sm:py-7">
          <div class="min-w-0">
            <p class="hero-period text-xs font-semibold uppercase tracking-[0.12em]">{{ t('leaderboard.periodToday') }}</p>
            <h1 class="mt-1 text-2xl font-bold tracking-tight sm:text-3xl">{{ t('leaderboard.title') }}</h1>
            <p class="hero-description mt-2 max-w-xl text-sm leading-6">{{ t('leaderboard.description') }}</p>
          </div>
          <div class="rank-summary shrink-0 rounded-2xl border px-4 py-3 text-right">
            <p class="hero-period text-xs">{{ t('leaderboard.myRank') }}</p>
            <p class="mt-1 text-2xl font-bold tabular-nums">{{ myRankLabel }}</p>
          </div>
        </div>
      </section>

      <section class="leaderboard-card overflow-hidden rounded-3xl border">
        <div class="card-toolbar flex flex-wrap items-center justify-between gap-3 border-b px-4 py-4 sm:px-6">
          <div class="min-w-0">
            <h2 class="text-base font-semibold">{{ t('leaderboard.title') }}</h2>
            <p class="muted mt-1 text-xs">{{ t('leaderboard.periodTimezone') }}</p>
          </div>
          <div class="flex w-full flex-col gap-2 sm:w-auto sm:flex-row sm:items-center">
            <div class="flex items-center gap-2">
              <div class="countdown-badge flex items-center gap-1.5 rounded-xl border px-3 py-2 text-xs font-semibold">
                <Icon name="clock" size="sm" />
                <span>{{ countdownText }}</span>
              </div>
              <button class="btn btn-secondary btn-sm shrink-0" type="button" :disabled="loading" :aria-label="t('common.refresh')" @click="manualRefresh">
                <Icon name="refresh" size="sm" :class="loading ? 'animate-spin' : ''" />
                <span class="hidden sm:inline">{{ t('common.refresh') }}</span>
              </button>
            </div>
            <button
              type="button"
              class="participation-toggle w-full rounded-xl border px-3 py-2 text-xs font-semibold transition-colors sm:w-auto"
              :class="participating ? 'participation-enabled' : 'participation-disabled'"
              :disabled="loading || participationLoading"
              :aria-pressed="participating"
              @click="toggleParticipation"
            >
              {{ participating ? t('leaderboard.participating') : t('leaderboard.notParticipating') }}
            </button>
          </div>
        </div>

        <div v-if="error" class="px-5 py-10 text-center text-sm text-rose-600 dark:text-rose-300">{{ error }}</div>
        <div v-else-if="loading && !entries.length" class="muted px-5 py-10 text-center text-sm">{{ t('common.loading') }}</div>
        <div v-else-if="!entries.length" class="muted px-5 py-8 text-center text-sm">{{ t('leaderboard.noData') }}</div>
        <div v-else class="overflow-x-auto">
          <table class="leaderboard-table w-full table-fixed text-left text-sm">
            <colgroup>
              <col class="rank-column" />
              <col class="user-column" />
              <col class="metric-column" />
              <col class="metric-column optional-column" />
              <col class="metric-column optional-column" />
            </colgroup>
            <thead class="table-heading text-xs uppercase tracking-wide">
              <tr>
                <th class="px-3 py-3 font-semibold sm:px-5">{{ t('leaderboard.rank') }}</th>
                <th class="px-3 py-3 font-semibold sm:px-5">{{ t('leaderboard.user') }}</th>
                <th class="px-3 py-3 text-right font-semibold sm:px-5">{{ t('leaderboard.cost') }}</th>
                <th class="hidden px-3 py-3 text-right font-semibold sm:table-cell sm:px-5">{{ t('leaderboard.requests') }}</th>
                <th class="hidden px-3 py-3 text-right font-semibold sm:table-cell sm:px-5">{{ t('leaderboard.tokens') }}</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="entry in entries" :key="entry.rank" class="table-row transition-colors">
                <td class="px-3 py-3 sm:px-5">
                  <span class="rank-badge inline-flex min-w-8 items-center justify-center rounded-lg border px-2 py-1 font-bold tabular-nums" :class="rankClass(entry.rank)">
                    {{ entry.rank }}
                  </span>
                </td>
                <td class="px-3 py-3 font-medium sm:px-5" :title="entry.display_name">
                  <div class="flex min-w-0 items-center gap-2">
                    <div class="user-avatar flex h-8 w-8 shrink-0 items-center justify-center overflow-hidden rounded-lg text-xs font-semibold" aria-hidden="true">
                      <img
                        v-if="avatarUrl(entry)"
                        :src="avatarUrl(entry)"
                        alt=""
                        class="h-full w-full object-cover"
                        @error="markAvatarFailed(entry)"
                      />
                      <span v-else>{{ avatarInitial(entry.display_name) }}</span>
                    </div>
                    <span class="min-w-0 truncate">{{ entry.display_name }}</span>
                  </div>
                </td>
                <td class="px-3 py-3 text-right tabular-nums sm:px-5">${{ entry.actual_cost.toFixed(2) }}</td>
                <td class="hidden px-3 py-3 text-right tabular-nums sm:table-cell sm:px-5">{{ formatNumber(entry.requests) }}</td>
                <td class="hidden px-3 py-3 text-right tabular-nums sm:table-cell sm:px-5">{{ formatNumber(entry.tokens) }}</td>
              </tr>
            </tbody>
          </table>
        </div>
      </section>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import Icon from '@/components/icons/Icon.vue'
import leaderboardAPI, { type LeaderboardEntry, type LeaderboardResponse } from '@/api/leaderboard'

const CACHE_KEY = 'leaderboard_cache_today'
const REFRESH_INTERVAL = 5 * 60 * 1000 // 5分钟

interface CachedLeaderboard {
  data: LeaderboardResponse
  timestamp: number
}

const { t } = useI18n()
const entries = ref<LeaderboardEntry[]>([])
const data = ref<LeaderboardResponse | null>(null)
const loading = ref(false)
const error = ref('')
const participating = ref(false)
const participationLoading = ref(false)
const failedAvatarUrls = ref(new Set<string>())
const nextUpdateTime = ref<number>(0)
const countdown = ref<number>(0)

let refreshTimer: number | null = null
let countdownTimer: number | null = null

const myRankLabel = computed(() => data.value?.my_rank ? `#${data.value.my_rank}` : t('leaderboard.unranked'))

const countdownText = computed(() => {
  if (loading.value || countdown.value <= 0) return t('leaderboard.updating')
  const minutes = Math.floor(countdown.value / 60)
  const seconds = countdown.value % 60
  return `${minutes}:${seconds.toString().padStart(2, '0')}`
})

function formatNumber(value: number): string {
  return new Intl.NumberFormat().format(value)
}

function avatarUrl(entry: LeaderboardEntry): string {
  const url = entry.avatar_url?.trim() || ''
  return failedAvatarUrls.value.has(url) ? '' : url
}

function markAvatarFailed(entry: LeaderboardEntry): void {
  const url = entry.avatar_url?.trim()
  if (url) failedAvatarUrls.value.add(url)
}

function avatarInitial(displayName: string): string {
  return Array.from(displayName.trim())[0]?.toUpperCase() || 'U'
}

function rankClass(rank: number): string {
  if (rank === 1) return 'rank-gold'
  if (rank === 2) return 'rank-silver'
  if (rank === 3) return 'rank-bronze'
  return 'rank-regular'
}

function loadFromCache(): boolean {
  try {
    const cached = localStorage.getItem(CACHE_KEY)
    if (!cached) return false

    const { data: cachedData, timestamp }: CachedLeaderboard = JSON.parse(cached)
    const now = Date.now()

    if (now - timestamp < REFRESH_INTERVAL) {
      data.value = cachedData
      participating.value = cachedData.participating
      entries.value = cachedData.entries.slice(0, 25)
      nextUpdateTime.value = timestamp + REFRESH_INTERVAL
      updateCountdown()
      return true
    }
  } catch {
    localStorage.removeItem(CACHE_KEY)
  }
  return false
}

function saveToCache(leaderboardData: LeaderboardResponse): void {
  try {
    const cache: CachedLeaderboard = {
      data: leaderboardData,
      timestamp: Date.now()
    }
    localStorage.setItem(CACHE_KEY, JSON.stringify(cache))
    nextUpdateTime.value = cache.timestamp + REFRESH_INTERVAL
  } catch {
    // 缓存失败不影响功能
  }
}

function updateCountdown(): void {
  const now = Date.now()
  const remaining = Math.max(0, Math.floor((nextUpdateTime.value - now) / 1000))
  countdown.value = remaining
}

async function fetchLeaderboard(): Promise<void> {
  loading.value = true
  error.value = ''
  try {
    const result = await leaderboardAPI.get('today')
    data.value = result
    participating.value = result.participating
    entries.value = result.entries.slice(0, 25)
    saveToCache(result)
    updateCountdown()
  } catch {
    error.value = t('leaderboard.loadFailed')
  } finally {
    loading.value = false
  }
}

async function manualRefresh(): Promise<void> {
  await fetchLeaderboard()
}

async function toggleParticipation(): Promise<void> {
  participationLoading.value = true
  error.value = ''
  try {
    const result = await leaderboardAPI.setParticipation(!participating.value)
    participating.value = result.participating
    await fetchLeaderboard()
  } catch {
    error.value = t('leaderboard.participationFailed')
  } finally {
    participationLoading.value = false
  }
}

function startAutoRefresh(): void {
  // 每秒更新倒计时
  countdownTimer = window.setInterval(() => {
    updateCountdown()

    // 倒计时归零时自动刷新
    if (countdown.value <= 0 && !loading.value) {
      void fetchLeaderboard()
    }
  }, 1000)

  // 定时器作为备份机制
  refreshTimer = window.setInterval(() => {
    if (!loading.value) {
      void fetchLeaderboard()
    }
  }, REFRESH_INTERVAL)
}

function stopAutoRefresh(): void {
  if (refreshTimer !== null) {
    clearInterval(refreshTimer)
    refreshTimer = null
  }
  if (countdownTimer !== null) {
    clearInterval(countdownTimer)
    countdownTimer = null
  }
}

onMounted(() => {
  // 先尝试从缓存加载
  const hasCache = loadFromCache()

  // 如果没有缓存或缓存过期，立即获取
  if (!hasCache) {
    void fetchLeaderboard()
  }

  // 启动自动刷新
  startAutoRefresh()
})

onUnmounted(() => {
  stopAutoRefresh()
})
</script>

<style scoped>
.leaderboard-hero,
.leaderboard-card {
  border-color: transparent;
  background: var(--md-sys-color-surface);
  color: var(--md-sys-color-on-surface);
  box-shadow: 0 12px 30px rgb(15 23 42 / 0.08);
}

.hero-content {
  background: color-mix(in srgb, var(--md-sys-color-primary) 12%, var(--md-sys-color-surface));
}

.hero-period { color: var(--md-sys-color-primary); }
.hero-description, .muted { color: var(--md-sys-color-on-surface-variant); }

.rank-summary {
  border-color: transparent;
  background: color-mix(in srgb, var(--md-sys-color-primary) 10%, var(--md-sys-color-surface));
}

.card-toolbar { border-color: transparent; }

.countdown-badge {
  border-color: transparent;
  background: color-mix(in srgb, var(--md-sys-color-primary) 12%, var(--md-sys-color-surface));
  color: var(--md-sys-color-primary);
}

.participation-toggle { white-space: nowrap; }
.participation-enabled {
  border-color: transparent;
  background: color-mix(in srgb, #e53935 85%, var(--md-sys-color-surface));
  color: #fff;
}
.participation-enabled:hover:not(:disabled) {
  background: color-mix(in srgb, #c62828 85%, var(--md-sys-color-surface));
}
.participation-disabled {
  border-color: transparent;
  background: #009688;
  color: #fff;
}
.participation-disabled:hover:not(:disabled) {
  background: #00796b;
}

.table-heading { background: var(--md-sys-color-surface-container); color: var(--md-sys-color-on-surface-variant); }
.table-row { border-top: 1px solid transparent; }
.table-row:hover { background: color-mix(in srgb, var(--md-sys-color-primary) 8%, transparent); }
.rank-column { width: 4.25rem; }
.user-column { width: 34%; }
.metric-column { width: 20%; }
.rank-badge { border-color: transparent; }
.user-avatar {
  background: color-mix(in srgb, var(--md-sys-color-secondary) 16%, var(--md-sys-color-surface));
  color: var(--md-sys-color-secondary);
}
.rank-regular { background: color-mix(in srgb, var(--md-sys-color-primary) 10%, transparent); color: var(--md-sys-color-primary); }

.rank-gold { border-color: #c7a34a; background: color-mix(in srgb, #e0bd63 26%, var(--md-sys-color-surface)); color: color-mix(in srgb, #9a7212 72%, var(--md-sys-color-on-surface)); }
.rank-silver { border-color: #93aeb0; background: color-mix(in srgb, #b9d2d3 25%, var(--md-sys-color-surface)); color: color-mix(in srgb, #587476 72%, var(--md-sys-color-on-surface)); }
.rank-bronze { border-color: #b98262; background: color-mix(in srgb, #c99572 25%, var(--md-sys-color-surface)); color: color-mix(in srgb, #87543c 72%, var(--md-sys-color-on-surface)); }

@media (max-width: 639px) {
  .leaderboard-page { max-width: 100%; }
  .hero-content { gap: 1rem; }
  .rank-summary { display: flex; width: 100%; align-items: center; justify-content: space-between; text-align: left; }
  .rank-column { width: 3.75rem; }
  .user-column { width: 40%; }
  .metric-column { width: 28%; }
  .leaderboard-table col.optional-column { display: none; }
}
</style>
