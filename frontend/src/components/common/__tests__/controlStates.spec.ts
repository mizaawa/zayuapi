import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import Input from '../Input.vue'
import TextArea from '../TextArea.vue'
import Toggle from '../Toggle.vue'
import SearchInput from '../SearchInput.vue'

describe('shared warm controls', () => {
  it('keeps search labelled and emits typed text', async () => {
    const search = mount(SearchInput, { props: { modelValue: '', placeholder: 'Search accounts' } })
    const field = search.get('input')
    expect(field.classes()).toContain('input')
    expect(field.attributes('aria-label')).toBe('Search accounts')
    await field.setValue('example')
    expect(search.emitted('update:modelValue')).toEqual([['example']])
  })

  for (const component of [Input, TextArea]) {
    it(`keeps validation and disabled semantics on ${component.__name}`, async () => {
      const wrapper = mount(component, { props: { modelValue: 'example' } })
      const field = wrapper.get('input, textarea')
      expect(field.classes()).toContain('input')
      await wrapper.setProps({ error: 'Invalid' })
      expect(field.attributes('aria-invalid')).toBe('true')
      expect(field.classes()).toContain('input-error')
      await wrapper.setProps({ error: '', disabled: true })
      expect(field.attributes('aria-invalid')).toBeUndefined()
      expect(field.attributes('disabled')).toBeDefined()
    })
  }

  it('reflects switch state both visually and to assistive technology', async () => {
    const wrapper = mount(Toggle, { props: { modelValue: false } })
    const button = wrapper.get('[role="switch"]')
    expect(button.classes()).toContain('switch-off')
    expect(button.attributes('aria-checked')).toBe('false')
    await button.trigger('click')
    expect(wrapper.emitted('update:modelValue')).toEqual([[true]])
    await wrapper.setProps({ modelValue: true })
    expect(button.classes()).toContain('switch-on')
    expect(button.attributes('aria-checked')).toBe('true')
  })
})
