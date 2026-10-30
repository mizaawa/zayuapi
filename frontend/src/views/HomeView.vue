<template>
  <div v-if="hasHomeContent" class="min-h-screen">
    <iframe
      v-if="isHomeContentUrl"
      :src="homeContent.trim()"
      class="h-screen w-full border-0"
      allowfullscreen
    ></iframe>
    <div v-else v-html="homeContent"></div>
  </div>

  <div
    v-else-if="compactHomeEnabled"
    data-testid="compact-home"
    class="compact-home-page flex min-h-screen flex-col"
  >
    <header class="compact-header px-4 py-4 sm:px-6">
      <nav class="mx-auto flex max-w-5xl flex-wrap items-center justify-between gap-3 sm:gap-4">
        <div class="flex min-w-0 flex-1 items-center gap-3">
          <img
            :src="siteLogo || '/logo.svg'"
            alt="Logo"
            class="h-10 w-10 shrink-0 rounded-2xl object-contain"
          />
          <span class="min-w-0 truncate text-base font-semibold">{{ siteName }}</span>
        </div>
        <div class="flex max-w-full shrink-0 flex-wrap items-center justify-end gap-2">
          <LocaleSwitcher />
          <a
            v-if="docUrl"
            :href="docUrl"
            target="_blank"
            rel="noopener noreferrer"
            class="compact-icon-button"
            :title="t('home.viewDocs')"
          >
            <Icon name="book" size="md" />
          </a>
          <router-link
            :to="isAuthenticated ? dashboardPath : '/login'"
            class="compact-primary-button"
          >
            {{ isAuthenticated ? t('home.dashboard') : t('home.login') }}
          </router-link>
        </div>
      </nav>
    </header>

    <main class="flex min-w-0 flex-1 items-center justify-center px-4 py-16 sm:px-6">
      <div class="compact-card">
        <div class="compact-logo">
          <img
            :src="siteLogo || '/logo.svg'"
            alt="Logo"
          />
        </div>
        <h1 class="compact-title">{{ siteName }}</h1>
        <p class="compact-subtitle">
          {{ siteSubtitle }}
        </p>
        <router-link
          :to="isAuthenticated ? dashboardPath : '/login'"
          class="compact-primary-button compact-cta"
        >
          {{ isAuthenticated ? t('home.goToDashboard') : t('home.login') }}
          <Icon name="arrowRight" size="sm" :stroke-width="2" />
        </router-link>
      </div>
    </main>

    <footer class="compact-footer px-4 py-5 text-center text-sm sm:px-6">
      &copy; {{ currentYear }} {{ siteName }}
    </footer>
  </div>

  <div v-else class="landing-page">
    <header class="landing-header">
      <nav class="landing-nav">
        <div class="landing-brand">
          <img :src="siteLogo || '/logo.svg'" alt="Logo" />
          <span>{{ siteName }}</span>
        </div>

        <div class="landing-actions">
          <LocaleSwitcher />
          <a
            v-if="docUrl"
            :href="docUrl"
            target="_blank"
            rel="noopener noreferrer"
            class="landing-icon-button"
            :title="t('home.viewDocs')"
          >
            <Icon name="book" size="md" />
          </a>
          <router-link
            :to="isAuthenticated ? dashboardPath : '/login'"
            class="landing-login"
          >
            <span v-if="isAuthenticated" class="landing-avatar">{{ userInitial }}</span>
            {{ isAuthenticated ? t('home.dashboard') : t('home.login') }}
          </router-link>
        </div>
      </nav>
    </header>

    <main>
      <section class="landing-hero">
        <div class="hero-signal-field" aria-hidden="true">
          <span class="signal-line signal-line-one"></span>
          <span class="signal-line signal-line-two"></span>
          <span class="signal-line signal-line-three"></span>
          <span class="signal-line signal-line-four"></span>
          <span class="signal-node signal-node-one"></span>
          <span class="signal-node signal-node-two"></span>
          <span class="signal-node signal-node-three"></span>
          <span class="signal-node signal-node-four"></span>
        </div>
        <div class="hero-content">
          <p class="hero-kicker">
            <span class="hero-kicker-dot" aria-hidden="true"></span>
            {{ siteSubtitle }}
          </p>
          <h1>{{ siteName }}</h1>
          <p class="hero-description">{{ t('home.heroDescription') }}</p>
          <div class="hero-actions">
            <router-link
              :to="isAuthenticated ? dashboardPath : '/login'"
              class="hero-primary-action"
            >
              {{ isAuthenticated ? t('home.goToDashboard') : t('home.getStarted') }}
              <Icon name="arrowRight" size="md" :stroke-width="2" />
            </router-link>
            <a
              :href="githubUrl"
              target="_blank"
              rel="noopener noreferrer"
              class="hero-secondary-action hero-github-action"
              data-testid="home-upstream-github"
            >
              <svg aria-hidden="true" viewBox="0 0 24 24">
                <path
                  fill="currentColor"
                  fill-rule="evenodd"
                  clip-rule="evenodd"
                  d="M12 2C6.477 2 2 6.477 2 12c0 4.42 2.865 8.17 6.839 9.49.5.092.682-.217.682-.482 0-.237-.008-.866-.013-1.7-2.782.604-3.369-1.34-3.369-1.34-.454-1.156-1.11-1.464-1.11-1.464-.908-.62.069-.608.069-.608 1.003.07 1.531 1.03 1.531 1.03.892 1.529 2.341 1.087 2.91.831.092-.646.35-1.086.636-1.336-2.22-.253-4.555-1.11-4.555-4.943 0-1.091.39-1.984 1.029-2.683-.103-.253-.446-1.27.098-2.647 0 0 .84-.269 2.75 1.025A9.578 9.578 0 0112 6.836c.85.004 1.705.114 2.504.336 1.909-1.294 2.747-1.025 2.747-1.025.546 1.377.203 2.394.1 2.647.64.699 1.028 1.592 1.028 2.683 0 3.842-2.339 4.687-4.566 4.935.359.309.678.919.678 1.852 0 1.336-.012 2.415-.012 2.743 0 .267.18.578.688.48C19.138 20.167 22 16.418 22 12c0-5.523-4.477-10-10-10z"
                />
              </svg>
              <strong>GitHub</strong>
            </a>
            <a
              v-if="docUrl"
              :href="docUrl"
              target="_blank"
              rel="noopener noreferrer"
              class="hero-docs-action"
            >
              <Icon name="book" size="sm" />
              {{ t('home.docs') }}
            </a>
          </div>
          <div class="routing-visual" aria-label="AI request routing preview">
            <div class="routing-source">
              <span class="routing-source-dot"></span>
              <div>
                <small>Incoming request</small>
                <code>POST /v1/messages</code>
              </div>
            </div>
            <div class="routing-track" aria-hidden="true">
              <span class="routing-packet"></span>
            </div>
            <div class="routing-targets">
              <div>
                <strong class="is-claude"><ProviderIcon provider="anthropic" :size="16" /></strong>
                <span>{{ t('home.providers.claude') }}</span>
              </div>
              <div>
                <strong class="is-gpt"><ProviderIcon provider="openai" :size="16" /></strong>
                <span>GPT</span>
              </div>
              <div>
                <strong class="is-gemini"><ProviderIcon provider="gemini" :size="16" /></strong>
                <span>{{ t('home.providers.gemini') }}</span>
              </div>
            </div>
            <div class="routing-result">
              <span>200</span>
              <small>{{ t('home.tags.stickySession') }}</small>
            </div>
          </div>
        </div>
      </section>

      <section class="product-stage">
        <div class="product-stage-inner">
          <div class="product-stage-heading">
            <h2>{{ t('home.heroSubtitle') }}</h2>
          </div>

          <div class="terminal-container api-console" aria-label="API request preview">
            <div class="api-console-bar">
              <div class="api-console-dots" aria-hidden="true">
                <span></span><span></span><span></span>
              </div>
              <span>API Console</span>
              <span class="api-console-status">Live</span>
            </div>
            <div class="api-console-body">
              <div class="console-line console-line-1">
                <span class="console-prompt">$</span>
                <span class="console-command">curl</span>
                <span class="console-option">-X POST</span>
                <span class="console-path">/v1/messages</span>
              </div>
              <div class="console-line console-line-2">
                <span class="console-note"># Routing to the best available upstream</span>
              </div>
              <div class="console-line console-line-3">
                <span class="console-success">200 OK</span>
                <span class="console-response">{ "content": "Hello!" }</span>
              </div>
              <div class="console-line console-line-4">
                <span class="console-prompt">$</span>
                <span class="console-cursor"></span>
              </div>
            </div>
          </div>
        </div>
      </section>

      <section class="capabilities-section">
        <div class="section-heading">
          <h2>{{ t('home.comparison.title') }}</h2>
        </div>

        <div class="capabilities-grid">
          <article>
            <div class="capability-icon capability-icon-coral">
              <Icon name="key" size="lg" />
            </div>
            <h3>{{ t('home.features.unifiedGateway') }}</h3>
            <p>{{ t('home.features.unifiedGatewayDesc') }}</p>
          </article>
          <article>
            <div class="capability-icon capability-icon-green">
              <Icon name="shield" size="lg" />
            </div>
            <h3>{{ t('home.features.multiAccount') }}</h3>
            <p>{{ t('home.features.multiAccountDesc') }}</p>
          </article>
          <article>
            <div class="capability-icon capability-icon-blue">
              <Icon name="chart" size="lg" />
            </div>
            <h3>{{ t('home.features.balanceQuota') }}</h3>
            <p>{{ t('home.features.balanceQuotaDesc') }}</p>
          </article>
        </div>

        <div class="comparison" data-testid="home-comparison">
          <table class="comparison-table">
            <thead>
              <tr>
                <th scope="col" class="comparison-feature-head">{{ t('home.comparison.headers.feature') }}</th>
                <th scope="col">{{ t('home.comparison.headers.official') }}</th>
                <th scope="col" class="comparison-us">{{ t('home.comparison.headers.us') }}</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="row in comparisonRows" :key="row">
                <th scope="row">{{ t(`home.comparison.items.${row}.feature`) }}</th>
                <td>
                  <span class="comparison-mark is-official" aria-hidden="true"><Icon name="x" size="xs" :stroke-width="2.5" /></span>
                  {{ t(`home.comparison.items.${row}.official`) }}
                </td>
                <td class="comparison-us">
                  <span class="comparison-mark is-us" aria-hidden="true"><Icon name="check" size="xs" :stroke-width="2.5" /></span>
                  {{ t(`home.comparison.items.${row}.us`) }}
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </section>

      <section class="providers-section">
        <div class="providers-inner">
          <div class="section-heading providers-heading">
            <h2>{{ t('home.providers.title') }}</h2>
            <p>{{ t('home.providers.description') }}</p>
          </div>
          <div class="provider-list">
            <div class="provider-item">
              <span class="provider-mark provider-mark-coral"><ProviderIcon provider="anthropic" :size="24" /></span>
              <span>{{ t('home.providers.claude') }}</span>
              <small>{{ t('home.providers.supported') }}</small>
            </div>
            <div class="provider-item">
              <span class="provider-mark provider-mark-green"><ProviderIcon provider="openai" :size="24" /></span>
              <span>GPT</span>
              <small>{{ t('home.providers.supported') }}</small>
            </div>
            <div class="provider-item">
              <span class="provider-mark provider-mark-blue"><ProviderIcon provider="gemini" :size="24" /></span>
              <span>{{ t('home.providers.gemini') }}</span>
              <small>{{ t('home.providers.supported') }}</small>
            </div>
            <div class="provider-item">
              <span class="provider-mark provider-mark-neutral"><ProviderIcon provider="grok" :size="24" /></span>
              <span>{{ t('home.providers.grok') }}</span>
              <small>{{ t('home.providers.supported') }}</small>
            </div>
            <div class="provider-item provider-item-muted">
              <span class="provider-mark"><Icon name="plus" size="md" :stroke-width="2" /></span>
              <span>{{ t('home.providers.more') }}</span>
              <small>{{ t('home.providers.viewMore') }}</small>
            </div>
          </div>
        </div>
      </section>
    </main>

    <footer class="landing-footer">
      <div>
        <p>&copy; {{ currentYear }} {{ siteName }}. {{ t('home.footer.allRightsReserved') }}</p>
        <nav>
          <a v-if="docUrl" :href="docUrl" target="_blank" rel="noopener noreferrer">
            {{ t('home.docs') }}
          </a>
          <a
            :href="githubUrl"
            target="_blank"
            rel="noopener noreferrer"
            class="footer-github-link"
            data-testid="home-upstream-github-footer"
          >
            <svg aria-hidden="true" viewBox="0 0 24 24">
              <path
                fill="currentColor"
                fill-rule="evenodd"
                clip-rule="evenodd"
                d="M12 2C6.477 2 2 6.477 2 12c0 4.42 2.865 8.17 6.839 9.49.5.092.682-.217.682-.482 0-.237-.008-.866-.013-1.7-2.782.604-3.369-1.34-3.369-1.34-.454-1.156-1.11-1.464-1.11-1.464-.908-.62.069-.608.069-.608 1.003.07 1.531 1.03 1.531 1.03.892 1.529 2.341 1.087 2.91.831.092-.646.35-1.086.636-1.336-2.22-.253-4.555-1.11-4.555-4.943 0-1.091.39-1.984 1.029-2.683-.103-.253-.446-1.27.098-2.647 0 0 .84-.269 2.75 1.025A9.578 9.578 0 0112 6.836c.85.004 1.705.114 2.504.336 1.909-1.294 2.747-1.025 2.747-1.025.546 1.377.203 2.394.1 2.647.64.699 1.028 1.592 1.028 2.683 0 3.842-2.339 4.687-4.566 4.935.359.309.678.919.678 1.852 0 1.336-.012 2.415-.012 2.743 0 .267.18.578.688.48C19.138 20.167 22 16.418 22 12c0-5.523-4.477-10-10-10z"
              />
            </svg>
            <strong>GitHub</strong>
          </a>
        </nav>
      </div>
    </footer>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore, useAuthStore } from '@/stores'
