import { defineComponent } from 'vue'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import type { UserMonitorView } from '@/api/channelMonitor'
import ChannelStatusView from '@/views/user/ChannelStatusView.vue'

const {
  listMonitors,
  showError,
  setAutoRefreshEnabled,
  stopAutoRefresh,
  startAutoRefresh,
  resetCountdown,
  publicSettings,
} = vi.hoisted(() => ({
  listMonitors: vi.fn(),
  showError: vi.fn(),
  setAutoRefreshEnabled: vi.fn(),
  stopAutoRefresh: vi.fn(),
  startAutoRefresh: vi.fn(),
  resetCountdown: vi.fn(),
  publicSettings: {
    channel_monitor_enabled: true,
    channel_monitor_announcement: 'Scheduled maintenance tonight.\nBrief interruptions may occur.',
  },
}))

vi.mock('@/api/channelMonitor', () => ({
  list: listMonitors,
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    cachedPublicSettings: publicSettings,
    siteName: 'Fallback Site',
    showError,
  }),
}))

vi.mock('@/composables/useAutoRefresh', async () => {
  const { ref } = await import('vue')
  return {
    useAutoRefresh: () => ({
      enabled: ref(false),
      intervalSeconds: ref(60),
      countdown: ref(60),
      fetching: ref(false),
      intervals: [30, 60, 120],
      setEnabled: setAutoRefreshEnabled,
      setInterval: vi.fn(),
      resetCountdown,
      start: startAutoRefresh,
      stop: stopAutoRefresh,
    }),
  }
})

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  const translations: Record<string, string> = {
    'channelStatus.title': 'Channel Status',
    'channelStatus.poweredBy': 'Powered by',
    'channelStatus.announcementTitle': 'Channel status announcement',
    'channelStatus.loadError': 'Failed to load channel status',
    'channelStatus.summary.operational': 'Channels are operational',
    'channelStatus.summary.degraded': 'Some channels are slow',
    'channelStatus.summary.outage': 'Some channels are failing',
    'channelStatus.summary.empty': 'No channels are monitored yet',
    'channelStatus.counts.operational': 'Operational',
    'channelStatus.counts.degraded': 'Degraded',
    'channelStatus.counts.down': 'Down',
    'channelStatus.counts.unknown': 'No data',
    'common.refresh': 'Refresh',
    'channelStatus.emptyProvider': 'No monitored channels configured',
    'channelStatus.uptimeUnavailable': '-- uptime',
    'channelStatus.providers.openai': 'OpenAI',
    'channelStatus.providers.anthropic': 'Anthropic',
    'channelStatus.providers.gemini': 'Gemini',
    'channelStatus.providers.grok': 'Grok',
    'channelStatus.providers.custom': 'Custom',
    'monitorCommon.status.operational': 'Operational',
    'monitorCommon.status.degraded': 'Degraded',
    'monitorCommon.status.failed': 'Failed',
    'monitorCommon.status.error': 'Error',
    'monitorCommon.status.unknown': 'Unknown',
  }
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, params?: Record<string, unknown>) => {
        if (key === 'channelStatus.componentCount') return `${params?.n} components`
        if (key === 'channelStatus.loadingProvider') return `Loading ${params?.provider}`
        if (key === 'channelStatus.uptime') return `${params?.value}% uptime`
        if (key === 'channelStatus.timelineLabel') return `${params?.n} recent status checks`
        return translations[key] ?? key
      },
    }),
  }
})

const AppLayoutStub = defineComponent({
  template: '<main><slot /></main>',
})

function makeMonitor(overrides: Partial<UserMonitorView> = {}): UserMonitorView {
  return {
    id: 1,
    sort_order: 0,
    name: 'Chat Completions',
    provider: 'openai',
    group_name: '',
    group_rate_multiplier: null,
    primary_model: 'gpt-4o-mini',
    primary_status: 'operational',
    primary_latency_ms: 120,
    primary_ping_latency_ms: 20,
    availability_7d: 100,
    extra_models: [],
    timeline: [
      {
        status: 'operational',
        latency_ms: 120,
        ping_latency_ms: 20,
        checked_at: '2026-09-19T00:00:00Z',
      },
    ],
    ...overrides,
  }
}

