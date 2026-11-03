import { describe, expect, it } from 'vitest'

import {
  formatMonitorLatency,
  monitorHealthBucket,
  monitorLatencyTone,
  resolveMonitorStatus,
} from '../channelMonitorHealth'

describe('channelMonitorHealth', () => {
  it('applies the current latency thresholds to successful checks', () => {
    expect(resolveMonitorStatus('degraded', 9_999)).toBe('operational')
    expect(resolveMonitorStatus('operational', 10_000)).toBe('degraded')
    expect(resolveMonitorStatus('operational', 59_999)).toBe('degraded')
    expect(resolveMonitorStatus('operational', 60_000)).toBe('failed')
  })

  it('preserves connection and validation failures', () => {
    expect(resolveMonitorStatus('failed', 100)).toBe('failed')
    expect(resolveMonitorStatus('error', null)).toBe('error')
  })

  it('groups checks into the buckets the status summary counts', () => {
    expect(monitorHealthBucket('operational', 800)).toBe('operational')
    expect(monitorHealthBucket('operational', 10_000)).toBe('degraded')
    expect(monitorHealthBucket('operational', 60_000)).toBe('down')
    expect(monitorHealthBucket('failed', 100)).toBe('down')
    expect(monitorHealthBucket('error', null)).toBe('down')
    expect(monitorHealthBucket('', null)).toBe('unknown')
  })

  it('prints first-token latency in milliseconds below 10 s and in seconds above', () => {
    expect(formatMonitorLatency(null)).toBe('')
    expect(formatMonitorLatency(undefined)).toBe('')
    expect(formatMonitorLatency(Number.NaN)).toBe('')
    expect(formatMonitorLatency(800)).toBe('800 ms')
    expect(formatMonitorLatency(9_999)).toBe('9999 ms')
    expect(formatMonitorLatency(10_000)).toBe('10 s')
    expect(formatMonitorLatency(12_480)).toBe('12.4 s')
    expect(formatMonitorLatency(59_999)).toBe('59.9 s')
  })

  it('colours latency with the same thresholds as the statuses', () => {
    expect(monitorLatencyTone(null)).toBe('unknown')
    expect(monitorLatencyTone(9_999)).toBe('good')
    expect(monitorLatencyTone(10_000)).toBe('warn')
    expect(monitorLatencyTone(59_999)).toBe('warn')
    expect(monitorLatencyTone(60_000)).toBe('bad')
  })
})