import LocaleSwitcher from '@/components/common/LocaleSwitcher.vue'
import Icon from '@/components/icons/Icon.vue'
import ProviderIcon from '@/components/user/monitor/ProviderIcon.vue'
import { sanitizeUrl } from '@/utils/url'

const { t } = useI18n()
const authStore = useAuthStore()
const appStore = useAppStore()

const siteName = computed(() => appStore.cachedPublicSettings?.site_name || appStore.siteName || 'Sub2API')
const siteLogo = computed(() => sanitizeUrl(appStore.cachedPublicSettings?.site_logo || appStore.siteLogo || '', {
  allowRelative: true,
  allowDataUrl: true
}))
const siteSubtitle = computed(() => appStore.cachedPublicSettings?.site_subtitle || 'AI API Gateway Platform')
const docUrl = computed(() => sanitizeUrl(appStore.cachedPublicSettings?.doc_url || appStore.docUrl || ''))
const homeContent = computed(() => appStore.cachedPublicSettings?.home_content || '')
const hasHomeContent = computed(() => homeContent.value.trim().length > 0)
const compactHomeEnabled = computed(() => appStore.cachedPublicSettings?.compact_home_enabled === true)
const isHomeContentUrl = computed(() => {
  const content = homeContent.value.trim()
  return content.startsWith('http://') || content.startsWith('https://')
})

