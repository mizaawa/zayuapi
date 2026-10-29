<template>
  <div class="key-mix" :class="`key-mix--${variant}`" data-testid="key-token-mix">
    <div class="key-mix-head">
      <span class="key-mix-label">{{ t('keys.tokenMixWindow') }}</span>
      <span
        class="key-mix-rate"
        :class="hitRateTone"
        :title="t('keys.cacheHitRateHint')"
        data-testid="key-cache-hit-rate"
      >
        {{ t('keys.cacheHitRate') }}
        <strong>{{ hitRate === null ? '—' : `${hitRate.toFixed(1)}%` }}</strong>
      </span>
    </div>

    <p class="key-mix-total">
      <span class="key-mix-total-value">{{ formatTokens(total) }}</span>
      <span v-if="total === 0" class="key-mix-empty">{{ t('keys.tokenMixEmpty') }}</span>
    </p>

    <!-- Same entity colours as the dashboard token mix; 2px surface gaps separate the parts. -->
    <div class="key-mix-bar" role="img" :aria-label="ariaLabel">
      <span
        v-for="part in visibleParts"
        :key="part.key"
        class="key-mix-segment"
        :style="{ width: part.percent + '%', background: part.color }"
      ></span>
    </div>

    <ul class="key-mix-legend">
      <li v-for="part in parts" :key="part.key" :class="{ 'is-zero': part.value === 0 }">
        <span class="key-mix-swatch" :style="{ background: part.color }" aria-hidden="true"></span>
        <span class="key-mix-name">{{ part.label }}</span>
        <span class="key-mix-value">{{ formatTokens(part.value) }}</span>
      </li>
    </ul>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { formatCompactNumber } from '@/utils/format'

export interface KeyTokenUsage {
  input_tokens?: number | null
  output_tokens?: number | null
  cache_creation_tokens?: number | null
  cache_read_tokens?: number | null
}

const props = withDefaults(defineProps<{
  usage?: KeyTokenUsage | null
  variant?: 'cell' | 'card'
}>(), {
  usage: null,
  variant: 'card'
})

const { t } = useI18n()

const TOKEN_COLORS = {
  input: '#3b82f6',
  output: '#10b981',
  cacheWrite: '#f59e0b',
  cacheRead: '#06b6d4'
} as const

const safe = (value: number | null | undefined) => {
  const numberValue = Number(value)
  return Number.isFinite(numberValue) && numberValue > 0 ? numberValue : 0
}

// Table cells use the short labels so the four-part legend fits a narrow column.
const label = (key: 'input' | 'output' | 'cacheWrite' | 'cacheRead') =>
  props.variant === 'cell' ? t(`keys.tokenParts.${key}`) : t(`dashboard.${key}`)

const parts = computed(() => {
  const usage = props.usage
  const values = [
    { key: 'input', label: label('input'), value: safe(usage?.input_tokens), color: TOKEN_COLORS.input },
    { key: 'output', label: label('output'), value: safe(usage?.output_tokens), color: TOKEN_COLORS.output },
    { key: 'cacheWrite', label: label('cacheWrite'), value: safe(usage?.cache_creation_tokens), color: TOKEN_COLORS.cacheWrite },
    { key: 'cacheRead', label: label('cacheRead'), value: safe(usage?.cache_read_tokens), color: TOKEN_COLORS.cacheRead }
  ]
  const sum = values.reduce((acc, part) => acc + part.value, 0)
  return values.map((part) => ({ ...part, percent: sum > 0 ? (part.value / sum) * 100 : 0 }))
})

const visibleParts = computed(() => parts.value.filter((part) => part.value > 0))

const total = computed(() => parts.value.reduce((acc, part) => acc + part.value, 0))

// Stored input tokens exclude cache reads and writes, so the prompt side is the sum of all three.
const hitRate = computed<number | null>(() => {
  const usage = props.usage
  const cacheRead = safe(usage?.cache_read_tokens)
  const prompt = safe(usage?.input_tokens) + safe(usage?.cache_creation_tokens) + cacheRead
  return prompt > 0 ? (cacheRead / prompt) * 100 : null
})

