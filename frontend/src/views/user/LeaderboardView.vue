<template>
  <AppLayout>
    <div class="lb-page">
      <div class="lb-top">
        <!-- Stage: today's top three on a podium; the gold step stands in the middle. -->
        <section class="lb-stage" aria-labelledby="lb-stage-title">
          <header class="lb-stage-head">
            <h2 id="lb-stage-title" class="lb-stage-title">{{ t('leaderboard.todayTitle') }}</h2>
            <p class="lb-stage-note">{{ t('leaderboard.stageNote') }}</p>
            <div class="lb-stage-tools">
              <span class="countdown-badge" :title="t('leaderboard.nextUpdate')">
                <svg class="countdown-ring" viewBox="0 0 20 20" aria-hidden="true">
                  <circle class="countdown-ring-track" cx="10" cy="10" r="8" />
                  <circle
                    class="countdown-ring-value"
                    cx="10"
                    cy="10"
                    r="8"
                    :stroke-dasharray="RING_LENGTH"
                    :stroke-dashoffset="ringOffset"
                  />
                </svg>
                <span>{{ countdownText }}</span>
              </span>
              <button
                type="button"
                class="lb-icon-button"
                :disabled="loading"
                :aria-label="t('common.refresh')"
                :title="t('common.refresh')"
                @click="manualRefresh"
              >
                <Icon name="refresh" size="sm" :class="loading ? 'animate-spin' : ''" />
              </button>
            </div>
          </header>

          <!-- A failed refresh keeps the last ranking on screen under the message. -->
          <p v-if="error" class="lb-error" role="alert">
            <Icon name="exclamationCircle" size="sm" class="shrink-0" />
            <span>{{ error }}</span>
          </p>
          <div v-if="loading && !entries.length" class="lb-podium is-skeleton" aria-hidden="true">
            <div v-for="rank in PODIUM_RANKS" :key="rank" class="podium-slot" :class="`is-rank-${rank}`">
              <div class="podium-person">
                <span class="podium-avatar skeleton"></span>
                <span class="podium-skeleton-line skeleton"></span>
              </div>
              <div class="podium-plinth"></div>
            </div>
          </div>
          <div v-else-if="!entries.length" class="lb-stage-message" :class="{ 'is-quiet': error }">
            <template v-if="!error">
              <span class="lb-empty-icon" aria-hidden="true"><Icon name="trophy" size="lg" /></span>
              <p>{{ t('leaderboard.noData') }}</p>
            </template>
          </div>
          <ol v-else class="lb-podium" :aria-label="t('leaderboard.podiumLabel')">
            <li
              v-for="slot in podiumSlots"
              :key="slot.rank"
              class="podium-slot"
              :class="[`is-rank-${slot.rank}`, { 'is-vacant': !slot.entry, 'is-me': isMe(slot.entry) }]"
              :data-testid="slot.entry ? `leaderboard-entry-${slot.rank}` : undefined"
            >
              <div class="podium-person">
                <template v-if="slot.entry">
                  <div class="user-avatar podium-avatar" :class="avatarTint(slot.entry)" aria-hidden="true">
                    <img
                      v-if="avatarUrl(slot.entry)"
                      :src="avatarUrl(slot.entry)"
                      alt=""
                      @error="markAvatarFailed(slot.entry)"
                    />
                    <span v-else>{{ avatarInitial(slot.entry.display_name) }}</span>
                  </div>
                  <p class="podium-name" :title="slot.entry.display_name">{{ slot.entry.display_name }}</p>
                  <span v-if="isMe(slot.entry)" class="lb-me-tag">{{ t('leaderboard.me') }}</span>
                  <p class="podium-meta">
                    <span class="podium-requests">{{ requestsLabel(slot.entry.requests) }}</span>
                    <span :title="exactNumber(slot.entry.tokens)">{{ tokensLabel(slot.entry.tokens) }}</span>
                  </p>
                </template>
                <template v-else>
                  <div class="podium-avatar is-vacant" aria-hidden="true"><Icon name="user" size="md" /></div>
                  <p class="podium-name">{{ t('leaderboard.vacant') }}</p>
                </template>
              </div>
              <div class="podium-plinth">
                <span class="podium-rank">{{ slot.rank }}</span>
                <span v-if="slot.entry" class="podium-cost">{{ formatCost(slot.entry.actual_cost) }}</span>
              </div>
            </li>
          </ol>
        </section>

        <!-- My standing today, with the opt-in switch that controls whether I am ranked at all. -->
        <section class="lb-me" aria-labelledby="lb-me-title">
          <div class="lb-me-head">
            <h2 id="lb-me-title" class="lb-me-title">{{ t('leaderboard.myToday') }}</h2>
            <button
              type="button"
              role="switch"
              class="lb-switch"
              :aria-checked="participating"
              :disabled="loading || participationLoading"
              @click="toggleParticipation"
            >
              <span>{{ t('leaderboard.participationLabel') }}</span>
              <span class="lb-switch-track" aria-hidden="true"><span class="lb-switch-thumb"></span></span>
            </button>
          </div>

          <div class="lb-me-body">
            <div class="min-w-0">
              <p class="lb-me-rank" :class="{ 'is-text': !data?.my_rank }" data-testid="leaderboard-my-rank">{{ myRankLabel }}</p>
              <p v-if="myNote" class="lb-me-note">{{ myNote }}</p>
            </div>

            <dl class="lb-me-stats">
              <div>
                <dt>{{ t('leaderboard.cost') }}</dt>
                <dd>{{ formatCost(data?.my_actual_cost ?? 0) }}</dd>
              </div>
              <div>
                <dt>{{ t('leaderboard.requests') }}</dt>
                <dd>{{ exactNumber(data?.my_requests ?? 0) }}</dd>
              </div>
              <div>
                <dt>{{ t('leaderboard.tokens') }}</dt>
                <dd :title="exactNumber(data?.my_tokens ?? 0)">{{ formatCompactNumber(data?.my_tokens ?? 0) }}</dd>
              </div>
            </dl>
          </div>

          <p class="lb-me-privacy">
            <Icon :name="participating ? 'eye' : 'eyeOff'" size="sm" class="shrink-0" />
            <span>{{ participating ? t('leaderboard.participationOnHint') : t('leaderboard.participationOffHint') }}</span>
          </p>
        </section>
      </div>

      <!-- Everyone after the podium. Phones fold the metrics under the name. -->
      <section v-if="restEntries.length" class="lb-board" aria-labelledby="lb-board-title">
        <h2 id="lb-board-title" class="lb-board-title">{{ restTitle }}</h2>
        <div class="lb-row lb-row-labels" aria-hidden="true">
          <span class="lb-rank">{{ t('leaderboard.rank') }}</span>
          <span>{{ t('leaderboard.user') }}</span>
          <span class="lb-metric">{{ t('leaderboard.requests') }}</span>
          <span class="lb-metric">{{ t('leaderboard.tokens') }}</span>
          <span class="lb-cost">{{ t('leaderboard.cost') }}</span>
        </div>
        <ol class="lb-list">
          <li
            v-for="entry in restEntries"
            :key="entry.rank"
            class="lb-row"
            :class="{ 'is-me': isMe(entry) }"
            :data-testid="`leaderboard-entry-${entry.rank}`"
          >
            <span class="lb-rank">{{ entry.rank }}</span>
            <div class="lb-user">
              <div class="user-avatar lb-avatar" :class="avatarTint(entry)" aria-hidden="true">
                <img
                  v-if="avatarUrl(entry)"
                  :src="avatarUrl(entry)"
                  alt=""
                  @error="markAvatarFailed(entry)"
                />
                <span v-else>{{ avatarInitial(entry.display_name) }}</span>
              </div>
              <span class="lb-name" :title="entry.display_name">{{ entry.display_name }}</span>
              <span v-if="isMe(entry)" class="lb-me-tag">{{ t('leaderboard.me') }}</span>
            </div>
            <div class="lb-meta">
              <span class="lb-metric">
                <span class="lb-metric-full">{{ requestsLabel(entry.requests) }}</span>
                <span class="lb-metric-short" aria-hidden="true">{{ exactNumber(entry.requests) }}</span>
              </span>
              <span class="lb-metric" :title="exactNumber(entry.tokens)">
                <span class="lb-metric-full">{{ tokensLabel(entry.tokens) }}</span>
                <span class="lb-metric-short" aria-hidden="true">{{ formatCompactNumber(entry.tokens) }}</span>
              </span>
            </div>
            <span class="lb-cost">{{ formatCost(entry.actual_cost) }}</span>
          </li>
        </ol>
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
import { formatCompactNumber } from '@/utils/format'

