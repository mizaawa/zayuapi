import { defineComponent, nextTick } from 'vue'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import type { LeaderboardEntry, LeaderboardResponse } from '@/api/leaderboard'
import enCommon from '@/i18n/locales/en/common'
import enDashboard from '@/i18n/locales/en/dashboard'
import zhCommon from '@/i18n/locales/zh/common'
import zhDashboard from '@/i18n/locales/zh/dashboard'
import LeaderboardView from '@/views/user/LeaderboardView.vue'

const { getLeaderboard, setParticipation } = vi.hoisted(() => ({
  getLeaderboard: vi.fn(),
  setParticipation: vi.fn(),
}))

vi.mock('@/api/leaderboard', () => ({
  default: { get: getLeaderboard, setParticipation },
}))

// The test config aliases i18n to its runtime-only build; real locale strings need the compiler.
vi.mock('vue-i18n', async () => {
  const { createRequire } = await import('node:module')
  const fullBuild = createRequire(import.meta.url).resolve('vue-i18n/dist/vue-i18n.esm-bundler.js')
  return vi.importActual<typeof import('vue-i18n')>(fullBuild)
})

const AppLayoutStub = defineComponent({
  template: '<main><slot /></main>',
})

const wrappers: VueWrapper[] = []

function makeEntry(overrides: Partial<LeaderboardEntry> = {}): LeaderboardEntry {
  return {
    rank: 1,
    display_name: 'Alice',
    actual_cost: 12.34,
    requests: 20,
    tokens: 1000,
    ...overrides,
  }
}

function makeResponse(entries: LeaderboardEntry[] = []): LeaderboardResponse {
  return {
    period: 'today',
    participating: true,
    period_days: 1,
    entries,
    my_rank: null,
    my_actual_cost: 0,
    my_requests: 0,
    my_tokens: 0,
  }
}

function deferredResponse() {
  let resolve!: (response: LeaderboardResponse) => void
  const promise = new Promise<LeaderboardResponse>((done) => {
    resolve = done
  })
  return { promise, resolve }
}

function mountView(locale: 'zh' | 'en' = 'en'): VueWrapper {
  const i18n = createI18n({
    legacy: false,
    locale,
    fallbackLocale: false,
    messages: {
      en: { ...enCommon, ...enDashboard },
      zh: { ...zhCommon, ...zhDashboard },
    },
  })
  const wrapper = mount(LeaderboardView, {
    global: {
      plugins: [i18n],
      stubs: { AppLayout: AppLayoutStub, Icon: true },
    },
  })
  wrappers.push(wrapper)
  return wrapper
}

