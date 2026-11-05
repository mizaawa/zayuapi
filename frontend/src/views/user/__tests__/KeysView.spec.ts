import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { nextTick } from 'vue'

import type { ApiKey, Group } from '@/types'
import GroupBadge from '@/components/common/GroupBadge.vue'
import KeysView from '../KeysView.vue'

const {
  listKeys,
  getPublicSettings,
  getUserEndpointSettings,
  getDashboardApiKeysUsage,
  getAvailableGroups,
  getUserGroupRates,
  showError,
  showSuccess,
  copyToClipboard,
  isCurrentStep,
  nextStep,
  deleteKey,
  createKey,
  updateKey,
  toggleStatus,
} = vi.hoisted(() => ({
  listKeys: vi.fn(),
  getPublicSettings: vi.fn(),
  getUserEndpointSettings: vi.fn(),
  getDashboardApiKeysUsage: vi.fn(),
  getAvailableGroups: vi.fn(),
  getUserGroupRates: vi.fn(),
  showError: vi.fn(),
  showSuccess: vi.fn(),
  copyToClipboard: vi.fn(),
  isCurrentStep: vi.fn(),
  nextStep: vi.fn(),
  deleteKey: vi.fn(),
  createKey: vi.fn(),
  updateKey: vi.fn(),
  toggleStatus: vi.fn(),
}))

const messages: Record<string, string> = {
  'common.actions': 'Actions',
  'common.name': 'Name',
  'common.refresh': 'Refresh',
  'common.status': 'Status',
  'keys.apiKey': 'API Key',
  'keys.allGroups': 'All Groups',
  'keys.allStatus': 'All Status',
  'keys.cardFields': 'Card fields',
  'keys.createKey': 'Create API Key',
  'keys.created': 'Created',
  'keys.onlyShownOnCreation': 'Full key shown only once',
  'keys.createdKeyTitle': 'Your new API key',
  'keys.createdKeyWarning': 'Copy and save this key now. It will not be shown again after you close this window.',
  'keys.copyCreatedKey': 'Copy API key',
  'keys.expiresAt': 'Expires',
  'keys.group': 'Group',
  'keys.id': 'ID',
  'keys.currentConcurrency': 'Current Concurrency',
  'keys.failover.group': 'Fallback Group',
  'keys.failover.disabled': 'Disabled',
  'keys.failover.healthy': 'Primary Group Healthy',
  'keys.failover.active': 'Fallback Group Active',
  'keys.lastUsedAt': 'Last Used',
  'keys.lastUsedIP': 'Last Used IP',
  'keys.rateLimitColumn': 'Rate Limit',
  'keys.searchPlaceholder': 'Search name or key...',
  'keys.status.active': 'Active',
  'keys.status.expired': 'Expired',
  'keys.status.inactive': 'Inactive',
  'keys.status.quota_exhausted': 'Quota exhausted',
  'keys.tokenMix': 'Token mix',
  'keys.usage': 'Usage',
}

vi.mock('@/api', () => ({
  keysAPI: {
    list: listKeys,
    create: createKey,
    update: updateKey,
    delete: deleteKey,
    toggleStatus,
  },
  authAPI: {
    getPublicSettings,
    getUserEndpointSettings,
  },
  usageAPI: {
    getDashboardApiKeysUsage,
  },
  userGroupsAPI: {
    getAvailable: getAvailableGroups,
    getUserGroupRates,
  },
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError,
    showSuccess,
  }),
}))

vi.mock('@/stores/onboarding', () => ({
  useOnboardingStore: () => ({
    isCurrentStep,
    nextStep,
  }),
}))

vi.mock('@/composables/useClipboard', () => ({
  useClipboard: () => ({
    copyToClipboard,
  }),
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => messages[key] ?? key,
    }),
  }
})

const createApiKey = (): ApiKey => ({
  id: 1,
  user_id: 1,
  key: 'sk-test-key',
  name: 'test-key',
  group_id: null,
  status: 'active',
  ip_whitelist: [],
  ip_blacklist: [],
  last_used_at: null,
  last_used_ip: null,
  quota: 0,
  quota_used: 0,
  expires_at: null,
  created_at: '2026-06-27T00:00:00Z',
  updated_at: '2026-06-27T00:00:00Z',
  current_concurrency: 3,
  rate_limit_5h: 0,
  rate_limit_1d: 0,
  rate_limit_7d: 0,
  usage_5h: 0,
  usage_1d: 0,
  usage_7d: 0,
  window_5h_start: null,
  window_1d_start: null,
  window_7d_start: null,
  reset_5h_at: null,
  reset_1d_at: null,
  reset_7d_at: null,
})

const page = (items: ApiKey[], total = items.length, pageNumber = 1, pages = Math.max(1, Math.ceil(total / 20))) =>
  ({ items, total, page: pageNumber, page_size: 20, pages })

// The usage batch answers for every requested key, with zeros when it has no traffic.
const usageFor = (id: number, overrides: Record<string, number> = {}) => ({
  api_key_id: id,
  today_actual_cost: 0,
  total_actual_cost: 0,
  input_tokens: 0,
  output_tokens: 0,
  cache_creation_tokens: 0,
  cache_read_tokens: 0,
  ...overrides,
})

const AppLayoutStub = {
  template: '<div><slot /></div>',
}

const SelectStub = {
  name: 'Select',
  props: ['modelValue', 'options'],
  emits: ['update:modelValue'],
  template: '<select :value="modelValue" @change="$emit(\'update:modelValue\', $event.target.value)"></select>',
}

const SearchInputStub = {
  name: 'SearchInput',
  props: ['modelValue'],
  emits: ['update:modelValue', 'search'],
  template: '<input :value="modelValue" @input="$emit(\'update:modelValue\', $event.target.value)" />',
}

const PaginationStub = {
  name: 'Pagination',
  props: ['page', 'total', 'pageSize'],
  emits: ['update:page', 'update:pageSize'],
  template: `
    <div>
      <button data-test="page-2" @click="$emit('update:page', 2)">2</button>
      <button data-test="page-size-50" @click="$emit('update:pageSize', 50)">50</button>
    </div>
  `,
}

const IconStub = {
  props: ['name'],
  template: '<span data-test="icon">{{ name }}</span>',
}

