import { describe, expect, it } from 'vitest'

import { monitorHealthBucket, resolveMonitorStatus } from '../channelMonitorHealth'

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
})
