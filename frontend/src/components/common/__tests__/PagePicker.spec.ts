import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { nextTick } from 'vue'
import PagePicker from '../PagePicker.vue'

const { messages } = vi.hoisted(() => ({
  messages: {
    'pagination.pageLabel': 'Page {page}',
    'pagination.totalPages': '{total} pages',
    'pagination.pageOf': 'Page {page} of {total}',
    'pagination.choosePage': 'Choose a page',
  } as Record<string, string>,
}))

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string, params: Record<string, unknown> = {}) =>
      (messages[key] ?? key).replace(/\{(\w+)\}/g, (_, name: string) => String(params[name])),
  }),
}))

const panel = () => document.body.querySelector<HTMLElement>('[data-testid="page-picker-panel"]')
const list = () => panel()!.querySelector<HTMLElement>('[role="listbox"]')!
const renderedPages = () =>
  [...list().querySelectorAll<HTMLElement>('[role="option"]')].map((option) => Number(option.dataset.page))
const activePage = () => list().getAttribute('aria-activedescendant')?.split('-').pop()

const setViewport = (width: number, height: number) => {
  Object.defineProperty(window, 'innerWidth', { configurable: true, value: width })
  Object.defineProperty(window, 'innerHeight', { configurable: true, value: height })
}

const mountPicker = (props: Record<string, unknown>, rect: Partial<DOMRect> = {}) => {
  const wrapper = mount(PagePicker, { props, attachTo: document.body, global: { stubs: { Icon: true } } })
  const trigger = wrapper.get('[data-testid="page-picker-trigger"]')
  const box = { top: 100, bottom: 140, left: 460, right: 560, width: 100, height: 40, x: 460, y: 100, ...rect }
  trigger.element.getBoundingClientRect = () => ({ ...box, toJSON: () => box }) as DOMRect
  return { wrapper, trigger }
}

// The panel's leave transition still runs inside the teleport, so closing settles a frame later.
const closed = () => vi.waitFor(() => expect(panel()).toBeNull())

const press = async (key: string) => {
  list().dispatchEvent(new KeyboardEvent('keydown', { key, bubbles: true, cancelable: true }))
  await nextTick()
}

describe('PagePicker', () => {
  beforeEach(() => setViewport(1024, 768))

  afterEach(() => {
    document.body.innerHTML = ''
  })

  it('lists the pages and emits the one picked', async () => {
    const { wrapper, trigger } = mountPicker({ page: 3, totalPages: 12 })
    expect(trigger.text()).toBe('Page 3')
    expect(trigger.attributes('title')).toBe('Page 3 of 12')

    await trigger.trigger('click')
    await flushPromises()
    expect(trigger.attributes('aria-expanded')).toBe('true')
    expect(panel()!.textContent).toContain('12 pages')
    expect(list().querySelector('[aria-selected="true"]')?.textContent).toContain('Page 3')
    expect(document.activeElement).toBe(list())

    list().querySelector<HTMLElement>('[data-page="9"]')!.click()
    await flushPromises()
    expect(wrapper.emitted('select')).toEqual([[9]])
    await closed()
    expect(document.activeElement).toBe(trigger.element)
    wrapper.unmount()
  })

  it('renders only the rows in view and centres the current page in long lists', async () => {
    const { wrapper, trigger } = mountPicker({ page: 25000, totalPages: 50000 })
    await trigger.trigger('click')
    await flushPromises()

    const pages = renderedPages()
    expect(pages.length).toBeLessThan(20)
    expect(pages).toContain(25000)
    expect(list().querySelector<HTMLElement>('.page-picker-track')!.style.height).toBe('2000000px')
    wrapper.unmount()
  })

  it('reaches the last page of a compressed track', async () => {
    const { wrapper, trigger } = mountPicker({ page: 1, totalPages: 1_000_000 })
    await trigger.trigger('click')
    await flushPromises()
    expect(renderedPages()[0]).toBe(1)
    expect(list().querySelector<HTMLElement>('.page-picker-track')!.style.height).toBe('6000000px')

    const maxScrollTop = 6_000_000 - parseFloat(list().style.height)
    list().scrollTop = maxScrollTop
    list().dispatchEvent(new Event('scroll'))
    await nextTick()
    expect(renderedPages().at(-1)).toBe(1_000_000)

    list().scrollTop = maxScrollTop / 2
    list().dispatchEvent(new Event('scroll'))
    await nextTick()
    expect(renderedPages()).toContain(500_000)
    wrapper.unmount()
  })

  it('moves with the keyboard and jumps to a typed page number', async () => {
    const { wrapper, trigger } = mountPicker({ page: 5, totalPages: 300 })
    await trigger.trigger('keydown', { key: 'ArrowDown' })
    await flushPromises()
    expect(activePage()).toBe('5')

    await press('ArrowDown')
    expect(activePage()).toBe('6')
    await press('End')
    expect(activePage()).toBe('300')
    expect(renderedPages()).toContain(300)
    await press('Home')
    expect(activePage()).toBe('1')
    await press('1')
    await press('2')
    await press('9')
    expect(activePage()).toBe('129')
    expect(renderedPages()).toContain(129)

    await press('Enter')
    expect(wrapper.emitted('select')).toEqual([[129]])
    await closed()

    await trigger.trigger('click')
    await flushPromises()
    await press('Escape')
    await closed()
    expect(wrapper.emitted('select')).toHaveLength(1)
    wrapper.unmount()
  })

  it('opens upward near the bottom of a phone screen and stays inside it', async () => {
    setViewport(320, 568)
    const { wrapper, trigger } = mountPicker(
      { page: 7, totalPages: 50000 },
      { top: 500, bottom: 540, left: 250, right: 310, width: 60, x: 250, y: 500 }
    )
    await trigger.trigger('click')
    await flushPromises()

    const style = panel()!.style
    expect(style.top).toBe('')
    expect(style.bottom).toBe(`${568 - 500 + 6}px`)
    const left = parseFloat(style.left)
    const width = parseFloat(style.width)
    expect(left).toBeGreaterThanOrEqual(8)
    expect(left + width).toBeLessThanOrEqual(320 - 8)
    // Header, list and padding fit in the room above the trigger.
    expect(parseFloat(list().style.height) + 30 + 12).toBeLessThanOrEqual(500 - 6 - 8)
    wrapper.unmount()
  })

  it('closes on a press outside', async () => {
    const { wrapper, trigger } = mountPicker({ page: 1, totalPages: 4 })
    await trigger.trigger('click')
    await flushPromises()
    list().dispatchEvent(new Event('pointerdown', { bubbles: true }))
    expect(panel()).not.toBeNull()

    document.body.dispatchEvent(new Event('pointerdown', { bubbles: true }))
    await closed()
    expect(trigger.attributes('aria-expanded')).toBe('false')
    wrapper.unmount()
  })

  it('marks a page count that is still a lower bound', async () => {
    const { wrapper, trigger } = mountPicker({ page: 2, totalPages: 3, totalIsExact: false })
    expect(trigger.attributes('title')).toBe('Page 2 of 3+')
    await trigger.trigger('click')
    await flushPromises()
    expect(panel()!.textContent).toContain('3+ pages')
    expect(list().querySelector('[role="option"]')!.getAttribute('aria-setsize')).toBe('-1')

    await wrapper.setProps({ totalPages: 129, totalIsExact: true })
    expect(panel()!.textContent).toContain('129 pages')
    wrapper.unmount()
  })
})
