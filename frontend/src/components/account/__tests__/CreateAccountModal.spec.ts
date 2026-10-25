import { defineComponent } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

const {
  createAccountMock,
  probeUpstreamBillingMock,
} = vi.hoisted(() => ({
  createAccountMock: vi.fn(),
  probeUpstreamBillingMock: vi.fn(),
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError: vi.fn(),
    showSuccess: vi.fn(),
    showWarning: vi.fn(),
  }),
}))

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({ isSimpleMode: false }),
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    accounts: {
      create: createAccountMock,
      probeUpstreamBilling: probeUpstreamBillingMock,
      checkMixedChannelRisk: vi.fn().mockResolvedValue({ has_risk: false }),
    },
    settings: {
      getWebSearchEmulationConfig: vi.fn().mockResolvedValue({ enabled: false, providers: [] }),
      getSettings: vi.fn().mockResolvedValue({}),
    },
    tlsFingerprintProfiles: {
      list: vi.fn().mockResolvedValue([]),
    },
  },
}))

vi.mock('@/api/admin/accounts', () => ({
  getAntigravityDefaultModelMapping: vi.fn().mockResolvedValue([]),
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key }),
  }
})

import CreateAccountModal from '../CreateAccountModal.vue'
import ProxySelector from '@/components/common/ProxySelector.vue'
import GroupSelector from '@/components/common/GroupSelector.vue'
import QuotaLimitCard from '@/components/account/QuotaLimitCard.vue'
import ModelWhitelistSelector from '@/components/account/ModelWhitelistSelector.vue'

const BaseDialogStub = defineComponent({
  name: 'BaseDialog',
  props: { show: { type: Boolean, default: false } },
  template: '<div v-if="show"><slot /><slot name="footer" /></div>',
})

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
      },
    },
  })
}

async function selectButtonByText(wrapper: ReturnType<typeof mountModal>, text: string) {
  const button = wrapper.findAll('button').find((candidate) => candidate.text().includes(text))
  expect(button).toBeDefined()
  await button?.trigger('click')
}

async function submitApiKeyAccount(
  platform: 'openai' | 'anthropic',
  enableLongContextBilling = false,
  disableUpstreamBillingProbe = false
) {
  const wrapper = mountModal()
  await selectButtonByText(wrapper, platform === 'openai' ? 'OpenAI' : 'Anthropic')
  await wrapper.get('form#create-account-form input[type="text"]').setValue(`${platform} account`)
  await wrapper.get('form#create-account-form input[type="password"]').setValue('test-api-key')
  if (enableLongContextBilling) {
    await wrapper.get('[data-testid="openai-long-context-billing-toggle"]').trigger('click')
  }
  if (disableUpstreamBillingProbe) {
    await wrapper.get('[data-testid="upstream-billing-auto-probe"]').trigger('click')
  }
  await wrapper.get('form#create-account-form').trigger('submit.prevent')
  await flushPromises()
  return wrapper
}

