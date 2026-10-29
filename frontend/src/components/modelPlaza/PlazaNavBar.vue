<template>
  <!-- Floating surface like the console header, on the same canvas. -->
  <div class="plaza-nav-shell sticky top-0 z-30 px-3 pt-3 sm:px-4">
    <header class="plaza-nav mx-auto flex max-w-7xl items-center justify-between gap-4 px-3 py-2.5 sm:px-4">
      <!-- 左:站点 logo + 名称 -->
      <div class="flex min-w-0 items-center gap-3">
        <template v-if="settings">
          <span
            class="plaza-nav-logo flex h-9 w-9 flex-shrink-0 items-center justify-center overflow-hidden rounded-xl"
          >
            <img :src="siteLogo || '/logo.svg'" alt="Logo" class="h-full w-full object-contain" />
          </span>
          <span class="truncate text-base font-bold text-gray-900">
            {{ siteName }}
          </span>
        </template>
        <template v-else>
          <span class="h-9 w-9 flex-shrink-0 animate-pulse rounded-xl bg-gray-200 dark:bg-dark-700" aria-hidden="true"></span>
          <span class="h-5 w-28 animate-pulse rounded bg-gray-200 dark:bg-dark-700" aria-hidden="true"></span>
        </template>
      </div>

      <!-- 右:登录 / 回到后台 -->
      <RouterLink
        v-if="isAuthenticated"
        :to="backTarget"
        class="btn btn-primary btn-md flex-shrink-0"
      >
        {{ t('modelPlaza.nav.backToDashboard') }}
      </RouterLink>
      <RouterLink
        v-else
        :to="{ path: '/login', query: { redirect: '/model-plaza' } }"
        class="btn btn-primary btn-md flex-shrink-0"
      >
        {{ t('modelPlaza.nav.login') }}
      </RouterLink>
    </header>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { sanitizeUrl } from '@/utils/url'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'

const { t } = useI18n()
const appStore = useAppStore()
const authStore = useAuthStore()

const settings = computed(() => appStore.cachedPublicSettings)
const siteName = computed(() => settings.value?.site_name || 'Sub2API')
const siteLogo = computed(() =>
  sanitizeUrl(settings.value?.site_logo || '', { allowRelative: true, allowDataUrl: true })
)
const isAuthenticated = computed(() => authStore.isAuthenticated)
const backTarget = computed(() => (authStore.isAdmin ? '/admin/dashboard' : '/dashboard'))
</script>

<style scoped>
/* The shell paints the canvas behind the sticky header so content scrolling
   under it never shows in the gap above the floating surface. */
.plaza-nav-shell {
  background: var(--md-sys-color-background);
}

.plaza-nav {
  border-radius: 1.375rem;
  background: color-mix(in srgb, var(--md-sys-color-surface) 88%, transparent);
  box-shadow:
    inset 0 1px 0 rgb(255 255 255 / 0.75),
    0 0.625rem 1.75rem -0.5rem rgb(63 52 32 / 0.16);
  backdrop-filter: blur(18px) saturate(1.08);
}

.plaza-nav-logo {
  background: #fff7d6;
  box-shadow: inset 0 0 0 1px rgb(215 193 106 / 0.35);
}
</style>
