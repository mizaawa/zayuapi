import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import SortableScrollArea from '../SortableScrollArea.vue'

describe('SortableScrollArea', () => {
  it('keeps overflow accessible using the scrollbar and keyboard', async () => {
    const wrapper = mount(SortableScrollArea, { props: { label: 'Sort order' }, slots: { default: '<div>Items</div>' } })
    const viewport = wrapper.get('.sort-scroll-viewport').element as HTMLElement
    Object.defineProperties(viewport, { clientHeight: { value: 200 }, scrollHeight: { value: 1000 } })
    await wrapper.get('.sort-scroll-viewport').trigger('scroll')
    const track = wrapper.get('[role="scrollbar"]')
    expect(track.attributes('aria-label')).toBe('Sort order')
    expect(track.attributes('aria-valuenow')).toBe('0')
    await track.trigger('keydown', { key: 'End' })
    expect(viewport.scrollTop).toBe(800)
    expect(track.attributes('aria-valuenow')).toBe('100')
    await track.trigger('keydown', { key: 'Home' })
    expect(viewport.scrollTop).toBe(0)
    const element = track.element as HTMLElement
    element.setPointerCapture = vi.fn()
    element.hasPointerCapture = () => true
    element.releasePointerCapture = vi.fn()
    await track.trigger('pointerdown', { pointerId: 1, clientY: 100, button: 2, isPrimary: true })
    expect(viewport.scrollTop).toBe(0)
    await track.trigger('pointerdown', { pointerId: 1, clientY: 100, button: 0, isPrimary: true })
    expect(viewport.scrollTop).toBe(400)
    expect(track.attributes('aria-valuenow')).toBe('50')
    await track.trigger('pointermove', { pointerId: 2, clientY: 200 })
    expect(viewport.scrollTop).toBe(400)
    await track.trigger('pointermove', { pointerId: 1, clientY: 300 })
    expect(viewport.scrollTop).toBe(800)
    await track.trigger('pointerup', { pointerId: 1 })
    expect(element.releasePointerCapture).toHaveBeenCalledWith(1)
    await track.trigger('pointermove', { pointerId: 1, clientY: 0 })
    expect(viewport.scrollTop).toBe(800)
    wrapper.unmount()
  })

  it('does not show a scrollbar for a short list', async () => {
    const wrapper = mount(SortableScrollArea, { props: { label: 'Sort order' } })
    const viewport = wrapper.get('.sort-scroll-viewport').element as HTMLElement
    Object.defineProperties(viewport, { clientHeight: { value: 200 }, scrollHeight: { value: 100 } })
    await wrapper.get('.sort-scroll-viewport').trigger('scroll')
    expect(wrapper.find('[role="scrollbar"]').exists()).toBe(false)
    wrapper.unmount()
  })
})
