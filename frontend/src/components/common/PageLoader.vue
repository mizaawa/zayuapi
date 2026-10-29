<template>
  <div class="page-loader" :class="{ 'is-compact': compact }" role="status" aria-live="polite">
    <span class="page-loader-mark" aria-hidden="true">
      <span class="page-loader-ring"></span>
      <span class="page-loader-core"></span>
    </span>
    <span class="page-loader-label">{{ label || t('common.loading') }}</span>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'

defineProps<{
  label?: string
  /** Smaller variant for in-card loading states. */
  compact?: boolean
}>()

const { t } = useI18n()
</script>

<style scoped>
.page-loader {
  display: inline-flex;
  align-items: center;
  gap: 0.75rem;
  padding: 0.75rem 1.125rem 0.75rem 0.875rem;
  border-radius: 999px;
  background: var(--md-sys-color-surface);
  box-shadow: var(--theme-highlight), var(--theme-elevation-2);
  font-size: 0.8125rem;
  font-weight: 600;
  color: var(--md-sys-color-on-surface-variant);
  animation: page-loader-in 220ms var(--theme-ease-out) both;
}

.page-loader.is-compact {
  gap: 0.5rem;
  padding: 0.375rem 0.75rem 0.375rem 0.5rem;
  box-shadow: none;
  background: #fffbe8;
  font-size: 0.75rem;
}

.page-loader-mark {
  position: relative;
  display: inline-flex;
  width: 1.75rem;
  height: 1.75rem;
  flex-shrink: 0;
  align-items: center;
  justify-content: center;
}

.is-compact .page-loader-mark {
  width: 1.25rem;
  height: 1.25rem;
}

/* A honey arc sweeping round a pale track, with a soft garden-green core. */
.page-loader-ring {
  position: absolute;
  inset: 0;
  border-radius: 999px;
  background:
    conic-gradient(from 0deg, rgb(233 184 36 / 0) 0deg, #e9b824 260deg, #c99a12 330deg, rgb(233 184 36 / 0) 360deg),
    rgb(233 184 36 / 0.16);
  -webkit-mask: radial-gradient(farthest-side, transparent calc(100% - 3px), #000 calc(100% - 3px));
  mask: radial-gradient(farthest-side, transparent calc(100% - 3px), #000 calc(100% - 3px));
  animation: page-loader-spin 0.9s linear infinite;
}

.page-loader-core {
  width: 0.4375rem;
  height: 0.4375rem;
  border-radius: 999px;
  background: #79b37b;
  animation: page-loader-pulse 1.2s ease-in-out infinite;
}

.is-compact .page-loader-core {
  width: 0.3125rem;
  height: 0.3125rem;
}

@keyframes page-loader-spin {
  to {
    transform: rotate(360deg);
  }
}

@keyframes page-loader-pulse {
  0%,
  100% {
    transform: scale(0.75);
    opacity: 0.6;
  }
  50% {
    transform: scale(1.15);
    opacity: 1;
  }
}

@keyframes page-loader-in {
  from {
    opacity: 0;
    transform: translateY(6px) scale(0.98);
  }
}

@media (prefers-reduced-motion: reduce) {
  .page-loader {
    animation: none;
  }

  .page-loader-ring {
    animation-duration: 2.4s;
  }

  .page-loader-core {
    animation: none;
  }
}
</style>