const ConfirmDialogStub = {
  name: 'ConfirmDialog',
  props: ['show', 'loading'],
  emits: ['confirm', 'cancel'],
  template: `
    <div v-if="show" data-test="delete-dialog">
      <button data-test="confirm-delete" :disabled="loading" @click="$emit('confirm')">confirm</button>
    </div>
  `,
}

const BaseDialogStub = {
  name: 'BaseDialog',
  props: ['show', 'title'],
  template: '<div v-if="show" role="dialog"><h3>{{ title }}</h3><slot /><slot name="footer" /></div>',
}

const EmptyStateStub = {
  name: 'EmptyState',
  props: ['title', 'actionText'],
  emits: ['action'],
  template: '<div data-test="keys-empty-state"><span>{{ title }}</span><button data-test="empty-create" @click="$emit(\'action\')">{{ actionText }}</button></div>',
}

const wrappers: VueWrapper[] = []
const mountView = async () => {
  const wrapper = mount(KeysView, {
    global: {
      stubs: {
        AppLayout: AppLayoutStub,
        Pagination: PaginationStub,
        BaseDialog: BaseDialogStub,
        ConfirmDialog: ConfirmDialogStub,
        EmptyState: EmptyStateStub,
        Select: SelectStub,
        SearchInput: SearchInputStub,
        Icon: IconStub,
        EndpointPopover: true,
        GroupBadge: true,
        GroupOptionItem: true,
        Teleport: true,
      },
    },
  })
  await flushPromises()
  await nextTick()
  wrappers.push(wrapper)
  return wrapper
}

const getButtonByText = (wrapper: VueWrapper, text: string) => {
  const button = wrapper.findAll('button').find((item) => item.text().includes(text))
  if (!button) {
    throw new Error(`Button not found: ${text}`)
  }
  return button
}

const findSelect = (wrapper: VueWrapper, dataTest: string) =>
  wrapper.findAllComponents({ name: 'Select' }).find((select) => select.attributes('data-test') === dataTest)!

const openFieldMenu = async (wrapper: VueWrapper) => {
  await wrapper.get('[data-test="keys-field-menu-button"]').trigger('click')
  return wrapper.get('[data-test="keys-field-menu"]')
}

const pickFormGroup = async (wrapper: VueWrapper, groupId: number) => {
  await wrapper.get('[data-tour="key-form-group"]').trigger('click')
  await wrapper.get(`[data-test="key-group-option-${groupId}"]`).trigger('click')
  await nextTick()
}

