import { enableAutoUnmount, mount, type VueWrapper } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { nextTick } from 'vue'

import type { MonitorTimelinePoint } from '@/api/channelMonitor'
import MonitorTimeline from '@/components/user/monitor/MonitorTimeline.vue'
import { MONITOR_TIMELINE_POINTS } from '@/constants/channelMonitor'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, params?: Record<string, unknown>) => {
        if (key === 'channelStatus.timelineLabel') return `${params?.n} recent status checks`
        if (key.startsWith('monitorCommon.status.')) return key.split('.').at(-1)
        return key
      },
    }),
  }
})

function point(
  status: MonitorTimelinePoint['status'],
  minute: number,
  latencyMs = {
    operational: 9_999,
    degraded: 10_000,
    failed: 60_000,
    error: 100,
  }[status],
): MonitorTimelinePoint {
  return {
    status,
    latency_ms: latencyMs,
    ping_latency_ms: 20,
    checked_at: `2026-09-19T00:${String(minute).padStart(2, '0')}:00Z`,
  }
}

enableAutoUnmount(afterEach)
afterEach(() => {
  vi.restoreAllMocks()
})

/** jsdom has no layout: give the bar a width so pointer positions map to segments. */
function stubBarWidth(wrapper: VueWrapper, width: number) {
  const bar = wrapper.get('[data-testid="monitor-status-bar"]')
  vi.spyOn(bar.element, 'getBoundingClientRect').mockReturnValue({
    x: 0, y: 100, left: 0, top: 100, right: width, bottom: 122, width, height: 22, toJSON: () => ({}),
  } as DOMRect)
  return bar
}

function tooltip(): HTMLElement | null {
  return document.body.querySelector('[data-testid="monitor-check-tooltip"]')
}

function tooltipTitle(): string | undefined {
  return tooltip()?.querySelector('.monitor-check-tip__status')?.textContent?.trim()
}

function tooltipRows(): Array<[string, string]> {
  return [...(tooltip()?.querySelectorAll('.monitor-check-tip__row') ?? [])].map(row => [
    row.querySelector('dt')?.textContent?.trim() ?? '',
    row.querySelector('dd')?.textContent?.trim() ?? '',
  ])
}