const CACHE_KEY = 'leaderboard_cache_today'
const REFRESH_INTERVAL = 5 * 60 * 1000 // 5分钟
const PODIUM_RANKS = [1, 2, 3] as const
const RING_LENGTH = 2 * Math.PI * 8
const AVATAR_TINTS = ['tint-honey', 'tint-garden', 'tint-coral', 'tint-slate', 'tint-plum']

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
const integerFormatter = new Intl.NumberFormat()

let refreshTimer: number | null = null
let countdownTimer: number | null = null

const podiumSlots = computed(() => PODIUM_RANKS.map(rank => ({
  rank,
  entry: entries.value.find(entry => entry.rank === rank) ?? null,
})))
const restEntries = computed(() => entries.value.filter(entry => entry.rank > PODIUM_RANKS.length))
const restTitle = computed(() => {
  const rest = restEntries.value
  return t('leaderboard.restTitle', { from: rest[0]?.rank ?? 0, to: rest[rest.length - 1]?.rank ?? 0 })
})

const myRankLabel = computed(() => {
  if (!data.value) return '—'
  if (!participating.value) return t('leaderboard.notParticipating')
  return data.value.my_rank ? `#${data.value.my_rank}` : t('leaderboard.unranked')
})

// How far the next rank up is, using the same snapshot the list shows.
const myNote = computed(() => {
  const current = data.value
  if (!current || !participating.value) return ''
  const rank = current.my_rank
  if (!rank) return current.my_requests > 0 ? '' : t('leaderboard.noUsage')
  if (rank === 1) return t('leaderboard.leading')
  const ahead = entries.value.find(entry => entry.rank === rank - 1)
  if (ahead) {
    const gap = ahead.actual_cost - current.my_actual_cost
    return gap >= 0.01 ? t('leaderboard.gapToNext', { rank: ahead.rank, amount: formatCost(gap) }) : ''
  }
  const last = entries.value[entries.value.length - 1]
  if (last && rank > last.rank) {
    const gap = last.actual_cost - current.my_actual_cost
    return gap >= 0.01 ? t('leaderboard.gapToTop', { n: last.rank, amount: formatCost(gap) }) : ''
  }
  return ''
})

