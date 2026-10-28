<script setup lang="ts">
import { RouterView, useRouter, useRoute } from 'vue-router'
import { computed, onMounted, onBeforeUnmount, watch } from 'vue'
import Toast from '@/components/common/Toast.vue'
import NavigationProgress from '@/components/common/NavigationProgress.vue'
import AdminComplianceDialog from '@/components/admin/AdminComplianceDialog.vue'
import { resolveDocumentTitle, resolveRouteDocumentTitle } from '@/router/title'
import { notFoundLocation } from '@/router/notFound'
import NotFoundView from '@/views/NotFoundView.vue'
import AnnouncementPopup from '@/components/common/AnnouncementPopup.vue'
import { useAppStore, useAuthStore, useSubscriptionStore, useAnnouncementStore, useAdminComplianceStore, useAdminSettingsStore } from '@/stores'
import { getSetupStatus } from '@/api/setup'
import { updateFavicon } from '@/utils/branding'
import { isUserPaymentPath, usePaymentAccessStore } from '@/stores/paymentAccess'

const router = useRouter()
const route = useRoute()
const appStore = useAppStore()
const authStore = useAuthStore()
const subscriptionStore = useSubscriptionStore()
const announcementStore = useAnnouncementStore()
const adminComplianceStore = useAdminComplianceStore()
const adminSettingsStore = useAdminSettingsStore()
const paymentAccess = usePaymentAccessStore()
const paymentPageHidden = computed(() => route.name !== 'NotFound' && isUserPaymentPath(route.path) && (
  authStore.isAuthenticated
    ? !paymentAccess.allowed
    : paymentAccess.access?.allowed === false || route.meta.requiresAuth !== false ||
      appStore.cachedPublicSettings?.payment_exclusive_enabled !== false
))
const PAYMENT_ACCESS_RETRY_MIN_MS = 3000
const PAYMENT_ACCESS_RETRY_MAX_MS = 30000
let paymentAccessTimer: ReturnType<typeof setTimeout> | null = null
let paymentAccessRefreshGeneration = 0
let paymentAccessRetryDelay = PAYMENT_ACCESS_RETRY_MIN_MS

function stopPaymentAccessRefresh() {
  paymentAccessRefreshGeneration++
  paymentAccess.stopWatching()
  if (paymentAccessTimer !== null) clearTimeout(paymentAccessTimer)
  paymentAccessTimer = null
}

async function refreshPaymentAccess() {
  stopPaymentAccessRefresh()
  paymentAccessRetryDelay = PAYMENT_ACCESS_RETRY_MIN_MS
  const generation = paymentAccessRefreshGeneration
  if (!authStore.isAuthenticated || document.visibilityState === 'hidden') return
  await paymentAccess.refresh()
  if (generation === paymentAccessRefreshGeneration && authStore.isAuthenticated && !document.hidden) {
    paymentAccessTimer = setTimeout(watchPaymentAccess, 0)
  }
}

async function watchPaymentAccess() {
  const generation = paymentAccessRefreshGeneration
  const received = await paymentAccess.waitForChange()
  if (generation === paymentAccessRefreshGeneration && authStore.isAuthenticated && !document.hidden) {
    // Failed watches keep the confirmed grant, so back off instead of polling a struggling server.
    const delay = received ? 0 : paymentAccessRetryDelay
    paymentAccessRetryDelay = received
      ? PAYMENT_ACCESS_RETRY_MIN_MS
      : Math.min(paymentAccessRetryDelay * 2, PAYMENT_ACCESS_RETRY_MAX_MS)
    paymentAccessTimer = setTimeout(watchPaymentAccess, delay)
  }
}

function onPaymentAccessDenied(event: Event) {
  if ((event as CustomEvent<{ status?: number }>).detail?.status === 404 && authStore.isAuthenticated) {
    // Missing orders use the same 404 as revoked access; confirm a fresh grant before hiding the menu.
    void paymentAccess.refresh(true)
    return
  }
  paymentAccess.deny()
}

watch(
  paymentPageHidden,
  (hidden) => {
    if (hidden) {
      // Remove the protected component immediately, even before the replacement navigation completes.
      document.title = resolveDocumentTitle('404 Not Found', appStore.siteName)
      void router.replace(notFoundLocation(route))
    }
  }
)

function updateDocumentTitle() {
  if (paymentPageHidden.value) {
    document.title = resolveDocumentTitle('404 Not Found', appStore.siteName)
    return
  }
  const customMenuItems = [
    ...(appStore.cachedPublicSettings?.custom_menu_items ?? []),
    ...(authStore.isAdmin ? adminSettingsStore.customMenuItems : []),
  ]
  document.title = resolveRouteDocumentTitle(route, appStore.siteName, customMenuItems)
}

