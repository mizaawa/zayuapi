<template>
  <section class="dash-actions-panel">
    <h2 class="dash-actions-title">{{ t('dashboard.quickActions') }}</h2>
    <div class="dash-actions">
      <button
        v-for="action in actions"
        :key="action.path"
        type="button"
        class="dash-action"
        :class="`dash-action-${action.tone}`"
        @click="router.push(action.path)"
      >
        <span class="dash-action-icon">
          <Icon :name="action.icon" size="md" />
        </span>
        <span class="min-w-0 flex-1">
          <span class="dash-action-label">{{ action.label }}</span>
          <span class="dash-action-desc">{{ action.description }}</span>
        </span>
        <Icon name="chevronRight" size="sm" class="dash-action-chevron" />
      </button>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import { useBatchImageAccess } from '@/composables/useBatchImageAccess'
const router = useRouter()
const { t } = useI18n()
const { canUseBatchImage, refreshBatchImageAccess } = useBatchImageAccess()

type QuickAction = {
  path: string
  icon: 'key' | 'chart' | 'sparkles' | 'gift'
  tone: 'key' | 'usage' | 'image' | 'redeem'
  label: string
  description: string
}

const actions = computed<QuickAction[]>(() => {
  const list: QuickAction[] = [
    { path: '/keys', icon: 'key', tone: 'key', label: t('dashboard.createApiKey'), description: t('dashboard.generateNewKey') },
    { path: '/usage', icon: 'chart', tone: 'usage', label: t('dashboard.viewUsage'), description: t('dashboard.checkDetailedLogs') }
  ]
  if (canUseBatchImage.value) {
    list.push({ path: '/batch-image', icon: 'sparkles', tone: 'image', label: t('dashboard.batchImageAgent'), description: t('dashboard.batchImageAgentDesc') })
  }
  list.push({ path: '/redeem', icon: 'gift', tone: 'redeem', label: t('dashboard.redeemCode'), description: t('dashboard.addBalanceWithCode') })
  return list
})

onMounted(() => {
  void refreshBatchImageAccess()
})
</script>

<style scoped>
.dash-actions-panel {
  height: 100%;
  padding: 1.25rem;
  border-radius: 1.5rem;
  background: var(--md-sys-color-surface);
  box-shadow: var(--theme-highlight), var(--theme-elevation-1);
}

.dash-actions-title {
  margin-bottom: 0.75rem;
  font-size: 0.9375rem;
  font-weight: 700;
  color: var(--md-sys-color-on-surface);
}

.dash-actions {
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
}

.dash-action {
  display: flex;
  width: 100%;
  align-items: center;
  gap: 0.875rem;
  padding: 0.75rem;
  border-radius: 1rem;
  text-align: left;
  background: #fffbe8;
  transition: background-color 160ms ease;
}

.dash-action:hover {
  background: #fff3bd;
}

.dash-action-icon {
  display: inline-flex;
  width: 2.5rem;
  height: 2.5rem;
  flex-shrink: 0;
  align-items: center;
  justify-content: center;
  border-radius: 0.875rem;
}

.dash-action-key .dash-action-icon {
  background: #fff3bd;
  color: #7e5600;
}

.dash-action-usage .dash-action-icon {
  background: #e4f0df;
  color: #386344;
}

.dash-action-image .dash-action-icon {
  background: #e0f2fe;
  color: #0369a1;
}

.dash-action-redeem .dash-action-icon {
  background: #ffedd5;
  color: #c2410c;
}

.dash-action:hover .dash-action-icon {
  background: var(--md-sys-color-surface);
}

.dash-action-label {
  display: block;
  font-size: 0.875rem;
  font-weight: 600;
  color: var(--md-sys-color-on-surface);
}

.dash-action-desc {
  display: block;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 0.75rem;
  color: var(--md-sys-color-on-surface-variant);
}

.dash-action-chevron {
  flex-shrink: 0;
  color: var(--md-sys-color-on-surface-variant);
  opacity: 0.6;
  transition: opacity 160ms ease, transform 200ms var(--theme-ease-out);
}

.dash-action:hover .dash-action-chevron {
  opacity: 1;
  transform: translateX(2px);
}

@media (min-width: 768px) {
  .dash-actions-panel {
    padding: 1.5rem;
  }
}

@media (prefers-reduced-motion: reduce) {
  .dash-action-chevron {
    transition: none;
  }

  .dash-action:hover .dash-action-chevron {
    transform: none;
  }
}
</style>
