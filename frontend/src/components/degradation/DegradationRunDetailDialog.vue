<template>
  <BaseDialog :show="runId !== null" :title="dialogTitle" width="full" @close="emit('close')">
    <div v-if="loading && !run" class="dg-detail-loading">
      <span class="dg-detail-spinner" aria-hidden="true"></span>
      <span>{{ t('degradationCheck.loading') }}</span>
    </div>
    <div v-else-if="error && !run" class="dg-detail-error" role="alert">{{ error }}</div>
    <div v-else-if="run" class="dg-detail" data-testid="degradation-run-detail">
      <section class="dg-detail-stage-wrap" :aria-label="t('degradationCheck.detail.animation')">
        <div class="dg-detail-stage">
          <DegradationPreviewFrame
            v-if="html"
            ref="frameRef"
            :html="html"
            :title="dialogTitle"
            interactive
            :lazy="false"
          />
          <div v-else class="dg-detail-empty">
            <template v-if="run.status === 'running'">
              <span class="dg-detail-spinner" aria-hidden="true"></span>
              <strong>{{ t('degradationCheck.status.running') }}</strong>
            </template>
            <template v-else-if="run.status === 'failed'">
              <Icon name="exclamationTriangle" size="xl" />
              <strong>{{ t('degradationCheck.status.failed') }}</strong>
            </template>
            <template v-else>
              <Icon name="document" size="xl" />
              <strong>{{ t('degradationCheck.noAnimation') }}</strong>
              <span>{{ t('degradationCheck.noAnimationHint') }}</span>
            </template>
          </div>
        </div>
        <div class="dg-detail-toolbar">
          <span class="dg-detail-sandbox">
            <Icon name="shield" size="sm" />
            {{ t('degradationCheck.detail.sandboxHint') }}
          </span>
          <div class="dg-detail-actions">
            <button type="button" class="btn btn-secondary btn-sm" :disabled="!html" @click="replay">
              <Icon name="refresh" size="sm" />
              {{ t('degradationCheck.detail.replay') }}
            </button>
            <button type="button" class="btn btn-primary btn-sm" :disabled="!codeText" data-testid="degradation-copy-code" @click="copyCode">
              <Icon name="copy" size="sm" />
              {{ t('degradationCheck.detail.copyCode') }}
            </button>
          </div>
        </div>
      </section>

      <aside class="dg-detail-info">
        <span class="dg-detail-status" :class="`is-${run.status}`">
          {{ t(`degradationCheck.status.${run.status}`) }}
        </span>
        <dl class="dg-detail-grid">
          <div class="dg-detail-item">
            <dt>{{ t('degradationCheck.detail.generatedAt') }}</dt>
            <dd>{{ generatedAt }}</dd>
          </div>
          <div class="dg-detail-item">
            <dt>{{ t('degradationCheck.detail.model') }}</dt>
            <dd class="is-mono">{{ run.model || '-' }}</dd>
          </div>
          <div v-if="run.response_model && run.response_model !== run.model" class="dg-detail-item">
            <dt>{{ t('degradationCheck.detail.responseModel') }}</dt>
            <dd class="is-mono">{{ run.response_model }}</dd>
          </div>
          <div class="dg-detail-item">
            <dt>{{ t('degradationCheck.detail.effort') }}</dt>
            <dd>{{ effortLabel }}</dd>
          </div>
          <div class="dg-detail-item">
            <dt>{{ t('degradationCheck.detail.firstToken') }}</dt>
            <dd class="is-num">{{ formatDegradationDuration(run.first_token_ms) }}</dd>
          </div>
          <div class="dg-detail-item">
            <dt>{{ t('degradationCheck.detail.duration') }}</dt>
            <dd class="is-num">{{ formatDegradationDuration(run.duration_ms) }}</dd>
          </div>
          <div class="dg-detail-item">
            <dt>{{ t('degradationCheck.detail.protocol') }}</dt>
            <dd>{{ protocolLabel }}</dd>
          </div>
          <div class="dg-detail-item">
            <dt>{{ t('degradationCheck.detail.tokens') }}</dt>
            <dd class="is-num">
              {{ formatNumberLocaleString(run.total_tokens) }}
              <small>{{ tokenBreakdown }}</small>
            </dd>
          </div>
          <div class="dg-detail-item">
            <dt>{{ t('degradationCheck.detail.cacheRate') }}</dt>
            <dd class="is-num">{{ cacheRateLabel }}</dd>
          </div>
          <div class="dg-detail-item">
            <dt>{{ t('degradationCheck.detail.cost') }}</dt>
            <dd class="is-num">{{ costLabel }}</dd>
          </div>
          <div v-if="run.attempts > 1" class="dg-detail-item">
            <dt>{{ t('degradationCheck.detail.attempts') }}</dt>
            <dd class="is-num">{{ run.attempts }}</dd>
          </div>
        </dl>

        <div v-if="run.status === 'failed' && run.error_message" class="dg-detail-block is-error">
          <h4>{{ t('degradationCheck.detail.error') }}</h4>
          <p>{{ run.error_message }}</p>
        </div>

        <div class="dg-detail-block">
          <h4>
            {{ t('degradationCheck.detail.prompt') }}
            <span v-if="run.prompt_name" class="dg-detail-preset">{{ run.prompt_name }}</span>
          </h4>
          <p class="dg-detail-prompt">{{ run.prompt || '-' }}</p>
        </div>

        <div v-if="codeText" class="dg-detail-block">
          <button type="button" class="dg-detail-code-toggle" :aria-expanded="showCode" @click="showCode = !showCode">
            <span>{{ t('degradationCheck.detail.code') }}</span>
            <span class="dg-detail-code-hint">{{ showCode ? t('degradationCheck.detail.hideCode') : t('degradationCheck.detail.showCode') }}</span>
          </button>
          <pre v-if="showCode" class="dg-detail-code" tabindex="0"><code>{{ codeText }}</code></pre>
        </div>
      </aside>
    </div>

    <template #footer>
      <div class="flex justify-end">
        <button type="button" class="btn btn-secondary" data-testid="degradation-detail-close" @click="emit('close')">
          {{ t('degradationCheck.detail.close') }}
        </button>
      </div>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { DegradationRun } from '@/api/degradation'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import DegradationPreviewFrame from './DegradationPreviewFrame.vue'
