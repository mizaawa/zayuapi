import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import SheetHandle from '../SheetHandle.vue'

function fixture() {
  const wrapper = mount(SheetHandle, { attachTo: document.body })
  const grip = wrapper.get('.sheet-handle').element as HTMLElement
  grip.setPointerCapture = vi.fn()
  const pointer = (type: string, x: number, y: number) => grip.dispatchEvent(Object.assign(new Event(type), {
    pointerId: 1, isPrimary: true, button: 0, clientX: x, clientY: y,
  }))
  return { wrapper, panel: grip.parentElement!, pointer }
}

describe('mobile sheet handle', () => {
  it('follows a deliberate downward drag, then dismisses only its own sheet', () => {
    const { wrapper, panel, pointer } = fixture()
    pointer('pointerdown', 10, 10)
    pointer('pointermove', 12, 90)
    expect(panel.style.getPropertyValue('--sheet-pull')).toBe('80px')
    pointer('pointerup', 12, 90)
    expect(wrapper.emitted('dismiss')).toHaveLength(1)
    expect(panel.style.getPropertyValue('--sheet-pull')).toBe('')
    wrapper.unmount()
  })

  it.each([['pointerup', 5, 10], ['pointerup', 160, 80], ['pointercancel', 2, 80]] as const)(
    'does not dismiss on %s (%d, %d) when short, horizontal or cancelled', (end, x, y) => {
      const { wrapper, panel, pointer } = fixture()
      pointer('pointerdown', 0, 0)
      pointer('pointermove', x, y)
      pointer(end, x, y)
      expect(wrapper.emitted('dismiss')).toBeUndefined()
      expect(panel.style.getPropertyValue('--sheet-pull')).toBe('')
      wrapper.unmount()
    },
  )
})
