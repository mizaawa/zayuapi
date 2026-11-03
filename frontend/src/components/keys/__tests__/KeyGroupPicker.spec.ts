import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'

import type { GroupPlatform } from '@/types'
import zhMessages from '@/i18n/locales/zh/dashboard'
import enMessages from '@/i18n/locales/en/dashboard'
import KeyGroupPicker from '../KeyGroupPicker.vue'
import {
  KEY_GROUP_PLATFORMS,
  keyGroupPlatform,
  keyGroupPlatformChips,
  matchesKeyGroupQuery,
  type KeyGroupOption
} from '../keyGroupFilters'

const device = vi.hoisted(() => ({ mobile: false }))

vi.mock('@/utils/device', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/utils/device')>()),
  isMobileDevice: () => device.mobile
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ cachedPublicSettings: { server_utc_offset: '+08:00' } })
}))

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key })
}))

const option = (value: number, platform: GroupPlatform, overrides: Partial<KeyGroupOption> = {}): KeyGroupOption => ({
  value,
  label: `Group ${value}`,
  description: null,
  rate: 1,
  userRate: null,
  peakRateEnabled: false,
  peakStart: '',
  peakEnd: '',
  peakRateMultiplier: 1,
  subscriptionType: 'standard',
  platform,
  ...overrides
})

const OPTIONS: KeyGroupOption[] = [
  option(1, 'anthropic', { label: 'CCMAX-05 可看号池', description: 'Claude Max 保真，零注入' }),
  option(2, 'anthropic', { label: 'CCMAX-08 轻微注入', description: '可蒸馏' }),
  option(3, 'openai', { label: 'Codex Pro' }),
  option(4, 'openai', { label: 'Codex Plus', description: 'Claude Code 也能用' }),
  option(5, 'gemini', { label: 'Gemini 2.5' }),
  option(6, 'custom', { label: 'Relay' }),
  option(7, 'composite', { label: 'Mixed' })
]

const wrappers: VueWrapper[] = []

const mountPicker = (props: Partial<{
  show: boolean
  options: KeyGroupOption[]
  selectedId: number | null
  title: string
  subtitle: string
}> = {}) => {
  const wrapper = mount(KeyGroupPicker, {
    props: { show: true, options: OPTIONS, selectedId: null, ...props },
    attachTo: document.body,
    global: { stubs: { teleport: true } }
  })
  wrappers.push(wrapper)
  return wrapper
}

const visibleIds = (wrapper: VueWrapper) =>
  wrapper.findAll('[role="option"]').map((row) => Number(row.attributes('data-test')!.replace('key-group-option-', '')))

const chipCount = (wrapper: VueWrapper, value: string) =>
  wrapper.get(`[data-test="key-group-platform-${value}"]`).get('[data-test="key-group-platform-count"]').text()

const search = (wrapper: VueWrapper) => wrapper.get('[data-test="key-group-picker-search"]')

beforeEach(() => {
  device.mobile = false
  document.body.classList.remove('modal-open')
})

afterEach(() => {
  wrappers.splice(0).forEach((wrapper) => wrapper.unmount())
  document.body.classList.remove('modal-open')
})