// Watch for site settings changes and update favicon/title
watch(
  () => appStore.siteLogo,
  (newLogo) => {
    if (newLogo) {
      updateFavicon(newLogo)
    }
  },
  { immediate: true }
)

watch(
  [
    () => route.fullPath,
    () => route.meta.title,
    () => route.meta.titleKey,
    () => paymentPageHidden.value,
    () => appStore.siteName,
    () => appStore.cachedPublicSettings?.custom_menu_items,
    () => authStore.isAdmin,
    () => adminSettingsStore.customMenuItems,
  ],
  updateDocumentTitle,
  { deep: true }
)

// Watch for authentication state and manage subscription data + announcements
function onVisibilityChange() {
  if (document.visibilityState === 'visible' && authStore.isAuthenticated) {
    announcementStore.fetchAnnouncements()
    void refreshPaymentAccess()
  } else {
    stopPaymentAccessRefresh()
  }
}

function onAdminComplianceRequired(event: Event) {
  const detail = (event as CustomEvent<Record<string, string>>).detail || {}
  adminComplianceStore.requireAcknowledgement(detail)
}

let announcementLoginTimer: ReturnType<typeof setTimeout> | null = null

function cancelAnnouncementLoginTimer() {
  if (announcementLoginTimer !== null) clearTimeout(announcementLoginTimer)
  announcementLoginTimer = null
}

watch(
  [() => authStore.isAuthenticated, () => authStore.user?.id],
  ([isAuthenticated, userId], oldValue) => {
    cancelAnnouncementLoginTimer()
    stopPaymentAccessRefresh()
    if (isAuthenticated) {
      void refreshPaymentAccess()
      if (authStore.isAdmin) {
        adminComplianceStore.fetchStatus().catch((error) => {
          console.error('Failed to fetch admin compliance status:', error)
        })
      }

      // User logged in: preload subscriptions and start polling
      subscriptionStore.fetchActiveSubscriptions().catch((error) => {
        console.error('Failed to preload subscriptions:', error)
      })
      subscriptionStore.startPolling()

      // Announcements: new login vs page refresh restore
      if (oldValue?.[0] === false) {
        // New login: delay 3s then force fetch
        announcementLoginTimer = setTimeout(() => {
          announcementLoginTimer = null
          if (authStore.isAuthenticated && authStore.user?.id === userId) {
            announcementStore.fetchAnnouncements(true)
          }
        }, 3000)
      } else {
        // Page refresh restore (oldValue was undefined)
        announcementStore.fetchAnnouncements()
      }

      // Register visibility change listener
      document.addEventListener('visibilitychange', onVisibilityChange)
    } else {
      paymentAccess.reset()
      // User logged out: clear data and stop polling
      subscriptionStore.clear()
      announcementStore.reset()
      adminComplianceStore.reset()
      document.removeEventListener('visibilitychange', onVisibilityChange)
    }
  },
  { immediate: true }
)

// Route change trigger (throttled by store)
router.afterEach(() => {
  if (authStore.isAuthenticated) {
    announcementStore.fetchAnnouncements()
  }
})

onBeforeUnmount(() => {
  stopPaymentAccessRefresh()
  window.removeEventListener('payment-access-denied', onPaymentAccessDenied)
  window.removeEventListener('payment-access-changed', refreshPaymentAccess)
  cancelAnnouncementLoginTimer()
  document.removeEventListener('visibilitychange', onVisibilityChange)
  window.removeEventListener('admin-compliance-required', onAdminComplianceRequired)
})

onMounted(async () => {
  window.addEventListener('payment-access-denied', onPaymentAccessDenied)
  window.addEventListener('payment-access-changed', refreshPaymentAccess)
  window.addEventListener('admin-compliance-required', onAdminComplianceRequired)

  // Check if setup is needed
  try {
    const status = await getSetupStatus()
    if (status.needs_setup && route.path !== '/setup') {
      router.replace('/setup')
      return
    }
  } catch {
    // If setup endpoint fails, assume normal mode and continue
  }

  // Load public settings into appStore (will be cached for other components)
  await appStore.fetchPublicSettings()

  // Re-resolve document title now that site settings are available
  updateDocumentTitle()
})
</script>

<template>
  <NavigationProgress />
  <RouterView v-slot="{ Component }">
    <NotFoundView v-if="paymentPageHidden" />
    <component :is="Component" v-else />
  </RouterView>
  <Toast />
  <AnnouncementPopup />
  <AdminComplianceDialog />
</template>
