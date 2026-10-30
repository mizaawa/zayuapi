<template>
  <article
    class="monitor-row"
    :data-testid="`monitor-status-row-${item.id}`"
  >
    <div class="monitor-row-head">
      <span
        role="img"
        class="monitor-row-status"
        :class="`is-${statusTone}`"
        :aria-label="statusLabel(currentStatus)"
        :title="statusLabel(currentStatus)"
      >
        <Icon :name="statusIcon" size="xs" :stroke-width="2.5" />
      </span>
      <div class="monitor-row-title">
        <h3 class="monitor-row-name">{{ item.name }}</h3>
        <span
          v-if="groupRateLabel"
          class="monitor-row-rate"
          data-testid="monitor-status-group-rate"
        >
          {{ groupRateLabel }}
        </span>
      </div>
      <div class="monitor-row-figures">
        <span
          v-if="latencyLabel"
          class="monitor-row-latency"
          :class="`is-${latencyTone}`"
          :title="t('channelStatus.latencyTitle')"
          data-testid="monitor-status-latency"
        >
          <Icon name="bolt" size="xs" />
          {{ latencyLabel }}
        </span>
        <span class="monitor-row-uptime" :class="`is-${uptimeTone}`">{{ uptimeLabel }}</span>
      </div>
    </div>

    <MonitorTimeline
      class="monitor-row-timeline"
      :buckets="item.timeline"
      :length="MONITOR_TIMELINE_POINTS"
    />
  </article>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { UserMonitorView } from '@/api/channelMonitor'
import Icon from '@/components/icons/Icon.vue'
import { useChannelMonitorFormat } from '@/composables/useChannelMonitorFormat'
import { MONITOR_TIMELINE_POINTS } from '@/constants/channelMonitor'
import { formatMultiplier } from '@/utils/formatters'
import { monitorHealthBucket, resolveMonitorStatus } from '@/utils/channelMonitorHealth'
import { firstTokenSeverity } from '@/utils/latencyHealth'
import MonitorTimeline from './MonitorTimeline.vue'

const props = defineProps<{
  item: UserMonitorView
}>()

const { t } = useI18n()
const { statusLabel } = useChannelMonitorFormat()

const groupRateLabel = computed(() => {
  const rate = props.item.group_rate_multiplier
  if (rate == null || !Number.isFinite(rate)) return ''
  return `${formatMultiplier(rate)}x`
})

const currentStatus = computed(() =>
  resolveMonitorStatus(props.item.primary_status, props.item.primary_latency_ms)
)

const statusTone = computed(() => monitorHealthBucket(props.item.primary_status, props.item.primary_latency_ms))

const statusIcon = computed<'check' | 'exclamationTriangle' | 'x' | 'infoCircle'>(() => {
  switch (currentStatus.value) {
    case 'operational':
      return 'check'
    case 'degraded':
      return 'exclamationTriangle'
    case 'failed':
    case 'error':
      return 'x'
    default:
      return 'infoCircle'
  }
})

const latencyLabel = computed(() => {
  const ms = props.item.primary_latency_ms
  if (ms == null || !Number.isFinite(ms)) return ''
  if (ms < 1000) return `${Math.round(ms)} ms`
  // Round down so a check just under a threshold never reads as the threshold itself.
  if (ms < 10_000) return `${(Math.floor(ms / 100) / 10).toFixed(1)} s`
  return `${Math.floor(ms / 1000)} s`
})

const latencyTone = computed(() => {
  const ms = props.item.primary_latency_ms
  if (ms == null || !Number.isFinite(ms)) return 'unknown'
  const severity = firstTokenSeverity(ms)
  return severity === 'good' ? 'good' : severity === 'critical' ? 'bad' : 'warn'
})

const hasHistory = computed(() =>
  Boolean(props.item.primary_status) || (props.item.timeline?.length ?? 0) > 0
)

const uptimeValue = computed(() => {
  if (!hasHistory.value || !Number.isFinite(props.item.availability_7d)) return null
  return Math.max(0, Math.min(100, props.item.availability_7d))
})

const uptimeLabel = computed(() => {
  if (uptimeValue.value === null) return t('channelStatus.uptimeUnavailable')
  const formatted = new Intl.NumberFormat(undefined, {
    minimumFractionDigits: 0,
    maximumFractionDigits: 2,
  }).format(uptimeValue.value)
  return t('channelStatus.uptime', { value: formatted })
})

const uptimeTone = computed(() => {
  const value = uptimeValue.value
  if (value === null) return 'unknown'
  if (value >= 99) return 'good'
  if (value >= 95) return 'warn'
  return 'bad'
})
</script>

<style scoped>
.monitor-row {
  padding: 0.875rem 0.75rem 1rem;
}

.monitor-row-head {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr) auto;
  align-items: start;
  gap: 0.625rem;
}

.monitor-row-status {
  display: inline-grid;
  width: 1.375rem;
  height: 1.375rem;
  margin-top: 0.0625rem;
  place-items: center;
  border-radius: 999px;
  color: #fffef8;
}

.monitor-row-status.is-operational { background: #4f9a66; }
.monitor-row-status.is-degraded { background: #e2a417; }
.monitor-row-status.is-down { background: #d4614a; }
.monitor-row-status.is-unknown { background: #b9ab80; }

.monitor-row-title {
  display: flex;
  min-width: 0;
  flex-wrap: wrap;
  align-items: center;
  gap: 0.25rem 0.5rem;
}

.monitor-row-name {
  min-width: 0;
  font-size: 0.9375rem;
  font-weight: 650;
  line-height: 1.375rem;
  overflow-wrap: anywhere;
  color: #3f3420;
}

.monitor-row-rate {
  display: inline-flex;
  flex-shrink: 0;
  align-items: center;
  padding: 0 0.4375rem;
  border-radius: 999px;
  background: #fff3bd;
  font-size: 0.6875rem;
  font-weight: 700;
  line-height: 1.25rem;
  font-variant-numeric: tabular-nums;
  color: #7e5600;
}

.monitor-row-figures {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: flex-end;
  gap: 0.25rem 0.75rem;
  line-height: 1.375rem;
}

.monitor-row-latency {
  display: inline-flex;
  align-items: center;
  gap: 0.25rem;
  white-space: nowrap;
  font-size: 0.75rem;
  font-weight: 600;
  font-variant-numeric: tabular-nums;
  color: #6b6045;
}

.monitor-row-latency.is-warn { color: #9a6a00; }
.monitor-row-latency.is-bad { color: #b4472f; }

.monitor-row-uptime {
  white-space: nowrap;
  font-size: 0.875rem;
  font-weight: 700;
  font-variant-numeric: tabular-nums;
}

.monitor-row-uptime.is-good { color: #386344; }
.monitor-row-uptime.is-warn { color: #9a6a00; }
.monitor-row-uptime.is-bad { color: #b4472f; }
.monitor-row-uptime.is-unknown { font-weight: 600; color: #9b8c62; }

.monitor-row-timeline {
  margin-top: 0.75rem;
}

@media (max-width: 639px) {
  .monitor-row {
    padding: 0.75rem 0.625rem 0.875rem;
  }

  .monitor-row-head {
    grid-template-columns: auto minmax(0, 1fr);
  }

  /* Figures drop under the name so long channel names keep their width. */
  .monitor-row-figures {
    grid-column: 2;
    justify-content: flex-start;
  }

  .monitor-row-name {
    font-size: 0.875rem;
  }
}
</style>
