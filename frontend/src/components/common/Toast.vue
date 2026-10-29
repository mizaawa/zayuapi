<template>
  <Teleport to="body">
    <!-- The region stays mounted so new messages are announced; errors interrupt. -->
    <section class="toast-viewport" :aria-label="t('common.notifications')">
      <TransitionGroup
        tag="div"
        name="toast"
        class="toast-stack"
        aria-live="polite"
        aria-relevant="additions"
        @before-leave="lockHeight"
      >
        <div
          v-for="toast in toasts"
          :key="toast.id"
          :class="['toast-card', `toast-tone-${toneOf(toast.type)}`, { 'is-paused': toast.paused }]"
          :role="toast.type === 'error' ? 'alert' : undefined"
          data-testid="toast"
          @mouseenter="appStore.pauseToast(toast.id)"
          @mouseleave="appStore.resumeToast(toast.id)"
          @focusin="appStore.pauseToast(toast.id)"
          @focusout="appStore.resumeToast(toast.id)"
          @keydown.esc="removeToast(toast.id)"
        >
          <span class="toast-icon" aria-hidden="true">
            <svg v-if="toneOf(toast.type) === 'success'" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.6" stroke-linecap="round" stroke-linejoin="round">
              <path class="toast-check" d="M6.5 12.5l3.75 3.75L17.5 8.5" pathLength="1" />
            </svg>
            <Icon v-else :name="iconOf(toast.type)" size="md" :stroke-width="2" />
          </span>

          <div class="toast-body">
            <p v-if="toast.title" class="toast-title">{{ toast.title }}</p>
            <p :class="toast.title ? 'toast-message-muted' : 'toast-message'">{{ toast.message }}</p>
          </div>

          <button
            type="button"
            class="toast-close"
            :aria-label="t('common.close')"
            @click="removeToast(toast.id)"
          >
            <Icon name="x" size="sm" />
          </button>

          <!-- Time left before it closes on its own; pauses while hovered or focused. -->
          <span v-if="toast.duration" class="toast-progress-track" aria-hidden="true">
            <span
              class="toast-progress"
              :style="{ animationDuration: `${toast.duration}ms` }"
            ></span>
          </span>
        </div>
      </TransitionGroup>
    </section>
  </Teleport>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import { useAppStore } from '@/stores/app'
import type { ToastType } from '@/types'

const appStore = useAppStore()
const { t } = useI18n()

const toasts = computed(() => appStore.toasts)

const TONES: ToastType[] = ['success', 'error', 'warning', 'info']
const toneOf = (type: string): ToastType => (TONES.includes(type as ToastType) ? (type as ToastType) : 'info')

const iconOf = (type: string): 'xCircle' | 'exclamationTriangle' | 'infoCircle' => {
  switch (toneOf(type)) {
    case 'error':
      return 'xCircle'
    case 'warning':
      return 'exclamationTriangle'
    default:
      return 'infoCircle'
  }
}

const removeToast = (id: string) => {
  appStore.hideToast(id)
}

// Pin the current height so a dismissed card can collapse smoothly instead of
// leaving a hole that snaps shut.
const lockHeight = (el: Element) => {
  const node = el as HTMLElement
  node.style.maxHeight = `${node.offsetHeight}px`
}
</script>

<style scoped>
/* Top-centre on phones (clear of the thumb), top-right on wider screens. */
.toast-viewport {
  position: fixed;
  top: max(0.75rem, env(safe-area-inset-top));
  right: 0.75rem;
  left: 0.75rem;
  z-index: 9999;
  pointer-events: none;
}

.toast-stack {
  display: flex;
  flex-direction: column;
  align-items: center;
}

.toast-card {
  --toast-accent: #e9b824;
  --toast-ink: #7e5600;
  --toast-wash: #fff3bd;
  position: relative;
  display: flex;
  width: min(24rem, 100%);
  align-items: flex-start;
  gap: 0.75rem;
  overflow: hidden;
  margin-bottom: 0.625rem;
  padding: 0.875rem 0.75rem 0.9375rem 0.875rem;
  border-radius: 1.25rem;
  background: var(--md-sys-color-surface);
  color: var(--md-sys-color-on-surface);
  pointer-events: auto;
  box-shadow:
    inset 0 1px 0 rgb(255 255 255 / 0.85),
    0 0 0 1px rgb(215 193 106 / 0.28),
    0 1rem 2.25rem -0.75rem rgb(63 52 32 / 0.3),
    0 0.25rem 0.75rem rgb(63 52 32 / 0.06);
}

.toast-tone-success {
  --toast-accent: #5c9865;
  --toast-ink: #2e5038;
  --toast-wash: #e4f0df;
}