describe('user KeysView', () => {
  beforeEach(() => {
    localStorage.clear()

    listKeys.mockReset()
    getPublicSettings.mockReset()
    getUserEndpointSettings.mockReset()
    getDashboardApiKeysUsage.mockReset()
    getAvailableGroups.mockReset()
    getUserGroupRates.mockReset()
    showError.mockReset()
    showSuccess.mockReset()
    copyToClipboard.mockReset()
    isCurrentStep.mockReset()
    nextStep.mockReset()
    deleteKey.mockReset()
    createKey.mockReset()
    updateKey.mockReset()
    toggleStatus.mockReset()

    listKeys.mockResolvedValue(page([createApiKey()]))
    getPublicSettings.mockResolvedValue({})
    getUserEndpointSettings.mockResolvedValue({ custom_endpoints: [] })
    getDashboardApiKeysUsage.mockResolvedValue({ stats: { 1: usageFor(1) } })
    getAvailableGroups.mockResolvedValue([])
    getUserGroupRates.mockResolvedValue({})
    isCurrentStep.mockReturnValue(false)
    deleteKey.mockResolvedValue({ message: 'deleted' })
    updateKey.mockResolvedValue(createApiKey())
    toggleStatus.mockResolvedValue({ ...createApiKey(), status: 'inactive' })
  })

  afterEach(() => {
    wrappers.splice(0).forEach((wrapper) => wrapper.unmount())
    vi.useRealTimers()
  })

  it('recovers a temporary list failure before showing an error', async () => {
    vi.useFakeTimers()
    listKeys.mockRejectedValueOnce({ status: 503 })
    const wrapper = await mountView()

    expect(showError).not.toHaveBeenCalled()
    await vi.advanceTimersByTimeAsync(500)
    await flushPromises()

    expect(listKeys).toHaveBeenCalledTimes(2)
    expect(wrapper.text()).toContain('test-key')
    expect(showError).not.toHaveBeenCalled()
    expect(wrapper.get('button[title="Refresh"]').element).toHaveProperty('disabled', false)
  })

  it('reports a persistent list failure after bounded retries and offers a reload', async () => {
    vi.useFakeTimers()
    listKeys.mockRejectedValue({ status: 503 })
    const wrapper = await mountView()
    await vi.advanceTimersByTimeAsync(1500)
    await flushPromises()

    expect(listKeys).toHaveBeenCalledTimes(3)
    expect(showError).toHaveBeenCalledExactlyOnceWith('keys.failedToLoad')
    expect(wrapper.get('button[title="Refresh"]').element).toHaveProperty('disabled', false)
    const failed = wrapper.get('[data-test="keys-load-failed"]')
    expect(failed.text()).toContain('keys.loadFailedHint')
    expect(wrapper.find('[data-test="keys-empty-state"]').exists()).toBe(false)

    listKeys.mockResolvedValue(page([createApiKey()]))
    await getButtonByText(wrapper, 'keys.reload').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-test="keys-load-failed"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="key-card-1"]').exists()).toBe(true)
  })

  it('keeps non-transient list errors visible without retrying them', async () => {
    listKeys.mockRejectedValueOnce({ status: 400 })
    const wrapper = await mountView()

    expect(listKeys).toHaveBeenCalledTimes(1)
    expect(showError).toHaveBeenCalledExactlyOnceWith('keys.failedToLoad')
    expect(wrapper.find('[data-test="keys-load-failed"]').exists()).toBe(true)
  })

  it('ignores a superseded request that rejects after the replacement succeeds', async () => {
    let rejectOld!: (error: unknown) => void
    listKeys.mockReturnValueOnce(new Promise((_, reject) => { rejectOld = reject }))
    const wrapper = await mountView()
    const oldSignal = listKeys.mock.calls[0]![3].signal as AbortSignal

    await wrapper.get('[data-test="keys-sort-order"]').trigger('click')
    await flushPromises()
    expect(oldSignal.aborted).toBe(true)
    expect(wrapper.text()).toContain('test-key')

    rejectOld({ status: 500 })
    await flushPromises()
    expect(showError).not.toHaveBeenCalled()
    expect(listKeys).toHaveBeenCalledTimes(2)
  })

  it('ignores a pending request error after leaving the page', async () => {
    let rejectPending!: (error: unknown) => void
    listKeys.mockReturnValueOnce(new Promise((_, reject) => { rejectPending = reject }))
    const wrapper = await mountView()
    const signal = listKeys.mock.calls[0]![3].signal as AbortSignal

    wrapper.unmount()
    expect(signal.aborted).toBe(true)
    rejectPending({ status: 500 })
    await flushPromises()

    expect(showError).not.toHaveBeenCalled()
    expect(listKeys).toHaveBeenCalledTimes(1)
  })

  it('cancels a scheduled list retry when leaving the page', async () => {
    vi.useFakeTimers()
    listKeys.mockRejectedValueOnce({ status: 503 })
    const wrapper = await mountView()

    wrapper.unmount()
    await vi.advanceTimersByTimeAsync(1500)
    await flushPromises()

    expect(listKeys).toHaveBeenCalledTimes(1)
    expect(showError).not.toHaveBeenCalled()
  })

  it('shows authenticated custom endpoints when the public settings omit them', async () => {
    const customEndpoints = [{ name: 'Relay', endpoint: 'https://relay.example.test/v1', description: 'For signed-in users' }]
    getUserEndpointSettings.mockResolvedValue({ custom_endpoints: customEndpoints })
    const wrapper = await mountView()

    expect(getUserEndpointSettings).toHaveBeenCalledTimes(1)
    const endpoints = wrapper.getComponent({ name: 'EndpointPopover' })
    expect(endpoints.props('customEndpoints')).toEqual(customEndpoints)
    expect(endpoints.props('apiBaseUrl')).toBe('')
  })

  it('locks the delete confirmation while the request is in flight and drops the card', async () => {
    let resolveDelete!: (value: { message: string }) => void
    deleteKey.mockReturnValue(new Promise((resolve) => { resolveDelete = resolve }))
    const wrapper = await mountView()

    await wrapper.get('[data-test="key-action-delete"]').trigger('click')
    const confirm = wrapper.get('[data-test="confirm-delete"]')
    await confirm.trigger('click')
    await confirm.trigger('click')
    await nextTick()

    expect(deleteKey).toHaveBeenCalledTimes(1)
    expect((confirm.element as HTMLButtonElement).disabled).toBe(true)

    let resolveReload!: (value: unknown) => void
    listKeys.mockReturnValueOnce(new Promise((resolve) => { resolveReload = resolve }))
    resolveDelete({ message: 'deleted' })
    await flushPromises()
    expect(wrapper.find('[data-test="delete-dialog"]').exists()).toBe(false)
    expect(showSuccess).toHaveBeenCalled()
    // Gone before the reload answers.
    expect(wrapper.find('[data-test="key-card-1"]').exists()).toBe(false)
    resolveReload(page([]))
    await flushPromises()
  })

  it('steps back a page when the last key on the last page is deleted', async () => {
    listKeys.mockResolvedValue(page([createApiKey()], 21, 1, 2))
    const wrapper = await mountView()
    await wrapper.get('[data-test="page-2"]').trigger('click')
    await flushPromises()
    expect(listKeys).toHaveBeenLastCalledWith(2, 20, expect.anything(), expect.anything())

    listKeys.mockReset()
    listKeys
      .mockResolvedValueOnce(page([], 20, 2, 1))
      .mockResolvedValueOnce(page([createApiKey()], 20, 1, 1))
    await wrapper.get('[data-test="key-action-delete"]').trigger('click')
    await wrapper.get('[data-test="confirm-delete"]').trigger('click')
    await flushPromises()

    expect(listKeys.mock.calls.map(([pageNumber]) => pageNumber)).toEqual([2, 1])
    expect(wrapper.find('[data-test="key-card-1"]').exists()).toBe(true)
  })

  it('shows a skeleton on the first load and keeps the cards on screen during a reload', async () => {
    let resolveFirst!: (value: unknown) => void
    listKeys.mockReturnValueOnce(new Promise((resolve) => { resolveFirst = resolve }))
    const wrapper = await mountView()

    expect(wrapper.find('[data-test="keys-skeleton"]').exists()).toBe(true)
    expect(wrapper.findAll('.keys-skeleton-card')).toHaveLength(6)
    resolveFirst(page([createApiKey()]))
    await flushPromises()
    expect(wrapper.find('[data-test="keys-skeleton"]').exists()).toBe(false)

    let resolveReload!: (value: unknown) => void
    listKeys.mockReturnValueOnce(new Promise((resolve) => { resolveReload = resolve }))
    await wrapper.get('button[title="Refresh"]').trigger('click')
    await nextTick()
    expect(wrapper.find('[data-test="keys-skeleton"]').exists()).toBe(false)
    expect(wrapper.get('[data-test="keys-grid"]').classes()).toContain('is-refreshing')
    expect(wrapper.find('[data-test="key-card-1"]').exists()).toBe(true)

    resolveReload(page([createApiKey()]))
    await flushPromises()
    expect(wrapper.get('[data-test="keys-grid"]').classes()).not.toContain('is-refreshing')
  })

  it('shows spend as pending until the usage batch answers', async () => {
    let resolveUsage!: (value: unknown) => void
    getDashboardApiKeysUsage.mockReturnValueOnce(new Promise((resolve) => { resolveUsage = resolve }))
    const wrapper = await mountView()

    expect(wrapper.get('[data-test="key-today-cost"]').text()).toBe('—')
    resolveUsage({ stats: { 1: usageFor(1, { today_actual_cost: 1.5, total_actual_cost: 2 }) } })
    await flushPromises()
    expect(wrapper.get('[data-test="key-today-cost"]').text()).toBe('$1.5000')
    expect(wrapper.get('[data-test="key-total-cost"]').text()).toBe('$2.0000')
  })

  it('tells filtered-out results apart from having no keys, and clears the filters', async () => {
    const wrapper = await mountView()
    listKeys.mockResolvedValue(page([]))

    await wrapper.findComponent({ name: 'SearchInput' }).vm.$emit('update:modelValue', 'nothing')
    await wrapper.findComponent({ name: 'SearchInput' }).vm.$emit('search')
    await flushPromises()
    expect(wrapper.find('[data-test="keys-no-match"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="keys-empty-state"]').exists()).toBe(false)

    await wrapper.get('[data-test="keys-clear-filters"]').trigger('click')
    await flushPromises()
    expect(listKeys).toHaveBeenLastCalledWith(
      1, 20, { sort_by: 'created_at', sort_order: 'desc' }, expect.objectContaining({ signal: expect.any(AbortSignal) })
    )
    expect(wrapper.find('[data-test="keys-no-match"]').exists()).toBe(false)
    expect(wrapper.get('[data-test="keys-empty-state"]').text()).toContain('keys.noKeysYet')

    await wrapper.get('[data-test="empty-create"]').trigger('click')
    expect(wrapper.get('[role="dialog"] h3').text()).toBe('Create API Key')
  })

  it('shows the default card fields with the ID and last used IP hidden', async () => {
    const wrapper = await mountView()

    expect(wrapper.find('[data-test="key-id"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="last-used-ip"]').exists()).toBe(false)
    expect(wrapper.get('[data-test="current-concurrency"]').text()).toBe('3')
    expect(wrapper.get('[data-test="key-last-used"]').text()).toBe('keys.neverUsed')
    expect(wrapper.find('[data-test="failover-status-1"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="key-expiry"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="key-token-mix"]').exists()).toBe(true)

    const menu = await openFieldMenu(wrapper)
    const checked = menu.findAll('[role="menuitemcheckbox"]')
      .filter((item) => item.attributes('aria-checked') === 'false')
      .map((item) => item.attributes('data-test'))
    expect(checked).toEqual(['keys-field-id', 'keys-field-last_used_ip'])
  })

  it('shows a hidden field when toggled and remembers the choice', async () => {
    listKeys.mockResolvedValue(page([{ ...createApiKey(), last_used_ip: '203.0.113.10' }]))
    const wrapper = await mountView()

    const menu = await openFieldMenu(wrapper)
    await menu.get('[data-test="keys-field-last_used_ip"]').trigger('click')
    expect(wrapper.get('[data-test="last-used-ip"]').text()).toBe('203.0.113.10')
    expect(localStorage.getItem('api-key-card-hidden-fields')).toBe(JSON.stringify(['id']))

    await menu.get('[data-test="keys-field-id"]').trigger('click')
    expect(wrapper.get('[data-test="key-id"]').text()).toBe('#1')
    await menu.get('[data-test="keys-field-tokens"]').trigger('click')
    expect(wrapper.find('[data-testid="key-token-mix"]').exists()).toBe(false)
    expect(localStorage.getItem('api-key-card-hidden-fields')).toBe(JSON.stringify(['tokens']))
  })

  it('restores card fields on mount and drops the old table column settings', async () => {
    localStorage.setItem('api-key-card-hidden-fields', JSON.stringify(['tokens', 'created_at']))
    localStorage.setItem('api-key-hidden-columns', JSON.stringify(['group', 'created_at']))
    localStorage.setItem('api-key-column-settings-version', '3')
    const wrapper = await mountView()

    expect(wrapper.get('[data-test="key-id"]').text()).toBe('#1')
    expect(wrapper.find('[data-testid="key-token-mix"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('Created')
    expect(localStorage.getItem('api-key-hidden-columns')).toBeNull()
    expect(localStorage.getItem('api-key-column-settings-version')).toBeNull()
  })

  it('lists only the optional parts of a card in the field menu and closes it on an outside click', async () => {
    const wrapper = await mountView()
    const menu = await openFieldMenu(wrapper)

    const labels = menu.findAll('[role="menuitemcheckbox"]').map((item) => item.get('span').text())
    expect(labels).toEqual(expect.arrayContaining(['ID', 'Current Concurrency', 'Rate Limit', 'Last Used IP', 'Token mix', 'Fallback Group']))
    expect(labels).not.toContain('Name')
    expect(labels).not.toContain('Actions')
    expect(labels).not.toContain('API Key')

    document.body.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    await nextTick()
    expect(wrapper.find('[data-test="keys-field-menu"]').exists()).toBe(false)
  })

  it('sorts from the sort control, starting each field in its natural direction, and remembers it', async () => {
    const wrapper = await mountView()
    listKeys.mockClear()

    findSelect(wrapper, 'keys-sort-field').vm.$emit('update:modelValue', 'name')
    await flushPromises()
    expect(listKeys).toHaveBeenLastCalledWith(1, 20, { sort_by: 'name', sort_order: 'asc' }, expect.anything())

    await wrapper.get('[data-test="keys-sort-order"]').trigger('click')
    await flushPromises()
    expect(listKeys).toHaveBeenLastCalledWith(1, 20, { sort_by: 'name', sort_order: 'desc' }, expect.anything())
    expect(JSON.parse(localStorage.getItem('api-key-sort')!)).toEqual({ sort_by: 'name', sort_order: 'desc' })

    // Picking the field that is already active changes nothing.
    listKeys.mockClear()
    findSelect(wrapper, 'keys-sort-field').vm.$emit('update:modelValue', 'name')
    await flushPromises()
    expect(listKeys).not.toHaveBeenCalled()

    const remounted = await mountView()
    expect(listKeys).toHaveBeenLastCalledWith(1, 20, { sort_by: 'name', sort_order: 'desc' }, expect.anything())
    expect(findSelect(remounted, 'keys-sort-field').props('modelValue')).toBe('name')
  })

  it('keeps filters and selected page size when sorting by current concurrency', async () => {
    getAvailableGroups.mockResolvedValue([{ id: 42, name: 'OpenAI' }])
    const wrapper = await mountView()

    await wrapper.get('[data-test="page-size-50"]').trigger('click')
    await flushPromises()

    await wrapper.findComponent({ name: 'SearchInput' }).vm.$emit('update:modelValue', 'target')
    await wrapper.findComponent({ name: 'SearchInput' }).vm.$emit('search')
    await flushPromises()

    const selects = wrapper.findAllComponents({ name: 'Select' })
    await selects[0].vm.$emit('update:modelValue', 42)
    await flushPromises()
    await selects[1].vm.$emit('update:modelValue', 'active')
    await flushPromises()

    listKeys.mockClear()

    findSelect(wrapper, 'keys-sort-field').vm.$emit('update:modelValue', 'current_concurrency')
    await flushPromises()

    expect(listKeys).toHaveBeenLastCalledWith(
      1,
      50,
      {
        search: 'target',
        status: 'active',
        group_id: 42,
        sort_by: 'current_concurrency',
        sort_order: 'desc',
      },
      expect.objectContaining({ signal: expect.any(AbortSignal) })
    )
  })

  it('disables a key on its card at once and ignores repeat clicks while saving', async () => {
    let resolveToggle!: (value: unknown) => void
    toggleStatus.mockReturnValueOnce(new Promise((resolve) => { resolveToggle = resolve }))
    const wrapper = await mountView()
    const toggle = wrapper.get('[data-test="key-action-toggle"]')

    await toggle.trigger('click')
    await toggle.trigger('click')
    expect(toggleStatus).toHaveBeenCalledExactlyOnceWith(1, 'inactive')
    expect((toggle.element as HTMLButtonElement).disabled).toBe(true)

    let resolveReload!: (value: unknown) => void
    listKeys.mockReturnValueOnce(new Promise((resolve) => { resolveReload = resolve }))
    resolveToggle({ ...createApiKey(), status: 'inactive' })
    await flushPromises()
    // The card flips before the list reload answers.
    expect(wrapper.get('[data-test="key-status"]').text()).toBe('Inactive')
    expect(wrapper.get('[data-test="key-action-toggle"] .key-action-label').text()).toBe('keys.enable')
    expect(showSuccess).toHaveBeenCalledWith('keys.keyDisabledSuccess')
    resolveReload(page([{ ...createApiKey(), status: 'inactive' }]))
    await flushPromises()
  })

  it('keeps list keys masked and removes copy, use, and import actions', async () => {
    const maskedKey = 'sk-123...cdef'
    listKeys.mockResolvedValue(page([{ ...createApiKey(), key: maskedKey }]))
    const wrapper = await mountView()

    expect(wrapper.get('[data-test="key-secret"]').text()).toContain(maskedKey)
    expect(wrapper.find('[data-test="key-copy"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="key-action-use"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="key-action-ccs"]').exists()).toBe(false)
    expect(copyToClipboard).not.toHaveBeenCalled()
  })

  it('shows and copies the raw key only in the successful creation dialog', async () => {
    const newKey = { ...createApiKey(), key: 'newly-created-secret-value' }
    createKey.mockResolvedValue(newKey)
    getAvailableGroups.mockResolvedValue([group(4)])
    copyToClipboard.mockResolvedValue(true)
    const wrapper = await mountView()

    await wrapper.get('[data-tour="keys-create-btn"]').trigger('click')
    await wrapper.get('[data-tour="key-form-name"]').setValue('new-key')
    await pickFormGroup(wrapper, 4)
    await wrapper.get('#key-form').trigger('submit')
    await flushPromises()

    expect(wrapper.get('[data-test="created-api-key"]').element).toHaveProperty('value', newKey.key)
    expect(wrapper.text()).toContain('Copy API key')
    expect(copyToClipboard).not.toHaveBeenCalled()

    await wrapper.get('[data-test="copy-created-api-key"]').trigger('click')
    expect(copyToClipboard).toHaveBeenCalledWith(newKey.key, 'keys.copied')

    await getButtonByText(wrapper, 'common.close').trigger('click')
    expect(wrapper.find('[data-test="created-api-key"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain(newKey.key)
  })

  it('renders the current concurrency value', async () => {
    const wrapper = await mountView()

    expect(wrapper.get('[data-test="current-concurrency"]').text()).toBe('3')
  })

  const group = (id: number, platform: Group['platform'] = 'openai', overrides: Partial<Group> = {}): Group => ({
    id,
    name: `Group ${id}`,
    description: null,
    platform,
    status: 'active',
    subscription_type: 'standard',
    rate_multiplier: 2,
    peak_rate_enabled: false,
    peak_start: '',
    peak_end: '',
    peak_rate_multiplier: 1,
    ...overrides,
  } as Group)

  const setKey = (key: ApiKey, availableGroups: Group[]) => {
    listKeys.mockResolvedValue({ items: [key], total: 1, page: 1, page_size: 20, pages: 1 })
    getAvailableGroups.mockResolvedValue(availableGroups)
  }

  it('caps long group names so a card cannot stretch the page sideways', async () => {
    const longName = '超长分组名称测试高速稳定专线'.repeat(7)
    const key = { ...createApiKey(), group_id: 1, group: group(1, 'openai', { name: longName }) }
    setKey(key, [group(1, 'openai', { name: longName })])
    const wrapper = await mountView()

    const button = wrapper.get('[data-test="key-group-button-1"]')
    const badge = button.getComponent(GroupBadge)
    // 名称原样传给徽章，由徽章按字数省略并在悬停时显示全名
    expect(badge.props('name')).toBe(longName)
    expect(badge.props('maxNameLength')).toBe(20)
    // 宽度约束兜底：徽章可以收缩，且不超过卡片里分组按钮的宽度
    expect(badge.classes()).toEqual(expect.arrayContaining(['min-w-0', 'max-w-full']))
  })

  it('marks a key whose group the user is blocked from', async () => {
    const key = { ...createApiKey(), group_id: 3, group: group(3) }
    setKey(key, [group(3, 'openai', { is_blocked_for_user: true })])
    const wrapper = await mountView()

    expect(wrapper.get('[data-test="key-group-button-1"]').text()).toContain('common.groupBlocked')
  })

  it('fills the key form from the platform-filtered group picker', async () => {
    setKey({ ...createApiKey(), group_id: 1, group: group(1) }, [
      group(1), group(2, 'anthropic', { name: 'Claude Max' }), group(3, 'grok'), group(4, 'custom')
    ])
    const wrapper = await mountView()
    await getButtonByText(wrapper, 'Create API Key').trigger('click')

    const field = wrapper.get('[data-tour="key-form-group"]')
    expect(field.text()).toContain('keys.selectGroup')
    await field.trigger('click')
    const picker = wrapper.get('[data-test="key-group-picker"]')
    expect(picker.get('h2').text()).toBe('keys.selectGroup')
    expect(field.attributes('aria-expanded')).toBe('true')

    await picker.get('[data-test="key-group-platform-anthropic"]').trigger('click')
    expect(picker.findAll('[role="option"]').map((option) => option.attributes('data-test')))
      .toEqual(['key-group-option-2'])
    await picker.get('[data-test="key-group-option-2"]').trigger('click')
    await nextTick()

    expect(wrapper.find('[data-test="key-group-picker"]').exists()).toBe(false)
    expect(field.attributes('aria-expanded')).toBe('false')
    expect(field.getComponent(GroupBadge).props('name')).toBe('Claude Max')
    expect(updateKey).not.toHaveBeenCalled()
  })

  it('rebinds a key from its card through the picker, skipping blocked and unchanged groups', async () => {
    const key = { ...createApiKey(), name: 'prod-key', group_id: 1, group: group(1) }
    setKey(key, [group(1), group(2, 'gemini'), group(3, 'openai', { is_blocked_for_user: true })])
    updateKey.mockResolvedValue({ ...key, group_id: 2 })
    const wrapper = await mountView()

    await wrapper.get('[data-test="key-group-button-1"]').trigger('click')
    const picker = wrapper.get('[data-test="key-group-picker"]')
    expect(picker.get('h2').text()).toBe('keys.groupPicker.changeTitle')
    expect(picker.text()).toContain('keys.groupPicker.forKey')
    expect(picker.get('[data-test="key-group-option-1"]').attributes('aria-selected')).toBe('true')

    await picker.get('[data-test="key-group-option-3"]').trigger('click')
    expect(picker.get('[data-test="key-group-option-3"]').attributes('aria-disabled')).toBe('true')
    expect(wrapper.find('[data-test="key-group-picker"]').exists()).toBe(true)

    await picker.get('[data-test="key-group-option-1"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-test="key-group-picker"]').exists()).toBe(false)
    expect(updateKey).not.toHaveBeenCalled()

    await wrapper.get('[data-test="key-group-button-1"]').trigger('click')
    await wrapper.get('[data-test="key-group-option-2"]').trigger('click')
    await flushPromises()
    expect(updateKey).toHaveBeenCalledExactlyOnceWith(1, { group_id: 2 })
    expect(showSuccess).toHaveBeenCalled()
    expect(wrapper.find('[data-test="key-group-picker"]').exists()).toBe(false)
  })

  it('shows green health when configured but not cooling', async () => {
    const key = { ...createApiKey(), group_id: 1, group: group(1), failover_enabled: true, failover_group_id: 2 }
    setKey(key, [group(1), group(2)])
    const wrapper = await mountView()
    expect(wrapper.get('[data-test="failover-status-1"]').text()).toBe('Primary Group Healthy')
    expect(wrapper.get('[data-test="failover-status-1"]').classes()).toContain('text-emerald-700')
    expect(wrapper.get('[data-test="failover-status-1"]').classes()).toContain('dark:text-emerald-300')
    expect((wrapper.get('[data-test="failover-status-1"]').element as HTMLButtonElement).disabled).toBe(false)

  })

  it.each(['custom', 'composite'] as const)('disables failover configuration for the Custom wire platform %s', async (platform) => {
    const key = { ...createApiKey(), group_id: 1, group: group(1, platform) }
    setKey(key, [group(1, platform), group(2, platform)])
    const wrapper = await mountView()
    expect((wrapper.get('[data-test="failover-status-1"]').element as HTMLButtonElement).disabled).toBe(true)
    expect(wrapper.get('[data-test="failover-status-1"]').text()).toBe('Disabled')
    expect(wrapper.get('[data-test="failover-status-1"]').classes()).toContain('text-gray-500')
    await getButtonByText(wrapper, 'common.edit').trigger('click')
    expect(wrapper.find('[data-test="failover-toggle"]').exists()).toBe(false)
  })

  it('offers custom system prompt configuration only in the edit dialog', async () => {
    const key = { ...createApiKey(), group_id: 1, group: group(1) }
    setKey(key, [group(1), group(2)])
    const wrapper = await mountView()
    await getButtonByText(wrapper, 'Create API Key').trigger('click')
    expect(wrapper.find('[data-test="system-prompt-settings"]').exists()).toBe(false)
    await getButtonByText(wrapper, 'common.cancel').trigger('click')
    await getButtonByText(wrapper, 'common.edit').trigger('click')

    const toggle = wrapper.get('[data-test="system-prompt-toggle"]')
    expect(toggle.attributes('aria-checked')).toBe('false')
    expect(wrapper.find('[data-test="system-prompt-controls"]').exists()).toBe(false)
    const sections = wrapper.findAll('[data-test="failover-settings"], [data-test="system-prompt-settings"]')
    expect(sections.map((section) => section.attributes('data-test'))).toEqual(['failover-settings', 'system-prompt-settings'])
  })

  it('saves the custom system prompt and protocol mode, and restores them when editing again', async () => {
    let key: ApiKey = { ...createApiKey(), group_id: 1, group: group(1) }
    getAvailableGroups.mockResolvedValue([group(1)])
    listKeys.mockImplementation(async () => ({ items: [key], total: 1, page: 1, page_size: 20, pages: 1 }))
    updateKey.mockImplementation(async (_id, updates) => { key = { ...key, ...updates }; return key })
    const wrapper = await mountView()
    await getButtonByText(wrapper, 'common.edit').trigger('click')
    await wrapper.get('[data-test="system-prompt-toggle"]').trigger('click')
    const forceToggle = wrapper.get('[data-test="system-prompt-force-toggle"]')
    expect(forceToggle.attributes('aria-checked')).toBe('false')
    await wrapper.get('[data-test="system-prompt-input"]').setValue('  Keep agent tools available.\nUse concise replies.  ')
    await forceToggle.trigger('click')
    expect(forceToggle.attributes('aria-checked')).toBe('true')
    await wrapper.get('#key-form').trigger('submit')
    await flushPromises()

    expect(updateKey).toHaveBeenLastCalledWith(1, expect.objectContaining({
      custom_system_prompt_enabled: true,
      custom_system_prompt_force: true,
      custom_system_prompt: '  Keep agent tools available.\nUse concise replies.  '
    }))
    await getButtonByText(wrapper, 'common.edit').trigger('click')
    expect(wrapper.get('[data-test="system-prompt-toggle"]').attributes('aria-checked')).toBe('true')
    expect(wrapper.get('[data-test="system-prompt-force-toggle"]').attributes('aria-checked')).toBe('true')
    expect(wrapper.get('[data-test="system-prompt-input"]').element).toHaveProperty('value', key.custom_system_prompt)
  })

  it('retains stored prompt content and protocol choice when custom prompts are disabled', async () => {
    const key = {
      ...createApiKey(), group_id: 1, group: group(1),
      custom_system_prompt_enabled: true, custom_system_prompt_force: true, custom_system_prompt: 'Saved prompt'
    }
    setKey(key, [group(1)])
    const wrapper = await mountView()
    await getButtonByText(wrapper, 'common.edit').trigger('click')
    await wrapper.get('[data-test="system-prompt-toggle"]').trigger('click')
    expect(wrapper.find('[data-test="system-prompt-controls"]').exists()).toBe(false)
    await wrapper.get('#key-form').trigger('submit')
    await flushPromises()
    expect(updateKey).toHaveBeenLastCalledWith(1, expect.objectContaining({
      custom_system_prompt_enabled: false,
      custom_system_prompt_force: true,
      custom_system_prompt: 'Saved prompt'
    }))
  })

  it('rejects an enabled whitespace-only prompt and oversized UTF-8 content before saving', async () => {
    const key = { ...createApiKey(), group_id: 1, group: group(1) }
    setKey(key, [group(1)])
    const wrapper = await mountView()
    await getButtonByText(wrapper, 'common.edit').trigger('click')
    await wrapper.get('[data-test="system-prompt-toggle"]').trigger('click')
    await wrapper.get('[data-test="system-prompt-input"]').setValue(' \n ')
    await wrapper.get('#key-form').trigger('submit')
    expect(updateKey).not.toHaveBeenCalled()
    expect(showError).toHaveBeenLastCalledWith('keys.customSystemPrompt.required')

    await wrapper.get('[data-test="system-prompt-input"]').setValue('界'.repeat(10923))
    await wrapper.get('#key-form').trigger('submit')
    expect(updateKey).not.toHaveBeenCalled()
    expect(showError).toHaveBeenLastCalledWith('keys.customSystemPrompt.tooLong')
  })

  it('renders active failover in themed red and restores green when cooldown expires', async () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-10-01T00:00:00Z'))
    const key = {
      ...createApiKey(), group_id: 1, group: group(1), failover_enabled: true,
      failover_group_id: 2, failover_cooldown_until: '2026-10-01T00:00:02Z'
    }
    setKey(key, [group(1), group(2)])
    const wrapper = await mountView()
    expect(wrapper.get('[data-test="failover-status-1"]').text()).toBe('Fallback Group Active')
    expect(wrapper.get('[data-test="failover-status-1"]').classes()).toContain('dark:text-red-300')
    await vi.advanceTimersByTimeAsync(2000)
    expect(wrapper.get('[data-test="failover-status-1"]').text()).toBe('Primary Group Healthy')
    expect(key.group_id).toBe(1)
  })

  it('offers only allowed, active groups on the primary platform with subscription and balance choices', async () => {
    const key = { ...createApiKey(), group_id: 1, group: group(1) }
    setKey(key, [
      group(1), group(2), group(3, 'openai', { subscription_type: 'subscription' }),
      group(4, 'anthropic'), group(5, 'openai', { is_blocked_for_user: true }),
      group(6, 'openai', { status: 'inactive' }), group(7, 'custom')
    ])
    const wrapper = await mountView()
    expect(wrapper.get('[data-test="failover-status-1"]').text()).toBe('Disabled')
    expect(wrapper.get('[data-test="failover-status-1"]').classes()).toContain('text-gray-500')
    expect(wrapper.get('[data-test="failover-status-1"]').classes()).toContain('dark:text-dark-400')
    await wrapper.get('[data-test="failover-status-1"]').trigger('click')
    await wrapper.get('[data-test="failover-toggle"]').trigger('click')
    const selector = wrapper.findAllComponents({ name: 'Select' }).find((select) => select.attributes('data-test') === 'failover-group')!
    expect(selector.props('options').map((option: { value: number }) => option.value)).toEqual([2, 3])
    expect(wrapper.get('[data-test="failover-max-retries"]').element).toHaveProperty('value', '3')
    expect(wrapper.get('[data-test="failover-cooldown"]').element).toHaveProperty('value', '300')
  })

  it.each(['anthropic', 'gemini', 'grok', 'antigravity'] as const)('allows %s failover settings with only eligible groups on that platform', async (platform) => {
    const key = { ...createApiKey(), group_id: 1, group: group(1, platform) }
    setKey(key, [
      group(1, platform), group(2, platform), group(3, platform, { subscription_type: 'subscription' }),
      group(4, 'openai'), group(5, platform, { is_blocked_for_user: true }),
      group(6, platform, { status: 'inactive' }), group(7, 'custom'), group(8, 'composite')
    ])
    const wrapper = await mountView()
    expect((wrapper.get('[data-test="failover-status-1"]').element as HTMLButtonElement).disabled).toBe(false)
    await wrapper.get('[data-test="failover-status-1"]').trigger('click')
    await wrapper.get('[data-test="failover-toggle"]').trigger('click')
    const selector = wrapper.findAllComponents({ name: 'Select' }).find((select) => select.attributes('data-test') === 'failover-group')!
    expect(selector.props('options').map((option: { value: number }) => option.value)).toEqual([2, 3])
    expect(selector.props('options').every((option: { platform: string }) => option.platform === platform)).toBe(true)
    selector.vm.$emit('update:modelValue', 2)
    await nextTick()
    await wrapper.get('#failover-form').trigger('submit')
    await flushPromises()
    expect(updateKey).toHaveBeenLastCalledWith(1, {
      failover_enabled: true, failover_group_id: 2, failover_max_retries: 3, failover_cooldown_seconds: 300
    })
  })

  it('synchronizes saved settings between the failover dialog and edit form without replacing the main group', async () => {
    let key: ApiKey = { ...createApiKey(), group_id: 1, group: group(1) }
    getAvailableGroups.mockResolvedValue([group(1), group(2)])
    listKeys.mockImplementation(async () => ({ items: [key], total: 1, page: 1, page_size: 20, pages: 1 }))
    updateKey.mockImplementation(async (_id, updates) => { key = { ...key, ...updates }; return key })
    const wrapper = await mountView()
    await wrapper.get('[data-test="failover-status-1"]').trigger('click')
    await wrapper.get('[data-test="failover-toggle"]').trigger('click')
    const selector = wrapper.findAllComponents({ name: 'Select' }).find((select) => select.attributes('data-test') === 'failover-group')!
    selector.vm.$emit('update:modelValue', 2)
    await nextTick()
    await wrapper.get('[data-test="failover-max-retries"]').setValue('4')
    await wrapper.get('[data-test="failover-cooldown"]').setValue('600')
    await wrapper.get('#failover-form').trigger('submit')
    await flushPromises()
    expect(updateKey).toHaveBeenLastCalledWith(1, {
      failover_enabled: true, failover_group_id: 2, failover_max_retries: 4, failover_cooldown_seconds: 600
    })
    expect(key.group_id).toBe(1)
    await getButtonByText(wrapper, 'common.edit').trigger('click')
    expect(wrapper.get('[data-test="failover-max-retries"]').element).toHaveProperty('value', '4')
    expect(wrapper.get('[data-test="failover-cooldown"]').element).toHaveProperty('value', '600')
    await wrapper.get('[data-test="failover-max-retries"]').setValue('2')
    await wrapper.get('#key-form').trigger('submit')
    await flushPromises()
    await wrapper.get('[data-test="failover-status-1"]').trigger('click')
    expect(wrapper.get('[data-test="failover-max-retries"]').element).toHaveProperty('value', '2')
    expect(key.group_id).toBe(1)
  })

  it.each([0, 11, 1.5])('rejects invalid total retry attempts %s', async (attempts) => {
    const key = { ...createApiKey(), group_id: 1, group: group(1), failover_enabled: true, failover_group_id: 2 }
    setKey(key, [group(1), group(2)])
    const wrapper = await mountView()
    await wrapper.get('[data-test="failover-status-1"]').trigger('click')
    await wrapper.get('[data-test="failover-max-retries"]').setValue(String(attempts))
    await wrapper.get('#failover-form').trigger('submit')
    expect(updateKey).not.toHaveBeenCalled()
    expect(showError).toHaveBeenLastCalledWith('keys.failover.invalidMaxRetries')
  })

  it('clears incompatible failover settings when editing the primary platform', async () => {
    const key = { ...createApiKey(), group_id: 1, group: group(1), failover_enabled: true, failover_group_id: 2 }
    setKey(key, [group(1), group(2), group(3, 'anthropic')])
    const wrapper = await mountView()
    await getButtonByText(wrapper, 'common.edit').trigger('click')
    await pickFormGroup(wrapper, 3)
    expect(wrapper.get('[data-test="failover-toggle"]').attributes('aria-checked')).toBe('false')
    await wrapper.get('[data-test="failover-toggle"]').trigger('click')
    const failoverSelector = wrapper.findAllComponents({ name: 'Select' }).find((select) => select.attributes('data-test') === 'failover-group')!
    expect(failoverSelector.props('modelValue')).toBe(null)
    expect(failoverSelector.props('options')).toEqual([])

    await pickFormGroup(wrapper, 2)
    expect(failoverSelector.props('options').map((option: { value: number }) => option.value)).toEqual([1])
  })

  it('refreshes active failover state without overwriting an unsaved dialog draft', async () => {
    vi.useFakeTimers()
    const key = { ...createApiKey(), group_id: 1, group: group(1), failover_enabled: true, failover_group_id: 2 }
    setKey(key, [group(1), group(2)])
    const wrapper = await mountView()
    await wrapper.get('[data-test="failover-status-1"]').trigger('click')
    await wrapper.get('[data-test="failover-max-retries"]').setValue('8')
    const coolingKey = { ...key, failover_cooldown_until: new Date(Date.now() + 300000).toISOString() }
    listKeys.mockResolvedValue({ items: [coolingKey], total: 1, page: 1, page_size: 20, pages: 1 })
    await vi.advanceTimersByTimeAsync(15000)
    expect(wrapper.get('[data-test="failover-status-1"]').text()).toBe('Fallback Group Active')
    expect(wrapper.get('[data-test="failover-max-retries"]').element).toHaveProperty('value', '8')
  })

  it.each(['custom', 'composite'] as const)('disables failover when an OpenAI key is changed to Custom wire platform %s', async (platform) => {
    const key = {
      ...createApiKey(), group_id: 1, group: group(1), failover_enabled: true, failover_group_id: 2,
      failover_cooldown_until: new Date(Date.now() + 300000).toISOString()
    }
    setKey(key, [group(1), group(2), group(3, platform)])
    const wrapper = await mountView()
    await getButtonByText(wrapper, 'common.edit').trigger('click')
    await pickFormGroup(wrapper, 3)
    expect(wrapper.find('[data-test="failover-toggle"]').exists()).toBe(false)
    await wrapper.get('#key-form').trigger('submit')
    await flushPromises()
    expect(updateKey).toHaveBeenCalledWith(1, expect.objectContaining({
      group_id: 3, failover_enabled: false, failover_group_id: null
    }))
  })

  it('releases only cooldown and preserves unsaved settings in the open dialog', async () => {
    const key = {
      ...createApiKey(), group_id: 1, group: group(1), failover_enabled: true, failover_group_id: 2,
      failover_cooldown_until: new Date(Date.now() + 60000).toISOString()
    }
    setKey(key, [group(1), group(2)])
    updateKey.mockResolvedValue({ ...key, failover_cooldown_until: null })
    const wrapper = await mountView()
    await wrapper.get('[data-test="failover-status-1"]').trigger('click')
    await wrapper.get('[data-test="failover-max-retries"]').setValue('7')
    await wrapper.get('[data-test="release-failover-cooldown"]').trigger('click')
    await flushPromises()
    expect(updateKey).toHaveBeenLastCalledWith(1, { release_failover_cooldown: true })
    expect(wrapper.get('[data-test="failover-status-1"]').text()).toBe('Primary Group Healthy')
    expect(wrapper.get('[data-test="failover-max-retries"]').element).toHaveProperty('value', '7')
    expect((wrapper.get('[data-test="release-failover-cooldown"]').element as HTMLButtonElement).disabled).toBe(true)
  })
})
