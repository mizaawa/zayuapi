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
    expect(wrapper.get('tbody').text()).toContain('Alice')
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

    const userCell = wrapper.get('tbody tr').findAll('td')[1]
    const avatar = userCell.get('.user-avatar img')
    expect(avatar.attributes('src')).toBe('https://example.com/alice.png')
    expect(userCell.text()).toContain('Alice')
    expect(userCell.get('.user-avatar').element.nextElementSibling?.textContent).toBe('Alice')
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
    expect(wrapper.get('tbody').text()).toContain('alice')
    expect(wrapper.get('tbody').text()).toContain('\u{1F680} Pilot')
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
    expect(wrapper.get('tbody').text()).toContain('Alice')

    getLeaderboard.mockResolvedValueOnce(makeResponse([
      makeEntry({ avatar_url: 'https://example.com/replacement.png' }),
    ]))
    await wrapper.get('button[aria-label="Refresh"]').trigger('click')
    await flushPromises()

    expect(wrapper.get('.user-avatar img').attributes('src'))
      .toBe('https://example.com/replacement.png')
  })
})
