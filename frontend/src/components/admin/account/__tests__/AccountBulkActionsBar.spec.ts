import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'

import AccountBulkActionsBar from '../AccountBulkActionsBar.vue'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string) => key
  })
}))

describe('AccountBulkActionsBar', () => {
  it('allows selecting all results before any row is selected', async () => {
    const wrapper = mount(AccountBulkActionsBar, {
      props: {
        selectedIds: [],
        totalResults: 45,
        selectingAll: false,
        allResultsSelected: false
      }
    })

    const button = wrapper.findAll('button').find(item =>
      item.text().includes('admin.accounts.bulkActions.selectAllResults')
    )

    expect(button).toBeDefined()
    await button!.trigger('click')
    expect(wrapper.emitted('select-all-results')).toHaveLength(1)
  })

  it('preserves the upstream billing probe action from v0.1.166', async () => {
    const wrapper = mount(AccountBulkActionsBar, {
      props: {
        selectedIds: [1],
        totalResults: 45,
        selectingAll: false,
        allResultsSelected: false
      }
    })

    const button = wrapper.findAll('button').find(item =>
      item.text().includes('admin.accounts.bulkActions.probeUpstreamBilling')
    )

    expect(button).toBeDefined()
    await button!.trigger('click')
    expect(wrapper.emitted('probe-upstream-billing')).toHaveLength(1)
  })

  it('offers only the filtered-results update until rows are selected', async () => {
    const wrapper = mount(AccountBulkActionsBar, {
      props: {
        selectedIds: [],
        totalResults: 45,
        selectingAll: false,
        allResultsSelected: false
      }
    })

    expect(wrapper.find('[data-testid="account-bulk-delete"]').exists()).toBe(false)
    await wrapper.get('[data-testid="account-bulk-edit-filtered"]').trigger('click')
    expect(wrapper.emitted('edit-filtered')).toHaveLength(1)
  })

  it('emits the target scheduling state from the enable and disable actions', async () => {
    const wrapper = mount(AccountBulkActionsBar, {
      props: {
        selectedIds: [1, 2],
        totalResults: 45,
        selectingAll: false,
        allResultsSelected: false
      }
    })

    await wrapper.get('[data-testid="account-bulk-enable-scheduling"]').trigger('click')
    await wrapper.get('[data-testid="account-bulk-disable-scheduling"]').trigger('click')
    expect(wrapper.emitted('toggle-schedulable')).toEqual([[true], [false]])
  })

  it('disables every action and marks the running one while a bulk action is pending', async () => {
    const wrapper = mount(AccountBulkActionsBar, {
      props: {
        selectedIds: [1, 2],
        totalResults: 45,
        selectingAll: false,
        allResultsSelected: false,
        busyAction: 'delete'
      }
    })

    const actions = wrapper.findAll('button.account-bulk-action')
    expect(actions.length).toBe(8)
    expect(actions.every(button => button.attributes('disabled') !== undefined)).toBe(true)

    const running = wrapper.get('[data-testid="account-bulk-delete"]')
    expect(running.attributes('aria-busy')).toBe('true')
    expect(running.text()).toContain('common.processing')

    await running.trigger('click')
    expect(wrapper.emitted('delete')).toBeUndefined()
  })
})
