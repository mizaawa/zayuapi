<template>
  <div class="app-shell min-h-screen">

    <!-- Sidebar -->
    <AppSidebar />

    <!-- Main Content Area -->
    <div
      class="relative min-h-screen transition-all duration-300"
      :class="[sidebarCollapsed ? 'lg:ml-[84px]' : 'lg:ml-64']"
    >
      <!-- Header -->
      <AppHeader />

      <!-- Main Content -->
      <main class="p-4 md:p-6 lg:p-8">
        <slot />
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

const appStore = useAppStore()
const authStore = useAuthStore()

// Console pages are light-only. Keep the saved preference untouched so Home
// can restore and toggle its own dark appearance independently.
document.documentElement.classList.remove('dark')

const sidebarCollapsed = computed(() => appStore.sidebarCollapsed)
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
.app-shell {
  background-color: var(--md-sys-color-background);
  /* Soft gold and garden glows give the open canvas some depth without
   * competing with content. Fixed so they stay put while pages scroll. */
  background-image:
    radial-gradient(42rem 28rem at 100% 0%, rgb(233 184 36 / 0.12), transparent 70%),
    radial-gradient(36rem 26rem at 0% 100%, rgb(121 179 123 / 0.1), transparent 70%);
  background-attachment: fixed;
  color: var(--md-sys-color-on-surface);
}
</style>
