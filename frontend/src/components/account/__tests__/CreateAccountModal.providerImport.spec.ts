import { defineComponent } from 'vue'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

const mocks = vi.hoisted(() => ({
  createAccount: vi.fn(),
  probeUpstreamBilling: vi.fn(),
  showError: vi.fn(),
  listProviderOptions: vi.fn(),
  listProviderKeys: vi.fn(),
  listProviderKeyGroups: vi.fn(),
  createProviderKey: vi.fn()
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showError: mocks.showError, showSuccess: vi.fn(), showWarning: vi.fn(), showInfo: vi.fn() })
}))

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({ isSimpleMode: false })
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    accounts: {
      create: mocks.createAccount,
      probeUpstreamBilling: mocks.probeUpstreamBilling,
      checkMixedChannelRisk: vi.fn().mockResolvedValue({ has_risk: false })
    },
    settings: {
      getWebSearchEmulationConfig: vi.fn().mockResolvedValue({ enabled: false, providers: [] }),
      getSettings: vi.fn().mockResolvedValue({})
    },
    tlsFingerprintProfiles: { list: vi.fn().mockResolvedValue([]) }
  }
}))

vi.mock('@/api/admin/accounts', () => ({
  getAntigravityDefaultModelMapping: vi.fn().mockResolvedValue([])
}))

vi.mock('@/api/admin/upstream', () => ({
  listProviderOptions: mocks.listProviderOptions,
  listProviderKeys: mocks.listProviderKeys,
  listProviderKeyGroups: mocks.listProviderKeyGroups,
  createProviderKey: mocks.createProviderKey
}))

vi.mock('vue-i18n', async () => ({
  ...(await vi.importActual<typeof import('vue-i18n')>('vue-i18n')),
  useI18n: () => ({ t: (key: string) => key })
}))

import CreateAccountModal from '../CreateAccountModal.vue'

const BaseDialogStub = defineComponent({
  name: 'BaseDialog',
  props: { show: { type: Boolean, default: false } },
  template: '<div v-if="show"><slot /><slot name="footer" /></div>'
})

const provider = { id: 'p1', name: '杂鱼AI', site_type: 'sub2api', base_url: 'https://www.zayuapi.com', status: 'connected' }
const key = {
  id: '11', name: '破甲', group_id: '7', group_name: 'CodexPro-不降智⑤', group_platform: 'openai',
  group_multiplier: 0.2, status: 'active', available: true
}

function mountModal() {
  return mount(CreateAccountModal, {
    props: { show: true, proxies: [], groups: [] },
    global: {
      stubs: {
        BaseDialog: BaseDialogStub,
        ConfirmDialog: true,
        Select: true,
        Icon: true,
        PlatformIcon: true,
        ProxySelector: true,
        ProxyAdBanner: true,
        GroupSelector: true,
        ModelWhitelistSelector: true,
        QuotaLimitCard: true,
        RouterLink: { template: '<a><slot /></a>' },
        teleport: true
      }
    }
  })
}

async function useProviderImport(wrapper: VueWrapper) {
  await wrapper.get('[data-testid="account-type-provider"] input').setValue(true)
  await flushPromises()
}

async function chooseFirstKey(wrapper: VueWrapper) {
  await wrapper.get('[data-testid="provider-import-key-picker"] button').trigger('click')
  const options = wrapper.get('[data-testid="provider-import-key-picker"]').findAll('[data-testid="provider-picker-option"]')
  await options[0].trigger('click')
}

async function submit(wrapper: VueWrapper) {
  await wrapper.get('form#create-account-form').trigger('submit.prevent')
  await flushPromises()
}

async function selectPlatform(wrapper: VueWrapper, label: string) {
  await wrapper.findAll('button').find((button) => button.text().includes(label))!.trigger('click')
  await flushPromises()
}

