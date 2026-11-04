import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import Pagination from '../Pagination.vue'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string, params?: { page: number; total: number | string }) =>
      key === 'pagination.pageOf' ? `${params?.page} / ${params?.total}` : key,
  }),
}))

describe('Pagination with a pending total', () => {
  it('marks lower bounds and allows requesting the next page', async () => {
    const wrapper = mount(Pagination, {
      props: { total: 21, page: 1, pageSize: 20, totalIsExact: false },
      global: { stubs: { Icon: true, Select: true } },
    })
    expect(wrapper.text()).toContain('21+')
    expect(wrapper.text()).toContain('1 / 2+')
    await wrapper.get('button[aria-label="pagination.next"]').trigger('click')
    expect(wrapper.emitted('update:page')).toEqual([[2]])
    await wrapper.setProps({ total: 85, totalIsExact: true })
    expect(wrapper.text()).not.toContain('+')
    expect(wrapper.text()).toContain('1 / 5')
    wrapper.unmount()
  })

  it('pages through previous, the page picker and next in the picker variant', async () => {
    const PagePickerStub = {
      props: ['page', 'totalPages', 'totalIsExact'],
      emits: ['select'],
      template: '<button data-test="picker" @click="$emit(\'select\', 4)">{{ page }}/{{ totalPages }}{{ totalIsExact ? \'\' : \'+\' }}</button>',
    }
    const wrapper = mount(Pagination, {
      props: { total: 21, page: 1, pageSize: 20, totalIsExact: false, variant: 'picker' },
      global: { stubs: { Icon: true, Select: true, PagePicker: PagePickerStub } },
    })
    expect(wrapper.find('button[aria-label="pagination.goToPage"]').exists()).toBe(false)
    expect(wrapper.get('[data-test="picker"]').text()).toBe('1/2+')
    expect(wrapper.get('[data-testid="pagination-previous"]').attributes('disabled')).toBeDefined()
    await wrapper.get('[data-testid="pagination-next"]').trigger('click')
    expect(wrapper.emitted('update:page')).toEqual([[2]])

    await wrapper.setProps({ total: 85, totalIsExact: true, page: 5 })
    expect(wrapper.get('[data-test="picker"]').text()).toBe('5/5')
    expect(wrapper.get('[data-testid="pagination-next"]').attributes('disabled')).toBeDefined()
    await wrapper.get('[data-test="picker"]').trigger('click')
    await wrapper.get('[data-testid="pagination-previous"]').trigger('click')
    expect(wrapper.emitted('update:page')).toEqual([[2], [4], [4]])
    wrapper.unmount()
  })

  it('keeps existing exact pagination behavior by default', async () => {
    const wrapper = mount(Pagination, {
      props: { total: 20, page: 1, pageSize: 20 },
      global: { stubs: { Icon: true, Select: true } },
    })
    expect(wrapper.text()).toContain('1 / 1')
    expect(wrapper.text()).not.toContain('+')
    expect(wrapper.get('button[aria-label="pagination.next"]').attributes('disabled')).toBeDefined()
    wrapper.unmount()
  })
})
