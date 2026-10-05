import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { defineComponent, ref } from 'vue'
import SystemPromptSettings from '../SystemPromptSettings.vue'
import type { ApiKeySystemPromptSettings } from '@/types'
import zhMessages from '@/i18n/locales/zh/dashboard'
import enMessages from '@/i18n/locales/en/dashboard'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key })
}))

const wrappers: VueWrapper[] = []
const mountSettings = (disabled = false) => {
  const settings = ref<ApiKeySystemPromptSettings>({
    custom_system_prompt_enabled: false,
    custom_system_prompt_force: false,
    custom_system_prompt: ''
  })
  const wrapper = mount(defineComponent({
    components: { SystemPromptSettings },
    setup: () => ({ settings, disabled }),
    template: '<SystemPromptSettings id="prompt" v-model="settings" :disabled="disabled" />'
  }), { attachTo: document.body })
  wrappers.push(wrapper)
  return { wrapper, settings }
}

afterEach(() => wrappers.splice(0).forEach((wrapper) => wrapper.unmount()))

describe('SystemPromptSettings', () => {
  it('defines the requested prompt text in the keys locale namespace', () => {
    expect(zhMessages.keys.customSystemPrompt).toEqual({
      enable: '自定义系统提示词',
      hint: '部分上游响应可能不支持系统提示词透传，默认为追加提示词策略。可选择强制使用系统提示词协议，不保证成功透传至上游响应',
      force: '强制使用系统提示词协议',
      label: '设置系统提示词',
      placeholder: '诶～杂鱼就是杂鱼♡果然不行呢(˃̶᷄ ⁻̫ ˂̶᷅)',
      required: '请设置系统提示词',
      tooLong: '系统提示词不能超过 32768 字节'
    })
    expect(Object.keys(enMessages.keys.customSystemPrompt)).toEqual(Object.keys(zhMessages.keys.customSystemPrompt))
  })

  it('labels each control, focuses the editor when enabled, and preserves the draft when toggled off', async () => {
    const { wrapper, settings } = mountSettings()
    const toggle = wrapper.get('[data-test="system-prompt-toggle"]')
    expect(toggle.attributes('role')).toBe('switch')
    expect(toggle.attributes('aria-checked')).toBe('false')
    expect(wrapper.get('label[for="prompt-enabled"]').text()).toBe('keys.customSystemPrompt.enable')
    await toggle.trigger('click')
    await flushPromises()
    const input = wrapper.get('[data-test="system-prompt-input"]')
    expect(document.activeElement).toBe(input.element)
    expect(toggle.attributes('aria-checked')).toBe('true')
    expect(wrapper.get('label[for="prompt-prompt"]').text()).toBe('keys.customSystemPrompt.label')
    expect(input.attributes('placeholder')).toBe('keys.customSystemPrompt.placeholder')
    expect(wrapper.get('label[for="prompt-force"]').text()).toBe('keys.customSystemPrompt.force')
    await input.setValue('Preserved prompt')
    await wrapper.get('[data-test="system-prompt-force-toggle"]').trigger('click')
    expect(settings.value.custom_system_prompt_force).toBe(true)
    await toggle.trigger('click')
    expect(wrapper.find('[data-test="system-prompt-controls"]').exists()).toBe(false)
    expect(settings.value.custom_system_prompt).toBe('Preserved prompt')
    expect(settings.value.custom_system_prompt_force).toBe(true)
  })

  it('disables the toggle while the parent form is saving', async () => {
    const { wrapper, settings } = mountSettings(true)
    expect((wrapper.get('[data-test="system-prompt-toggle"]').element as HTMLButtonElement).disabled).toBe(true)
    await wrapper.get('[data-test="system-prompt-toggle"]').trigger('click')
    expect(settings.value.custom_system_prompt_enabled).toBe(false)
    settings.value.custom_system_prompt_enabled = true
    await flushPromises()
    expect((wrapper.get('[data-test="system-prompt-force-toggle"]').element as HTMLButtonElement).disabled).toBe(true)
    expect((wrapper.get('[data-test="system-prompt-input"]').element as HTMLTextAreaElement).disabled).toBe(true)
  })
})
