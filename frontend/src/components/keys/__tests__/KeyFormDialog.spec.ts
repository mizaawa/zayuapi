import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import { i18n } from '@/i18n'
import KeyFormDialog from '../KeyFormDialog.vue'
import type { ApiKey } from '@/types'

describe('key editor sheet', () => {
  it('keeps the edit panel present for the leave transition when the card selection clears', async () => {
    const wrapper = mount(KeyFormDialog, {
      props: { show: false, editingKey: null, submitting: false },
      global: { plugins: [i18n], stubs: { transition: false } },
    })
    try {
      await wrapper.setProps({ show: true, editingKey: { id: 1, name: 'Test key' } as ApiKey })
      const layer = document.body.querySelector('.modal-overlay.mobile-sheet')!
      const panel = layer.querySelector('.key-editor-panel.sheet-panel')!
      expect(layer.classList.contains('modal-enter-active')).toBe(true)
      expect(panel.textContent).toContain('Test key')

      await wrapper.setProps({ show: false, editingKey: null })
      expect(layer.classList.contains('modal-leave-active')).toBe(true)
      expect(panel.isConnected).toBe(true)
      expect(panel.textContent).toContain('Test key')
      await vi.waitFor(() => expect(panel.isConnected).toBe(false))
    } finally {
      wrapper.unmount()
    }
  })
})