const countdownText = computed(() => {
  if (loading.value || countdown.value <= 0) return t('leaderboard.updating')
  const minutes = Math.floor(countdown.value / 60)
  const seconds = countdown.value % 60
  return `${minutes}:${seconds.toString().padStart(2, '0')}`
})

const ringOffset = computed(() => {
  const total = REFRESH_INTERVAL / 1000
  const remaining = loading.value ? 0 : Math.min(total, Math.max(0, countdown.value))
  return RING_LENGTH * (1 - remaining / total)
})

function isMe(entry: LeaderboardEntry | null): boolean {
  return Boolean(entry && participating.value && data.value?.my_rank === entry.rank)
}

function exactNumber(value: number): string {
  return integerFormatter.format(value)
}

function formatCost(value: number): string {
  return `$${value.toFixed(2)}`
}

function requestsLabel(value: number): string {
  return t('leaderboard.requestsCount', { count: exactNumber(value) }, value)
}

function tokensLabel(value: number): string {
  return t('leaderboard.tokensCount', { count: formatCompactNumber(value) })
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

// Initials get a stable tint per name so neighbouring rows are easy to tell apart.
function avatarTint(entry: LeaderboardEntry): string {
  let hash = 0
  for (const char of entry.display_name) hash = (hash * 31 + (char.codePointAt(0) ?? 0)) >>> 0
  return AVATAR_TINTS[hash % AVATAR_TINTS.length]
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
.lb-page {
  container: lb / inline-size;
  --lb-ink: #3f3420;
  --lb-ink-soft: #6b6045;
  --lb-honey-ink: #7e5600;
  --lb-hairline: rgb(215 193 106 / 0.28);
  display: flex;
  width: 100%;
  max-width: 72rem;
  flex-direction: column;
  gap: 1rem;
  margin: 0 auto;
  color: var(--lb-ink);
}

.lb-top {
  display: grid;
  grid-template-columns: minmax(0, 1fr);
  gap: 1rem;
}

/* ---------- Stage ---------- */
.lb-stage {
  position: relative;
  isolation: isolate;
  display: flex;
  min-width: 0;
  flex-direction: column;
  overflow: hidden;
  padding: 1.25rem 1.25rem 0;
  border-radius: 1.75rem;
  background:
    radial-gradient(65% 55% at 50% 100%, rgb(255 231 138 / 0.55), transparent 72%),
    linear-gradient(180deg, #fffaf0 0%, #fff4c9 100%);
  box-shadow: var(--theme-highlight), var(--theme-elevation-1);
}

/* Title and tools share the first line; the note runs underneath at any width. */
.lb-stage-head {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  grid-template-areas:
    'title tools'
    'note note';
  align-items: center;
  gap: 0.125rem 1rem;
}

.lb-stage-title {
  grid-area: title;
  min-width: 0;
  font-size: 1.25rem;
  font-weight: 750;
  line-height: 1.3;
}

.lb-stage-note {
  grid-area: note;
  font-size: 0.8125rem;
  line-height: 1.25rem;
  color: var(--lb-ink-soft);
}

.lb-stage-tools {
  display: flex;
  grid-area: tools;
  align-items: center;
  gap: 0.375rem;
}

.countdown-badge {
  display: inline-flex;
  height: 2.25rem;
  align-items: center;
  gap: 0.375rem;
  padding: 0 0.75rem 0 0.5rem;
  border-radius: 999px;
  background: rgb(255 254 248 / 0.8);
  font-size: 0.8125rem;
  font-weight: 700;
  font-variant-numeric: tabular-nums;
  color: var(--lb-honey-ink);
  box-shadow: inset 0 1px 0 rgb(255 255 255 / 0.85), 0 1px 2px rgb(63 52 32 / 0.06);
}

.countdown-ring {
  width: 1.125rem;
  height: 1.125rem;
  flex-shrink: 0;
  transform: rotate(-90deg);
}

.countdown-ring circle {
  fill: none;
  stroke-width: 2.5;
}

.countdown-ring-track {
  stroke: rgb(233 184 36 / 0.25);
}

.countdown-ring-value {
  stroke: #e9b824;
  stroke-linecap: round;
  transition: stroke-dashoffset 1s linear;
}

.lb-icon-button {
  display: inline-flex;
  width: 2.25rem;
  height: 2.25rem;
  align-items: center;
  justify-content: center;
  border-radius: 999px;
  background: rgb(255 254 248 / 0.8);
  color: var(--lb-honey-ink);
  box-shadow: inset 0 1px 0 rgb(255 255 255 / 0.85), 0 1px 2px rgb(63 52 32 / 0.06);
  transition: background-color 160ms ease;
}

.lb-icon-button:hover:not(:disabled) {
  background: #fffef8;
}

.lb-icon-button:disabled {
  cursor: not-allowed;
  opacity: 0.7;
}

.lb-stage-message {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 0.625rem;
  padding: 2.75rem 1rem 3.25rem;
  font-size: 0.875rem;
  text-align: center;
  color: var(--lb-ink-soft);
}

.lb-stage-message.is-quiet {
  padding: 0 0 1.5rem;
}

.lb-error {
  display: flex;
  align-items: center;
  align-self: center;
  gap: 0.5rem;
  margin-top: 1rem;
  padding: 0.5rem 0.875rem;
  border-radius: 999px;
  background: #fdf1ec;
  font-size: 0.8125rem;
  font-weight: 600;
  color: #a43c2b;
}

.lb-empty-icon {
  display: inline-flex;
  width: 3.5rem;
  height: 3.5rem;
  align-items: center;
  justify-content: center;
  border-radius: 999px;
  background: rgb(255 254 248 / 0.85);
  color: #c99a12;
}

/* ---------- Podium ---------- */
.lb-podium {
  position: relative;
  display: grid;
  width: 100%;
  max-width: 38rem;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  align-items: end;
  gap: 0.625rem;
  margin: 1.5rem auto 0;
}

/* Soft rays behind the winner; static, and clipped by the stage. */
.lb-podium::before {
  position: absolute;
  z-index: -1;
  top: -12.5rem;
  left: 50%;
  width: 30rem;
  height: 30rem;
  background: repeating-conic-gradient(from 0deg, rgb(255 255 255 / 0.6) 0deg 7deg, transparent 7deg 20deg);
  -webkit-mask-image: radial-gradient(circle, #000 0 16%, transparent 56%);
  mask-image: radial-gradient(circle, #000 0 16%, transparent 56%);
  content: '';
  pointer-events: none;
  transform: translateX(-50%);
}

.lb-podium.is-skeleton::before {
  display: none;
}

.podium-slot {
  display: flex;
  min-width: 0;
  flex-direction: column;
}

.podium-slot.is-rank-1 {
  --medal: #e9b824;
  --plinth-h: 8.5rem;
  --plinth-top: #ffe58c;
  --plinth-bottom: #f1c43a;
  --rank-ink: #6e4b00;
  order: 2;
}

.podium-slot.is-rank-2 {
  --medal: #bdb6a3;
  --plinth-h: 6.75rem;
  --plinth-top: #faf7ef;
  --plinth-bottom: #e6e0cf;
  --rank-ink: #5f5843;
  order: 1;
}

.podium-slot.is-rank-3 {
  --medal: #cf9670;
  --plinth-h: 5.5rem;
  --plinth-top: #fbe8d9;
  --plinth-bottom: #efcbb0;
  --rank-ink: #7c4729;
  order: 3;
}

.podium-person {
  display: flex;
  min-width: 0;
  flex-direction: column;
  align-items: center;
  gap: 0.25rem;
  padding: 0 0.25rem 0.75rem;
  text-align: center;
}

.podium-avatar {
  display: flex;
  width: 3.5rem;
  height: 3.5rem;
  flex-shrink: 0;
  align-items: center;
  justify-content: center;
  margin-bottom: 0.5rem;
  overflow: hidden;
  border-radius: 999px;
  font-size: 1.125rem;
  font-weight: 750;
  box-shadow:
    0 0 0 3px #fffef8,
    0 0 0 6px var(--medal),
    0 14px 24px -12px rgb(63 52 32 / 0.55);
}

.is-rank-1 .podium-avatar {
  width: 4.5rem;
  height: 4.5rem;
  font-size: 1.5rem;
}

.podium-avatar img {
  width: 100%;
  height: 100%;
  object-fit: cover;
}

.podium-avatar.is-vacant {
  background: rgb(255 254 248 / 0.65);
  color: #b8a46a;
  box-shadow: 0 0 0 2px rgb(255 254 248 / 0.9);
  outline: 2px dashed #d7c16a;
  outline-offset: 2px;
}

.podium-name {
  display: -webkit-box;
  max-width: 100%;
  overflow: hidden;
  font-size: 0.875rem;
  font-weight: 700;
  line-height: 1.3;
  overflow-wrap: anywhere;
  -webkit-box-orient: vertical;
  -webkit-line-clamp: 2;
}

.is-vacant .podium-name {
  font-weight: 600;
  color: #9b8c62;
}

.podium-meta {
  display: flex;
  flex-wrap: wrap;
  justify-content: center;
  gap: 0 0.5rem;
  font-size: 0.6875rem;
  line-height: 1.1rem;
  font-variant-numeric: tabular-nums;
  color: var(--lb-ink-soft);
}

.podium-plinth {
  display: flex;
  height: var(--plinth-h);
  flex-direction: column;
  align-items: center;
  gap: 0.25rem;
  padding: 0.875rem 0.25rem 0;
  border-radius: 1.125rem 1.125rem 0 0;
  background: linear-gradient(180deg, var(--plinth-top), var(--plinth-bottom));
  box-shadow:
    inset 0 1px 0 rgb(255 255 255 / 0.8),
    inset 0 -14px 20px -16px rgb(63 52 32 / 0.35);
  transform-origin: bottom;
  animation: plinth-rise 640ms var(--theme-ease-out) both;
}

.is-rank-2 .podium-plinth { animation-delay: 80ms; }
.is-rank-1 .podium-plinth { animation-delay: 160ms; }

.is-vacant .podium-plinth {
  background:
    repeating-linear-gradient(-45deg, rgb(255 255 255 / 0.55) 0 6px, transparent 6px 12px),
    #f7efd3;
}

.podium-rank {
  font-size: 2.25rem;
  font-weight: 800;
  line-height: 1;
  letter-spacing: -0.02em;
  color: var(--rank-ink);
  text-shadow: 0 1px 0 rgb(255 255 255 / 0.65);
}

.is-vacant .podium-rank {
  color: #c4b27a;
}

.podium-cost {
  max-width: 100%;
  overflow: hidden;
  font-size: 0.9375rem;
  font-weight: 750;
  font-variant-numeric: tabular-nums;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.is-skeleton .podium-plinth {
  animation: none;
  background: rgb(238 229 201 / 0.7);
}

.podium-skeleton-line {
  width: 70%;
  height: 0.75rem;
  border-radius: 999px;
}

.is-skeleton .podium-avatar {
  box-shadow: none;
}

@keyframes plinth-rise {
  from { opacity: 0; transform: scaleY(0.55); }
  to { opacity: 1; transform: scaleY(1); }
}

/* ---------- Me ---------- */
.lb-me {
  container: lbme / inline-size;
  display: flex;
  min-width: 0;
  flex-direction: column;
  gap: 1rem;
  padding: 1.25rem;
  border-radius: 1.75rem;
  background: var(--md-sys-color-surface);
  box-shadow: var(--theme-highlight), var(--theme-elevation-1);
}

.lb-me-head {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 0.5rem 0.75rem;
}

.lb-me-title {
  font-size: 0.9375rem;
  font-weight: 700;
}

.lb-switch {
  display: inline-flex;
  align-items: center;
  gap: 0.5rem;
  padding: 0.25rem 0.25rem 0.25rem 0.75rem;
  border-radius: 999px;
  background: #fffbe8;
  font-size: 0.75rem;
  font-weight: 650;
  color: var(--lb-ink);
  transition: background-color 160ms ease;
}

.lb-switch:hover:not(:disabled) {
  background: #fff3bd;
}

.lb-switch:disabled {
  cursor: not-allowed;
  opacity: 0.65;
}

.lb-switch-track {
  position: relative;
  width: 2.25rem;
  height: 1.25rem;
  flex-shrink: 0;
  border-radius: 999px;
  background: #e3d9b8;
  transition: background-color 180ms ease;
}

.lb-switch-thumb {
  position: absolute;
  top: 0.125rem;
  left: 0.125rem;
  width: 1rem;
  height: 1rem;
  border-radius: 999px;
  background: #fff;
  box-shadow: 0 1px 2px rgb(63 52 32 / 0.3);
  transition: transform 200ms var(--theme-ease-out);
}

.lb-switch[aria-checked='true'] .lb-switch-track {
  background: #e9b824;
}

.lb-switch[aria-checked='true'] .lb-switch-thumb {
  transform: translateX(1rem);
}

.lb-me-rank {
  font-size: 3rem;
  font-weight: 800;
  line-height: 1;
  letter-spacing: -0.03em;
  font-variant-numeric: tabular-nums;
}

.lb-me-rank.is-text {
  font-size: 1.5rem;
  line-height: 1.25;
  letter-spacing: 0;
  color: var(--lb-ink-soft);
}

.lb-me-note {
  display: inline-flex;
  margin-top: 0.625rem;
  padding: 0.25rem 0.625rem;
  border-radius: 999px;
  background: #fff3bd;
  font-size: 0.8125rem;
  font-weight: 650;
  color: var(--lb-honey-ink);
}

/* In the narrow side card the rank stays up top and the numbers sit low. */
.lb-me-body {
  display: flex;
  flex: 1;
  flex-direction: column;
  justify-content: space-between;
  gap: 1rem;
}

/* A wide card (stacked under the stage) puts the rank beside the numbers. */
@container lbme (min-width: 34rem) {
  .lb-me-body {
    display: grid;
    grid-template-columns: minmax(0, 0.8fr) minmax(0, 1.5fr);
    align-items: center;
    gap: 1.5rem;
  }
}

.lb-me-stats {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 0.5rem;
}

.lb-me-stats > div {
  min-width: 0;
  padding: 0.625rem 0.75rem;
  border-radius: 1rem;
  background: #fffbe8;
}

.lb-me-stats dt {
  font-size: 0.6875rem;
  font-weight: 600;
  color: var(--lb-ink-soft);
}

.lb-me-stats dd {
  margin-top: 0.125rem;
  overflow: hidden;
  font-size: 1rem;
  font-weight: 750;
  font-variant-numeric: tabular-nums;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.lb-me-privacy {
  display: flex;
  align-items: flex-start;
  gap: 0.5rem;
  margin-top: auto;
  font-size: 0.75rem;
  line-height: 1.125rem;
  color: var(--lb-ink-soft);
}

.lb-me-tag {
  display: inline-flex;
  flex-shrink: 0;
  align-items: center;
  padding: 0 0.5rem;
  border-radius: 999px;
  background: #e9b824;
  font-size: 0.6875rem;
  font-weight: 750;
  line-height: 1.125rem;
  color: var(--lb-ink);
}

/* ---------- Board ---------- */
.lb-board {
  padding: 0.5rem;
  border-radius: 1.75rem;
  background: var(--md-sys-color-surface);
  box-shadow: var(--theme-highlight), var(--theme-elevation-1);
}

.lb-board-title {
  padding: 0.75rem 0.875rem 0.5rem;
  font-size: 0.9375rem;
  font-weight: 700;
}

.lb-row {
  position: relative;
  display: grid;
  grid-template-columns: 1.75rem minmax(0, 1fr) auto;
  grid-template-areas:
    'rank user cost'
    '. meta meta';
  align-items: center;
  gap: 0.25rem 0.625rem;
  padding: 0.75rem 0.75rem;
  border-radius: 1rem;
  transition: background-color 150ms ease;
}

.lb-row-labels {
  display: none;
}

.lb-list > .lb-row + .lb-row::before {
  position: absolute;
  top: 0;
  right: 0.875rem;
  left: 0.875rem;
  height: 1px;
  background: var(--lb-hairline);
  content: '';
}

.lb-list > .lb-row:hover {
  background: #fffbe8;
}

.lb-list > .lb-row.is-me {
  background: #fff3bd;
}

.lb-list > .lb-row.is-me::before,
.lb-list > .lb-row.is-me + .lb-row::before,
.lb-list > .lb-row:hover::before,
.lb-list > .lb-row:hover + .lb-row::before {
  opacity: 0;
}

.lb-rank {
  grid-area: rank;
  font-size: 0.875rem;
  font-weight: 750;
  font-variant-numeric: tabular-nums;
  text-align: center;
  color: var(--lb-ink-soft);
}

.is-me .lb-rank {
  color: var(--lb-honey-ink);
}

.lb-user {
  display: flex;
  min-width: 0;
  grid-area: user;
  align-items: center;
  gap: 0.625rem;
}

.lb-avatar {
  display: flex;
  width: 2.25rem;
  height: 2.25rem;
  flex-shrink: 0;
  align-items: center;
  justify-content: center;
  overflow: hidden;
  border-radius: 999px;
  font-size: 0.8125rem;
  font-weight: 700;
}

.lb-avatar img {
  width: 100%;
  height: 100%;
  object-fit: cover;
}

.lb-name {
  min-width: 0;
  font-size: 0.875rem;
  font-weight: 600;
  line-height: 1.3;
  overflow-wrap: anywhere;
}

.lb-meta {
  display: flex;
  grid-area: meta;
  flex-wrap: wrap;
  gap: 0 0.875rem;
  padding-left: 2.875rem;
}

.lb-metric {
  font-size: 0.75rem;
  font-variant-numeric: tabular-nums;
  color: var(--lb-ink-soft);
}

.lb-metric-short {
  display: none;
}

.lb-cost {
  grid-area: cost;
  font-size: 0.9375rem;
  font-weight: 750;
  font-variant-numeric: tabular-nums;
  text-align: right;
}

.tint-honey { background: #fff3bd; color: #7e5600; }
.tint-garden { background: #e4f0df; color: #386344; }
.tint-coral { background: #fce7df; color: #a44f3b; }
.tint-slate { background: #e5eff5; color: #456a7a; }
.tint-plum { background: #eee8f5; color: #6e5d84; }

@container lb (min-width: 44rem) {
  .lb-row {
    grid-template-columns: 3rem minmax(0, 1fr) 8rem 8rem 8.5rem;
    grid-template-areas: none;
    gap: 0 1rem;
    padding: 0.6875rem 1rem;
  }

  .lb-row > *,
  .lb-meta > * {
    grid-area: auto;
  }

  .lb-row-labels {
    display: grid;
    padding-top: 0;
    padding-bottom: 0.375rem;
    font-size: 0.75rem;
    font-weight: 600;
    color: var(--lb-ink-soft);
  }

  .lb-row-labels .lb-rank,
  .lb-row-labels .lb-metric,
  .lb-row-labels .lb-cost {
    font-size: 0.75rem;
    font-weight: 600;
  }

  .lb-meta {
    display: contents;
  }

  .lb-metric {
    font-size: 0.875rem;
    text-align: right;
  }

  /* Column headers name the metrics, so only screen readers keep the units. */
  .lb-metric-full {
    position: absolute;
    width: 1px;
    height: 1px;
    overflow: hidden;
    clip: rect(0 0 0 0);
    white-space: nowrap;
  }

  .lb-metric-short {
    display: inline;
  }
}

@container lb (min-width: 60rem) {
  .lb-top {
    grid-template-columns: minmax(0, 1.7fr) minmax(18rem, 1fr);
  }
}

@container lb (max-width: 37rem) {
  .lb-stage {
    padding: 1rem 0.875rem 0;
    border-radius: 1.5rem;
  }

  .lb-stage-title {
    font-size: 1.125rem;
  }

  .lb-podium {
    gap: 0.375rem;
    margin-top: 1.25rem;
  }

  .lb-podium::before {
    top: -9.5rem;
    width: 22rem;
    height: 22rem;
  }

  .podium-slot.is-rank-1 { --plinth-h: 6.75rem; }
  .podium-slot.is-rank-2 { --plinth-h: 5.25rem; }
  .podium-slot.is-rank-3 { --plinth-h: 4.25rem; }

  .podium-avatar {
    width: 2.75rem;
    height: 2.75rem;
    margin-bottom: 0.375rem;
    font-size: 0.9375rem;
    box-shadow:
      0 0 0 2px #fffef8,
      0 0 0 4.5px var(--medal),
      0 10px 18px -10px rgb(63 52 32 / 0.55);
  }

  .is-rank-1 .podium-avatar {
    width: 3.5rem;
    height: 3.5rem;
    font-size: 1.25rem;
  }

  .podium-name {
    font-size: 0.75rem;
  }

  .podium-requests {
    display: none;
  }

  .podium-plinth {
    padding-top: 0.625rem;
    border-radius: 0.875rem 0.875rem 0 0;
  }

  .podium-rank {
    font-size: 1.625rem;
  }

  .podium-cost {
    font-size: 0.8125rem;
  }

  .lb-me,
  .lb-board {
    border-radius: 1.5rem;
  }

  .lb-me {
    padding: 1rem;
  }

  .lb-me-rank {
    font-size: 2.5rem;
  }

  .lb-me-stats dd {
    font-size: 0.9375rem;
  }
}

@media (prefers-reduced-motion: reduce) {
  .podium-plinth {
    animation: none;
  }

  .countdown-ring-value,
  .lb-switch-thumb {
    transition: none;
  }
}
</style>
