<template>
  <article
    ref="cardRef"
    class="dg-card"
    :class="`is-${run.status}`"
    data-testid="degradation-run-card"
  >
    <button
      type="button"
      class="dg-card-button"
      :aria-label="`${t('degradationCheck.openDetail')} · ${modelLabel} · ${timeLabel}`"
      @click="emit('open', run.id)"
    >
      <div class="dg-card-stage">
        <DegradationPreviewFrame
          v-if="run.status === 'success' && html"
          :html="html"
          :title="`${title} · ${timeLabel}`"
        />
        <div v-else class="dg-card-placeholder" :class="placeholderTone">
          <template v-if="run.status === 'running'">
            <span class="dg-spinner" aria-hidden="true"></span>
            <span class="dg-placeholder-title">{{ t('degradationCheck.status.running') }}</span>
          </template>
          <template v-else-if="run.status === 'failed'">
            <Icon name="exclamationTriangle" size="lg" />
            <span class="dg-placeholder-title">{{ t('degradationCheck.status.failed') }}</span>
            <span v-if="run.error_message" class="dg-placeholder-text">{{ run.error_message }}</span>
          </template>
          <template v-else-if="loadError">
            <Icon name="exclamationCircle" size="lg" />
            <span class="dg-placeholder-title">{{ t('degradationCheck.animationError') }}</span>
          </template>
          <template v-else-if="loaded">
            <Icon name="document" size="lg" />
            <span class="dg-placeholder-title">{{ t('degradationCheck.noAnimation') }}</span>
          </template>
          <template v-else>
            <span class="dg-spinner is-soft" aria-hidden="true"></span>
            <span class="dg-placeholder-text">{{ t('degradationCheck.loadingAnimation') }}</span>
          </template>
        </div>
        <span v-if="run.status !== 'success'" class="dg-card-status" :class="`is-${run.status}`">
          {{ t(`degradationCheck.status.${run.status}`) }}
        </span>
      </div>
      <dl class="dg-card-meta">
        <div class="dg-card-meta-row">
          <dt>{{ t('degradationCheck.card.time') }}</dt>
          <dd :title="fullTimeLabel">{{ timeLabel }}</dd>
        </div>
        <div class="dg-card-meta-row">
          <dt>{{ t('degradationCheck.card.model') }}</dt>
          <dd class="is-mono" :title="modelLabel">{{ modelLabel }}</dd>
        </div>
        <div class="dg-card-meta-row">
          <dt>{{ t('degradationCheck.card.effort') }}</dt>
          <dd><span class="dg-effort-chip">{{ effortLabel }}</span></dd>
        </div>
      </dl>
    </button>
  </article>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { DegradationRun } from '@/api/degradation'
import Icon from '@/components/icons/Icon.vue'
import DegradationPreviewFrame from './DegradationPreviewFrame.vue'
import { loadDegradationRun } from '@/composables/useDegradationRuns'
import { extractDegradationHTML } from '@/utils/degradationPreview'
import { formatDate, formatDateTime, formatReasoningEffort } from '@/utils/format'

const props = defineProps<{
  run: DegradationRun
  title: string
}>()

const emit = defineEmits<{
  (e: 'open', id: number): void
}>()

const { t } = useI18n()
const cardRef = ref<HTMLElement | null>(null)
const html = ref('')
const loaded = ref(false)
const loadError = ref(false)
let observer: IntersectionObserver | null = null
let requested = false

const modelLabel = computed(() => props.run.response_model || props.run.model || '-')
const effortLabel = computed(() => formatReasoningEffort(props.run.reasoning_effort) === '-'
  ? (props.run.reasoning_effort || '-')
  : formatReasoningEffort(props.run.reasoning_effort))
