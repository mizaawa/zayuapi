import type { MonitorStatus } from '@/api/admin/channelMonitor'
import {
  STATUS_DEGRADED,
  STATUS_ERROR,
  STATUS_FAILED,
  STATUS_OPERATIONAL,
} from '@/constants/channelMonitor'
import { firstTokenSeverity } from '@/utils/latencyHealth'

export type MonitorHealthBucket = 'operational' | 'degraded' | 'down' | 'unknown'

export type MonitorLatencyTone = 'good' | 'warn' | 'bad' | 'unknown'

/**
 * First-token latency as the status page prints it: whole milliseconds below
 * 10 s, seconds above. Empty when nothing was measured.
 */
export function formatMonitorLatency(ms: number | null | undefined): string {
  if (ms == null || !Number.isFinite(ms)) return ''
  if (ms < 10_000) return `${Math.round(ms)} ms`
  // Slow checks switch to seconds, rounded down so a check just under a
  // threshold never reads as the threshold itself.
  const seconds = Math.floor(ms / 100) / 10
  return `${Number.isInteger(seconds) ? seconds.toFixed(0) : seconds.toFixed(1)} s`
}

/** Colour a first-token latency with the same thresholds the statuses use. */
export function monitorLatencyTone(ms: number | null | undefined): MonitorLatencyTone {
  if (ms == null || !Number.isFinite(ms)) return 'unknown'
  const severity = firstTokenSeverity(ms)
  if (severity === 'good') return 'good'
  return severity === 'critical' ? 'bad' : 'warn'
}

/** Group a monitor's current state for summaries: failed and error both count as down. */
export function monitorHealthBucket(
  status: MonitorStatus | '',
  latencyMs: number | null | undefined,
): MonitorHealthBucket {
  const resolved = resolveMonitorStatus(status, latencyMs)
  if (resolved === STATUS_OPERATIONAL) return 'operational'
  if (resolved === STATUS_DEGRADED) return 'degraded'
  if (resolved === STATUS_FAILED || resolved === STATUS_ERROR) return 'down'
  return 'unknown'
}

/** Apply the current latency thresholds while preserving explicit check failures. */
export function resolveMonitorStatus(
  status: MonitorStatus | '',
  latencyMs: number | null | undefined,
): MonitorStatus | '' {
  if (!status || status === STATUS_FAILED || status === STATUS_ERROR) return status
  if (latencyMs == null || !Number.isFinite(latencyMs)) return status

  switch (firstTokenSeverity(latencyMs)) {
    case 'critical':
      return STATUS_FAILED
    case 'warn':
    case 'slow':
      return STATUS_DEGRADED
    default:
      return STATUS_OPERATIONAL
  }
}