const githubUrl = 'https://github.com/Wei-Shaw/sub2api'
const comparisonRows = ['pricing', 'models', 'management', 'stability', 'control'] as const
const isAuthenticated = computed(() => authStore.isAuthenticated)
const isAdmin = computed(() => authStore.isAdmin)
const dashboardPath = computed(() => isAdmin.value ? '/admin/dashboard' : '/dashboard')
const userInitial = computed(() => authStore.user?.email?.charAt(0).toUpperCase() || '')
const currentYear = computed(() => new Date().getFullYear())

onMounted(() => {
  document.documentElement.classList.remove('dark')
  authStore.checkAuth()
  if (!appStore.publicSettingsLoaded) {
    appStore.fetchPublicSettings()
  }
})
</script>

<style scoped>
.compact-home-page,
.landing-page {
  min-height: 100vh;
  background: var(--md-sys-color-background);
  color: var(--md-sys-color-on-surface);
}

.landing-page {
  --landing-hero-bg: #fffbe8;
  --landing-hero-text: #3f3420;
  --landing-hero-muted: #6b6045;
  --landing-hero-line: #d7c16a;
  --landing-hero-panel: #fffef8;
  --landing-hero-panel-muted: #fff3bd;
  --landing-hero-control: #ffe78a;
  --landing-hero-control-active: #ffffff;
  --landing-accent: #976800;
  --landing-accent-soft: #ffe78a;
  --landing-accent-band: #e9b824;
  --landing-accent-band-text: #3f3420;
  --landing-success-bg: #e4f0df;
  --landing-success-text: #477b54;
  --landing-console-bg: #294536;
  --landing-console-border: #56705b;
  font-weight: 500;
}

