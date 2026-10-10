import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

import RedeemView from '../RedeemView.vue'

const { listRedeemCodes, generateRedeemCodes, batchUpdateRedeemCodes, getAllGroups, showSuccess, showError, showInfo, adminSettingsStore } =
  vi.hoisted(() => ({
    listRedeemCodes: vi.fn(),
    generateRedeemCodes: vi.fn(),
    batchUpdateRedeemCodes: vi.fn(),
    getAllGroups: vi.fn(),
    showSuccess: vi.fn(),
    showError: vi.fn(),
    showInfo: vi.fn(),
    adminSettingsStore: {
      disableRedeemCodeCreationLimit: false,
      fetch: vi.fn()
    }
  }))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    redeem: {
      list: listRedeemCodes,
      generate: generateRedeemCodes,
      delete: vi.fn(),
      batchDelete: vi.fn(),
      batchUpdate: batchUpdateRedeemCodes,
      exportCodes: vi.fn()
    },
    groups: {
      getAll: getAllGroups
    }
  }
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showSuccess,
    showError,
    showInfo
  })
}))

vi.mock('@/stores/adminSettings', () => ({
  useAdminSettingsStore: () => adminSettingsStore
}))

vi.mock('@/composables/useClipboard', () => ({
  useClipboard: () => ({
    copyToClipboard: vi.fn()
  })
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => key
    })
  }
})

const DataTableStub = {
  props: ['columns', 'data'],
  template: `
    <table>
      <thead>
        <tr>
          <th v-for="column in columns" :key="column.key">
            <slot :name="'header-' + column.key" :column="column">{{ column.label }}</slot>
          </th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="row in data" :key="row.id">
          <td v-for="column in columns" :key="column.key">
            <slot :name="'cell-' + column.key" :row="row" :value="row[column.key]">
              {{ row[column.key] }}
            </slot>
          </td>
        </tr>
      </tbody>
    </table>
  `
}

const SelectStub = {
  props: ['modelValue', 'options'],
  emits: ['update:modelValue', 'change'],
  setup(props: { options: Array<{ value: unknown; label: string }> }, { emit }: { emit: (event: string, ...args: unknown[]) => void }) {
    const onChange = (event: Event) => {
      const raw = (event.target as HTMLSelectElement).value
      const option = props.options.find((item) => String(item.value ?? '') === raw)
      const value = option ? option.value : raw
      emit('update:modelValue', value)
      emit('change', value, option ?? null)
    }
    return { onChange }
  },
  template: `
    <select v-bind="$attrs" :value="modelValue ?? ''" @change="onChange">
      <option v-for="option in options" :key="String(option.value ?? '')" :value="option.value ?? ''">
        {{ option.label }}
      </option>
    </select>
  `
}

function mountView() {
  return mount(RedeemView, {
    attachTo: document.body,
    global: {
      stubs: {
        AppLayout: { template: '<div><slot /></div>' },
        TablePageLayout: {
          template: '<div><slot name="filters" /><slot name="table" /><slot name="pagination" /></div>'
        },
        DataTable: DataTableStub,
        Pagination: true,
        ConfirmDialog: true,
        Select: SelectStub,
        GroupBadge: true,
        GroupOptionItem: true,
        Icon: true,
        Teleport: true
      }
    }
  })
}

describe('admin RedeemView', () => {
  beforeEach(() => {
    localStorage.clear()
    document.body.innerHTML = ''

    listRedeemCodes.mockReset()
    generateRedeemCodes.mockReset()
    batchUpdateRedeemCodes.mockReset()
    getAllGroups.mockReset()
    showSuccess.mockReset()
    showError.mockReset()
    showInfo.mockReset()
    adminSettingsStore.disableRedeemCodeCreationLimit = false
    adminSettingsStore.fetch.mockReset().mockResolvedValue(undefined)

    listRedeemCodes.mockResolvedValue({
      items: [
        {
          id: 1,
          code: 'CODE-1',
          type: 'balance',
          value: 10,
          status: 'unused',
          used_by: null,
          used_at: null,
          created_at: '2026-01-01T00:00:00Z',
          expires_at: null
        },
        {
          id: 2,
          code: 'CODE-2',
          type: 'balance',
          value: 20,
          status: 'unused',
          used_by: null,
          used_at: null,
          created_at: '2026-01-01T00:00:00Z',
          expires_at: null
        }
      ],
      total: 2,
      page: 1,
      page_size: 20,
      pages: 1
    })
    batchUpdateRedeemCodes.mockResolvedValue({ updated: 1, message: 'ok' })
    generateRedeemCodes.mockResolvedValue([])
    getAllGroups.mockResolvedValue([])
  })

  it('submits only checked fields for selected redeem codes', async () => {
    const wrapper = mountView()

    await flushPromises()
    await wrapper.findAll('[data-test="select-code"]')[0].setValue(true)
    await wrapper.get('[data-test="batch-update-open"]').trigger('click')
    await flushPromises()

    await wrapper.get('[data-test="batch-field-status"]').setValue(true)
    await wrapper.get('[data-test="batch-status-select"]').setValue('disabled')
    await wrapper.get('[data-test="batch-field-notes"]').setValue(true)
    await wrapper.get('[data-test="batch-notes-input"]').setValue('maintenance')
    await wrapper.get('[data-test="batch-update-form"]').trigger('submit')
    await flushPromises()

    expect(batchUpdateRedeemCodes).toHaveBeenCalledWith([1], {
      status: 'disabled',
      notes: 'maintenance'
    })
    expect(showSuccess).toHaveBeenCalledWith('admin.redeem.batchUpdateSuccess')
    wrapper.unmount()
  })

  it('refreshes the setting and keeps the default maximum at 100', async () => {
    const wrapper = mountView()
    await flushPromises()
    expect(adminSettingsStore.fetch).toHaveBeenCalledWith(true)
    await wrapper.get('[data-test="generate-open"]').trigger('click')

    const input = wrapper.get('[data-test="generate-count"]')
    expect(input.attributes('max')).toBe('100')
    await input.setValue('100')
    expect((input.element as HTMLInputElement).validity.valid).toBe(true)
    await input.setValue('101')
    expect((input.element as HTMLInputElement).validity.rangeOverflow).toBe(true)
    wrapper.unmount()
  })

  it('allows generating more than 300 codes when the limit is disabled', async () => {
    adminSettingsStore.disableRedeemCodeCreationLimit = true
    const wrapper = mountView()
    await flushPromises()
    await wrapper.get('[data-test="generate-open"]').trigger('click')

    const input = wrapper.get('[data-test="generate-count"]')
    expect(input.attributes('max')).toBeUndefined()
    await input.setValue('1001')
    expect((input.element as HTMLInputElement).validity.valid).toBe(true)
    await wrapper.get('[data-test="generate-form"]').trigger('submit')
    await flushPromises()

    expect(generateRedeemCodes).toHaveBeenCalledWith(1001, 'balance', 10, undefined, undefined, undefined)
    wrapper.unmount()
  })

  it.each(['0', '-1', '1.5', ''])('still rejects invalid count %s when the limit is disabled', async (count) => {
    adminSettingsStore.disableRedeemCodeCreationLimit = true
    const wrapper = mountView()
    await flushPromises()
    await wrapper.get('[data-test="generate-open"]').trigger('click')

    const input = wrapper.get('[data-test="generate-count"]')
    await input.setValue(count)
    expect((input.element as HTMLInputElement).validity.valid).toBe(false)
    expect(generateRedeemCodes).not.toHaveBeenCalled()
    wrapper.unmount()
  })
})
