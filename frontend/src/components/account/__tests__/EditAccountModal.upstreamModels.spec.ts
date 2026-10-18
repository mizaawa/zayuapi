import { beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'

const { syncUpstreamModels, updateAccount } = vi.hoisted(() => ({
  syncUpstreamModels: vi.fn(),
  updateAccount: vi.fn()
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showError: vi.fn(), showSuccess: vi.fn(), showInfo: vi.fn() })
}))

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({ isSimpleMode: true })
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    accounts: { update: updateAccount, syncUpstreamModels },
    settings: {
      getWebSearchEmulationConfig: vi.fn().mockResolvedValue({ enabled: false, providers: [] }),
      getSettings: vi.fn().mockResolvedValue({})
    },
    tlsFingerprintProfiles: { list: vi.fn().mockResolvedValue([]) }
  }
}))

vi.mock('@/api/admin/accounts', () => ({
  accountsAPI: { syncUpstreamModels },
  getAntigravityDefaultModelMapping: vi.fn()
}))

vi.mock('@/composables/useClipboard', () => ({
  useClipboard: () => ({ copyToClipboard: vi.fn() })
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return { ...actual, useI18n: () => ({ t: (key: string) => key }) }
})

import EditAccountModal from '../EditAccountModal.vue'

const BaseDialogStub = defineComponent({
  props: { show: Boolean },
  template: '<div v-if="show"><slot /><slot name="footer" /></div>'
})

function mountModal(platform = 'openai', type = 'apikey') {
  return mount(EditAccountModal, {
    props: {
      show: true,
      account: {
        id: 17,
        name: 'Saved upstream',
        notes: '',
        platform,
        type,
        credentials: { base_url: 'https://saved.example/v1' },
        credentials_status: { has_api_key: true },
        extra: {},
        proxy_id: null,
        concurrency: 1,
        priority: 1,
        rate_multiplier: 1,
        status: 'active',
        group_ids: [],
        expires_at: null,
        auto_pause_on_expired: false
      } as any,
      proxies: [],
      groups: []
    },
    global: {
      stubs: {
        BaseDialog: BaseDialogStub,
        Select: true,
        Icon: true,
        ModelIcon: true,
        ProxySelector: true,
        GroupSelector: true
      }
    }
  })
}

function getBaseUrl(wrapper: ReturnType<typeof mountModal>) {
  return wrapper.findAll('input').find(input => input.element.value === 'https://saved.example/v1')!
}

async function syncModels(wrapper: ReturnType<typeof mountModal>) {
  const button = wrapper.findAll('button').find(button => button.text() === 'admin.accounts.syncUpstreamModels')!
  await button.trigger('click')
  await flushPromises()
}

describe('EditAccountModal upstream model credentials', () => {
  beforeEach(() => {
    syncUpstreamModels.mockReset().mockResolvedValue({ models: ['upstream-model'] })
    updateAccount.mockReset()
  })

  it.each(['openai', 'anthropic', 'gemini', 'grok', 'custom'])(
    'uses the current endpoint and key before saving a %s account',
    async platform => {
      const wrapper = mountModal(platform)
      const baseUrl = getBaseUrl(wrapper)
      const apiKey = wrapper.get('input[type="password"]')

      await baseUrl.setValue('  https://first.example/v1  ')
      await apiKey.setValue('  first-key  ')
      await syncModels(wrapper)
      expect(syncUpstreamModels).toHaveBeenLastCalledWith(17, {
        base_url: 'https://first.example/v1',
        api_key: 'first-key'
      })

      await baseUrl.setValue('https://second.example/v1')
      await apiKey.setValue('second-key')
      await syncModels(wrapper)
      expect(syncUpstreamModels).toHaveBeenLastCalledWith(17, {
        base_url: 'https://second.example/v1',
        api_key: 'second-key'
      })
      expect(updateAccount).not.toHaveBeenCalled()
    }
  )

  it('uses the current endpoint while leaving the redacted saved key on the server', async () => {
    const wrapper = mountModal()
    expect(wrapper.get<HTMLInputElement>('input[type="password"]').element.value).toBe('')

    await getBaseUrl(wrapper).setValue('https://changed.example/v1')
    await syncModels(wrapper)

    expect(syncUpstreamModels).toHaveBeenCalledWith(17, {
      base_url: 'https://changed.example/v1',
      api_key: undefined
    })
    expect(updateAccount).not.toHaveBeenCalled()
  })

  it.each(['apikey', 'upstream'])(
    'uses edited Antigravity %s credentials through its dedicated sync button',
    async type => {
      const wrapper = mountModal('antigravity', type)

      await getBaseUrl(wrapper).setValue('https://changed.example/v1')
      await wrapper.get('input[type="password"]').setValue('changed-key')
      await syncModels(wrapper)

      expect(syncUpstreamModels).toHaveBeenCalledWith(17, {
        base_url: 'https://changed.example/v1',
        api_key: 'changed-key'
      })
      expect(updateAccount).not.toHaveBeenCalled()
    }
  )

  it('uses the current key with the existing displayed endpoint', async () => {
    const wrapper = mountModal()

    await wrapper.get('input[type="password"]').setValue('replacement-key')
    await syncModels(wrapper)

    expect(syncUpstreamModels).toHaveBeenCalledWith(17, {
      base_url: 'https://saved.example/v1',
      api_key: 'replacement-key'
    })
    expect(updateAccount).not.toHaveBeenCalled()
  })

  it('omits cleared or whitespace-only fields so the server can reuse saved values', async () => {
    const wrapper = mountModal()

    await getBaseUrl(wrapper).setValue('   ')
    await wrapper.get('input[type="password"]').setValue('   ')
    await syncModels(wrapper)

    expect(syncUpstreamModels).toHaveBeenCalledWith(17, {
      base_url: undefined,
      api_key: undefined
    })
    expect(updateAccount).not.toHaveBeenCalled()
  })
})
