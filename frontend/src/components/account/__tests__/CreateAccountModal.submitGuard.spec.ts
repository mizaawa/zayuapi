import { defineComponent } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

const {
  createAccountMock,
  checkMixedChannelRiskMock,
} = vi.hoisted(() => ({
  createAccountMock: vi.fn(),
  checkMixedChannelRiskMock: vi.fn(),
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
      probeUpstreamBilling: vi.fn().mockResolvedValue({}),
      checkMixedChannelRisk: checkMixedChannelRiskMock,
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

const BaseDialogStub = defineComponent({
  name: 'BaseDialog',
  props: { show: { type: Boolean, default: false } },
  emits: ['close'],
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

const deferred = <T>() => {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((res) => { resolve = res })
  return { promise, resolve }
}

async function fillApiKeyAccount(wrapper: ReturnType<typeof mountModal>) {
  await wrapper.get('[data-tour="account-form-name"]').setValue('guarded account')
  await wrapper.get('input[type="password"]').setValue('fixture-key')
}

describe('CreateAccountModal submit guard', () => {
  beforeEach(() => {
    createAccountMock.mockReset().mockResolvedValue({ id: 42, platform: 'anthropic', type: 'apikey' })
    checkMixedChannelRiskMock.mockReset().mockResolvedValue({ has_risk: false })
  })

  it('keeps browser password managers away from the key and the numeric fields', () => {
    const wrapper = mountModal()

    const key = wrapper.get('input[type="password"]')
    expect(key.attributes('autocomplete')).toBe('new-password')
    expect(key.attributes('data-lpignore')).toBe('true')
    expect(wrapper.get('[data-testid="account-concurrency-input"]').attributes('autocomplete')).toBe('off')
    expect(wrapper.get('form#create-account-form').attributes('autocomplete')).toBe('off')
    wrapper.unmount()
  })

  it('stays busy through the mixed-channel pre-check so a second click cannot create twice', async () => {
    const precheck = deferred<{ has_risk: boolean }>()
    checkMixedChannelRiskMock.mockReturnValueOnce(precheck.promise)
    const wrapper = mountModal()
    await flushPromises()
    await fillApiKeyAccount(wrapper)

    const form = wrapper.get('form#create-account-form')
    await form.trigger('submit.prevent')
    await form.trigger('submit.prevent')

    const submit = wrapper.get('[data-testid="account-create-submit"]')
    expect(submit.attributes('disabled')).toBeDefined()
    expect(submit.text()).toContain('admin.accounts.creating')
    expect(checkMixedChannelRiskMock).toHaveBeenCalledTimes(1)

    precheck.resolve({ has_risk: false })
    await flushPromises()

    expect(createAccountMock).toHaveBeenCalledTimes(1)
    expect(createAccountMock).toHaveBeenCalledWith(expect.objectContaining({ concurrency: 0 }))
    expect(wrapper.emitted('created')).toHaveLength(1)
    wrapper.unmount()
  })

  it('ignores cancel and dialog close while the account is being created', async () => {
    const creating = deferred<{ id: number; platform: string; type: string }>()
    createAccountMock.mockReturnValueOnce(creating.promise)
    const wrapper = mountModal()
    await flushPromises()
    await fillApiKeyAccount(wrapper)

    await wrapper.get('form#create-account-form').trigger('submit.prevent')
    await flushPromises()

    const cancel = wrapper.findAll('button').find((button) => button.text() === 'common.cancel')
    expect(cancel?.attributes('disabled')).toBeDefined()
    wrapper.getComponent(BaseDialogStub).vm.$emit('close')
    expect(wrapper.emitted('close')).toBeUndefined()

    creating.resolve({ id: 42, platform: 'anthropic', type: 'apikey' })
    await flushPromises()

    expect(wrapper.emitted('close')).toHaveLength(1)
    expect(wrapper.get('[data-testid="account-create-submit"]').attributes('disabled')).toBeUndefined()
    wrapper.unmount()
  })
})