describe('MonitorTimeline', () => {
  it('pads to a stable length and renders each status at the same segment height', () => {
    const wrapper = mount(MonitorTimeline, {
      props: {
        length: 8,
        buckets: [
          point('operational', 4),
          point('degraded', 3),
          point('failed', 2),
          point('error', 1),
        ],
      },
    })

    const segments = wrapper.findAll('.monitor-status-bar__segment')
    expect(segments).toHaveLength(8)
    expect(segments.slice(0, 4).every(segment => segment.classes().includes('monitor-status-bar__segment--empty'))).toBe(true)
    expect(segments.slice(4).map(segment => segment.classes().find(name => name.includes('--')))).toEqual([
      'monitor-status-bar__segment--error',
      'monitor-status-bar__segment--failed',
      'monitor-status-bar__segment--degraded',
      'monitor-status-bar__segment--operational',
    ])
    expect(segments.every(segment => segment.attributes('style') === undefined)).toBe(true)
    expect(segments.every(segment => segment.attributes('title') === undefined)).toBe(true)
    expect(wrapper.get('[data-testid="monitor-status-bar"]').attributes('aria-label')).toBe(
      '4 recent status checks. operational: 1; degraded: 1; failed: 1; error: 1',
    )
  })

  it('renders an empty fixed-width timeline without history', () => {
    const wrapper = mount(MonitorTimeline, { props: { length: 6, buckets: [] } })
    const segments = wrapper.findAll('.monitor-status-bar__segment--empty')
    expect(segments).toHaveLength(6)
    expect(wrapper.get('[data-testid="monitor-status-bar"]').attributes('aria-label')).toBe('0 recent status checks')
  })

  it('recolors historical successful checks with the current latency rules', () => {
    const wrapper = mount(MonitorTimeline, {
      props: {
        length: 3,
        buckets: [
          point('operational', 3, 60_000),
          point('operational', 2, 10_000),
          point('degraded', 1, 9_999),
        ],
      },
    })

    expect(wrapper.findAll('.monitor-status-bar__segment').map(segment =>
      segment.classes().find(name => name.includes('--'))
    )).toEqual([
      'monitor-status-bar__segment--operational',
      'monitor-status-bar__segment--degraded',
      'monitor-status-bar__segment--failed',
    ])
  })

  it('shows 75 checks by default', () => {
    const wrapper = mount(MonitorTimeline, { props: { buckets: [] } })

    expect(MONITOR_TIMELINE_POINTS).toBe(75)
    expect(wrapper.findAll('.monitor-status-bar__segment')).toHaveLength(75)
  })

  it('describes the hovered check and hides when the pointer leaves', async () => {
    const wrapper = mount(MonitorTimeline, {
      props: { length: 4, buckets: [point('degraded', 4, 12_400), point('operational', 3, 800)] },
    })
    // Oldest on the left: [empty, empty, operational, degraded], 20px each.
    const bar = stubBarWidth(wrapper, 80)

    await bar.trigger('pointermove', { pointerType: 'mouse', clientX: 70 })
    expect(tooltipTitle()).toBe('degraded')
    expect(tooltip()?.querySelector('.monitor-check-tip__hint')?.textContent).toBe('channelStatus.tooltip.hint.degraded')
    const rows = tooltipRows()
    expect(rows[0][0]).toBe('channelStatus.tooltip.checkedAt')
    expect(rows[0][1]).toMatch(/\d{2}:\d{2}:\d{2}/)
    expect(rows[1]).toEqual(['channelStatus.tooltip.firstToken', '12.4 s'])
    expect(tooltip()?.querySelector('dd.is-warn')?.textContent).toBe('12.4 s')
    expect(bar.attributes('aria-describedby')).toBe(tooltip()?.id)
    expect(bar.classes()).toContain('has-active')
    expect(bar.findAll('.monitor-status-bar__segment')[3].classes()).toContain('is-active')

    await bar.trigger('pointermove', { pointerType: 'mouse', clientX: 50 })
    expect(tooltipTitle()).toBe('operational')
    expect(tooltipRows()[1]).toEqual(['channelStatus.tooltip.firstToken', '800 ms'])
    expect(tooltip()?.querySelector('.monitor-check-tip__hint')).toBeNull()

    await bar.trigger('pointermove', { pointerType: 'mouse', clientX: 5 })
    expect(tooltipTitle()).toBe('channelStatus.tooltip.noRecord')
    expect(tooltipRows()).toEqual([])

    await bar.trigger('pointerleave', { pointerType: 'mouse' })
    expect(tooltip()).toBeNull()
    expect(bar.attributes('aria-describedby')).toBeUndefined()
    expect(bar.classes()).not.toContain('has-active')
  })

  it('pins a tapped check and puts it away on a second tap or a tap elsewhere', async () => {
    const wrapper = mount(MonitorTimeline, {
      props: { length: 4, buckets: [point('failed', 4, 2_100), point('operational', 3, 800)] },
    })
    const bar = stubBarWidth(wrapper, 80)

    // A press alone shows nothing: it may be the start of a page scroll.
    await bar.trigger('pointerdown', { pointerType: 'touch', pointerId: 1, clientX: 70 })
    expect(tooltip()).toBeNull()
    await bar.trigger('pointerup', { pointerType: 'touch', pointerId: 1, clientX: 70 })
    expect(tooltipTitle()).toBe('failed')
    // A failed check has no usable first token: its time is neutral elapsed time.
    expect(tooltipRows()[1]).toEqual(['channelStatus.tooltip.elapsed', '2100 ms'])
    expect(tooltip()?.querySelector('dd.is-warn, dd.is-bad')).toBeNull()

    await bar.trigger('pointerdown', { pointerType: 'touch', pointerId: 2, clientX: 70 })
    await bar.trigger('pointerup', { pointerType: 'touch', pointerId: 2, clientX: 70 })
    expect(tooltip()).toBeNull()

    await bar.trigger('pointerdown', { pointerType: 'touch', pointerId: 3, clientX: 50 })
    await bar.trigger('pointerup', { pointerType: 'touch', pointerId: 3, clientX: 50 })
    expect(tooltipTitle()).toBe('operational')

    document.body.dispatchEvent(new Event('pointerdown', { bubbles: true }))
    await nextTick()
    expect(tooltip()).toBeNull()
  })

  it('scrubs along the bar with a finger and gives way to page scrolling', async () => {
    const wrapper = mount(MonitorTimeline, {
      props: {
        length: 4,
        buckets: [point('operational', 4), point('degraded', 3), point('error', 2), point('operational', 1)],
      },
    })
    // [operational, error, degraded, operational]
    const bar = stubBarWidth(wrapper, 80)

    await bar.trigger('pointerdown', { pointerType: 'touch', pointerId: 7, clientX: 5 })
    await bar.trigger('pointermove', { pointerType: 'touch', pointerId: 7, clientX: 8 })
    expect(tooltip()).toBeNull()
    await bar.trigger('pointermove', { pointerType: 'touch', pointerId: 7, clientX: 30 })
    expect(tooltipTitle()).toBe('error')
    expect(tooltip()?.querySelector('.monitor-check-tip__hint')?.textContent).toBe('channelStatus.tooltip.hint.error')
    await bar.trigger('pointermove', { pointerType: 'touch', pointerId: 7, clientX: 50 })
    expect(tooltipTitle()).toBe('degraded')
    await bar.trigger('pointerup', { pointerType: 'touch', pointerId: 7, clientX: 50 })
    expect(tooltipTitle()).toBe('degraded')

    await bar.trigger('pointerdown', { pointerType: 'touch', pointerId: 8, clientX: 50 })
    await bar.trigger('pointercancel', { pointerType: 'touch', pointerId: 8 })
    expect(tooltip()).toBeNull()
  })

  it('walks the recorded checks with the keyboard', async () => {
    const wrapper = mount(MonitorTimeline, {
      props: { length: 5, buckets: [point('operational', 3), point('degraded', 2), point('failed', 1)] },
    })
    // [empty, empty, failed, degraded, operational]
    const bar = wrapper.get('[data-testid="monitor-status-bar"]')
    expect(bar.attributes('tabindex')).toBe('0')

    await bar.trigger('focus')
    expect(tooltipTitle()).toBe('operational')
    await bar.trigger('keydown', { key: 'ArrowLeft' })
    expect(tooltipTitle()).toBe('degraded')
    await bar.trigger('keydown', { key: 'Home' })
    expect(tooltipTitle()).toBe('failed')
    await bar.trigger('keydown', { key: 'ArrowLeft' })
    expect(tooltipTitle()).toBe('failed')
    await bar.trigger('keydown', { key: 'End' })
    expect(tooltipTitle()).toBe('operational')
    await bar.trigger('keydown', { key: 'Escape' })
    expect(tooltip()).toBeNull()
  })

  it('explains the paused timeline during maintenance', async () => {
    const wrapper = mount(MonitorTimeline, {
      props: { length: 3, maintenance: true, buckets: [point('operational', 1)] },
    })

    await wrapper.get('[data-testid="monitor-status-bar"]').trigger('focus')
    expect(tooltipTitle()).toBe('monitorCommon.maintenancePaused')
    expect(tooltipRows()).toEqual([])
  })
})
