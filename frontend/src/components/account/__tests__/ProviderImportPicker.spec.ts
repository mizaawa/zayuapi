import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import ProviderImportPicker from '../ProviderImportPicker.vue'

const options = [
  { id: 'a', name: 'Alpha', detail: 'first', badge: 'Sub2API' },
  { id: 'b', name: 'Bravo', detail: 'disabled', available: false },
  { id: 'c', name: 'Charlie', detail: 'third', badge: 'New API', badgeTone: 'warning' as const }
]

function mountPicker(props: Record<string, unknown> = {}) {
  return mount(ProviderImportPicker, {
    props: {
      modelValue: '',
      options,
      icon: 'server',
      label: 'Provider',
      placeholder: 'Choose',
      hint: 'Pick one',
      countText: '3 sites',
      ...props
    },
    global: { stubs: { teleport: true, Icon: true } },
    attachTo: document.body
  })
}

describe('ProviderImportPicker', () => {
  it('moves past unavailable rows with the keyboard and selects with Enter', async () => {
    const wrapper = mountPicker()
    const trigger = wrapper.get('button[role="combobox"]')
    expect(trigger.text()).toContain('Choose')

    await trigger.trigger('keydown', { key: 'ArrowDown' })
    expect(trigger.attributes('aria-expanded')).toBe('true')
    const active = () => wrapper.get(`#${trigger.attributes('aria-activedescendant')}`)
    expect(active().text()).toContain('Alpha')

    await trigger.trigger('keydown', { key: 'ArrowDown' })
    expect(active().text()).toContain('Charlie')
    await trigger.trigger('keydown', { key: 'Enter' })

    expect(wrapper.emitted('update:modelValue')).toEqual([['c']])
    expect(trigger.attributes('aria-expanded')).toBe('false')
    wrapper.unmount()
  })

  it('closes on Escape without letting the key reach the surrounding dialog', async () => {
    const wrapper = mountPicker({ modelValue: 'a' })
    let reachedDocument = false
    const listener = () => { reachedDocument = true }
    document.addEventListener('keydown', listener)
    const trigger = wrapper.get('button[role="combobox"]')
    await trigger.trigger('click')
    expect(wrapper.find('[role="listbox"]').exists()).toBe(true)
    expect(wrapper.get('[aria-selected="true"]').text()).toContain('Alpha')

    await trigger.trigger('keydown', { key: 'Escape' })

    expect(wrapper.find('[role="listbox"]').exists()).toBe(false)
    expect(reachedDocument).toBe(false)
    document.removeEventListener('keydown', listener)
    wrapper.unmount()
  })

  it('offers the action row first and closes when clicking outside', async () => {
    const wrapper = mountPicker({ icon: 'key', action: { name: 'Create from provider', detail: 'New key' } })
    const trigger = wrapper.get('button[role="combobox"]')
    await trigger.trigger('click')
    const rows = wrapper.findAll('[role="option"]')
    expect(rows[0].text()).toContain('Create from provider')
    await rows[0].trigger('click')
    expect(wrapper.emitted('action')).toHaveLength(1)
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()

    await trigger.trigger('click')
    expect(wrapper.find('[role="listbox"]').exists()).toBe(true)
    document.body.dispatchEvent(new Event('pointerdown', { bubbles: true }))
    await wrapper.vm.$nextTick()
    expect(wrapper.find('[role="listbox"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('stays closed while loading', async () => {
    const wrapper = mountPicker({ loading: true, loadingText: 'Loading…' })
    const trigger = wrapper.get('button[role="combobox"]')
    expect(trigger.attributes('disabled')).toBeDefined()
    expect(trigger.text()).toContain('Loading…')
    await trigger.trigger('keydown', { key: 'ArrowDown' })
    expect(wrapper.find('[role="listbox"]').exists()).toBe(false)
    wrapper.unmount()
  })
})
