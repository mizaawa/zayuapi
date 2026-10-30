<template>
  <div
    class="monitor-provider-grid grid grid-cols-1 items-start gap-4 lg:grid-cols-2"
    data-testid="monitor-provider-grid"
  >
    <section
      v-for="provider in providerSections"
      :key="provider.value"
      class="monitor-provider-card"
      :class="[
        provider.value === 'custom' ? 'lg:col-span-2' : '',
        expanded[provider.value] ? 'is-open' : '',
      ]"
      :data-testid="`monitor-provider-${provider.value}`"
    >
      <h2 class="monitor-provider-heading">
        <button
          type="button"
          class="monitor-provider-toggle"
          :aria-expanded="expanded[provider.value]"
          :aria-controls="`monitor-provider-content-${provider.value}`"
          :data-testid="`monitor-provider-toggle-${provider.value}`"
          @click="toggleProvider(provider.value)"
        >
          <span class="monitor-provider-mark" :class="`is-${provider.value}`" aria-hidden="true">
            <ProviderIcon :provider="provider.value" :size="18" />
          </span>
          <span class="flex min-w-0 flex-1 flex-wrap items-baseline gap-x-2">
            <span class="monitor-provider-name">{{ provider.label }}</span>
            <span class="monitor-provider-count">
              {{ t('channelStatus.componentCount', { n: provider.items.length }) }}
            </span>
          </span>
          <span
            v-if="!loading && provider.items.length"
            class="monitor-provider-health"
            aria-hidden="true"
          >
            <span
              v-for="item in provider.items"
              :key="item.id"
              class="monitor-provider-dot"
              :class="`is-${healthOf(item)}`"
            ></span>
          </span>
          <Icon
            name="chevronDown"
            size="sm"
            class="monitor-provider-chevron"
          />
        </button>
      </h2>

      <div
        v-show="expanded[provider.value]"
        :id="`monitor-provider-content-${provider.value}`"
        class="monitor-provider-body"
      >
        <div
          v-if="loading"
          class="space-y-4 px-4 pb-5 pt-1 sm:px-5"
          :data-testid="`monitor-provider-loading-${provider.value}`"
          :aria-label="t('channelStatus.loadingProvider', { provider: provider.label })"
        >
          <div class="flex items-center gap-3">
            <div class="skeleton h-5 w-5 flex-shrink-0 rounded-full"></div>
            <div class="skeleton h-4 w-36 rounded-full"></div>
            <div class="skeleton ml-auto h-4 w-16 rounded-full"></div>
          </div>
          <div class="skeleton h-5 rounded-md"></div>
        </div>

        <p
          v-else-if="provider.items.length === 0"
          class="monitor-provider-empty"
          :data-testid="`monitor-provider-empty-${provider.value}`"
        >
          {{ t('channelStatus.emptyProvider') }}
        </p>

        <div
          v-else
          :class="provider.value === 'custom' ? 'monitor-items--custom' : 'monitor-items'"
          :data-testid="`monitor-provider-items-${provider.value}`"
        >
          <div
            v-for="item in provider.items"
            :key="item.id"
            class="monitor-item min-w-0"
          >
            <MonitorStatusRow :item="item" />
          </div>
        </div>
      </div>
    </section>
  </div>
</template>

<script setup lang="ts">
import { computed, reactive } from 'vue'
import { useI18n } from 'vue-i18n'
import type { UserMonitorView } from '@/api/channelMonitor'
import Icon from '@/components/icons/Icon.vue'
import { monitorHealthBucket } from '@/utils/channelMonitorHealth'
import MonitorStatusRow from './MonitorStatusRow.vue'
import ProviderIcon from './ProviderIcon.vue'

type ProviderKey = 'openai' | 'anthropic' | 'gemini' | 'grok' | 'custom'

const PROVIDER_KEYS: readonly ProviderKey[] = [
  'openai',
  'anthropic',
  'gemini',
  'grok',
  'custom',
]

const props = defineProps<{
  items: UserMonitorView[]
  loading: boolean
}>()

const { t } = useI18n()

const expanded = reactive<Record<ProviderKey, boolean>>({
  openai: true,
  anthropic: true,
  gemini: true,
  grok: true,
  custom: true,
})

const providerSections = computed(() => PROVIDER_KEYS.map((value) => ({
  value,
  label: t(`channelStatus.providers.${value}`),
  items: props.items.filter((item) => String(item.provider) === value),
})))

