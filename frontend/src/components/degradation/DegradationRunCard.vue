<template>
  <article
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
        <!-- A still poster; the animation itself only plays in the detail dialog. -->
        <DegradationRunPoster
          v-if="run.status === 'success'"
          :run="run"
          :title="`${title} · ${timeLabel}`"
        />
        <div v-else class="dg-card-placeholder is-failed">
          <Icon name="exclamationTriangle" size="lg" />
          <span class="dg-placeholder-title">{{ t('degradationCheck.status.failed') }}</span>
        </div>
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
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { DegradationRun } from '@/api/degradation'
import Icon from '@/components/icons/Icon.vue'
import DegradationRunPoster from './DegradationRunPoster.vue'
import { formatDate, formatDateTime, formatReasoningEffort } from '@/utils/format'

const props = defineProps<{
  run: DegradationRun
  title: string
}>()

const emit = defineEmits<{
  (e: 'open', id: number): void
}>()

const { t } = useI18n()

const modelLabel = computed(() => props.run.response_model || props.run.model || '-')
const effortLabel = computed(() => formatReasoningEffort(props.run.reasoning_effort) === '-'
  ? (props.run.reasoning_effort || '-')
  : formatReasoningEffort(props.run.reasoning_effort))
const timestamp = computed(() => props.run.finished_at || props.run.started_at)
const timeLabel = computed(() => formatDate(timestamp.value, { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', hour12: false }))
const fullTimeLabel = computed(() => formatDateTime(timestamp.value))
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

@media (prefers-reduced-motion: reduce) {
  .dg-card,
  .dg-card:hover { transform: none; transition: none; }
}
</style>