import { loadDegradationRun } from '@/composables/useDegradationRuns'
import { useClipboard } from '@/composables/useClipboard'
import { extractDegradationHTML, formatDegradationDuration } from '@/utils/degradationPreview'
import { extractApiErrorMessage } from '@/utils/apiError'
import { formatCurrency, formatDateTime, formatNumberLocaleString } from '@/utils/format'

const props = defineProps<{
  runId: number | null
  title: string
}>()

const emit = defineEmits<{
  (e: 'close'): void
}>()

const { t, te } = useI18n()
const { copyToClipboard } = useClipboard()
const run = ref<DegradationRun | null>(null)
const loading = ref(false)
const error = ref('')
const showCode = ref(false)
const frameRef = ref<InstanceType<typeof DegradationPreviewFrame> | null>(null)
let generation = 0

const html = computed(() => (run.value?.status === 'success' ? extractDegradationHTML(run.value.output) : ''))
const codeText = computed(() => html.value || run.value?.output || '')
const dialogTitle = computed(() => (props.title ? `${t('degradationCheck.detail.title')} · ${props.title}` : t('degradationCheck.detail.title')))
const generatedAt = computed(() => formatDateTime(run.value?.finished_at || run.value?.started_at) || '-')
const effortLabel = computed(() => {
  const effort = run.value?.reasoning_effort
  if (!effort) return t('degradationCheck.efforts.default')
  const key = `degradationCheck.efforts.${effort}`
  return te(key) ? t(key) : effort
})
const protocolLabel = computed(() => {
  const protocol = run.value?.protocol
  if (!protocol) return '-'
  const key = `degradationCheck.protocols.${protocol}`
  return te(key) ? t(key) : protocol
})
const tokenBreakdown = computed(() => t('degradationCheck.detail.tokenBreakdown', {
  input: formatNumberLocaleString(run.value?.input_tokens ?? 0),
  output: formatNumberLocaleString(run.value?.output_tokens ?? 0),
  cache: formatNumberLocaleString((run.value?.cache_read_tokens ?? 0) + (run.value?.cache_creation_tokens ?? 0)),
}))
const cacheRateLabel = computed(() => {
  const rate = run.value?.cache_rate
  return rate === null || rate === undefined ? '-' : `${(rate * 100).toFixed(1)}%`
})
const costLabel = computed(() => {
  const cost = run.value?.cost
  return cost === null || cost === undefined ? '-' : formatCurrency(cost)
})

async function load(id: number) {
  const current = ++generation
  loading.value = true
  error.value = ''
  try {
    const detail = await loadDegradationRun(id)
    if (current !== generation) return
    run.value = detail
  } catch (err: unknown) {
    if (current !== generation) return
    error.value = extractApiErrorMessage(err, t('degradationCheck.detail.loadFailed'))
  } finally {
    if (current === generation) loading.value = false
  }
}

function replay() {
  frameRef.value?.replay()
}

async function copyCode() {
  if (!codeText.value) return
  await copyToClipboard(codeText.value, t('degradationCheck.detail.copied'))
}

watch(() => props.runId, (id) => {
  run.value = null
  showCode.value = false
  if (id !== null) void load(id)
  else generation += 1
}, { immediate: true })
</script>

<style scoped>
.dg-detail {
  display: grid;
  gap: 1.25rem;
}

@media (min-width: 1024px) {
  .dg-detail {
    grid-template-columns: minmax(0, 1.65fr) minmax(18rem, 1fr);
    align-items: start;
  }
}