const hitRateTone = computed(() => {
  if (hitRate.value === null) return 'is-none'
  if (hitRate.value >= 70) return 'is-high'
  if (hitRate.value >= 20) return 'is-mid'
  return 'is-low'
})

const ariaLabel = computed(() =>
  parts.value.map((part) => `${part.label} ${formatTokens(part.value)}`).join(', ')
)

const formatTokens = (value: number) => formatCompactNumber(value)
</script>

<style scoped>
.key-mix {
  display: flex;
  min-width: 0;
  flex-direction: column;
  gap: 0.375rem;
}

.key-mix--card {
  padding: 0.625rem 0.75rem 0.6875rem;
  border-radius: 0.875rem;
  background: #fffbe8;
}

.key-mix--cell {
  width: 11.5rem;
}

.key-mix-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 0.5rem;
}

.key-mix-label {
  font-size: 0.6875rem;
  color: var(--md-sys-color-on-surface-variant);
}

.key-mix-rate {
  display: inline-flex;
  flex-shrink: 0;
  align-items: baseline;
  gap: 0.25rem;
  padding: 0.0625rem 0.5rem;
  border-radius: 999px;
  font-size: 0.6875rem;
  white-space: nowrap;
}

.key-mix-rate strong {
  font-size: 0.75rem;
  font-weight: 700;
  font-variant-numeric: tabular-nums;
}

.key-mix-rate.is-high {
  background: #e4f0df;
  color: #2e5038;
}

.key-mix-rate.is-mid {
  background: #fff3bd;
  color: #7e5600;
}

.key-mix-rate.is-low {
  background: #fdecea;
  color: #a33a2c;
}

.key-mix-rate.is-none {
  background: #faf6e8;
  color: #9b8c62;
}

.key-mix-total {
  display: flex;
  min-width: 0;
  align-items: baseline;
  gap: 0.5rem;
}

.key-mix-total-value {
  font-size: 0.9375rem;
  font-weight: 700;
  color: var(--md-sys-color-on-surface);
}

.key-mix--cell .key-mix-total-value {
  font-size: 0.875rem;
}

.key-mix-empty {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 0.6875rem;
  color: var(--md-sys-color-on-surface-variant);
}

.key-mix-bar {
  display: flex;
  height: 0.5rem;
  gap: 2px;
  overflow: hidden;
  border-radius: 999px;
  background: #eee5c9;
}

.key-mix--cell .key-mix-bar {
  height: 0.375rem;
}

.key-mix-segment {
  min-width: 2px;
  height: 100%;
}

.key-mix-legend {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 0.1875rem 0.75rem;
  font-size: 0.6875rem;
}

.key-mix--cell .key-mix-legend {
  gap: 0.125rem 0.5rem;
  font-size: 0.65625rem;
}

.key-mix--cell .key-mix-legend li {
  gap: 0.25rem;
}

.key-mix-legend li {
  display: flex;
  min-width: 0;
  align-items: center;
  gap: 0.3125rem;
  color: var(--md-sys-color-on-surface-variant);
}

.key-mix-legend li.is-zero {
  opacity: 0.55;
}

.key-mix-swatch {
  width: 0.4375rem;
  height: 0.4375rem;
  flex-shrink: 0;
  border-radius: 0.125rem;
}

.key-mix-name {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.key-mix-value {
  margin-left: auto;
  font-weight: 650;
  font-variant-numeric: tabular-nums;
  color: var(--md-sys-color-on-surface);
}

@media (min-width: 400px) {
  .key-mix--card .key-mix-legend {
    grid-template-columns: repeat(4, minmax(0, auto));
    justify-content: space-between;
  }

  .key-mix--card .key-mix-value {
    margin-left: 0.125rem;
  }
}
</style>
