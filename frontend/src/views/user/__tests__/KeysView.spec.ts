import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { nextTick } from 'vue'

import type { ApiKey, Group } from '@/types'
import KeysView from '../KeysView.vue'

const {
  listKeys,
  getPublicSettings,
  getDashboardApiKeysUsage,
  getAvailableGroups,
  getUserGroupRates,
  showError,
  showSuccess,
  copyToClipboard,
  isCurrentStep,
  nextStep,
  deleteKey,
  updateKey,
} = vi.hoisted(() => ({
  listKeys: vi.fn(),
  getPublicSettings: vi.fn(),
  getDashboardApiKeysUsage: vi.fn(),
  getAvailableGroups: vi.fn(),
  getUserGroupRates: vi.fn(),
  showError: vi.fn(),
  showSuccess: vi.fn(),
  copyToClipboard: vi.fn(),
  isCurrentStep: vi.fn(),
  nextStep: vi.fn(),
  deleteKey: vi.fn(),
  updateKey: vi.fn(),
}))

const messages: Record<string, string> = {
  'common.actions': 'Actions',
  'common.name': 'Name',
  'common.refresh': 'Refresh',
  'common.status': 'Status',
  'keys.apiKey': 'API Key',
  'keys.allGroups': 'All Groups',
  'keys.allStatus': 'All Status',
  'keys.columnSettings': 'Column Settings',
  'keys.createKey': 'Create API Key',
  'keys.created': 'Created',
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
  'keys.usage': 'Usage',
}

