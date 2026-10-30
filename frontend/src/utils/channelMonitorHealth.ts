import type { MonitorStatus } from '@/api/admin/channelMonitor'
import {
  STATUS_DEGRADED,
  STATUS_ERROR,
  STATUS_FAILED,
  STATUS_OPERATIONAL,
} from '@/constants/channelMonitor'
import { firstTokenSeverity } from '@/utils/latencyHealth'

export type MonitorHealthBucket = 'operational' | 'degraded' | 'down' | 'unknown'

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
