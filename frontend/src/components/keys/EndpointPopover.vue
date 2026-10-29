<script setup lang="ts">
import { computed, onBeforeUnmount, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useClipboard } from '@/composables/useClipboard'
import type { CustomEndpoint } from '@/types'

const props = defineProps<{
  apiBaseUrl: string
  customEndpoints: CustomEndpoint[]
}>()

const { t } = useI18n()
const { copyToClipboard } = useClipboard()
const copiedEndpoint = ref<string | null>(null)

let copiedResetTimer: number | undefined

const allEndpoints = computed(() => {
  const items: Array<{ name: string; endpoint: string; description: string; isDefault: boolean }> = []
  if (props.apiBaseUrl) {
    items.push({
      name: t('keys.endpoints.title'),
      endpoint: props.apiBaseUrl,
      description: '',
      isDefault: true,
    })
  }
  for (const ep of props.customEndpoints) {
    items.push({ ...ep, isDefault: false })
  }
  return items
})

async function copy(url: string) {
  const success = await copyToClipboard(url, t('keys.endpoints.copied'))
  if (!success) return

  copiedEndpoint.value = url
  if (copiedResetTimer !== undefined) {
    window.clearTimeout(copiedResetTimer)
  }
  copiedResetTimer = window.setTimeout(() => {
    if (copiedEndpoint.value === url) {
      copiedEndpoint.value = null
    }
  }, 1800)
}

function tooltipHint(endpoint: string): string {
  return copiedEndpoint.value === endpoint
    ? t('keys.endpoints.copiedHint')
    : t('keys.endpoints.clickToCopy')
}

function speedTestUrl(endpoint: string): string {
  return `https://www.tcptest.cn/http/${encodeURIComponent(endpoint)}`
}

onBeforeUnmount(() => {
  if (copiedResetTimer !== undefined) {
    window.clearTimeout(copiedResetTimer)
  }
})
</script>

<template>
  <div v-if="allEndpoints.length > 0" class="endpoint-list">
    <div
      v-for="(item, index) in allEndpoints"
      :key="index"
      class="endpoint-item group/endpoint"
      :class="{ 'is-copied': copiedEndpoint === item.endpoint }"
    >
      <div class="endpoint-head">
        <span class="endpoint-name">{{ item.name }}</span>
        <span v-if="item.isDefault" class="endpoint-default">{{ t('keys.endpoints.default') }}</span>
      </div>

      <!-- Phones show the whole URL, wrapped anywhere, so a long hostname can never widen the page. -->
      <code
        class="endpoint-url"
        role="button"
        tabindex="0"
        @click="copy(item.endpoint)"
        @keydown.enter.prevent="copy(item.endpoint)"
        @keydown.space.prevent="copy(item.endpoint)"
      >{{ item.endpoint }}</code>

      <div class="endpoint-actions">
        <button
          type="button"
          class="endpoint-action"
          :class="{ 'is-done': copiedEndpoint === item.endpoint }"
          :aria-label="tooltipHint(item.endpoint)"
          @click="copy(item.endpoint)"
        >
          <svg v-if="copiedEndpoint === item.endpoint" class="h-3.5 w-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.2" aria-hidden="true">
            <path stroke-linecap="round" stroke-linejoin="round" d="M5 13l4 4L19 7" />
          </svg>
          <svg v-else class="h-3.5 w-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2" aria-hidden="true">
            <path stroke-linecap="round" stroke-linejoin="round" d="M8 16H6a2 2 0 01-2-2V6a2 2 0 012-2h8a2 2 0 012 2v2m-6 12h8a2 2 0 002-2v-8a2 2 0 00-2-2h-8a2 2 0 00-2 2v8a2 2 0 002 2z" />
          </svg>
        </button>
        <a
          :href="speedTestUrl(item.endpoint)"
          target="_blank"
          rel="noopener noreferrer"
          class="endpoint-action is-speed"
          :title="t('keys.endpoints.speedTest')"
          :aria-label="t('keys.endpoints.speedTest')"
        >
          <svg class="h-3.5 w-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2" aria-hidden="true">
            <path stroke-linecap="round" stroke-linejoin="round" d="M13 10V3L4 14h7v7l9-11h-7z" />
          </svg>
        </a>
      </div>

      <p v-if="item.description" class="endpoint-note">{{ item.description }}</p>

      <!-- Desktop hover card. display:none until hovered, so it never adds scrollable width. -->
      <div class="endpoint-tip" aria-hidden="true">
        <p class="endpoint-tip-url">{{ item.endpoint }}</p>
        <p v-if="item.description" class="endpoint-tip-desc">{{ item.description }}</p>
        <p class="endpoint-tip-hint">
          <span class="endpoint-tip-dot"></span>
          {{ tooltipHint(item.endpoint) }}
        </p>
      </div>
    </div>
  </div>
</template>

<style scoped>
.endpoint-list {
  display: flex;
  width: 100%;
  min-width: 0;
  flex-direction: column;
  gap: 0.5rem;
}

