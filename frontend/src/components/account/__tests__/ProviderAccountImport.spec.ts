import { defineComponent } from 'vue'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

const mocks = vi.hoisted(() => ({
  listProviderOptions: vi.fn(),
  listProviderKeys: vi.fn(),
  listProviderKeyGroups: vi.fn(),
  createProviderKey: vi.fn()
}))

vi.mock('@/api/admin/upstream', () => mocks)
vi.mock('vue-i18n', async () => ({
  ...(await vi.importActual<typeof import('vue-i18n')>('vue-i18n')),
  useI18n: () => ({
    t: (key: string, params?: Record<string, unknown>) => (params ? `${key}:${JSON.stringify(params)}` : key)
  })
}))

import ProviderAccountImport from '../ProviderAccountImport.vue'

const BaseDialogStub = defineComponent({
  name: 'BaseDialog',
  props: { show: { type: Boolean, default: false } },
  template: '<div v-if="show" data-testid="dialog"><slot /><slot name="footer" /></div>'
})

const provider = { id: 'p1', name: '杂鱼AI', site_type: 'sub2api', base_url: 'https://www.zayuapi.com', status: 'connected' }
const relay = { id: 'p2', name: 'Relay', site_type: 'newapi', base_url: 'https://relay.example', status: 'disconnected' }
const activeKey = {
  id: '11', name: '破甲', group_id: '7', group_name: 'CodexPro-不降智⑤', group_platform: 'openai',
  group_multiplier: 0.2, status: 'active', available: true
}
const disabledKey = { ...activeKey, id: '12', name: '旧密钥', status: 'disabled', available: false }

function keyList(keys: unknown[], overrides: Record<string, unknown> = {}) {
  return { provider_id: 'p1', site_type: 'sub2api', endpoint: 'https://www.zayuapi.com', synced_at: '2026-10-28T13:47:00Z', keys, ...overrides }
}

function mountImport(platform = 'anthropic') {
  return mount(ProviderAccountImport, {
    props: { platform },
    global: {
      stubs: { teleport: true, BaseDialog: BaseDialogStub, RouterLink: { template: '<a><slot /></a>' } }
    }
  })
}

async function openPicker(wrapper: VueWrapper, testId: string) {
  await wrapper.get(`[data-testid="${testId}"] button`).trigger('click')
}

function rows(wrapper: VueWrapper, testId: string) {
  return wrapper.get(`[data-testid="${testId}"]`).findAll('[role="option"]')
}

function lastSelection(wrapper: VueWrapper) {
  return wrapper.emitted('select')?.at(-1)?.[0]
}

