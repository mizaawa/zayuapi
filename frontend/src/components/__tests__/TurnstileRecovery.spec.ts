import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { ref } from 'vue'
import TurnstileWidget from '../TurnstileWidget.vue'
import type { TurnstileAPI } from '@/utils/turnstile'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@vueuse/core', () => ({ useElementSize: () => ({ width }) }))
const width = ref(360)
const sdkURL = 'https://challenges.cloudflare.com/turnstile/v0/api.js?render=explicit'
const scripts = () => Array.from(document.scripts).filter(s => s.src === sdkURL)
const mountWidget = (siteKey = 'site-key') => mount(TurnstileWidget, { props: { siteKey } })
function installSDK() {
  let id = 0
  const render = vi.fn<TurnstileAPI['render']>(() => String(++id))
  const api = { render, remove: vi.fn(), reset: vi.fn() }
  window.turnstile = api
  return api
}

enableAutoUnmount(afterEach)
beforeEach(() => { width.value = 360 })
afterEach(() => {
  delete window.turnstile
  for (const s of scripts()) s.remove()
  document.getElementById('config-nonce')?.remove()
  vi.useRealTimers()
})

describe('Turnstile recovery', () => {
  it('shares one official async script and its CSP nonce, never widget tokens', async () => {
    const config = document.createElement('script')
    config.id = 'config-nonce'
    config.nonce = 'request-nonce'
    document.head.appendChild(config)
    const first = mountWidget(), second = mountWidget()
    await flushPromises()
    expect(scripts()).toHaveLength(1)
    expect(scripts()[0].nonce).toBe('request-nonce')
    const api = installSDK()
    scripts()[0].dispatchEvent(new Event('load'))
    await flushPromises()
    expect(api.render).toHaveBeenCalledTimes(2)
    const [[firstEl, a], [secondEl, b]] = api.render.mock.calls
    expect(firstEl).not.toBe(secondEl)
    a.callback('one')
    expect(second.emitted('verify')).toBeUndefined()
    b.callback('two')
    expect(first.emitted('verify')).toEqual([['one']])
    expect(second.emitted('verify')).toEqual([['two']])
  })

  it.each(['network error', 'missing API', 'timeout'])('recovers in place after %s without a proof', async failure => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
    const form = mountWidget()
    await flushPromises()
    const old = scripts()[0]
    if (failure === 'timeout') await vi.advanceTimersByTimeAsync(30000)
    else old.dispatchEvent(new Event(failure === 'network error' ? 'error' : 'load'))
    await flushPromises()
    expect(old.isConnected).toBe(false)
    expect(form.emitted('error')).toHaveLength(1)
    expect(form.emitted('verify')).toBeUndefined()
    await form.get('button[type="button"]').trigger('click')
    expect(scripts()).toHaveLength(1)
    expect(scripts()[0]).not.toBe(old)
    const api = installSDK()
    scripts()[0].dispatchEvent(new Event('load'))
    await flushPromises()
    expect(form.emitted('verify')).toBeUndefined()
    api.render.mock.calls[0][1].callback('fresh')
    expect(form.emitted('verify')).toEqual([['fresh']])
  })

  it('ignores old callbacks after retry or unmount and clears proof on timeout/expiry', async () => {
    const onVerify = vi.fn()
    const api = installSDK()
    const form = mount(TurnstileWidget, { props: { siteKey: 'site-key', onVerify } })
    await flushPromises()
    const a = api.render.mock.calls[0][1]
    a.callback('original'); a['expired-callback']()
    expect(form.emitted('expire')).toHaveLength(1)
    a['timeout-callback']()
    await flushPromises()
    await form.get('button').trigger('click')
    await flushPromises()
    expect(api.remove).toHaveBeenCalledWith('1')
    a.callback('late'); a['error-callback']()
    const b = api.render.mock.calls[1][1]
    b.callback('replacement')
    expect(form.emitted('error')).toHaveLength(1)
    form.unmount()
    b.callback('also late')
    expect(onVerify.mock.calls).toEqual([['original'], ['replacement']])
  })

  it('does not load without a site key or render into a form that left during loading', async () => {
    const form = mountWidget('')
    await flushPromises()
    expect(scripts()).toHaveLength(0)
    await form.setProps({ siteKey: 'new-key' })
    await flushPromises()
    const script = scripts()[0]
    form.unmount()
    const api = installSDK()
    script.dispatchEvent(new Event('load'))
    await flushPromises()
    expect(api.render).not.toHaveBeenCalled()
  })

  it('offers manual recovery for a silent iframe without interrupting the challenge or unlocking the form', async () => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
    width.value = 260
    const api = installSDK(), form = mountWidget()
    await flushPromises()
    expect(api.render.mock.calls[0][1].size).toBe('compact')
    await vi.advanceTimersByTimeAsync(16000)
    expect(form.find('button').exists()).toBe(true)
    expect(api.remove).not.toHaveBeenCalled()
    expect(form.emitted('verify')).toBeUndefined()
    api.render.mock.calls[0][1].callback('solved')
    await flushPromises()
    expect(form.find('button').exists()).toBe(false)
  })
})