function healthOf(item: UserMonitorView) {
  return monitorHealthBucket(item.primary_status, item.primary_latency_ms)
}

function toggleProvider(provider: ProviderKey) {
  expanded[provider] = !expanded[provider]
}
</script>

<style scoped>
.monitor-provider-card {
  display: flex;
  min-width: 0;
  flex-direction: column;
  overflow: hidden;
  border-radius: 1.5rem;
  background: var(--md-sys-color-surface);
  box-shadow: var(--theme-highlight), var(--theme-elevation-1);
}

.monitor-provider-heading {
  margin: 0;
}

.monitor-provider-toggle {
  display: flex;
  width: 100%;
  align-items: center;
  gap: 0.75rem;
  padding: 0.875rem 1rem;
  text-align: left;
  transition: background-color 160ms ease;
}

.monitor-provider-toggle:hover {
  background: #fffbe8;
}

.monitor-provider-toggle:focus-visible {
  outline: 3px solid rgb(233 184 36 / 0.45);
  outline-offset: -3px;
}

.monitor-provider-mark {
  display: inline-flex;
  width: 2.25rem;
  height: 2.25rem;
  flex-shrink: 0;
  align-items: center;
  justify-content: center;
  border-radius: 0.75rem;
  background: #fff3bd;
  color: #7e5600;
}

.monitor-provider-mark.is-openai { background: #e4f0df; color: #2e5038; }
.monitor-provider-mark.is-anthropic { background: #fce7df; color: #a44f3b; }
.monitor-provider-mark.is-gemini { background: #e5eff5; color: #456a7a; }
.monitor-provider-mark.is-grok { background: #eeeae0; color: #3f3420; }

.monitor-provider-name {
  min-width: 0;
  overflow: hidden;
  font-size: 1rem;
  font-weight: 700;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: #3f3420;
}

.monitor-provider-count {
  white-space: nowrap;
  font-size: 0.8125rem;
  font-weight: 500;
  color: #6b6045;
}

.monitor-provider-health {
  display: flex;
  max-width: 7.5rem;
  flex-wrap: wrap;
  justify-content: flex-end;
  gap: 0.25rem;
}

.monitor-provider-dot {
  width: 0.5rem;
  height: 0.5rem;
  border-radius: 999px;
  background: #cfc29a;
}

.monitor-provider-dot.is-operational { background: #4f9a66; }
.monitor-provider-dot.is-degraded { background: #e2a417; }
.monitor-provider-dot.is-down { background: #d4614a; }

.monitor-provider-chevron {
  flex-shrink: 0;
  color: #9b8c62;
  transition: transform 200ms ease;
}

.is-open .monitor-provider-chevron {
  transform: rotate(180deg);
}

.monitor-provider-body {
  flex: 1;
  padding: 0 0.375rem 0.375rem;
}

.monitor-provider-empty {
  margin: 0 0.25rem 0.375rem;
  padding: 0.875rem 1rem;
  border-radius: 1rem;
  background: #fffbe8;
  font-size: 0.8125rem;
  color: #6b6045;
}

.monitor-items > .monitor-item + .monitor-item,
.monitor-items--custom > .monitor-item + .monitor-item {
  box-shadow: inset 0 1px 0 rgb(215 193 106 / 0.28);
}

.monitor-items--custom {
  display: grid;
  grid-template-columns: minmax(0, 1fr);
}

@media (min-width: 1024px) {
  .monitor-items--custom {
    grid-template-columns: repeat(2, minmax(0, 1fr));
    column-gap: 0.75rem;
  }

  .monitor-items--custom > .monitor-item + .monitor-item {
    box-shadow: none;
  }

  .monitor-items--custom > .monitor-item:nth-child(n + 3) {
    box-shadow: inset 0 1px 0 rgb(215 193 106 / 0.28);
  }
}

@media (max-width: 639px) {
  .monitor-provider-card {
    border-radius: 1.25rem;
  }

  .monitor-provider-toggle {
    gap: 0.625rem;
    padding: 0.75rem 0.875rem;
  }

  .monitor-provider-mark {
    width: 2rem;
    height: 2rem;
    border-radius: 0.625rem;
  }

  .monitor-provider-health {
    max-width: 4.5rem;
  }
}

@media (prefers-reduced-motion: reduce) {
  .monitor-provider-chevron {
    transition: none;
  }
}
</style>
