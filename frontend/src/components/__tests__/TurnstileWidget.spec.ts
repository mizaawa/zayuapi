import { ref } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import TurnstileWidget from '../TurnstileWidget.vue'
import type { TurnstileAPI } from '@/utils/turnstile'

const measured = ref(380)
vi.mock('@vueuse/core', () => ({ useElementSize: () => ({ width: measured }) }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
const sdk = () => ({ ready: (fn: () => void) => fn(), render: vi.fn<TurnstileAPI['render']>().mockReturnValue('widget'), reset: vi.fn(), remove: vi.fn() })
const script = () => document.head.querySelector<HTMLScriptElement>('script[src*="challenges.cloudflare.com/turnstile/"]')!
const mountWidget = () => mount(TurnstileWidget, { props: { siteKey: 'unit-test-key' } })

beforeEach(() => { measured.value = 380 })
afterEach(() => { delete window.turnstile; script()?.remove() })

describe('TurnstileWidget', () => {
  it('changes native size only at the width boundary and invalidates the replaced proof', async () => {
    const api = window.turnstile = sdk()
    const view = mountWidget()
    await flushPromises()
    const options = api.render.mock.calls[0][1]
    expect(options.size).toBe('flexible')
    options.callback('test-proof')
    measured.value = 300
    await flushPromises()
    expect(api.render).toHaveBeenCalledTimes(1)
    measured.value = 299
    await flushPromises()
    expect(api.render.mock.calls[1][1].size).toBe('compact')
    expect(api.remove).toHaveBeenCalledWith('widget')
    expect(view.emitted('expire')).toHaveLength(1)
    options.callback('obsolete')
    expect(view.emitted('verify')).toEqual([['test-proof']])
    view.unmount()
  })

  it('keeps expiry, errors, manual retry and reset separate from successful verification', async () => {
    const api = window.turnstile = sdk()
    const view = mountWidget()
    await flushPromises()
    const opts = api.render.mock.calls[0][1]
    opts['expired-callback']()
    opts['error-callback']()
    await flushPromises()
    expect(view.emitted('verify')).toBeUndefined()
    expect(view.emitted('expire')).toHaveLength(1)
    expect(view.emitted('error')).toHaveLength(1)
    await view.get('button').trigger('click')
    await flushPromises()
    expect(api.render).toHaveBeenCalledTimes(2)
    expect(view.find('button').exists()).toBe(false)
    view.vm.reset()
    expect(api.reset).toHaveBeenCalledWith('widget')
    view.unmount()
  })

  it('shares a pending SDK load; leaving the first form does not steal the second callback', async () => {
    const a = mountWidget(), b = mountWidget()
    await flushPromises()
    expect(document.head.querySelectorAll('script[src*="challenges.cloudflare.com/turnstile/"]')).toHaveLength(1)
    a.unmount()
    const api = window.turnstile = sdk()
    script().dispatchEvent(new Event('load'))
    await flushPromises()
    expect(api.render).toHaveBeenCalledTimes(1)
    api.render.mock.calls[0][1].callback('second')
    expect(b.emitted('verify')).toEqual([['second']])
    b.unmount()
  })

  it('retries a failed SDK download without leaving a stale script or issuing a proof', async () => {
    const view = mountWidget()
    await flushPromises()
    const failed = script()
    failed.dispatchEvent(new Event('error'))
    await flushPromises()
    expect(failed.isConnected).toBe(false)
    expect(view.emitted('verify')).toBeUndefined()
    await view.get('button').trigger('click')
    await flushPromises()
    const api = window.turnstile = sdk()
    script().dispatchEvent(new Event('load'))
    await flushPromises()
    expect(api.render).toHaveBeenCalledTimes(1)
    view.unmount()
  })
})