describe('key group picker filters', () => {
  it('treats custom and composite as the one Custom platform', () => {
    expect(KEY_GROUP_PLATFORMS).toEqual(['anthropic', 'openai', 'gemini', 'grok', 'composite'])
    expect(keyGroupPlatform('custom')).toBe('composite')
    expect(keyGroupPlatform('composite')).toBe('composite')
    expect(keyGroupPlatform('openai')).toBe('openai')
  })

  it('keeps the five platform chips and adds a legacy platform only while it has groups', () => {
    expect(keyGroupPlatformChips(OPTIONS)).toEqual(['anthropic', 'openai', 'gemini', 'grok', 'composite'])
    expect(keyGroupPlatformChips([...OPTIONS, option(9, 'antigravity')]))
      .toEqual(['anthropic', 'openai', 'gemini', 'grok', 'composite', 'antigravity'])
  })

  it('needs every search term somewhere in the name, description or platform name', () => {
    expect(matchesKeyGroupQuery(OPTIONS[0], 'ccmax 零注入')).toBe(true)
    expect(matchesKeyGroupQuery(OPTIONS[1], 'ccmax 零注入')).toBe(false)
    expect(matchesKeyGroupQuery(OPTIONS[2], '  OPENAI  ')).toBe(true)
    expect(matchesKeyGroupQuery(OPTIONS[5], 'custom')).toBe(true)
    expect(matchesKeyGroupQuery(OPTIONS[4], '')).toBe(true)
  })
})
describe('KeyGroupPicker', () => {
  it('defines the same picker copy in both locales', () => {
    expect(zhMessages.keys.groupPicker.changeTitle).toBe('更换分组')
    expect(Object.keys(enMessages.keys.groupPicker)).toEqual(Object.keys(zhMessages.keys.groupPicker))
  })

  it('offers All plus the five platforms with per-platform counts', () => {
    const wrapper = mountPicker()

    expect(wrapper.findAll('.kgp-chip').map((chip) => chip.attributes('data-test'))).toEqual([
      'key-group-platform-all',
      'key-group-platform-anthropic',
      'key-group-platform-openai',
      'key-group-platform-gemini',
      'key-group-platform-grok',
      'key-group-platform-composite'
    ])
    expect(['all', 'anthropic', 'openai', 'gemini', 'grok', 'composite'].map((value) => chipCount(wrapper, value)))
      .toEqual(['7', '2', '2', '1', '0', '2'])
    expect(wrapper.get('[data-test="key-group-platform-all"]').attributes('aria-pressed')).toBe('true')
    // A platform without groups stays visible but cannot lead to an empty list.
    expect(wrapper.get('[data-test="key-group-platform-grok"]').attributes('disabled')).toBeDefined()
    expect(visibleIds(wrapper)).toEqual([1, 2, 3, 4, 5, 6, 7])
  })

  it('filters the list by platform and returns to every group with All', async () => {
    const wrapper = mountPicker()

    await wrapper.get('[data-test="key-group-platform-openai"]').trigger('click')
    expect(visibleIds(wrapper)).toEqual([3, 4])
    expect(wrapper.get('[data-test="key-group-platform-openai"]').attributes('aria-pressed')).toBe('true')

    await wrapper.get('[data-test="key-group-platform-composite"]').trigger('click')
    expect(visibleIds(wrapper)).toEqual([6, 7])

    await wrapper.get('[data-test="key-group-platform-all"]').trigger('click')
    expect(visibleIds(wrapper)).toEqual([1, 2, 3, 4, 5, 6, 7])
  })

  it('combines search with the platform filter and recounts the chips', async () => {
    const wrapper = mountPicker()

    await search(wrapper).setValue('claude')
    expect(visibleIds(wrapper)).toEqual([1, 4])
    expect(['all', 'anthropic', 'openai', 'gemini'].map((value) => chipCount(wrapper, value)))
      .toEqual(['2', '1', '1', '0'])
    expect(wrapper.get('[data-test="key-group-platform-gemini"]').attributes('disabled')).toBeDefined()

    await wrapper.get('[data-test="key-group-platform-openai"]').trigger('click')
    expect(visibleIds(wrapper)).toEqual([4])

    await search(wrapper).setValue('nothing like this')
    expect(visibleIds(wrapper)).toEqual([])
    expect(wrapper.get('[data-test="key-group-picker-empty"]').text()).toContain('keys.groupPicker.noMatch')

    await wrapper.get('[data-test="key-group-picker-reset"]').trigger('click')
    expect((search(wrapper).element as HTMLInputElement).value).toBe('')
    expect(wrapper.get('[data-test="key-group-platform-all"]').attributes('aria-pressed')).toBe('true')
    expect(visibleIds(wrapper)).toHaveLength(OPTIONS.length)
  })

  it('says so when the user has no groups at all', () => {
    const wrapper = mountPicker({ options: [] })

    expect(wrapper.get('[data-test="key-group-picker-empty"]').text()).toContain('keys.groupPicker.empty')
    expect(wrapper.find('[data-test="key-group-picker-reset"]').exists()).toBe(false)
  })

  it('shows rate, user rate, peak window, subscription and description on each row', () => {
    const wrapper = mountPicker({
      options: [
        option(1, 'openai', {
          rate: 1.5,
          userRate: 0.8,
          peakRateEnabled: true,
          peakStart: '14:00',
          peakEnd: '18:00',
          peakRateMultiplier: 2,
          subscriptionType: 'subscription',
          description: 'Line one\nLine two'
        })
      ],
      selectedId: 1
    })
    const row = wrapper.get('[data-test="key-group-option-1"]')

    expect(row.attributes('aria-selected')).toBe('true')
    expect(row.classes()).toContain('is-selected')
    expect(row.get('.kgp-rate-old').text()).toBe('1.5x')
    expect(row.get('.kgp-rate').text()).toContain('0.8x')
    expect(row.get('.kgp-peak').text()).toBe('14:00-18:00 ×2 (UTC+08:00)')
    expect(row.text()).toContain('groups.subscription')
    expect(row.get('.kgp-option-desc').text()).toBe('Line one\nLine two')
  })

  it('explains a blocked group in its row and never selects it', async () => {
    const wrapper = mountPicker({ options: [option(1, 'openai', { blocked: true, description: 'hidden' }), option(2, 'openai')] })
    const blocked = wrapper.get('[data-test="key-group-option-1"]')

    expect(blocked.attributes('aria-disabled')).toBe('true')
    expect(blocked.get('.kgp-option-blocked-hint').text()).toBe('keys.groupBlockedToast')
    await blocked.trigger('click')
    expect(wrapper.emitted('select')).toBeUndefined()

    await wrapper.get('[data-test="key-group-option-2"]').trigger('click')
    expect(wrapper.emitted('select')).toEqual([[expect.objectContaining({ value: 2 })]])
  })
})
describe('KeyGroupPicker keyboard and focus', () => {
  it('starts on the current group and walks the visible rows with the arrow keys', async () => {
    const wrapper = mountPicker({ selectedId: 3 })
    await flushPromises()
    const input = search(wrapper)

    expect(document.activeElement).toBe(input.element)
    expect(input.attributes('aria-activedescendant')).toBe(wrapper.get('[data-test="key-group-option-3"]').attributes('id'))

    await input.trigger('keydown', { key: 'ArrowDown' })
    expect(input.attributes('aria-activedescendant')).toBe(wrapper.get('[data-test="key-group-option-4"]').attributes('id'))
    expect(wrapper.get('.kgp-list').classes()).toContain('is-keyboard')

    await input.trigger('keydown', { key: 'Enter' })
    expect(wrapper.emitted('select')).toEqual([[expect.objectContaining({ value: 4 })]])
  })

  it('wraps around and selects the first match after typing', async () => {
    const wrapper = mountPicker()
    const input = search(wrapper)

    await input.trigger('keydown', { key: 'ArrowUp' })
    expect(input.attributes('aria-activedescendant')).toBe(wrapper.get('[data-test="key-group-option-7"]').attributes('id'))

    await input.setValue('codex')
    await input.trigger('keydown', { key: 'Enter' })
    expect(wrapper.emitted('select')).toEqual([[expect.objectContaining({ value: 3 })]])
  })

  it('leaves Enter to the input method while a candidate is being composed', async () => {
    const wrapper = mountPicker({ selectedId: 1 })

    await search(wrapper).trigger('keydown', { key: 'Enter', isComposing: true })
    expect(wrapper.emitted('select')).toBeUndefined()
  })

  it('clears the search on the first Escape, closes on the second, and keeps Escape from the dialog below', async () => {
    const dialogEscape = vi.fn()
    document.addEventListener('keydown', dialogEscape)
    try {
      const wrapper = mountPicker()
      const input = search(wrapper)
      await input.setValue('codex')

      await input.trigger('keydown', { key: 'Escape' })
      expect((input.element as HTMLInputElement).value).toBe('')
      expect(wrapper.emitted('close')).toBeUndefined()

      await input.trigger('keydown', { key: 'Escape' })
      expect(wrapper.emitted('close')).toHaveLength(1)
      expect(dialogEscape).not.toHaveBeenCalled()
    } finally {
      document.removeEventListener('keydown', dialogEscape)
    }
  })

  it('closes from the backdrop and the close button', async () => {
    const wrapper = mountPicker()

    await wrapper.get('.kgp-backdrop').trigger('click')
    await wrapper.get('[data-test="key-group-picker-close"]').trigger('click')
    expect(wrapper.emitted('close')).toHaveLength(2)
  })

  it('resets search and platform every time it opens and hands focus back on close', async () => {
    const trigger = document.createElement('button')
    document.body.appendChild(trigger)
    trigger.focus()
    try {
      const wrapper = mountPicker({ show: false })
      await wrapper.setProps({ show: true })
      await flushPromises()
      await search(wrapper).setValue('codex')
      await wrapper.get('[data-test="key-group-platform-openai"]').trigger('click')
      expect(visibleIds(wrapper)).toEqual([3, 4])

      await wrapper.setProps({ show: false })
      expect(document.activeElement).toBe(trigger)

      await wrapper.setProps({ show: true })
      await flushPromises()
      expect((search(wrapper).element as HTMLInputElement).value).toBe('')
      expect(wrapper.get('[data-test="key-group-platform-all"]').attributes('aria-pressed')).toBe('true')
      expect(visibleIds(wrapper)).toHaveLength(OPTIONS.length)
    } finally {
      trigger.remove()
    }
  })

  it('locks the page only when no dialog already holds the lock', async () => {
    const wrapper = mountPicker({ show: false })

    await wrapper.setProps({ show: true })
    expect(document.body.classList.contains('modal-open')).toBe(true)
    await wrapper.setProps({ show: false })
    expect(document.body.classList.contains('modal-open')).toBe(false)

    document.body.classList.add('modal-open')
    await wrapper.setProps({ show: true })
    await wrapper.setProps({ show: false })
    expect(document.body.classList.contains('modal-open')).toBe(true)
  })

  it('keeps the phone keyboard closed and uses its return key only to dismiss it', async () => {
    device.mobile = true
    const wrapper = mountPicker({ selectedId: 1 })
    await flushPromises()

    expect(document.activeElement).toBe(wrapper.get('.kgp-panel').element)
    const input = search(wrapper)
    ;(input.element as HTMLInputElement).focus()
    await input.setValue('codex')
    expect(wrapper.get('.kgp-list').classes()).not.toContain('is-keyboard')

    await input.trigger('keydown', { key: 'Enter' })
    expect(wrapper.emitted('select')).toBeUndefined()
    expect(document.activeElement).not.toBe(input.element)

    await wrapper.get('[data-test="key-group-option-4"]').trigger('click')
    expect(wrapper.emitted('select')).toEqual([[expect.objectContaining({ value: 4 })]])
  })

  it('does not hand focus back to a text field on a phone, which would reopen its keyboard', async () => {
    device.mobile = true
    const field = document.createElement('input')
    document.body.appendChild(field)
    field.focus()
    try {
      const wrapper = mountPicker({ show: false })
      await wrapper.setProps({ show: true })
      await flushPromises()
      await wrapper.setProps({ show: false })
      expect(document.activeElement).not.toBe(field)
    } finally {
      field.remove()
    }
  })
})