/* ---------- Compact home ---------- */
.compact-home-page {
  background:
    radial-gradient(42rem 28rem at 50% 38%, rgb(255 231 138 / 0.55), transparent 70%),
    var(--md-sys-color-background);
}

.compact-header,
.compact-footer {
  border-color: var(--md-sys-color-outline);
}

.compact-header { border-bottom-width: 1px; }
.compact-footer { border-top-width: 1px; color: var(--md-sys-color-on-surface-variant); }

.compact-card {
  display: flex;
  min-width: 0;
  max-width: 36rem;
  flex-direction: column;
  align-items: center;
  text-align: center;
}

.compact-logo {
  display: grid;
  width: 6rem;
  height: 6rem;
  margin-bottom: 1.75rem;
  place-items: center;
  border-radius: 2rem;
  background: var(--md-sys-color-surface);
  box-shadow:
    0 0 0 6px rgb(255 243 189 / 0.9),
    0 22px 40px -22px rgb(126 86 0 / 0.55);
}

.compact-logo img {
  width: 4.5rem;
  height: 4.5rem;
  border-radius: 1.5rem;
  object-fit: contain;
}

.compact-title {
  font-size: clamp(2.5rem, 1.75rem + 3vw, 3.5rem);
  font-weight: 800;
  line-height: 1.1;
  letter-spacing: -0.02em;
  overflow-wrap: anywhere;
}

.compact-subtitle {
  max-width: 30rem;
  margin-top: 1.125rem;
  font-size: 1.0625rem;
  line-height: 1.7;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
  color: var(--md-sys-color-on-surface-variant);
}

.compact-cta {
  gap: 0.5rem;
  margin-top: 2rem;
  padding: 0.875rem 1.75rem;
  font-size: 0.9375rem;
}

.compact-cta:hover {
  transform: translateY(-2px);
}

.compact-icon-button,
.landing-icon-button {
  display: inline-flex;
  width: 2.75rem;
  height: 2.75rem;
  align-items: center;
  justify-content: center;
  border-radius: 50%;
  color: var(--md-sys-color-on-surface-variant);
  transition: background-color 180ms ease, color 180ms ease;
}

.compact-icon-button:hover,
.landing-icon-button:hover { background: var(--md-sys-color-surface-container); color: var(--md-sys-color-on-surface); }

.compact-primary-button,
.landing-login,
.hero-primary-action {
  display: inline-flex;
  min-height: 2.75rem;
  align-items: center;
  justify-content: center;
  border-radius: 1rem;
  background: var(--md-sys-color-primary);
  color: var(--md-sys-color-on-primary);
  font-size: 0.875rem;
  font-weight: 600;
  transition: opacity 180ms ease, transform 180ms ease, box-shadow 180ms ease;
}

.compact-primary-button { padding: 0.625rem 1.125rem; }
.compact-primary-button:hover,
.landing-login:hover { opacity: 0.86; }

/* ---------- Header ---------- */
.landing-header {
  position: sticky;
  top: 0;
  z-index: 20;
  border-bottom: 1px solid rgb(215 193 106 / 0.55);
  background: rgb(255 251 232 / 0.86);
  color: var(--landing-hero-text);
  backdrop-filter: blur(14px) saturate(1.1);
}

.landing-nav {
  display: flex;
  width: min(100% - 3rem, 76rem);
  min-height: 4.5rem;
  margin: 0 auto;
  align-items: center;
  justify-content: space-between;
  gap: 1rem;
}

.landing-brand { display: flex; min-width: 0; align-items: center; gap: 0.75rem; font-weight: 750; }
.landing-brand img { width: 2.5rem; height: 2.5rem; border-radius: 0.875rem; object-fit: contain; }
.landing-brand span { overflow: hidden; font-size: 1.0625rem; text-overflow: ellipsis; white-space: nowrap; }
.landing-actions { display: flex; align-items: center; gap: 0.375rem; }
.landing-actions :deep(button) { color: var(--landing-hero-muted); }
.landing-icon-button { color: var(--landing-hero-muted); }
.landing-icon-button:hover { background: var(--landing-hero-panel-muted); color: var(--landing-hero-text); }
.landing-login { gap: 0.5rem; margin-left: 0.375rem; padding: 0.625rem 1.125rem; background: var(--landing-accent-band); color: var(--landing-accent-band-text); font-weight: 700; box-shadow: inset 0 1px 0 rgb(255 255 255 / 0.4), 0 6px 14px -8px rgb(151 104 0 / 0.55); }
.landing-avatar { display: grid; width: 1.5rem; height: 1.5rem; place-items: center; border-radius: 50%; background: var(--landing-hero-panel); color: var(--landing-accent); font-size: 0.6875rem; font-weight: 800; }

/* ---------- Hero ---------- */
.landing-hero {
  position: relative;
  display: flex;
  overflow: hidden;
  min-height: calc(100svh - 8.5rem);
  align-items: center;
  justify-content: center;
  padding: 4.5rem 1.5rem 5rem;
  background:
    radial-gradient(52rem 30rem at 50% 30%, rgb(255 231 138 / 0.5), transparent 70%),
    var(--landing-hero-bg);
  color: var(--landing-hero-text);
  text-align: center;
}