.endpoint-item {
  position: relative;
  display: grid;
  min-width: 0;
  grid-template-columns: minmax(0, 1fr) auto;
  grid-template-areas:
    'head actions'
    'url url'
    'note note';
  align-items: center;
  gap: 0.25rem 0.5rem;
  padding: 0.5rem 0.5rem 0.5625rem 0.75rem;
  border-radius: 0.875rem;
  background: #fffbe8;
  transition: background-color 160ms ease;
}

.endpoint-item:hover,
.endpoint-item:focus-within {
  background: #fff3bd;
}

.endpoint-head {
  grid-area: head;
  display: flex;
  min-width: 0;
  align-items: center;
  gap: 0.375rem;
}

.endpoint-name {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 0.75rem;
  font-weight: 600;
  color: var(--md-sys-color-on-surface);
}

.endpoint-default {
  flex-shrink: 0;
  padding: 0 0.375rem;
  border-radius: 0.375rem;
  background: #fff3bd;
  font-size: 0.625rem;
  font-weight: 650;
  line-height: 1.125rem;
  color: #7e5600;
}

.endpoint-url {
  grid-area: url;
  min-width: 0;
  cursor: pointer;
  overflow-wrap: anywhere;
  word-break: break-all;
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  font-size: 0.75rem;
  line-height: 1.125rem;
  color: #5b5238;
  text-decoration-color: rgb(155 140 98 / 0.6);
  text-decoration-style: dashed;
  text-underline-offset: 3px;
}

.endpoint-url:hover,
.endpoint-url:focus-visible {
  color: #7e5600;
  text-decoration-line: underline;
  outline: none;
}

.endpoint-actions {
  grid-area: actions;
  display: flex;
  align-items: center;
  gap: 0.125rem;
}

.endpoint-action {
  display: inline-flex;
  width: 1.75rem;
  height: 1.75rem;
  align-items: center;
  justify-content: center;
  border-radius: 0.5rem;
  color: #9b8c62;
  transition: background-color 160ms ease, color 160ms ease;
}

.endpoint-action:hover,
.endpoint-action:focus-visible {
  background: rgb(255 254 248 / 0.85);
  color: #7e5600;
}

.endpoint-action.is-speed:hover {
  color: #b45309;
}

.endpoint-action.is-done {
  color: #386344;
}

.endpoint-note {
  grid-area: note;
  font-size: 0.6875rem;
  line-height: 1rem;
  color: var(--md-sys-color-on-surface-variant);
}

.endpoint-tip {
  display: none;
}

/* Desktop: one compact pill per endpoint, URL shortened with an ellipsis and shown in full on hover. */
@media (min-width: 768px) {
  .endpoint-list {
    width: auto;
    flex-direction: row;
    flex-wrap: wrap;
  }

  .endpoint-item {
    display: inline-flex;
    max-width: 100%;
    align-items: center;
    gap: 0.5rem;
    padding: 0.3125rem 0.375rem 0.3125rem 0.75rem;
    border-radius: 999px;
  }

  .endpoint-head {
    flex-shrink: 0;
  }

  .endpoint-head::after {
    content: '';
    width: 1px;
    height: 0.875rem;
    margin-left: 0.125rem;
    background: rgb(215 193 106 / 0.6);
  }

  .endpoint-url {
    max-width: 22rem;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    word-break: normal;
  }

  .endpoint-note {
    display: none;
  }

  .endpoint-tip {
    position: absolute;
    bottom: calc(100% + 0.5rem);
    left: 0;
    z-index: 20;
    width: max-content;
    max-width: min(26rem, calc(100vw - 4rem));
    padding: 0.625rem 0.75rem;
    border-radius: 0.875rem;
    background: var(--md-sys-color-surface);
    box-shadow: var(--theme-highlight), var(--theme-elevation-3);
    pointer-events: none;
  }

  .endpoint-item:hover .endpoint-tip,
  .endpoint-item:focus-within .endpoint-tip {
    display: block;
    animation: endpoint-tip-in 150ms var(--theme-ease-out);
  }

  .endpoint-tip-url {
    overflow-wrap: anywhere;
    font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
    font-size: 0.75rem;
    line-height: 1.125rem;
    color: var(--md-sys-color-on-surface);
  }

  .endpoint-tip-desc {
    margin-top: 0.25rem;
    font-size: 0.75rem;
    line-height: 1.25rem;
    color: #5b5238;
  }

  .endpoint-tip-hint {
    display: flex;
    align-items: center;
    gap: 0.375rem;
    margin-top: 0.375rem;
    font-size: 0.6875rem;
    color: #7e5600;
  }

  .endpoint-tip-dot {
    width: 0.375rem;
    height: 0.375rem;
    border-radius: 999px;
    background: #e9b824;
  }
}

@keyframes endpoint-tip-in {
  from {
    opacity: 0;
    transform: translateY(4px);
  }
}

@media (prefers-reduced-motion: reduce) {
  .endpoint-item:hover .endpoint-tip,
  .endpoint-item:focus-within .endpoint-tip {
    animation: none;
  }
}
</style>