.toast-tone-error {
  --toast-accent: #c73545;
  --toast-ink: #a1202f;
  --toast-wash: #fde4e4;
}

.toast-tone-warning {
  --toast-accent: #d48a06;
  --toast-ink: #8a5300;
  --toast-wash: #fdf0cf;
}

.toast-tone-info {
  --toast-accent: #e9b824;
  --toast-ink: #7e5600;
  --toast-wash: #fff3bd;
}

.toast-icon {
  display: inline-flex;
  width: 2rem;
  height: 2rem;
  flex-shrink: 0;
  align-items: center;
  justify-content: center;
  border-radius: 999px;
  background: var(--toast-wash);
  color: var(--toast-ink);
}

.toast-icon svg {
  width: 1.125rem;
  height: 1.125rem;
}

.toast-body {
  min-width: 0;
  flex: 1;
  padding-top: 0.3125rem;
}

.toast-title {
  font-size: 0.875rem;
  font-weight: 650;
  line-height: 1.25rem;
  color: var(--md-sys-color-on-surface);
}

.toast-message {
  font-size: 0.875rem;
  font-weight: 550;
  line-height: 1.375rem;
  overflow-wrap: anywhere;
  color: var(--md-sys-color-on-surface);
}

.toast-message-muted {
  margin-top: 0.125rem;
  font-size: 0.8125rem;
  line-height: 1.25rem;
  overflow-wrap: anywhere;
  color: var(--md-sys-color-on-surface-variant);
}

.toast-close {
  display: inline-flex;
  width: 1.75rem;
  height: 1.75rem;
  flex-shrink: 0;
  align-items: center;
  justify-content: center;
  margin-top: 0.125rem;
  border-radius: 999px;
  color: var(--md-sys-color-on-surface-variant);
  transition: background-color 150ms ease, color 150ms ease;
}

.toast-close:hover,
.toast-close:focus-visible {
  background: var(--toast-wash);
  color: var(--toast-ink);
}

.toast-progress-track {
  position: absolute;
  right: 0;
  bottom: 0;
  left: 0;
  height: 3px;
  background: color-mix(in srgb, var(--toast-accent) 14%, transparent);
}

.toast-progress {
  display: block;
  height: 100%;
  background: var(--toast-accent);
  transform-origin: left center;
  animation-name: toast-progress-shrink;
  animation-timing-function: linear;
  animation-fill-mode: forwards;
}

.toast-card.is-paused .toast-progress {
  animation-play-state: paused;
}

@keyframes toast-progress-shrink {
  from { transform: scaleX(1); }
  to { transform: scaleX(0); }
}

/* Success draws its tick once the card has landed. */
.toast-check {
  stroke-dasharray: 1;
  stroke-dashoffset: 1;
  animation: toast-check-draw 360ms 160ms cubic-bezier(0.65, 0, 0.35, 1) forwards;
}

@keyframes toast-check-draw {
  to { stroke-dashoffset: 0; }
}

/* Enter: drop in with a small spring. Leave: lift away while the stack closes the gap. */
.toast-enter-active {
  transition:
    transform 420ms cubic-bezier(0.34, 1.4, 0.64, 1),
    opacity 220ms ease-out;
}

.toast-leave-active {
  transition:
    transform 200ms ease-in,
    opacity 160ms ease-in,
    max-height 240ms var(--theme-ease-out) 80ms,
    margin-bottom 240ms var(--theme-ease-out) 80ms,
    padding 240ms var(--theme-ease-out) 80ms;
}

.toast-enter-from {
  opacity: 0;
  transform: translateY(-0.875rem) scale(0.94);
}

.toast-leave-to {
  max-height: 0 !important;
  margin-bottom: 0;
  padding-top: 0;
  padding-bottom: 0;
  opacity: 0;
  transform: translateY(-0.5rem) scale(0.96);
}

.toast-move {
  transition: transform 320ms var(--theme-ease-out);
}

@media (min-width: 640px) {
  .toast-viewport {
    top: 1rem;
    right: 1rem;
    left: auto;
    width: 24rem;
  }

  .toast-stack {
    align-items: stretch;
  }

  .toast-card {
    width: 100%;
  }

  .toast-enter-from {
    transform: translateX(1.5rem) scale(0.96);
  }

  .toast-leave-to {
    transform: translateX(1.25rem) scale(0.98);
  }
}

@media (prefers-reduced-motion: reduce) {
  .toast-enter-active,
  .toast-leave-active,
  .toast-move {
    transition: opacity 150ms linear;
  }

  .toast-enter-from,
  .toast-leave-to {
    transform: none;
  }

  .toast-check {
    animation: none;
    stroke-dashoffset: 0;
  }

  .toast-progress {
    animation: none;
    transform: scaleX(0.5);
    opacity: 0.4;
  }
}
</style>