.hero-signal-field { position: absolute; inset: 0; pointer-events: none; }
.signal-line { position: absolute; height: 1px; background: var(--landing-hero-line); transform-origin: left center; }
.signal-line-one { top: 18%; left: -4rem; width: 27rem; transform: rotate(12deg); }
.signal-line-two { top: 29%; right: -6rem; width: 31rem; transform: rotate(-16deg); }
.signal-line-three { bottom: 24%; left: -5rem; width: 24rem; transform: rotate(-9deg); }
.signal-line-four { right: -4rem; bottom: 17%; width: 28rem; transform: rotate(11deg); }
.signal-node { position: absolute; width: 0.625rem; height: 0.625rem; border: 2px solid var(--landing-accent); border-radius: 50%; background: var(--landing-hero-bg); box-shadow: 0 0 0 0 var(--landing-accent-soft); animation: signal-pulse 3.2s ease-out infinite; }
.signal-node-one { top: 21%; left: 12%; }
.signal-node-two { top: 24%; right: 14%; animation-delay: 0.8s; }
.signal-node-three { bottom: 22%; left: 9%; animation-delay: 1.6s; }
.signal-node-four { right: 11%; bottom: 18%; animation-delay: 2.4s; }
.hero-content { position: relative; z-index: 1; width: min(100%, 72rem); animation: hero-enter 700ms cubic-bezier(0.2, 0, 0, 1) both; }

.hero-kicker {
  display: inline-flex;
  align-items: center;
  gap: 0.5rem;
  margin-bottom: 1.5rem;
  padding: 0.375rem 0.875rem 0.375rem 0.75rem;
  border-radius: 999px;
  background: rgb(255 254 248 / 0.85);
  color: var(--landing-accent);
  font-size: 0.875rem;
  font-weight: 750;
  box-shadow: 0 0 0 1px rgb(215 193 106 / 0.5), 0 6px 16px -10px rgb(126 86 0 / 0.5);
}

.hero-kicker-dot {
  width: 0.5rem;
  height: 0.5rem;
  border-radius: 999px;
  background: #4f9a66;
  box-shadow: 0 0 0 3px rgb(79 154 102 / 0.18);
}

.hero-content h1 {
  margin: 0;
  color: var(--landing-hero-text);
  font-size: clamp(3.25rem, 1.25rem + 7.4vw, 7rem);
  line-height: 0.98;
  font-weight: 800;
  letter-spacing: -0.035em;
  overflow-wrap: anywhere;
}

.hero-description { max-width: 40rem; margin: 1.75rem auto 0; color: var(--landing-hero-muted); font-size: 1.1875rem; font-weight: 550; line-height: 1.7; text-wrap: balance; }
.hero-actions { display: flex; flex-wrap: wrap; align-items: center; justify-content: center; gap: 0.75rem; margin-top: 2.25rem; }
.hero-primary-action { min-height: 3.25rem; gap: 0.625rem; padding: 0.75rem 1.5rem; border-radius: 1.125rem; background: var(--landing-accent-band); color: var(--landing-accent-band-text); font-size: 1rem; font-weight: 750; box-shadow: inset 0 1px 0 rgb(255 255 255 / 0.45), 0 12px 24px -12px rgb(151 104 0 / 0.65); }
.hero-primary-action:hover { transform: translateY(-2px); box-shadow: inset 0 1px 0 rgb(255 255 255 / 0.5), 0 16px 28px -12px rgb(151 104 0 / 0.7); }
.hero-secondary-action,
.hero-docs-action { display: inline-flex; min-height: 3.25rem; align-items: center; justify-content: center; gap: 0.5rem; padding: 0.75rem 1.5rem; border: 1px solid var(--landing-hero-line); border-radius: 1.125rem; color: var(--landing-hero-text); font-weight: 700; transition: background-color 180ms ease, border-color 180ms ease, transform 180ms ease; }
.hero-secondary-action { gap: 0.625rem; background: var(--landing-hero-panel); }
.hero-secondary-action svg { width: 1.25rem; height: 1.25rem; }
.hero-secondary-action strong { font-weight: 800; }
.hero-secondary-action:hover,
.hero-docs-action:hover { border-color: var(--landing-accent); background: var(--landing-hero-panel-muted); transform: translateY(-2px); }

.routing-visual {
  display: grid;
  grid-template-columns: minmax(12rem, 1fr) minmax(7rem, 0.55fr) minmax(19rem, 1.35fr) auto;
  gap: 1rem;
  width: min(100%, 66rem);
  margin: 3.5rem auto 0;
  align-items: center;
  padding: 1rem;
  border: 1px solid var(--landing-hero-line);
  border-radius: 1.75rem;
  background: var(--landing-hero-panel);
  box-shadow: 0 1.5rem 4rem rgb(83 66 24 / 0.14);
  text-align: left;
  animation: stage-enter 700ms 120ms cubic-bezier(0.2, 0, 0, 1) both;
}

