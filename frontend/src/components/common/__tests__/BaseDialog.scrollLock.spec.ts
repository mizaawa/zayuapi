import { afterEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, mount } from '@vue/test-utils'

vi.mock('@/utils/device', () => ({ isIOSDevice: () => true }))

import BaseDialog from '../BaseDialog.vue'

enableAutoUnmount(afterEach)

describe('BaseDialog on iOS', () => {
  afterEach(() => {
    vi.restoreAllMocks()
    document.body.removeAttribute('style')
    Object.defineProperty(window, 'scrollY', { configurable: true, writable: true, value: 0 })
  })

  it('pins the page while any dialog is open and puts it back after the last one closes', async () => {
    const scrollTo = vi.spyOn(window, 'scrollTo').mockImplementation(() => {})
    Object.defineProperty(window, 'scrollY', { configurable: true, writable: true, value: 900 })
    const trigger = document.createElement('button')
    document.body.appendChild(trigger)
    trigger.focus()
    const focus = vi.spyOn(trigger, 'focus')

    const parent = mount(BaseDialog, { props: { show: false, title: 'Create account' } })
    await parent.setProps({ show: true })
    expect(document.body.style.position).toBe('fixed')
    expect(document.body.style.top).toBe('-900px')

    const child = mount(BaseDialog, { props: { show: true, title: 'Confirm' } })
    await child.setProps({ show: false })
    expect(document.body.style.position).toBe('fixed')
    expect(scrollTo).not.toHaveBeenCalled()

    await parent.setProps({ show: false })
    expect(document.body.style.position).toBe('')
    expect(document.body.classList.contains('modal-open')).toBe(false)
    expect(scrollTo).toHaveBeenCalledWith(0, 900)
    expect(focus).toHaveBeenCalledWith({ preventScroll: true })
    trigger.remove()
  })
})