.dg-detail-stage {
  position: relative;
  overflow: hidden;
  border-radius: 1.25rem;
  background: #fff;
  box-shadow: inset 0 0 0 1px rgb(215 193 106 / 0.35), var(--theme-elevation-1);
}

.dg-detail-empty {
  display: flex;
  aspect-ratio: 4 / 3;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 0.5rem;
  padding: 1rem;
  color: var(--md-sys-color-on-surface-variant);
  text-align: center;
  background: radial-gradient(28rem 14rem at 50% 0%, rgb(233 184 36 / 0.14), transparent 70%), var(--md-sys-color-surface);
}

.dg-detail-toolbar {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 0.75rem;
  margin-top: 0.75rem;
}

.dg-detail-sandbox {
  display: inline-flex;
  align-items: center;
  gap: 0.375rem;
  font-size: 0.75rem;
  color: var(--md-sys-color-on-surface-variant);
}

.dg-detail-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 0.5rem;
}

.dg-detail-info {
  display: flex;
  min-width: 0;
  flex-direction: column;
  gap: 1rem;
}

.dg-detail-status {
  align-self: flex-start;
  padding: 0.1875rem 0.75rem;
  border-radius: 999px;
  font-size: 0.75rem;
  font-weight: 700;
  background: #eef7ea;
  color: #2e5038;
}

.dg-detail-status.is-failed { background: #fdf1ec; color: #7f3424; }
.dg-detail-status.is-running { background: #fff3bd; color: #65460b; }

.dg-detail-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 0.5rem;
}

.dg-detail-item {
  min-width: 0;
  padding: 0.625rem 0.75rem;
  border-radius: 0.875rem;
  background: var(--md-sys-color-surface-container);
}

.dg-detail-item dt {
  font-size: 0.6875rem;
  color: var(--md-sys-color-on-surface-variant);
}

.dg-detail-item dd {
  margin-top: 0.125rem;
  overflow-wrap: anywhere;
  font-size: 0.875rem;
  font-weight: 650;
  color: var(--md-sys-color-on-surface);
}

.dg-detail-item dd small {
  display: block;
  margin-top: 0.125rem;
  font-size: 0.6875rem;
  font-weight: 500;
  color: var(--md-sys-color-on-surface-variant);
}

.dg-detail-item dd.is-mono {
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  font-size: 0.8125rem;
}

.dg-detail-item dd.is-num { font-variant-numeric: tabular-nums; }

.dg-detail-block h4 {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 0.5rem;
  margin-bottom: 0.375rem;
  font-size: 0.8125rem;
  font-weight: 700;
  color: var(--md-sys-color-on-surface);
}

.dg-detail-block.is-error p {
  padding: 0.625rem 0.75rem;
  border-radius: 0.875rem;
  background: #fdf1ec;
  color: #7f3424;
  font-size: 0.8125rem;
  overflow-wrap: anywhere;
}

.dg-detail-preset {
  padding: 0 0.5rem;
  border-radius: 999px;
  background: var(--md-sys-color-surface-container-high);
  font-size: 0.6875rem;
  font-weight: 600;
  color: #7e5600;
}

.dg-detail-prompt {
  max-height: 10rem;
  overflow-y: auto;
  padding: 0.75rem;
  border-radius: 0.875rem;
  background: var(--md-sys-color-surface-container);
  font-size: 0.8125rem;
  line-height: 1.6;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}

.dg-detail-code-toggle {
  display: flex;
  width: 100%;
  align-items: center;
  justify-content: space-between;
  gap: 0.5rem;
  padding: 0.5rem 0.75rem;
  border-radius: 0.875rem;
  background: var(--md-sys-color-surface-container);
  font-size: 0.8125rem;
  font-weight: 700;
}

.dg-detail-code-hint {
  font-size: 0.75rem;
  font-weight: 500;
  color: var(--md-sys-color-on-surface-variant);
}

.dg-detail-code {
  max-height: 18rem;
  margin-top: 0.5rem;
  overflow: auto;
  padding: 0.75rem;
  border-radius: 0.875rem;
  background: #2b2416;
  color: #fdf6dc;
  font-size: 0.75rem;
  line-height: 1.5;
  white-space: pre;
}

.dg-detail-loading,
.dg-detail-error {
  display: flex;
  min-height: 12rem;
  align-items: center;
  justify-content: center;
  gap: 0.75rem;
  color: var(--md-sys-color-on-surface-variant);
}

.dg-detail-error { color: #7f3424; }

.dg-detail-spinner {
  width: 1.5rem;
  height: 1.5rem;
  border: 2.5px solid rgb(233 184 36 / 0.3);
  border-top-color: var(--md-sys-color-primary);
  border-radius: 999px;
  animation: dg-detail-spin 0.9s linear infinite;
}

@keyframes dg-detail-spin {
  to { transform: rotate(360deg); }
}

@media (max-width: 639px) {
  .dg-detail-grid { grid-template-columns: minmax(0, 1fr); }
  .dg-detail-actions { width: 100%; }
  .dg-detail-actions .btn { flex: 1 1 auto; }
}
</style>