describe('CreateAccountModal provider import', () => {
  beforeEach(() => {
    Object.values(mocks).forEach((mock) => mock.mockReset())
    mocks.createAccount.mockResolvedValue({ id: 42, platform: 'anthropic', type: 'apikey' })
    mocks.probeUpstreamBilling.mockResolvedValue({})
    mocks.listProviderOptions.mockResolvedValue([provider])
    mocks.listProviderKeys.mockResolvedValue({
      provider_id: 'p1', site_type: 'sub2api', endpoint: 'https://www.zayuapi.com', synced_at: '2026-10-28T13:47:00Z', keys: [key]
    })
  })

  it('offers the provider import beside API Key without any account type buttons', async () => {
    const wrapper = mountModal()
    await flushPromises()
    const chooser = wrapper.get('[data-tour="account-form-type"]')
    expect(chooser.findAll('button')).toHaveLength(0)
    expect(chooser.findAll('input[type="radio"]')).toHaveLength(2)
    expect(wrapper.get('[data-testid="account-type-api-key"]').text()).toContain('API Key')
    expect(wrapper.get('[data-testid="account-type-provider"]').text()).toContain('admin.providerImport.sourceProvider')
    expect(mocks.listProviderOptions).not.toHaveBeenCalled()
  })

  it('replaces the Base URL and API Key fields with provider and key pickers', async () => {
    const wrapper = mountModal()
    await useProviderImport(wrapper)

    expect(mocks.listProviderOptions).toHaveBeenCalledTimes(1)
    expect(wrapper.find('input[type="password"]').exists()).toBe(false)
    expect(wrapper.find('input[placeholder="https://api.anthropic.com"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="provider-account-import"]').exists()).toBe(true)
  })

  it('creates the account from a provider key without sending a secret', async () => {
    const wrapper = mountModal()
    await useProviderImport(wrapper)

    await submit(wrapper)
    expect(mocks.createAccount).not.toHaveBeenCalled()
    expect(mocks.showError).toHaveBeenCalledWith('admin.accounts.pleaseEnterAccountName')

    await chooseFirstKey(wrapper)
    expect((wrapper.get('[data-tour="account-form-name"]').element as HTMLInputElement).value).toBe('杂鱼AI · 破甲')
    await submit(wrapper)

    expect(mocks.createAccount).toHaveBeenCalledTimes(1)
    const payload = mocks.createAccount.mock.calls[0][0]
    expect(payload).toEqual(expect.objectContaining({
      name: '杂鱼AI · 破甲', platform: 'anthropic', type: 'apikey', provider_id: 'p1', provider_key_id: '11',
      upstream_billing_probe_enabled: true
    }))
    expect(payload.credentials).not.toHaveProperty('api_key')
    expect(payload.credentials).not.toHaveProperty('base_url')
    expect(wrapper.emitted('created')).toHaveLength(1)
  })

  it('requires a key before creating and keeps a typed account name', async () => {
    mocks.createAccount.mockResolvedValue({ id: 43, platform: 'grok', type: 'apikey' })
    const wrapper = mountModal()
    await selectPlatform(wrapper, 'Grok')
    await wrapper.get('[data-tour="account-form-name"]').setValue('My relay')
    await useProviderImport(wrapper)

    await submit(wrapper)
    expect(mocks.createAccount).not.toHaveBeenCalled()
    expect(mocks.showError).toHaveBeenCalledWith('admin.providerImport.selectKeyRequired')

    await chooseFirstKey(wrapper)
    expect((wrapper.get('[data-tour="account-form-name"]').element as HTMLInputElement).value).toBe('My relay')
    expect(wrapper.get('[data-testid="provider-import-endpoint"]').text()).toContain('https://www.zayuapi.com/v1')
    await submit(wrapper)

    const payload = mocks.createAccount.mock.calls[0][0]
    expect(payload).toEqual(expect.objectContaining({ name: 'My relay', platform: 'grok', provider_id: 'p1', provider_key_id: '11' }))
    expect(payload.credentials).not.toHaveProperty('base_url')
  })

  it('drops the provider selection when switching back to API Key', async () => {
    const wrapper = mountModal()
    await useProviderImport(wrapper)
    await chooseFirstKey(wrapper)
    await wrapper.get('[data-testid="account-type-api-key"] input').setValue(true)
    await wrapper.get('input[type="password"]').setValue('sk-manual')
    await submit(wrapper)

    const payload = mocks.createAccount.mock.calls[0][0]
    expect(payload).not.toHaveProperty('provider_id')
    expect(payload).not.toHaveProperty('provider_key_id')
    expect(payload.credentials).toEqual(expect.objectContaining({ api_key: 'sk-manual', base_url: 'https://api.anthropic.com' }))
  })

  it('explains provider import failures reported by the server', async () => {
    mocks.createAccount.mockRejectedValue({ status: 400, reason: 'PROVIDER_KEY_UNAVAILABLE', message: 'The selected API key is disabled' })
    const wrapper = mountModal()
    await useProviderImport(wrapper)
    await chooseFirstKey(wrapper)
    await submit(wrapper)

    expect(mocks.showError).toHaveBeenLastCalledWith('admin.providerImport.errors.PROVIDER_KEY_UNAVAILABLE')
    expect(wrapper.emitted('created')).toBeUndefined()
  })

  it('starts the next account in API Key mode after closing', async () => {
    const wrapper = mountModal()
    await useProviderImport(wrapper)
    await wrapper.setProps({ show: false })
    await wrapper.setProps({ show: true })
    await flushPromises()

    expect((wrapper.get('[data-testid="account-type-api-key"] input').element as HTMLInputElement).checked).toBe(true)
    expect(wrapper.find('input[type="password"]').exists()).toBe(true)
  })
})
