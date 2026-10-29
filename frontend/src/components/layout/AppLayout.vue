<template>
  <div class="app-shell min-h-screen">

    <!-- Sidebar -->
    <AppSidebar />

    <!-- Main Content Area -->
    <div
      class="relative min-h-screen transition-all duration-300"
      :class="[sidebarCollapsed ? 'lg:ml-[84px]' : 'lg:ml-64']"
    >
      <!-- Keeps content that scrolls under the floating header from showing in the gap above it. -->
      <div class="app-topbar-guard" aria-hidden="true"></div>

      <!-- Header -->
      <AppHeader />

      <!-- Main Content -->
      <main
        class="app-main p-4 md:p-6 lg:p-8"
        :class="{ 'is-navigating': isNavigating }"
        :aria-busy="isNavigating ? 'true' : undefined"
      >
        <div class="app-main-content">
          <slot />
        </div>
        <!-- While the next page's code and guards load, the current page fades and a loader holds its place. -->
        <Transition name="route-loader">
          <div v-if="isNavigating" class="route-loader" data-testid="route-loader">
            <div class="route-loader-anchor">
              <PageLoader />
            </div>
          </div>
        </Transition>
      </main>
    </div>
  </div>
</template>

<script setup lang="ts">
import '@/styles/onboarding.css'
import { computed, onMounted } from 'vue'
import { useAppStore } from '@/stores'
import { useAuthStore } from '@/stores/auth'
import { useOnboardingTour } from '@/composables/useOnboardingTour'
import { useOnboardingStore } from '@/stores/onboarding'
import AppSidebar from './AppSidebar.vue'
import AppHeader from './AppHeader.vue'
import PageLoader from '@/components/common/PageLoader.vue'
import { useNavigationLoadingState } from '@/composables/useNavigationLoading'

const appStore = useAppStore()
const authStore = useAuthStore()

// Console pages are light-only. Keep the saved preference untouched so Home
// can restore and toggle its own dark appearance independently.
document.documentElement.classList.remove('dark')

const sidebarCollapsed = computed(() => appStore.sidebarCollapsed)
// Already delayed by the anti-flicker window, so quick switches never flash the loader.
const { isLoading: isNavigating } = useNavigationLoadingState()
const isAdmin = computed(() => authStore.user?.role === 'admin')

const { replayTour } = useOnboardingTour({
  storageKey: isAdmin.value ? 'admin_guide' : 'user_guide',
  autoStart: true
})

const onboardingStore = useOnboardingStore()

onMounted(() => {
  onboardingStore.setReplayCallback(replayTour)
})

defineExpose({ replayTour })
</script>

<style scoped>
/* Phones get one flat canvas, the same colour as the browser bars
 * (theme-color) and the overscroll area, so nothing at the top reads as a
 * separate block. iOS ignores fixed backgrounds, which made the glows below
 * scroll with the page and tint only its top. */
.app-shell {
  background-color: var(--md-sys-color-background);
  color: var(--md-sys-color-on-surface);
}

.app-topbar-guard {
  position: fixed;
  top: 0;
  right: 0;
  left: 0;
  z-index: 29;
  height: 0.75rem;
  background-color: var(--md-sys-color-background);
  pointer-events: none;
}

/* overflow-x: clip keeps a stray wide child from making the whole page swipe
 * sideways on phones; unlike hidden it creates no scroll container, so sticky
 * headers inside pages keep working. */
.app-main {
  position: relative;
  min-width: 0;
  overflow-x: clip;
}

.app-main-content {
  /* Opacity only: a transform or filter here would re-anchor the fixed-position
   * dialogs some pages render inline to this wrapper instead of the viewport. */
  animation: app-page-enter 240ms ease-out backwards;
  transition: opacity 180ms ease;
}

.app-main.is-navigating .app-main-content {
  opacity: 0.38;
  pointer-events: none;
}

.route-loader {
  position: absolute;
  inset: 0;
  z-index: 20;
  pointer-events: none;
}

/* Sticky inside the overlay so the loader stays in view on a long, scrolled page. */
.route-loader-anchor {
  position: sticky;
  top: 38vh;
  display: flex;
  justify-content: center;
  padding-top: 3rem;
}

.route-loader-enter-active,
.route-loader-leave-active {
  transition: opacity 180ms ease;
}

.route-loader-enter-from,
.route-loader-leave-to {
  opacity: 0;
}

@keyframes app-page-enter {
  from {
    opacity: 0;
  }
}

@media (prefers-reduced-motion: reduce) {
  .app-main-content {
    animation: none;
    transition: none;
  }
}

/* Desktop: soft gold and garden glows give the open canvas some depth
 * without competing with content. The guard paints the same fixed layers so
 * it stays invisible against the canvas. */
@media (min-width: 1024px) {
  .app-shell,
  .app-topbar-guard {
    background-image:
      radial-gradient(42rem 28rem at 100% 0%, rgb(233 184 36 / 0.12), transparent 70%),
      radial-gradient(36rem 26rem at 0% 100%, rgb(121 179 123 / 0.1), transparent 70%);
    background-attachment: fixed;
  }
}
</style>