vi.mock('@/api', () => ({
  keysAPI: {
    list: listKeys,
    create: vi.fn(),
    update: updateKey,
    delete: deleteKey,
    toggleStatus: vi.fn(),
  },
  authAPI: {
    getPublicSettings,
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

const AppLayoutStub = {
  template: '<div><slot /></div>',
}

const TablePageLayoutStub = {
  template: `
    <div>
      <slot name="filters" />
      <slot name="actions" />
      <slot name="table" />
      <slot name="pagination" />
    </div>
  `,
}

const DataTableStub = {
  name: 'DataTable',
  props: ['columns', 'data'],
  emits: ['sort'],
  template: `
    <div>
      <div data-test="columns">{{ columns.map((col) => col.key).join(',') }}</div>
      <div data-test="columns-meta">{{ JSON.stringify(columns.map((col) => ({ key: col.key, sortable: !!col.sortable }))) }}</div>
      <button data-test="sort-current-concurrency" @click="$emit('sort', 'current_concurrency', 'asc')">
        Sort Current Concurrency
      </button>
      <div v-for="row in data" :key="row.id">
        <div
          v-if="columns.some((col) => col.key === 'id')"
          data-test="key-id"
        >
          <slot name="cell-id" :value="row.id" :row="row" />
        </div>
        <slot name="cell-name" :value="row.name" :row="row" />
        <div data-test="current-concurrency">
          <slot name="cell-current_concurrency" :value="row.current_concurrency" :row="row" />
        </div>
        <slot name="cell-failover" :row="row" />
        <div
          v-if="columns.some((col) => col.key === 'last_used_ip')"
          data-test="last-used-ip"
        >
        <slot name="cell-last_used_ip" :value="row.last_used_ip" :row="row" />
        </div>
        <slot name="cell-actions" :row="row" />
      </div>
      <slot name="empty" />
    </div>
  `,
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

const wrappers: VueWrapper[] = []
const mountView = async () => {
  const wrapper = mount(KeysView, {
    global: {
      stubs: {
        AppLayout: AppLayoutStub,
        TablePageLayout: TablePageLayoutStub,
        DataTable: DataTableStub,
        Pagination: PaginationStub,
        BaseDialog: BaseDialogStub,
        ConfirmDialog: ConfirmDialogStub,
        EmptyState: true,
        Select: SelectStub,
        SearchInput: SearchInputStub,
        Icon: IconStub,
        UseKeyModal: true,
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

const visibleColumnKeys = (wrapper: VueWrapper) =>
  wrapper.get('[data-test="columns"]').text().split(',').filter(Boolean)

const visibleColumnMeta = (wrapper: VueWrapper): Array<{ key: string; sortable: boolean }> =>
  JSON.parse(wrapper.get('[data-test="columns-meta"]').text())

const getButtonByText = (wrapper: VueWrapper, text: string) => {
  const button = wrapper.findAll('button').find((item) => item.text().includes(text))
  if (!button) {
    throw new Error(`Button not found: ${text}`)
  }
  return button
}

describe('user KeysView column settings', () => {
  beforeEach(() => {
    localStorage.clear()

    listKeys.mockReset()
    getPublicSettings.mockReset()
    getDashboardApiKeysUsage.mockReset()
    getAvailableGroups.mockReset()
    getUserGroupRates.mockReset()
    showError.mockReset()
    showSuccess.mockReset()
    copyToClipboard.mockReset()
    isCurrentStep.mockReset()
    nextStep.mockReset()
    deleteKey.mockReset()
    updateKey.mockReset()

    listKeys.mockResolvedValue({
      items: [createApiKey()],
      total: 1,
      page: 1,
      page_size: 20,
      pages: 1,
    })
    getPublicSettings.mockResolvedValue({})
    getDashboardApiKeysUsage.mockResolvedValue({ stats: {} })
    getAvailableGroups.mockResolvedValue([])
    getUserGroupRates.mockResolvedValue({})
    isCurrentStep.mockReturnValue(false)
    deleteKey.mockResolvedValue({ message: 'deleted' })
    updateKey.mockResolvedValue(createApiKey())
  })

  afterEach(() => {
    wrappers.splice(0).forEach((wrapper) => wrapper.unmount())
    vi.useRealTimers()
  })

  it('locks the delete confirmation while the request is in flight', async () => {
    let resolveDelete!: (value: { message: string }) => void
    deleteKey.mockReturnValue(new Promise((resolve) => { resolveDelete = resolve }))
    const wrapper = await mountView()

    const deleteButton = wrapper.findAll('button').find((button) => button.text().includes('common.delete'))
    expect(deleteButton).toBeTruthy()
    await deleteButton!.trigger('click')
    const confirm = wrapper.get('[data-test="confirm-delete"]')
    await confirm.trigger('click')
    await confirm.trigger('click')
    await nextTick()

    expect(deleteKey).toHaveBeenCalledTimes(1)
    expect((confirm.element as HTMLButtonElement).disabled).toBe(true)

    resolveDelete({ message: 'deleted' })
    await flushPromises()
    expect(wrapper.find('[data-test="delete-dialog"]').exists()).toBe(false)
    expect(showSuccess).toHaveBeenCalled()
  })

  it('uses the default API key columns with low-frequency columns hidden', async () => {
    const wrapper = await mountView()

    expect(visibleColumnKeys(wrapper)).toEqual([
      'name',
      'key',
      'group',
      'current_concurrency',
      'failover',
      'usage',
      'expires_at',
      'status',
      'created_at',
      'actions',
    ])
    expect(visibleColumnKeys(wrapper)).not.toContain('rate_limit')
    expect(visibleColumnKeys(wrapper)).not.toContain('last_used_at')
    expect(visibleColumnKeys(wrapper)).not.toContain('last_used_ip')
    expect(visibleColumnKeys(wrapper)).not.toContain('id')
  })

  it('shows a hidden column when toggled and persists the preference', async () => {
    const wrapper = await mountView()

    await wrapper.get('button[title="Column Settings"]').trigger('click')
    await getButtonByText(wrapper, 'Rate Limit').trigger('click')
    await nextTick()

    expect(visibleColumnKeys(wrapper)).toContain('rate_limit')
    expect(localStorage.getItem('api-key-hidden-columns')).toBe(
      JSON.stringify(['id', 'last_used_at', 'last_used_ip'])
    )
    expect(localStorage.getItem('api-key-column-settings-version')).toBe('3')
  })

  it('shows the API key ID column when toggled', async () => {
    const wrapper = await mountView()

    await wrapper.get('button[title="Column Settings"]').trigger('click')
    await getButtonByText(wrapper, 'ID').trigger('click')
    await nextTick()

    expect(visibleColumnKeys(wrapper)).toContain('id')
    expect(wrapper.get('[data-test="key-id"]').text()).toBe('#1')
    expect(visibleColumnMeta(wrapper).find((column) => column.key === 'id')?.sortable).toBe(true)
  })

  it('shows the last used IP column when toggled', async () => {
    listKeys.mockResolvedValueOnce({
      items: [{ ...createApiKey(), last_used_ip: '203.0.113.10' }],
      total: 1,
      page: 1,
      page_size: 20,
      pages: 1,
    })
    const wrapper = await mountView()

    await wrapper.get('button[title="Column Settings"]').trigger('click')
    await getButtonByText(wrapper, 'Last Used IP').trigger('click')
    await nextTick()

    expect(visibleColumnKeys(wrapper)).toContain('last_used_ip')
    expect(wrapper.get('[data-test="last-used-ip"]').text()).toBe('203.0.113.10')
  })

  it('restores column preferences from localStorage on mount', async () => {
    localStorage.setItem('api-key-hidden-columns', JSON.stringify(['group', 'created_at']))
    localStorage.setItem('api-key-column-settings-version', '1')

    const wrapper = await mountView()

    expect(visibleColumnKeys(wrapper)).toEqual([
      'name',
      'key',
      'current_concurrency',
      'failover',
      'usage',
      'rate_limit',
      'expires_at',
      'status',
      'last_used_at',
      'actions',
    ])
    expect(localStorage.getItem('api-key-hidden-columns')).toBe(
      JSON.stringify(['group', 'created_at', 'last_used_ip', 'id'])
    )
    expect(localStorage.getItem('api-key-column-settings-version')).toBe('3')
  })

  it('does not include always-visible columns in the toggleable menu', async () => {
    const wrapper = await mountView()

    await wrapper.get('button[title="Column Settings"]').trigger('click')
    await nextTick()

    const columnMenuText = wrapper.text()
    expect(columnMenuText).toContain('API Key')
    expect(columnMenuText).toContain('ID')
    expect(columnMenuText).toContain('Current Concurrency')
    expect(columnMenuText).toContain('Rate Limit')
    expect(columnMenuText).toContain('Last Used IP')
    expect(columnMenuText).not.toContain('Name')
    expect(columnMenuText).not.toContain('Actions')
  })

  it('renders the current concurrency value', async () => {
    const wrapper = await mountView()

    expect(wrapper.get('[data-test="current-concurrency"]').text()).toBe('3')
  })

  it('marks current concurrency as sortable', async () => {
    const wrapper = await mountView()

    const currentConcurrencyColumn = visibleColumnMeta(wrapper).find(
      (column) => column.key === 'current_concurrency'
    )
    expect(currentConcurrencyColumn?.sortable).toBe(true)
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

    await wrapper.get('[data-test="sort-current-concurrency"]').trigger('click')
    await flushPromises()

    expect(listKeys).toHaveBeenLastCalledWith(
      1,
      50,
      {
        search: 'target',
        status: 'active',
        group_id: 42,
        sort_by: 'current_concurrency',
        sort_order: 'asc',
      },
      expect.objectContaining({ signal: expect.any(AbortSignal) })
    )
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
    const primarySelector = wrapper.findAllComponents({ name: 'Select' }).find((select) => select.attributes('data-tour') === 'key-form-group')!
    primarySelector.vm.$emit('update:modelValue', 3)
    await nextTick()
    expect(wrapper.get('[data-test="failover-toggle"]').attributes('aria-checked')).toBe('false')
    await wrapper.get('[data-test="failover-toggle"]').trigger('click')
    const failoverSelector = wrapper.findAllComponents({ name: 'Select' }).find((select) => select.attributes('data-test') === 'failover-group')!
    expect(failoverSelector.props('modelValue')).toBe(null)
    expect(failoverSelector.props('options')).toEqual([])

    primarySelector.vm.$emit('update:modelValue', 2)
    await nextTick()
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
    const primarySelector = wrapper.findAllComponents({ name: 'Select' }).find((select) => select.attributes('data-tour') === 'key-form-group')!
    primarySelector.vm.$emit('update:modelValue', 3)
    await nextTick()
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