describe('LeaderboardView', () => {
  beforeEach(() => {
    vi.useFakeTimers({ toFake: ['Date', 'setInterval', 'clearInterval'] })
    vi.setSystemTime(new Date('2026-09-30T04:00:00Z'))
    localStorage.clear()
    getLeaderboard.mockReset()
    setParticipation.mockReset()
  })

  afterEach(() => {
    for (const wrapper of wrappers.splice(0)) wrapper.unmount()
    vi.clearAllTimers()
    vi.useRealTimers()
    localStorage.clear()
  })

  it.each([
    { locale: 'zh' as const, updating: '更新中...', refresh: '刷新' },
    { locale: 'en' as const, updating: 'Updating...', refresh: 'Refresh' },
  ])('translates initial loading and manual refresh in $locale', async ({ locale, updating, refresh }) => {
    const initialRequest = deferredResponse()
    const refreshRequest = deferredResponse()
    getLeaderboard
      .mockReturnValueOnce(initialRequest.promise)
      .mockReturnValueOnce(refreshRequest.promise)

    const wrapper = mountView(locale)
    await nextTick()

    expect(wrapper.get('.countdown-badge').text()).toBe(updating)
    expect(wrapper.text()).not.toContain('leaderboard.updating')
    expect(getLeaderboard).toHaveBeenCalledWith('today')

    initialRequest.resolve(makeResponse([makeEntry()]))
    await flushPromises()
    expect(wrapper.get('.countdown-badge').text()).toBe('5:00')

    await wrapper.get(`button[aria-label="${refresh}"]`).trigger('click')
    expect(wrapper.get('.countdown-badge').text()).toBe(updating)
    expect(wrapper.text()).not.toContain('leaderboard.updating')
    expect(wrapper.get('[data-testid="leaderboard-entry-1"]').text()).toContain('Alice')
    expect(getLeaderboard).toHaveBeenCalledTimes(2)

    refreshRequest.resolve(makeResponse([makeEntry()]))
    await flushPromises()
    expect(wrapper.get('.countdown-badge').text()).toBe('5:00')
  })

  it('renders the user avatar before the display name', async () => {
    getLeaderboard.mockResolvedValue(makeResponse([
      makeEntry({ avatar_url: 'https://example.com/alice.png' }),
    ]))

    const wrapper = mountView()
    await flushPromises()

    const winner = wrapper.get('[data-testid="leaderboard-entry-1"]')
    const avatar = winner.get('.user-avatar img')
    expect(avatar.attributes('src')).toBe('https://example.com/alice.png')
    expect(winner.text()).toContain('Alice')
    expect(winner.get('.user-avatar').element.nextElementSibling?.textContent).toBe('Alice')
  })

  it('uses initials for missing avatars, including Unicode names and empty names', async () => {
    getLeaderboard.mockResolvedValue(makeResponse([
      makeEntry({ rank: 1, display_name: 'alice', avatar_url: null }),
      makeEntry({ rank: 2, display_name: '\u{1F680} Pilot' }),
      makeEntry({ rank: 3, display_name: '   ' }),
    ]))

    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.findAll('.user-avatar').map(avatar => avatar.text())).toEqual([
      'A', '\u{1F680}', 'U',
    ])
    expect(wrapper.find('.user-avatar img').exists()).toBe(false)
    expect(wrapper.get('.lb-podium').text()).toContain('alice')
    expect(wrapper.get('.lb-podium').text()).toContain('\u{1F680} Pilot')
  })

  it('falls back after an image error and accepts a replacement avatar URL on refresh', async () => {
    getLeaderboard.mockResolvedValueOnce(makeResponse([
      makeEntry({ avatar_url: 'https://example.com/broken.png' }),
    ]))

    const wrapper = mountView()
    await flushPromises()
    await wrapper.get('.user-avatar img').trigger('error')

    expect(wrapper.find('.user-avatar img').exists()).toBe(false)
    expect(wrapper.get('.user-avatar').text()).toBe('A')
    expect(wrapper.get('.lb-podium').text()).toContain('Alice')

    getLeaderboard.mockResolvedValueOnce(makeResponse([
      makeEntry({ avatar_url: 'https://example.com/replacement.png' }),
    ]))
    await wrapper.get('button[aria-label="Refresh"]').trigger('click')
    await flushPromises()

    expect(wrapper.get('.user-avatar img').attributes('src'))
      .toBe('https://example.com/replacement.png')
  })

  it('puts the top three on the podium in rank order and lists the rest', async () => {
    getLeaderboard.mockResolvedValue(makeResponse([
      makeEntry({ rank: 1, display_name: 'Gold', actual_cost: 30 }),
      makeEntry({ rank: 2, display_name: 'Silver', actual_cost: 20 }),
      makeEntry({ rank: 3, display_name: 'Bronze', actual_cost: 10 }),
      makeEntry({ rank: 4, display_name: 'Fourth', actual_cost: 5, requests: 1234, tokens: 2_500_000 }),
      makeEntry({ rank: 5, display_name: 'Fifth', actual_cost: 1 }),
    ]))

    const wrapper = mountView()
    await flushPromises()

    const podium = wrapper.get('.lb-podium')
    expect(podium.attributes('aria-label')).toBe('Top three')
    expect(podium.findAll('.podium-name').map(name => name.text())).toEqual(['Gold', 'Silver', 'Bronze'])
    expect(podium.find('[data-testid="leaderboard-entry-4"]').exists()).toBe(false)

    const rows = wrapper.findAll('.lb-list > li')
    expect(rows.map(row => row.attributes('data-testid'))).toEqual(['leaderboard-entry-4', 'leaderboard-entry-5'])
    expect(wrapper.get('.lb-board-title').text()).toBe('Ranks 4–5')
    expect(rows[0].text()).toContain('1,234 requests')
    expect(rows[0].text()).toContain('2.5M tokens')
    expect(rows[0].get('.lb-cost').text()).toBe('$5.00')
  })

  it('keeps an open spot on the podium when fewer than three users are ranked', async () => {
    getLeaderboard.mockResolvedValue(makeResponse([
      makeEntry({ rank: 1, display_name: 'Solo' }),
    ]))

    const wrapper = mountView()
    await flushPromises()

    const vacant = wrapper.findAll('.podium-slot.is-vacant')
    expect(vacant).toHaveLength(2)
    expect(vacant[0].text()).toContain('Open spot')
    expect(wrapper.find('.lb-board').exists()).toBe(false)
  })

  it('shows my rank, the gap to the next rank and highlights my row', async () => {
    getLeaderboard.mockResolvedValue({
      ...makeResponse([
        makeEntry({ rank: 1, display_name: 'Gold', actual_cost: 30 }),
        makeEntry({ rank: 2, display_name: 'Silver', actual_cost: 20 }),
        makeEntry({ rank: 3, display_name: 'Bronze', actual_cost: 10 }),
        makeEntry({ rank: 4, display_name: 'Fourth', actual_cost: 7.5 }),
        makeEntry({ rank: 5, display_name: 'Me', actual_cost: 5.25 }),
      ]),
      my_rank: 5,
      my_actual_cost: 5.25,
      my_requests: 42,
      my_tokens: 1_500_000,
    })

    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.get('[data-testid="leaderboard-my-rank"]').text()).toBe('#5')
    expect(wrapper.get('.lb-me-note').text()).toBe('$2.25 behind #4')
    expect(wrapper.get('.lb-me-stats').text()).toContain('$5.25')
    expect(wrapper.get('.lb-me-stats').text()).toContain('1.5M')
    const mine = wrapper.get('[data-testid="leaderboard-entry-5"]')
    expect(mine.classes()).toContain('is-me')
    expect(mine.get('.lb-me-tag').text()).toBe('You')
    expect(wrapper.findAll('.is-me')).toHaveLength(1)
  })

  it('tells a ranked user outside the list how far the last listed rank is', async () => {
    getLeaderboard.mockResolvedValue({
      ...makeResponse([
        makeEntry({ rank: 1, display_name: 'Gold', actual_cost: 30 }),
        makeEntry({ rank: 2, display_name: 'Silver', actual_cost: 20 }),
      ]),
      my_rank: 31,
      my_actual_cost: 4,
      my_requests: 3,
    })

    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.get('[data-testid="leaderboard-my-rank"]').text()).toBe('#31')
    expect(wrapper.get('.lb-me-note').text()).toBe('$16.00 short of the top 2')
  })

  it('toggles participation through the switch and reflects a private ranking', async () => {
    getLeaderboard
      .mockResolvedValueOnce({ ...makeResponse([makeEntry()]), my_rank: 1, my_actual_cost: 12.34, my_requests: 20 })
      .mockResolvedValueOnce({ ...makeResponse([]), participating: false })
    setParticipation.mockResolvedValue({ participating: false })

    const wrapper = mountView()
    await flushPromises()

    const toggle = wrapper.get('[role="switch"]')
    expect(toggle.attributes('aria-checked')).toBe('true')
    expect(wrapper.get('.lb-me-note').text()).toBe("You're first today")
    expect(wrapper.get('[data-testid="leaderboard-entry-1"]').classes()).toContain('is-me')

    await toggle.trigger('click')
    await flushPromises()

    expect(setParticipation).toHaveBeenCalledWith(false)
    expect(getLeaderboard).toHaveBeenCalledTimes(2)
    expect(wrapper.get('[role="switch"]').attributes('aria-checked')).toBe('false')
    expect(wrapper.get('[data-testid="leaderboard-my-rank"]').text()).toBe('Private ranking')
    expect(wrapper.find('.lb-me-note').exists()).toBe(false)
    expect(wrapper.get('.lb-me-privacy').text()).toContain('hidden from the leaderboard')
  })

  it('keeps the last ranking on screen when a refresh fails', async () => {
    getLeaderboard.mockResolvedValueOnce(makeResponse([makeEntry()]))
    const wrapper = mountView()
    await flushPromises()

    getLeaderboard.mockRejectedValueOnce(new Error('offline'))
    await wrapper.get('button[aria-label="Refresh"]').trigger('click')
    await flushPromises()

    expect(wrapper.get('[role="alert"]').text()).toBe('Failed to load leaderboard')
    expect(wrapper.get('[data-testid="leaderboard-entry-1"]').text()).toContain('Alice')
  })

  it('says so when a participating user has no usage today', async () => {
    getLeaderboard.mockResolvedValue({ ...makeResponse([makeEntry()]), my_rank: null, my_requests: 0 })

    const wrapper = mountView('zh')
    await flushPromises()

    expect(wrapper.get('[data-testid="leaderboard-my-rank"]').text()).toBe('暂未上榜')
    expect(wrapper.get('.lb-me-note').text()).toBe('今天还没有产生用量')
  })
})