.routing-source { display: flex; align-items: center; gap: 0.75rem; padding: 0.75rem; }
.routing-source-dot { width: 0.75rem; height: 0.75rem; flex: 0 0 auto; border-radius: 50%; background: var(--landing-accent); box-shadow: 0 0 0 0.35rem var(--landing-accent-soft); }
.routing-source div { display: flex; min-width: 0; flex-direction: column; gap: 0.25rem; }
.routing-source small,
.routing-result small { color: var(--landing-hero-muted); font-size: 0.6875rem; font-weight: 650; }
.routing-source code { overflow: hidden; color: var(--landing-hero-text); font-size: 0.75rem; font-weight: 700; text-overflow: ellipsis; white-space: nowrap; }
.routing-track { position: relative; height: 2px; overflow: visible; border-radius: 999px; background: var(--landing-hero-line); }
.routing-track::after { position: absolute; top: -0.2rem; right: -0.125rem; width: 0.5rem; height: 0.5rem; border: 1px solid var(--landing-accent); border-radius: 50%; background: var(--landing-hero-panel); content: ''; }
.routing-packet { position: absolute; top: -0.25rem; left: 0; width: 0.625rem; height: 0.625rem; border-radius: 50%; background: var(--landing-accent); box-shadow: 0 0 0 0.25rem var(--landing-accent-soft); animation: route-packet 2.4s cubic-bezier(0.4, 0, 0.2, 1) infinite; }
.routing-targets { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 0.5rem; }
.routing-targets > div { display: flex; min-width: 0; align-items: center; gap: 0.5rem; padding: 0.625rem; border: 1px solid transparent; border-radius: 0.875rem; background: var(--landing-hero-panel-muted); animation: target-breathe 4.8s ease-in-out infinite; }
.routing-targets > div:nth-child(2) { animation-delay: 1.6s; }
.routing-targets > div:nth-child(3) { animation-delay: 3.2s; }
.routing-targets strong { display: grid; width: 1.875rem; height: 1.875rem; flex: 0 0 auto; place-items: center; border-radius: 0.625rem; background: var(--landing-hero-panel); color: var(--landing-accent); }
.routing-targets strong.is-claude { color: #b4573f; }
.routing-targets strong.is-gpt { color: #2e5038; }
.routing-targets strong.is-gemini { color: #456a7a; }
.routing-targets span { overflow: hidden; color: var(--landing-hero-muted); font-size: 0.75rem; font-weight: 650; text-overflow: ellipsis; white-space: nowrap; }
.routing-result { display: flex; align-items: center; gap: 0.625rem; padding: 0.75rem; }
.routing-result > span { padding: 0.375rem 0.625rem; border-radius: 0.625rem; background: var(--landing-success-bg); color: var(--landing-success-text); font-family: ui-monospace, monospace; font-size: 0.75rem; font-weight: 800; }

/* ---------- Product stage ---------- */
.product-stage { padding: 6.5rem 1.5rem 7rem; background: var(--landing-accent-band); color: var(--landing-accent-band-text); }
.product-stage-inner { width: min(100%, 70rem); margin: 0 auto; }
.product-stage-heading { max-width: 48rem; margin: 0 auto 3rem; text-align: center; }
.product-stage-heading h2 { margin: 0; color: var(--landing-accent-band-text); font-size: clamp(2rem, 1.25rem + 2.6vw, 3rem); line-height: 1.18; font-weight: 800; letter-spacing: -0.02em; text-wrap: balance; word-break: keep-all; overflow-wrap: anywhere; }

.api-console { position: relative; overflow: hidden; border: 1px solid var(--landing-console-border); border-radius: 2rem; background: var(--landing-console-bg); box-shadow: 0 2rem 5rem rgb(35 66 46 / 0.22); animation: stage-enter 700ms 140ms cubic-bezier(0.2, 0, 0, 1) both; }
.api-console::after { position: absolute; top: 4rem; right: 0; left: 0; height: 1px; background: rgb(233 184 36 / 0.5); content: ''; animation: console-scan 5s ease-in-out infinite; }
.api-console-bar { display: grid; grid-template-columns: 1fr auto 1fr; min-height: 4rem; align-items: center; padding: 0 1.5rem; border-bottom: 1px solid var(--landing-console-border); color: #e8edda; font-size: 0.8125rem; font-weight: 650; }
.api-console-dots { display: flex; gap: 0.5rem; }
.api-console-dots span { width: 0.75rem; height: 0.75rem; border-radius: 50%; }
.api-console-dots span:nth-child(1) { background: #de8c78; }
.api-console-dots span:nth-child(2) { background: #e9b824; }
.api-console-dots span:nth-child(3) { background: #79b37b; }
.api-console-status { justify-self: end; color: #bfe0b5; }
.api-console-status::before { display: inline-block; width: 0.5rem; height: 0.5rem; margin-right: 0.5rem; border-radius: 50%; background: #79b37b; content: ''; }
.api-console-body { min-height: 23rem; padding: 4rem clamp(1.5rem, 8%, 6rem); font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace; font-size: 1.125rem; line-height: 2.4; }
.console-line { display: flex; flex-wrap: wrap; align-items: center; gap: 0.75rem; opacity: 0; animation: line-enter 420ms ease forwards; }
.console-line-1 { animation-delay: 500ms; }
.console-line-2 { animation-delay: 850ms; }
.console-line-3 { animation-delay: 1200ms; }
.console-line-4 { animation-delay: 1550ms; }
.console-prompt { color: #bfe0b5; font-weight: 700; }
.console-command { color: #f2b6a6; }
.console-option { color: #c8ddc2; }
.console-path { color: #ffe078; }
.console-note { color: #c8c9aa; font-style: italic; }
.console-success { padding: 0 0.625rem; border-radius: 0.625rem; background: #477b54; color: #f2f8ef; font-weight: 700; }
.console-response { color: #f5d991; }
.console-cursor { width: 0.625rem; height: 1.25rem; background: #bfe0b5; animation: cursor-blink 1s step-end infinite; }

/* ---------- Capabilities & comparison ---------- */
.capabilities-section { padding: 7rem 1.5rem; background: var(--md-sys-color-surface); }
.section-heading { width: min(100%, 46rem); margin: 0 auto 4rem; text-align: center; }
.section-heading h2 { margin: 0; color: var(--md-sys-color-on-surface); font-size: clamp(2rem, 1.4rem + 2vw, 2.75rem); line-height: 1.15; font-weight: 800; letter-spacing: -0.02em; text-wrap: balance; word-break: keep-all; overflow-wrap: anywhere; }
.section-heading p { margin-top: 0.875rem; color: var(--md-sys-color-on-surface-variant); font-size: 1.125rem; font-weight: 550; line-height: 1.6; text-wrap: balance; }
.capabilities-grid { display: grid; width: min(100%, 72rem); margin: 0 auto; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 1rem; }
.capabilities-grid article { padding: 2rem; border-radius: 1.75rem; background: var(--landing-hero-bg); box-shadow: inset 0 0 0 1px rgb(215 193 106 / 0.35); transition: transform 180ms ease, box-shadow 180ms ease; }
.capabilities-grid article:hover { transform: translateY(-3px); box-shadow: inset 0 0 0 1px rgb(215 193 106 / 0.6), 0 1rem 2.5rem -1rem rgb(83 66 24 / 0.25); }
.capability-icon { display: grid; width: 3.5rem; height: 3.5rem; margin-bottom: 1.5rem; place-items: center; border-radius: 1.25rem; }
.capability-icon-coral { background: #fce7df; color: #a44f3b; }
.capability-icon-green { background: #e4f0df; color: #477b54; }
.capability-icon-blue { background: #e5eff5; color: #456a7a; }
.capabilities-grid h3 { margin-bottom: 0.75rem; font-size: 1.25rem; font-weight: 750; }
.capabilities-grid p { color: var(--md-sys-color-on-surface-variant); line-height: 1.75; }

.comparison {
  width: min(100%, 60rem);
  margin: 4rem auto 0;
  overflow: hidden;
  border-radius: 1.75rem;
  background: var(--md-sys-color-surface);
  box-shadow: 0 0 0 1px rgb(215 193 106 / 0.45), 0 1.5rem 3rem -1.75rem rgb(83 66 24 / 0.35);
}

.comparison-table {
  width: 100%;
  border-collapse: collapse;
  font-size: 0.9375rem;
  text-align: left;
}

.comparison-table th,
.comparison-table td {
  padding: 1rem 1.5rem;
  vertical-align: middle;
}

.comparison-table thead th {
  background: #fffbe8;
  color: var(--landing-hero-muted);
  font-size: 0.8125rem;
  font-weight: 700;
}

.comparison-table thead th.comparison-us {
  background: var(--landing-accent-band);
  color: var(--landing-accent-band-text);
}

.comparison-table tbody tr + tr > * {
  box-shadow: inset 0 1px 0 rgb(215 193 106 / 0.3);
}

.comparison-table tbody th {
  width: 22%;
  color: var(--landing-hero-text);
  font-weight: 750;
}

.comparison-table tbody td {
  color: var(--landing-hero-muted);
}

.comparison-table tbody td.comparison-us {
  background: #fff8dc;
  color: var(--landing-hero-text);
  font-weight: 700;
}

.comparison-mark {
  display: inline-grid;
  width: 1.25rem;
  height: 1.25rem;
  margin-right: 0.5rem;
  place-items: center;
  border-radius: 999px;
  vertical-align: -0.2rem;
}

.comparison-mark.is-official { background: #f3eedd; color: #9b8c62; }
.comparison-mark.is-us { background: #4f9a66; color: #fffef8; }

/* ---------- Providers ---------- */
.providers-section { padding: 7rem 1.5rem; background: var(--md-sys-color-surface-container); }
.providers-inner { width: min(100%, 72rem); margin: 0 auto; }
.providers-heading { margin-bottom: 3rem; }
.provider-list { display: flex; flex-wrap: wrap; justify-content: center; gap: 0.75rem; }
.provider-item { display: grid; min-height: 11rem; flex: 1 1 11rem; max-width: 14rem; place-items: center; align-content: center; gap: 0.75rem; padding: 1.25rem; border: 1px solid var(--md-sys-color-outline); border-radius: 1.75rem; background: var(--md-sys-color-surface); font-weight: 750; transition: border-color 180ms ease, transform 180ms ease, box-shadow 180ms ease; }
.provider-item:hover { border-color: var(--landing-accent); box-shadow: 0 1rem 2.5rem rgb(83 66 24 / 0.14); transform: translateY(-3px); }
.provider-mark { display: grid; width: 3.25rem; height: 3.25rem; place-items: center; border-radius: 1.125rem; background: var(--md-sys-color-surface-container); color: var(--landing-accent); font-weight: 750; }
.provider-mark-coral { background: #fce7df; color: #a44f3b; }
.provider-mark-green { background: #e4f0df; color: #2e5038; }
.provider-mark-blue { background: #e5eff5; color: #456a7a; }
.provider-mark-neutral { background: #e8eaed; color: #3c4043; }
.provider-item small { padding: 0.125rem 0.5rem; border-radius: 999px; background: #fffbe8; color: var(--landing-accent); font-size: 0.6875rem; font-weight: 700; }
.provider-item-muted { opacity: 0.78; }

/* ---------- Footer ---------- */
.landing-footer { padding: 2.5rem 1.5rem; border-top: 1px solid var(--md-sys-color-outline); background: var(--md-sys-color-background); color: var(--md-sys-color-on-surface-variant); }
.landing-footer > div { display: flex; width: min(100%, 72rem); margin: 0 auto; align-items: center; justify-content: space-between; gap: 1rem; font-size: 0.875rem; }
.landing-footer nav { display: flex; gap: 1.5rem; }
.landing-footer a { font-weight: 650; }
.landing-footer a:hover { color: var(--md-sys-color-on-surface); }
.footer-github-link { display: inline-flex; align-items: center; gap: 0.45rem; color: var(--md-sys-color-on-surface); }
.footer-github-link svg { width: 1rem; height: 1rem; }
.footer-github-link strong { font-weight: 800; }

@keyframes hero-enter { from { opacity: 0; transform: translateY(1.25rem); } to { opacity: 1; transform: translateY(0); } }
@keyframes stage-enter { from { opacity: 0; transform: translateY(1.5rem); } to { opacity: 1; transform: translateY(0); } }
@keyframes line-enter { from { opacity: 0; transform: translateY(0.5rem); } to { opacity: 1; transform: translateY(0); } }
@keyframes cursor-blink { 0%, 50% { opacity: 1; } 51%, 100% { opacity: 0; } }
@keyframes route-packet { 0% { left: 0; opacity: 0; } 12% { opacity: 1; } 88% { opacity: 1; } 100% { left: calc(100% - 0.625rem); opacity: 0; } }
@keyframes signal-pulse { 0%, 30% { box-shadow: 0 0 0 0 var(--landing-accent-soft); } 70%, 100% { box-shadow: 0 0 0 0.8rem transparent; } }
@keyframes target-breathe { 0%, 82%, 100% { border-color: transparent; } 88%, 94% { border-color: var(--landing-accent); } }
@keyframes console-scan { 0% { transform: translateY(0); opacity: 0; } 10% { opacity: 0.75; } 85% { opacity: 0.35; } 100% { transform: translateY(22rem); opacity: 0; } }

@media (max-width: 900px) {
  .routing-visual { grid-template-columns: minmax(0, 1fr) auto; }
  .routing-track { grid-column: 1 / -1; }
  .routing-targets { min-width: 0; }
  .capabilities-grid { grid-template-columns: 1fr; gap: 0.75rem; }
  .capabilities-grid article { display: grid; grid-template-columns: auto minmax(0, 1fr); column-gap: 1.25rem; padding: 1.5rem; }
  .capabilities-grid article .capability-icon { grid-row: span 2; margin-bottom: 0; }
  .capabilities-grid h3 { margin-bottom: 0.375rem; }
}

@media (max-width: 640px) {
  .landing-nav { width: min(100% - 2rem, 76rem); min-height: 4rem; }
  .landing-brand span { display: none; }
  .landing-actions { gap: 0; }
  .landing-actions :deep(.relative button) { padding-inline: 0.5rem; }
  .landing-login { min-height: 2.5rem; padding: 0.5rem 0.875rem; }
  .landing-icon-button { width: 2.5rem; height: 2.5rem; }
  .landing-hero { min-height: 39rem; padding: 4rem 1rem 4.5rem; }
  .signal-line-one,
  .signal-line-three,
  .signal-node-one,
  .signal-node-three { display: none; }
  .hero-kicker { margin-bottom: 1.25rem; }
  .hero-content h1 { line-height: 1.02; }
  .hero-description { margin-top: 1.25rem; font-size: 1rem; }
  .hero-actions { gap: 0.625rem; }
  .hero-primary-action { width: 100%; }
  .hero-secondary-action,
  .hero-docs-action { flex: 1 1 0; padding-inline: 1rem; }
  .routing-visual { margin-top: 2.75rem; padding: 0.75rem; border-radius: 1.5rem; }
  .routing-source { padding: 0.5rem; }
  .routing-targets { grid-column: 1 / -1; grid-template-columns: repeat(3, minmax(0, 1fr)); }
  .routing-targets > div { justify-content: center; padding: 0.5rem; }
  .routing-targets span { display: none; }
  .routing-result { grid-column: 1 / -1; justify-content: center; padding: 0.5rem; }
  .routing-result small { display: none; }
  .product-stage { padding: 4.5rem 1rem; }
  .product-stage-heading { margin-bottom: 2rem; }
  .api-console { border-radius: 1.5rem; }
  .api-console-bar { padding: 0 1rem; }
  .api-console-body { min-height: 18rem; padding: 2.5rem 1rem; font-size: 0.8125rem; line-height: 2.15; }
  .console-line { gap: 0.45rem; }
  .capabilities-section,
  .providers-section { padding: 4.5rem 1rem; }
  .section-heading { margin-bottom: 2.5rem; }
  .section-heading p { font-size: 1rem; }
  .capabilities-grid article { padding: 1.25rem; column-gap: 1rem; }
  .capability-icon { width: 3rem; height: 3rem; border-radius: 1rem; }
  .comparison { margin-top: 2.5rem; border-radius: 1.5rem; }
  /* Each comparison row becomes a small card: the topic, then both answers side by side. */
  .comparison-table,
  .comparison-table thead,
  .comparison-table tbody { display: block; }
  .comparison-table thead tr { display: grid; grid-template-columns: 1fr 1fr; }
  .comparison-table thead .comparison-feature-head { display: none; }
  .comparison-table tbody tr { display: grid; grid-template-columns: 1fr 1fr; }
  .comparison-table th,
  .comparison-table td { padding: 0.75rem 1rem; }
  .comparison-table tbody th { width: auto; grid-column: 1 / -1; padding-bottom: 0.25rem; font-size: 0.875rem; }
  .comparison-table tbody tr + tr > * { box-shadow: none; }
  .comparison-table tbody tr + tr { box-shadow: inset 0 1px 0 rgb(215 193 106 / 0.3); }
  .comparison-table tbody td { font-size: 0.8125rem; line-height: 1.5; }
  .comparison-mark { display: grid; margin: 0 0 0.375rem; }
  .provider-item { min-height: 8.5rem; flex-basis: 9rem; padding: 1rem; }
  .landing-footer > div { flex-direction: column; text-align: center; }
}

@media (prefers-reduced-motion: reduce) {
  .hero-content,
  .api-console,
  .console-line,
  .routing-visual,
  .routing-targets > div,
  .signal-node,
  .api-console::after { animation: none; opacity: 1; transform: none; }
  .routing-packet { animation: none; left: calc(50% - 0.3125rem); }
  .console-cursor { animation: none; }
  .hero-primary-action:hover,
  .compact-cta:hover,
  .capabilities-grid article:hover,
  .provider-item:hover { transform: none; }
}
</style>

<style>
/* Keep legacy dark-class mounts on the same light-only palette. */
.dark .landing-page { color-scheme: light; }
.dark .landing-page .provider-item:hover { border-color: #976800; }
</style>