const timestamp = computed(() => props.run.finished_at || props.run.started_at)
const timeLabel = computed(() => formatDate(timestamp.value, { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', hour12: false }))
const fullTimeLabel = computed(() => formatDateTime(timestamp.value))
const placeholderTone = computed(() => ({
  'is-failed': props.run.status === 'failed' || loadError.value,
  'is-running': props.run.status === 'running',
}))

async function loadDocument() {
  if (requested || props.run.status !== 'success') return
  requested = true
  try {
    const detail = await loadDegradationRun(props.run.id)
    html.value = extractDegradationHTML(detail.output)
    loadError.value = false
  } catch {
    loadError.value = true
    requested = false
  } finally {
    loaded.value = true
  }
}

watch(() => [props.run.id, props.run.status] as const, () => {
  requested = false
  loaded.value = false
  loadError.value = false
  html.value = ''
  if (!observer) void loadDocument()
})

onMounted(() => {
  if (typeof IntersectionObserver === 'undefined' || !cardRef.value) {
    void loadDocument()
    return
  }
  observer = new IntersectionObserver((entries) => {
    if (entries.some((entry) => entry.isIntersecting)) void loadDocument()
  }, { rootMargin: '320px 0px' })
  observer.observe(cardRef.value)
})

onBeforeUnmount(() => observer?.disconnect())
</script>

<style scoped>
.dg-card {
  min-width: 0;
  border-radius: 1.25rem;
  background: var(--md-sys-color-surface);
  box-shadow: var(--theme-highlight), var(--theme-elevation-1);
  transition: transform 240ms var(--theme-ease-out), box-shadow 240ms var(--theme-ease-out);
}

.dg-card:hover {
  transform: translateY(-2px);
  box-shadow: var(--theme-highlight), var(--theme-elevation-2);
}

.dg-card-button {
  display: flex;
  width: 100%;
  flex-direction: column;
  border-radius: inherit;
  text-align: left;
}

.dg-card-button:focus-visible {
  outline: 2px solid var(--md-sys-color-primary);
  outline-offset: 2px;
}

.dg-card-stage {
  position: relative;
  overflow: hidden;
  border-radius: 1.25rem 1.25rem 0.75rem 0.75rem;
  background: #fff;
  box-shadow: inset 0 0 0 1px rgb(215 193 106 / 0.28);
  aspect-ratio: 4 / 3;
}

.dg-card-placeholder {
  position: absolute;
  inset: 0;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 0.375rem;
  padding: 0.75rem;
  background:
    radial-gradient(18rem 9rem at 50% 0%, rgb(233 184 36 / 0.12), transparent 70%),
    var(--md-sys-color-surface);
  color: var(--md-sys-color-on-surface-variant);
  text-align: center;
}

.dg-card-placeholder.is-failed {
  background: radial-gradient(18rem 9rem at 50% 0%, rgb(212 97 74 / 0.12), transparent 70%), #fffaf7;
  color: #b4513d;
}

.dg-placeholder-title {
  font-size: 0.8125rem;
  font-weight: 650;
}

.dg-placeholder-text {
  display: -webkit-box;
  max-width: 100%;
  overflow: hidden;
  font-size: 0.6875rem;
  line-height: 1.35;
  word-break: break-word;
  -webkit-line-clamp: 3;
  -webkit-box-orient: vertical;
}

.dg-card-status {
  position: absolute;
  top: 0.5rem;
  right: 0.5rem;
  padding: 0.125rem 0.5rem;
  border-radius: 999px;
  font-size: 0.6875rem;
  font-weight: 650;
  background: rgb(255 254 248 / 0.92);
  box-shadow: 0 1px 2px rgb(63 52 32 / 0.12);
}

.dg-card-status.is-failed { color: #b4513d; }
.dg-card-status.is-running { color: #7e5600; }

.dg-spinner {
  width: 1.5rem;
  height: 1.5rem;
  border: 2.5px solid rgb(233 184 36 / 0.3);
  border-top-color: var(--md-sys-color-primary);
  border-radius: 999px;
  animation: dg-spin 0.9s linear infinite;
}

.dg-spinner.is-soft {
  width: 1.125rem;
  height: 1.125rem;
}

.dg-card-meta {
  display: grid;
  gap: 0.25rem;
  padding: 0.625rem 0.75rem 0.75rem;
  font-size: 0.75rem;
}

.dg-card-meta-row {
  display: flex;
  min-width: 0;
  align-items: center;
  justify-content: space-between;
  gap: 0.5rem;
}

.dg-card-meta-row dt {
  flex-shrink: 0;
  color: var(--md-sys-color-on-surface-variant);
}

.dg-card-meta-row dd {
  min-width: 0;
  overflow: hidden;
  color: var(--md-sys-color-on-surface);
  font-weight: 600;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.dg-card-meta-row dd.is-mono {
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  font-size: 0.6875rem;
}

.dg-effort-chip {
  display: inline-flex;
  padding: 0 0.4rem;
  border-radius: 999px;
  background: var(--md-sys-color-surface-container);
  color: #7e5600;
  font-size: 0.6875rem;
}

@keyframes dg-spin {
  to { transform: rotate(360deg); }
}

@media (prefers-reduced-motion: reduce) {
  .dg-card,
  .dg-card:hover { transform: none; transition: none; }
  .dg-spinner { animation-duration: 2.4s; }
}
</style>
