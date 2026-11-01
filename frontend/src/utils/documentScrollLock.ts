import { isIOSDevice } from '@/utils/device'

type PinnedProperty = 'position' | 'top' | 'left' | 'right'

let pinned: { scrollX: number; scrollY: number; previous: Record<PinnedProperty, string> } | null = null

/**
 * Keeps the page behind open dialogs from scrolling.
 *
 * overflow: hidden on <body> is enough for desktop browsers. iOS Safari still
 * scrolls the document when the keyboard opens for a field in a fixed dialog,
 * and once the keyboard closes the dialog is hit-tested at that stale offset:
 * taps land on the wrong control and swipes stop scrolling the dialog. On iOS
 * the body is therefore pinned with position: fixed, which leaves the document
 * nothing to scroll; the negative top keeps the page where it was.
 */
export function setDocumentScrollLock(locked: boolean, pinBody: boolean = isIOSDevice()): void {
  const { body } = document
  body.classList.toggle('modal-open', locked)

  if (locked && pinBody && !pinned) {
    const { scrollX, scrollY } = window
    const { position, top, left, right } = body.style
    pinned = { scrollX, scrollY, previous: { position, top, left, right } }
    Object.assign(body.style, { position: 'fixed', top: `${-scrollY}px`, left: '0', right: '0' })
  } else if (!locked && pinned) {
    const { scrollX, scrollY, previous } = pinned
    pinned = null
    Object.assign(body.style, previous)
    // <html> scrolls smoothly; restoring the page must not animate from the top.
    const root = document.documentElement
    const behavior = root.style.scrollBehavior
    root.style.scrollBehavior = 'auto'
    window.scrollTo(scrollX, scrollY)
    root.style.scrollBehavior = behavior
  }
}
