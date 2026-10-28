import { mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { nextTick } from 'vue'

import Select from '../Select.vue'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key }),
  }
})

const originalInnerWidth = window.innerWidth
let unmountWrapper: (() => void) | undefined

const setViewportWidth = (width: number) => {
  Object.defineProperty(window, 'innerWidth', {
    configurable: true,
    value: width,
  })
}

const mockTriggerRect = (left: number, width: number) => {
  vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockReturnValue({
    x: left,
    y: 20,
    top: 20,
    right: left + width,
    bottom: 60,
    left,
    width,
    height: 40,
    toJSON: () => ({}),
  })
}

const openSelect = async (centered = false, dropdownMinWidth?: number) => {
  const wrapper = mount(Select, {
    props: {
      modelValue: null,
      options: [
        {
          value: 'example',
          label: 'very-long-unbroken-option-value-that-must-not-overflow',
        },
      ],
      centered,
      dropdownMinWidth,
    },
  })
  unmountWrapper = () => wrapper.unmount()

  await wrapper.get('button').trigger('click')
  await nextTick()

  return document.body.querySelector<HTMLElement>('.select-dropdown-portal')
}

afterEach(() => {
  unmountWrapper?.()
  unmountWrapper = undefined
  document.body.innerHTML = ''
  setViewportWidth(originalInnerWidth)
  vi.restoreAllMocks()
})

describe('Select dropdown viewport constraints', () => {
  it('emits a blocked event without selecting a blocked option', async () => {
    const wrapper = mount(Select, {
      props: {
        modelValue: null,
        options: [
          { value: 'blocked', label: 'Blocked group', blocked: true },
          { value: 'allowed', label: 'Allowed group' },
        ],
      },
    })

    await wrapper.get('button').trigger('click')
    await nextTick()
    const options = document.body.querySelectorAll('[role="option"]')
    expect(options).toHaveLength(2)
    await (options[0] as HTMLElement).click()

    expect(wrapper.emitted('blocked')).toHaveLength(1)
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    wrapper.unmount()
  })

  it('preserves the existing 200px minimum width when space is available', async () => {
    setViewportWidth(1024)
    mockTriggerRect(20, 80)

    const dropdown = await openSelect()

    expect(dropdown).not.toBeNull()
    expect(dropdown?.style.left).toBe('20px')
    expect(dropdown?.style.minWidth).toBe('200px')
    expect(dropdown?.style.maxWidth).toBe('996px')
  })

  it('shrinks the minimum width to fit near the right viewport edge', async () => {
    setViewportWidth(320)
    mockTriggerRect(220, 80)

    const dropdown = await openSelect()

    expect(dropdown).not.toBeNull()
    expect(dropdown?.style.left).toBe('220px')
    expect(dropdown?.style.minWidth).toBe('92px')
    expect(dropdown?.style.maxWidth).toBe('92px')
  })

  it('clamps a trigger left of the viewport to the safe padding', async () => {
    setViewportWidth(320)
    mockTriggerRect(-20, 80)

    const dropdown = await openSelect()

    expect(dropdown).not.toBeNull()
    expect(dropdown?.style.left).toBe('8px')
    expect(dropdown?.style.minWidth).toBe('200px')
    expect(dropdown?.style.maxWidth).toBe('304px')
  })

  it('clamps an offscreen-right trigger position to the viewport boundary', async () => {
    setViewportWidth(320)
    mockTriggerRect(400, 80)

    const dropdown = await openSelect()

    expect(dropdown).not.toBeNull()
    expect(dropdown?.style.left).toBe('312px')
    expect(dropdown?.style.minWidth).toBe('0px')
    expect(dropdown?.style.maxWidth).toBe('0px')
  })

  it('centers the opt-in group picker within the viewport', async () => {
    setViewportWidth(320)
    mockTriggerRect(220, 80)

    const dropdown = await openSelect(true)

    expect(dropdown).not.toBeNull()
    expect(dropdown?.style.left).toBe('50%')
    expect(dropdown?.style.top).toBe('50%')
    expect(dropdown?.style.transform).toBe('translate(-50%, -50%)')
    expect(dropdown?.style.width).toBe('304px')
    expect(document.body.querySelector('.select-centered-backdrop')).not.toBeNull()
  })

  it('opens an opt-in wide menu leftward from a trigger near the right edge', async () => {
    setViewportWidth(1440)
    mockTriggerRect(1244, 160)

    const dropdown = await openSelect(false, 320)

    expect(dropdown).not.toBeNull()
    expect(dropdown?.style.left).toBe('')
    expect(dropdown?.style.right).toBe('36px')
    expect(dropdown?.style.minWidth).toBe('320px')
    expect(dropdown?.style.maxWidth).toBe('1396px')
  })

  it('keeps an opt-in wide menu left-aligned when it fits', async () => {
    setViewportWidth(1440)
    mockTriggerRect(400, 160)

    const dropdown = await openSelect(false, 320)

    expect(dropdown).not.toBeNull()
    expect(dropdown?.style.left).toBe('400px')
    expect(dropdown?.style.right).toBe('')
    expect(dropdown?.style.minWidth).toBe('320px')
    expect(dropdown?.style.maxWidth).toBe('1032px')
  })
})