describe('ProviderAccountImport', () => {
  beforeEach(() => {
    Object.values(mocks).forEach((mock) => mock.mockReset())
  })

  it('auto-selects the only provider and emits the chosen key with its account endpoint', async () => {
    mocks.listProviderOptions.mockResolvedValue([provider])
    mocks.listProviderKeys.mockResolvedValue(keyList([activeKey, disabledKey]))
    const wrapper = mountImport('grok')
    await flushPromises()

    expect(mocks.listProviderKeys).toHaveBeenCalledWith('p1')
    expect(wrapper.get('[data-testid="provider-import-provider-picker"]').text()).toContain('杂鱼AI')
    expect(wrapper.text()).toContain('10/28')
    expect(wrapper.emitted('select')).toBeUndefined()

    await openPicker(wrapper, 'provider-import-key-picker')
    const keyRows = rows(wrapper, 'provider-import-key-picker')
    expect(keyRows).toHaveLength(3)
    expect(keyRows[0].text()).toContain('admin.providerImport.createFromProvider')
    expect(keyRows[1].text()).toContain('破甲')
    expect(keyRows[1].text()).toContain('CodexPro-不降智⑤')
    expect(keyRows[1].text()).toContain('0.2×')
    expect(keyRows[1].text()).toContain('admin.providerImport.keyStatus.available')
    expect(keyRows[2].attributes('aria-disabled')).toBe('true')
    expect(keyRows[2].text()).toContain('admin.providerImport.keyStatus.disabled')

    await keyRows[2].trigger('click')
    expect(wrapper.emitted('select')).toBeUndefined()
    await keyRows[1].trigger('click')

    expect(lastSelection(wrapper)).toEqual({
      providerId: 'p1', providerName: '杂鱼AI', keyId: '11', keyName: '破甲',
      groupName: 'CodexPro-不降智⑤', groupMultiplier: 0.2, endpoint: 'https://www.zayuapi.com/v1'
    })
    expect(wrapper.get('[data-testid="provider-import-endpoint"]').text()).toContain('https://www.zayuapi.com/v1')
  })

  it('updates the bound endpoint when the account platform changes', async () => {
    mocks.listProviderOptions.mockResolvedValue([provider])
    mocks.listProviderKeys.mockResolvedValue(keyList([activeKey]))
    const wrapper = mountImport('custom')
    await flushPromises()
    await openPicker(wrapper, 'provider-import-key-picker')
    await rows(wrapper, 'provider-import-key-picker')[1].trigger('click')
    expect(lastSelection(wrapper)).toEqual(expect.objectContaining({ endpoint: 'https://www.zayuapi.com/v1' }))

    await wrapper.setProps({ platform: 'anthropic' })
    expect(lastSelection(wrapper)).toEqual(expect.objectContaining({ endpoint: 'https://www.zayuapi.com' }))
  })

  it('creates a key from the provider, refreshes the list and selects the new key', async () => {
    const createdKey = { ...activeKey, id: '99', name: '[杂鱼]Gemini-默认通道', group_name: 'Gemini-默认通道', group_platform: 'gemini' }
    mocks.listProviderOptions.mockResolvedValue([provider])
    mocks.listProviderKeys.mockResolvedValueOnce(keyList([])).mockResolvedValueOnce(keyList([createdKey]))
    mocks.listProviderKeyGroups.mockResolvedValue({
      provider_id: 'p1', site_type: 'sub2api',
      groups: [{ id: '12', name: 'Gemini-默认通道', platform: 'gemini', multiplier: 0.25, key_name: '[杂鱼]Gemini-默认通道' }]
    })
    mocks.createProviderKey.mockResolvedValue({ provider_id: 'p1', created: true, name: '[杂鱼]Gemini-默认通道', group_id: '12', key_id: '99' })
    const wrapper = mountImport()
    await flushPromises()
    expect(wrapper.text()).toContain('admin.providerImport.noKeys')

    await openPicker(wrapper, 'provider-import-key-picker')
    await wrapper.get('[data-testid="provider-picker-action"]').trigger('click')
    await flushPromises()
    expect(mocks.listProviderKeyGroups).toHaveBeenCalledWith('p1')
    expect(wrapper.get('[data-testid="provider-key-create-submit"]').attributes('disabled')).toBeDefined()

    await openPicker(wrapper, 'provider-key-group-picker')
    const groupRows = rows(wrapper, 'provider-key-group-picker')
    expect(groupRows[0].text()).toContain('Gemini · admin.providerImport.groupMultiplier')
    await groupRows[0].trigger('click')
    expect(wrapper.get('[data-testid="provider-key-name-preview"]').text()).toBe('[杂鱼]Gemini-默认通道')

    await wrapper.get('#provider-key-create-form').trigger('submit')
    await flushPromises()

    expect(mocks.createProviderKey).toHaveBeenCalledWith('p1', '12')
    expect(mocks.listProviderKeys).toHaveBeenCalledTimes(2)
    expect(wrapper.find('[data-testid="dialog"]').exists()).toBe(false)
    expect(lastSelection(wrapper)).toEqual(expect.objectContaining({ keyId: '99', keyName: '[杂鱼]Gemini-默认通道' }))
    expect(wrapper.get('[data-testid="provider-import-notice"]').text()).toContain('admin.providerImport.createdSelected')
  })

  it('asks for a manual refresh when a created key is not listed yet', async () => {
    mocks.listProviderOptions.mockResolvedValue([provider])
    mocks.listProviderKeys.mockResolvedValue(keyList([activeKey]))
    mocks.listProviderKeyGroups.mockResolvedValue({
      provider_id: 'p1', site_type: 'newapi', groups: [{ id: 'vip', name: 'vip', multiplier: 0.5, key_name: '[杂鱼]vip' }]
    })
    mocks.createProviderKey.mockResolvedValue({ provider_id: 'p1', created: true, name: '[杂鱼]vip', group_id: 'vip', key_id: '' })
    const wrapper = mountImport()
    await flushPromises()
    await openPicker(wrapper, 'provider-import-key-picker')
    await wrapper.get('[data-testid="provider-picker-action"]').trigger('click')
    await flushPromises()
    await openPicker(wrapper, 'provider-key-group-picker')
    await rows(wrapper, 'provider-key-group-picker')[0].trigger('click')
    await wrapper.get('#provider-key-create-form').trigger('submit')
    await flushPromises()

    expect(wrapper.get('[data-testid="provider-import-notice"]').text()).toContain('admin.providerImport.createdPending')
    expect(wrapper.emitted('select')).toBeUndefined()
  })

  it('ignores a slow key list from a provider that is no longer selected', async () => {
    mocks.listProviderOptions.mockResolvedValue([provider, relay])
    let resolveFirst: (value: unknown) => void = () => undefined
    mocks.listProviderKeys
      .mockImplementationOnce(() => new Promise((resolve) => { resolveFirst = resolve }))
      .mockResolvedValueOnce(keyList([{ ...activeKey, id: '21', name: 'relay key' }], { provider_id: 'p2', endpoint: 'https://relay.example' }))
    const wrapper = mountImport()
    await flushPromises()
    expect(mocks.listProviderKeys).not.toHaveBeenCalled()

    await openPicker(wrapper, 'provider-import-provider-picker')
    const providerRows = rows(wrapper, 'provider-import-provider-picker')
    expect(providerRows[1].text()).toContain('admin.providerImport.providerDisconnected')
    await providerRows[0].trigger('click')
    await openPicker(wrapper, 'provider-import-provider-picker')
    await rows(wrapper, 'provider-import-provider-picker')[1].trigger('click')
    await flushPromises()
    resolveFirst(keyList([activeKey]))
    await flushPromises()

    await openPicker(wrapper, 'provider-import-key-picker')
    const keyText = rows(wrapper, 'provider-import-key-picker').map((row) => row.text()).join('\n')
    expect(keyText).toContain('relay key')
    expect(keyText).not.toContain('破甲')
  })

  it('reports a deleted provider after refreshing the sites and clears the selection', async () => {
    mocks.listProviderOptions.mockResolvedValueOnce([provider]).mockResolvedValueOnce([])
    mocks.listProviderKeys.mockResolvedValue(keyList([activeKey]))
    const wrapper = mountImport()
    await flushPromises()
    await openPicker(wrapper, 'provider-import-key-picker')
    await rows(wrapper, 'provider-import-key-picker')[1].trigger('click')
    expect(lastSelection(wrapper)).toEqual(expect.objectContaining({ keyId: '11' }))

    await wrapper.get('[data-testid="provider-import-refresh-providers"]').trigger('click')
    await flushPromises()

    expect(lastSelection(wrapper)).toBeNull()
    expect(wrapper.text()).toContain('admin.providerImport.providerMissing')
    expect(wrapper.get('[data-testid="provider-import-key-picker"] button').attributes('disabled')).toBeDefined()
  })

  it('points to Upstream Management when no provider exists', async () => {
    mocks.listProviderOptions.mockResolvedValue([])
    const wrapper = mountImport()
    await flushPromises()

    expect(wrapper.get('[data-testid="provider-import-empty"]').text()).toContain('admin.providerImport.addProviderLink')
    expect(wrapper.get('[data-testid="provider-import-key-picker"] button').attributes('disabled')).toBeDefined()
    expect(mocks.listProviderKeys).not.toHaveBeenCalled()
  })

  it('shows the server reason when keys cannot be read', async () => {
    mocks.listProviderOptions.mockResolvedValue([provider])
    mocks.listProviderKeys.mockRejectedValue({ status: 502, reason: 'PROVIDER_REMOTE_FAILED', message: 'Provider login failed' })
    const wrapper = mountImport()
    await flushPromises()

    expect(wrapper.get('[role="alert"]').text()).toContain('Provider login failed')
    expect(wrapper.find('[data-testid="provider-picker-action"]').exists()).toBe(false)
  })
})