describe('CreateAccountModal OpenAI long-context billing', () => {
  beforeEach(() => {
    createAccountMock.mockReset().mockResolvedValue({ id: 42, platform: 'openai', type: 'apikey' })
    probeUpstreamBillingMock.mockReset().mockResolvedValue({})
  })

  it('sends false explicitly for normal OpenAI account creation by default', async () => {
    await submitApiKeyAccount('openai')

    expect(createAccountMock).toHaveBeenCalledTimes(1)
    expect(createAccountMock.mock.calls[0]?.[0]?.extra?.openai_long_context_billing_enabled).toBe(false)
  })

  // namespace 摊平是仅 OAuth 的兼容开关：API Key 走 chat completions 回退桥时由桥自行摊平
  it('enables upstream billing probes by default for new OpenAI API key accounts', async () => {
    await submitApiKeyAccount('openai')

    expect(createAccountMock.mock.calls[0]?.[0]?.upstream_billing_probe_enabled).toBe(true)
  })

  it('waits for the initial upstream billing probe before refreshing the account list', async () => {
    let resolveProbe: (() => void) | undefined
    probeUpstreamBillingMock.mockImplementationOnce(
      () => new Promise<void>((resolve) => {
        resolveProbe = resolve
      })
    )

    const wrapper = await submitApiKeyAccount('openai')

    expect(probeUpstreamBillingMock).toHaveBeenCalledWith(42)
    expect(wrapper.emitted('created')).toBeUndefined()

    resolveProbe?.()
    await flushPromises()

    expect(wrapper.emitted('created')).toHaveLength(1)
  })

  it('sends an explicit disabled state when the create toggle is turned off', async () => {
    await submitApiKeyAccount('openai', false, true)

    expect(createAccountMock.mock.calls[0]?.[0]?.upstream_billing_probe_enabled).toBe(false)
    expect(probeUpstreamBillingMock).not.toHaveBeenCalled()
  })

  it('sends true explicitly when OpenAI long-context billing is enabled', async () => {
    await submitApiKeyAccount('openai', true)

    expect(createAccountMock).toHaveBeenCalledTimes(1)
    expect(createAccountMock.mock.calls[0]?.[0]?.extra?.openai_long_context_billing_enabled).toBe(true)
  })

  it('omits the OpenAI setting for non-OpenAI account creation', async () => {
    await submitApiKeyAccount('anthropic')

    expect(createAccountMock).toHaveBeenCalledTimes(1)
    expect(createAccountMock.mock.calls[0]?.[0]?.extra?.openai_long_context_billing_enabled).toBeUndefined()
    // 上游倍率探测已放宽到全部 API-key 平台：非 OpenAI 平台与 OpenAI 一致，默认开启。
    expect(createAccountMock.mock.calls[0]?.[0]?.upstream_billing_probe_enabled).toBe(true)
  })

  it('sends an explicit disabled state when the non-OpenAI create toggle is turned off', async () => {
    await submitApiKeyAccount('anthropic', false, true)

    expect(createAccountMock.mock.calls[0]?.[0]?.upstream_billing_probe_enabled).toBe(false)
  })

  it('antigravity upstream 创建默认携带上游倍率探测开关', async () => {
    // antigravity upstream 走独立创建 helper，
    // 也必须与其余 API-key 平台一样默认开启探测并传递开关。
    const wrapper = mountModal()
    await selectButtonByText(wrapper, 'Antigravity')
    await wrapper.get('form#create-account-form input[type="text"]').setValue('antigravity relay')
    const baseInput = wrapper
      .findAll('input')
      .find((candidate) => candidate.attributes('placeholder') === 'https://cloudcode-pa.googleapis.com')
    expect(baseInput).toBeDefined()
    await baseInput?.setValue('https://relay.example')
    await wrapper.get('form#create-account-form input[type="password"]').setValue('sk-upstream')
    await wrapper.get('form#create-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(createAccountMock).toHaveBeenCalledTimes(1)
    const payload = createAccountMock.mock.calls[0]?.[0]
    expect(payload?.platform).toBe('antigravity')
    expect(payload?.type).toBe('apikey')
    expect(payload?.upstream_billing_probe_enabled).toBe(true)
    // 创建成功后前端立即发起一次首探（与其他 apikey 平台一致）。
    expect(probeUpstreamBillingMock).toHaveBeenCalledWith(42)
  })

  it.each([
    ['8yes', 'https://8yes.cc/v1'],
    ['sora', 'https://momoapi.xin/v1'],
  ])('creates Custom %s video accounts through the API Key path and requires Base URL', async (protocol, baseUrl) => {
    createAccountMock.mockResolvedValue({ id: 42, platform: 'custom', type: 'apikey' })
    const wrapper = mountModal()

    await wrapper.get('[data-testid="account-platform-custom"]').trigger('click')
    await flushPromises()

    const typeChooser = wrapper.get('[data-tour="account-form-type"]')
    expect(typeChooser.findAll('button')).toHaveLength(0)
    expect(wrapper.get('[data-testid="account-type-api-key"]').text()).toContain('API Key')

    await wrapper.get('[data-tour="account-form-name"]').setValue('Custom video upstream')
    await wrapper.get('input[type="password"]').setValue('sk-custom')
    await wrapper.get('form#create-account-form').trigger('submit.prevent')
    await flushPromises()
    expect(createAccountMock).not.toHaveBeenCalled()

    await wrapper.get('input[placeholder="https://api.example.com/v1"]').setValue(baseUrl)
    expect(wrapper.get('#account-media-protocol').findAll('option').map(option => option.attributes('value'))).toEqual(['', 'sora', '8yes'])
    await wrapper.get('#account-media-protocol').setValue(protocol)
    await wrapper.get('form#create-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(createAccountMock).toHaveBeenCalledWith(expect.objectContaining({
      platform: 'custom',
      type: 'apikey',
      credentials: expect.objectContaining({
        base_url: baseUrl,
        api_key: 'sk-custom',
        media_protocol: protocol,
      }),
    }))
  })

  it.each([
    ['anthropic', 'Anthropic'], ['openai', 'OpenAI'], ['gemini', 'Gemini'],
    ['antigravity', 'Antigravity'], ['grok', 'Grok'], ['custom', 'Custom'],
  ])('creates %s directly with an API Key and rejects empty credentials', async (platform, label) => {
    const wrapper = mountModal()
    await selectButtonByText(wrapper, label)
    await flushPromises()
    await wrapper.get('[data-tour="account-form-name"]').setValue(platform + ' fixture')
    if (platform === 'antigravity' || platform === 'custom') {
      const placeholder = platform === 'antigravity' ? 'https://cloudcode-pa.googleapis.com' : 'https://api.example.com/v1'
      await wrapper.get('input[placeholder="' + placeholder + '"]').setValue('https://relay.example/v1')
    }
    expect(wrapper.get('[data-testid="account-type-api-key"]').text()).toContain('API Key')
    expect(wrapper.get('[data-tour="account-form-type"]').findAll('button')).toHaveLength(0)
    await wrapper.get('form#create-account-form').trigger('submit.prevent')
    await flushPromises()
    expect(createAccountMock).not.toHaveBeenCalled()
    await wrapper.get('input[type="password"]').setValue('fixture-key')
    await wrapper.get('form#create-account-form').trigger('submit.prevent')
    await flushPromises()
    expect(createAccountMock).toHaveBeenCalledTimes(1)
    expect(createAccountMock).toHaveBeenCalledWith(expect.objectContaining({
      platform, type: 'apikey', credentials: expect.objectContaining({ api_key: 'fixture-key' }),
      upstream_billing_probe_enabled: true,
    }))
    expect(wrapper.emitted('created')).toHaveLength(1)
    wrapper.unmount()
  })

  it('clears API Key credentials when changing platforms', async () => {
    const wrapper = mountModal()
    await selectButtonByText(wrapper, 'OpenAI')
    await wrapper.get('[data-tour="account-form-name"]').setValue('fixture')
    await wrapper.get('input[type="password"]').setValue('fixture-key')
    await selectButtonByText(wrapper, 'Grok')
    expect((wrapper.get('input[type="password"]').element as HTMLInputElement).value).toBe('')
    await wrapper.get('form#create-account-form').trigger('submit.prevent')
    await flushPromises()
    expect(createAccountMock).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('requires a nonblank account name', async () => {
    const wrapper = mountModal()
    await wrapper.get('[data-tour="account-form-name"]').setValue('  ')
    await wrapper.get('input[type="password"]').setValue('fixture-key')
    await wrapper.get('form#create-account-form').trigger('submit.prevent')
    await flushPromises()
    expect(createAccountMock).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('preserves proxy, groups, model whitelist and quota settings', async () => {
    const wrapper = mountModal()
    await wrapper.get('[data-tour="account-form-name"]').setValue('fixture')
    await wrapper.get('input[type="password"]').setValue('fixture-key')
    wrapper.getComponent(ProxySelector).vm.$emit('update:modelValue', 7)
    wrapper.getComponent(GroupSelector).vm.$emit('update:modelValue', [11, 12])
    wrapper.getComponent(ModelWhitelistSelector).vm.$emit('update:modelValue', ['model-test'])
    const quota = wrapper.getComponent(QuotaLimitCard)
    quota.vm.$emit('update:totalLimit', 500)
    quota.vm.$emit('update:dailyLimit', 20)
    quota.vm.$emit('update:weeklyLimit', 100)
    quota.vm.$emit('update:dailyResetMode', 'fixed')
    quota.vm.$emit('update:dailyResetHour', 5)
    quota.vm.$emit('update:resetTimezone', 'Asia/Shanghai')
    await wrapper.get('form#create-account-form').trigger('submit.prevent')
    await flushPromises()
    expect(createAccountMock).toHaveBeenCalledWith(expect.objectContaining({
      proxy_id: 7, group_ids: [11, 12],
      credentials: expect.objectContaining({ model_mapping: { 'model-test': 'model-test' } }),
      extra: expect.objectContaining({ quota_limit: 500, quota_daily_limit: 20, quota_weekly_limit: 100, quota_daily_reset_mode: 'fixed', quota_daily_reset_hour: 5, quota_reset_timezone: 'Asia/Shanghai' }),
    }))
    wrapper.unmount()
  })

  it.each(['Grok', 'Antigravity', 'Custom'])('preserves model mapping for %s', async label => {
    const wrapper = mountModal()
    await selectButtonByText(wrapper, label)
    await flushPromises()
    await wrapper.get('[data-tour="account-form-name"]').setValue('fixture')
    await wrapper.get('input[type="password"]').setValue('fixture-key')
    if (label === 'Antigravity' || label === 'Custom') {
      const placeholder = label === 'Antigravity' ? 'https://cloudcode-pa.googleapis.com' : 'https://api.example.com/v1'
      await wrapper.get('input[placeholder="' + placeholder + '"]').setValue('https://relay.example/v1')
    }
    await selectButtonByText(wrapper, 'admin.accounts.addMapping')
    await wrapper.get('input[placeholder="admin.accounts.requestModel"]').setValue('client-model')
    await wrapper.get('input[placeholder="admin.accounts.actualModel"]').setValue('upstream-model')
    await wrapper.get('form#create-account-form').trigger('submit.prevent')
    await flushPromises()
    expect(createAccountMock).toHaveBeenCalledWith(expect.objectContaining({ credentials: expect.objectContaining({ model_mapping: { 'client-model': 'upstream-model' } }) }))
    wrapper.unmount()
  })

})
