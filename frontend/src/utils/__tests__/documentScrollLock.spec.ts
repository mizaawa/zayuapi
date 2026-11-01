import { afterEach, describe, expect, it, vi } from 'vitest'
import { setDocumentScrollLock } from '../documentScrollLock'

function scrollPageTo(y: number) {
  Object.defineProperty(window, 'scrollY', { configurable: true, writable: true, value: y })
}

describe('document scroll lock', () => {
  afterEach(() => {
    vi.spyOn(window, 'scrollTo').mockImplementation(() => {})
    setDocumentScrollLock(false)
    vi.restoreAllMocks()
    document.body.removeAttribute('style')
    document.documentElement.removeAttribute('style')
    scrollPageTo(0)
  })

  it('only hides overflow on desktop browsers', () => {
    const scrollTo = vi.spyOn(window, 'scrollTo').mockImplementation(() => {})
    setDocumentScrollLock(true, false)
    expect(document.body.classList.contains('modal-open')).toBe(true)
    expect(document.body.style.position).toBe('')

    setDocumentScrollLock(false, false)
    expect(document.body.classList.contains('modal-open')).toBe(false)
    expect(scrollTo).not.toHaveBeenCalled()
  })

  it('pins the page on iOS so the keyboard cannot scroll it under the dialog', () => {
    scrollPageTo(640)
    document.body.style.top = '4px'
    setDocumentScrollLock(true, true)
    expect(document.body.classList.contains('modal-open')).toBe(true)
    expect(document.body.style.position).toBe('fixed')
    expect(document.body.style.top).toBe('-640px')
    expect(document.body.style.left).toBe('0px')
    expect(document.body.style.right).toBe('0px')

    // A dialog stacked on top keeps the first offset; the pinned page reads 0.
    scrollPageTo(0)
    setDocumentScrollLock(true, true)
    expect(document.body.style.top).toBe('-640px')
  })

  it('restores the page instantly even though <html> scrolls smoothly', () => {
    document.documentElement.style.scrollBehavior = 'smooth'
    let behaviorWhileRestoring = ''
    const scrollTo = vi.spyOn(window, 'scrollTo').mockImplementation(() => {
      behaviorWhileRestoring = document.documentElement.style.scrollBehavior
    })
    scrollPageTo(320)
    document.body.style.top = '4px'
    setDocumentScrollLock(true, true)
    setDocumentScrollLock(false, true)

    expect(document.body.classList.contains('modal-open')).toBe(false)
    expect(document.body.style.position).toBe('')
    expect(document.body.style.top).toBe('4px')
    expect(scrollTo).toHaveBeenCalledTimes(1)
    expect(scrollTo).toHaveBeenCalledWith(0, 320)
    expect(behaviorWhileRestoring).toBe('auto')
    expect(document.documentElement.style.scrollBehavior).toBe('smooth')
  })
})