function mountView(): VueWrapper {
  return mount(ChannelStatusView, {
    global: {
      stubs: { AppLayout: AppLayoutStub },
    },
  })
}

describe('ChannelStatusView', () => {
  beforeEach(() => {
    for (const mock of [
      listMonitors,
      showError,
      setAutoRefreshEnabled,
      stopAutoRefresh,
      startAutoRefresh,
      resetCountdown,
    ]) {
      mock.mockReset()
    }
    publicSettings.channel_monitor_announcement =
      'Scheduled maintenance tonight.\nBrief interruptions may occur.'
  })

  it('groups monitors into five fixed provider sections and renders the status-page contract', async () => {
    listMonitors.mockResolvedValue({
      items: [
        makeMonitor({ id: 5, name: 'Custom Gateway', provider: 'custom' as UserMonitorView['provider'], availability_7d: 99.95 }),
        makeMonitor({ id: 6, name: 'Custom Backup', provider: 'custom' as UserMonitorView['provider'] }),
        makeMonitor({ id: 7, name: 'Custom Third', provider: 'custom' as UserMonitorView['provider'] }),
        makeMonitor({ id: 2, name: 'Responses', provider: 'openai', availability_7d: 99.5 }),
        makeMonitor({ id: 4, name: 'Grok Chat', provider: 'grok' }),
        makeMonitor({ id: 1, name: 'Chat Completions', provider: 'openai', group_name: 'OpenAI Default', group_rate_multiplier: 0.1 }),
      ],
    })

    const wrapper = mountView()
    await flushPromises()

    const sections = wrapper.findAll('section[data-testid^="monitor-provider-"]')
    expect(sections).toHaveLength(5)
    expect(sections.map(section => section.attributes('data-testid'))).toEqual([
      'monitor-provider-openai',
      'monitor-provider-anthropic',
      'monitor-provider-gemini',
      'monitor-provider-grok',
      'monitor-provider-custom',
    ])
    expect(sections.map(section => section.find('button').text())).toEqual([
      'OpenAI2 components',
      'Anthropic0 components',
      'Gemini0 components',
      'Grok1 components',
      'Custom3 components',
    ])

    const providerGrid = wrapper.get('[data-testid="monitor-provider-grid"]')
    expect(providerGrid.classes()).toContain('lg:grid-cols-2')
    expect(wrapper.get('[data-testid="monitor-provider-custom"]').classes()).toContain('lg:col-span-2')
    expect(wrapper.get('[data-testid="monitor-provider-openai"]').classes()).toEqual(expect.arrayContaining([
      'monitor-provider-card',
      'is-open',
    ]))
    expect(wrapper.get('[data-testid="monitor-provider-toggle-openai"]').element.parentElement?.tagName).toBe('H2')

    const openAISection = wrapper.get('[data-testid="monitor-provider-openai"]')
    expect(openAISection.text()).toContain('Chat Completions')
    expect(openAISection.text()).toContain('Responses')
    expect(openAISection.text()).not.toContain('Custom Gateway')
    expect(openAISection.findAll('[data-testid^="monitor-status-row-"]').map(row => row.text()))
      .toEqual([
        expect.stringContaining('Responses'),
        expect.stringContaining('Chat Completions'),
      ])
    const customItems = wrapper.get('[data-testid="monitor-provider-items-custom"]')
    expect(customItems.classes()).toContain('monitor-items--custom')
    expect(customItems.findAll('.monitor-item').map(item => item.text())).toEqual([
      expect.stringContaining('Custom Gateway'),
      expect.stringContaining('Custom Backup'),
      expect.stringContaining('Custom Third'),
    ])
    expect(wrapper.get('[data-testid="monitor-status-row-1"]').text()).toContain('100% uptime')
    const groupedMonitor = wrapper.get('[data-testid="monitor-status-row-1"]')
    expect(groupedMonitor.find('[data-testid="monitor-status-group-name"]').exists()).toBe(false)
    expect(groupedMonitor.get('[data-testid="monitor-status-group-rate"]').text()).toBe('0.10x')
    expect(groupedMonitor.text()).not.toContain('OpenAI Default')
    expect(wrapper.get('[data-testid="monitor-status-row-5"]').text()).toContain('99.95% uptime')

    expect(wrapper.text()).not.toContain('View history')
    expect(wrapper.text()).not.toContain('7 days')
    expect(wrapper.text()).not.toContain('System status')
    const notice = wrapper.get('[data-testid="channel-status-notice"]')
    expect(notice.get('h2').text()).toBe('Channel status announcement')
    expect(notice.text()).toContain('Scheduled maintenance tonight.')
    expect(notice.text()).toContain('Brief interruptions may occur.')
    expect(notice.classes()).toContain('cs-notice')
    expect(wrapper.get('[data-testid="channel-status-grid"]').text()).not.toContain(
      'Scheduled maintenance tonight.'
    )
    expect(wrapper.get('[data-testid="monitor-status-row-1"]').findAll('.monitor-status-bar__segment')).toHaveLength(120)
    const poweredBy = wrapper.get('a[href="https://mizaawa.com"]')
    expect(poweredBy.text()).toBe('mizaawa.com')
    expect(poweredBy.attributes('target')).toBe('_blank')
    expect(poweredBy.attributes('rel')).toBe('noopener noreferrer')

    expect(listMonitors).toHaveBeenCalledTimes(1)
    expect(listMonitors).toHaveBeenCalledWith({ signal: expect.any(AbortSignal) })
    expect(setAutoRefreshEnabled).toHaveBeenCalledWith(true)
    wrapper.unmount()
  })

  it('hides the announcement panel when the configured content is blank', async () => {
    publicSettings.channel_monitor_announcement = '  \n  '
    listMonitors.mockResolvedValue({ items: [] })

    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.find('[data-testid="channel-status-notice"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('starts expanded and collapses only the selected provider', async () => {
    listMonitors.mockResolvedValue({ items: [makeMonitor()] })
    const wrapper = mountView()
    await flushPromises()

    const openAIToggle = wrapper.get('[data-testid="monitor-provider-toggle-openai"]')
    const openAIContent = wrapper.get('#monitor-provider-content-openai')
    const customContent = wrapper.get('#monitor-provider-content-custom')
    expect(openAIToggle.attributes('aria-expanded')).toBe('true')
    expect(openAIContent.isVisible()).toBe(true)
    expect(customContent.isVisible()).toBe(true)

    await openAIToggle.trigger('click')
    expect(openAIToggle.attributes('aria-expanded')).toBe('false')
    expect(wrapper.get('#monitor-provider-content-openai').attributes('style')).toContain('display: none')
    expect(customContent.isVisible()).toBe(true)

    await openAIToggle.trigger('click')
    expect(openAIToggle.attributes('aria-expanded')).toBe('true')
    expect(wrapper.get('#monitor-provider-content-openai').attributes('style') || '').not.toContain('display: none')
    wrapper.unmount()
  })

  it('keeps the leading status icon in sync with the current latency rules', async () => {
    listMonitors.mockResolvedValue({
      items: [
        makeMonitor({ id: 1, name: 'Fast', primary_status: 'degraded', primary_latency_ms: 9_999 }),
        makeMonitor({ id: 2, name: 'Slow', primary_status: 'operational', primary_latency_ms: 10_000 }),
        makeMonitor({ id: 3, name: 'Too Slow', primary_status: 'operational', primary_latency_ms: 60_000 }),
        makeMonitor({ id: 4, name: 'Disconnected', primary_status: 'error', primary_latency_ms: null }),
      ],
    })

    const wrapper = mountView()
    await flushPromises()

    const statusIcon = (id: number) =>
      wrapper.get(`[data-testid="monitor-status-row-${id}"] [role="img"]`)

    expect(statusIcon(1).classes()).toContain('is-operational')
    expect(statusIcon(1).attributes('title')).toBe('Operational')
    expect(statusIcon(2).classes()).toContain('is-degraded')
    expect(statusIcon(2).attributes('title')).toBe('Degraded')
    expect(statusIcon(3).classes()).toContain('is-down')
    expect(statusIcon(3).attributes('title')).toBe('Failed')
    expect(statusIcon(4).classes()).toContain('is-down')
    expect(statusIcon(4).attributes('title')).toBe('Error')
    wrapper.unmount()
  })

  it('settles into the five empty provider sections after a load failure', async () => {
    listMonitors.mockRejectedValue(new Error('status unavailable'))
    const wrapper = mountView()
    await flushPromises()

    expect(showError).toHaveBeenCalledWith('status unavailable')
    expect(wrapper.findAll('[data-testid^="monitor-provider-empty-"]')).toHaveLength(5)
    expect(wrapper.findAll('[data-testid^="monitor-provider-loading-"]')).toHaveLength(0)
    const summary = wrapper.get('[data-testid="channel-status-summary"]')
    expect(summary.classes()).toContain('is-error')
    expect(summary.get('h2').text()).toBe('Failed to load channel status')
    wrapper.unmount()
  })

  it('sums up the channels with the same latency rules as the rows', async () => {
    listMonitors.mockResolvedValue({
      items: [
        makeMonitor({ id: 1, name: 'Fast', primary_status: 'operational', primary_latency_ms: 800 }),
        makeMonitor({ id: 2, name: 'Slow', primary_status: 'operational', primary_latency_ms: 12_000 }),
        makeMonitor({ id: 3, name: 'Broken', provider: 'anthropic', primary_status: 'error', primary_latency_ms: null }),
        makeMonitor({ id: 4, name: 'Fresh', provider: 'gemini', primary_status: '', timeline: [] }),
      ],
    })

    const wrapper = mountView()
    await flushPromises()

    const summary = wrapper.get('[data-testid="channel-status-summary"]')
    expect(summary.classes()).toContain('is-outage')
    expect(summary.get('h2').text()).toBe('Some channels are failing')
    expect(summary.findAll('.cs-count').map(count => count.text())).toEqual([
      'Operational1',
      'Degraded1',
      'Down1',
      'No data1',
    ])
    expect(wrapper.get('[data-testid="monitor-provider-openai"]').findAll('.monitor-provider-dot').map(dot => dot.classes()))
      .toEqual([
        expect.arrayContaining(['is-operational']),
        expect.arrayContaining(['is-degraded']),
      ])
    expect(wrapper.get('[data-testid="monitor-status-row-1"] [data-testid="monitor-status-latency"]').text()).toBe('800 ms')
    expect(wrapper.get('[data-testid="monitor-status-row-2"] [data-testid="monitor-status-latency"]').text()).toBe('12 s')
    expect(wrapper.find('[data-testid="monitor-status-row-3"] [data-testid="monitor-status-latency"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('reports an all-clear and refreshes in place without the loading skeleton', async () => {
    listMonitors.mockResolvedValue({ items: [makeMonitor()] })
    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.get('[data-testid="channel-status-summary"]').classes()).toContain('is-operational')

    let resolveRefresh!: (value: { items: UserMonitorView[] }) => void
    listMonitors.mockReturnValueOnce(new Promise((resolve) => { resolveRefresh = resolve }))
    await wrapper.get('button[aria-label="Refresh"]').trigger('click')

    expect(listMonitors).toHaveBeenCalledTimes(2)
    expect(wrapper.findAll('[data-testid^="monitor-provider-loading-"]')).toHaveLength(0)
    expect(wrapper.get('button[aria-label="Refresh"]').attributes('disabled')).toBeDefined()

    resolveRefresh({ items: [makeMonitor({ primary_status: 'degraded', primary_latency_ms: 15_000 })] })
    await flushPromises()

    expect(wrapper.get('[data-testid="channel-status-summary"]').classes()).toContain('is-degraded')
    expect(wrapper.get('button[aria-label="Refresh"]').attributes('disabled')).toBeUndefined()
    wrapper.unmount()
  })
})
