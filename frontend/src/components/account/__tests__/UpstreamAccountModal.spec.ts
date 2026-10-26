import { defineComponent } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import UpstreamAccountModal from '../UpstreamAccountModal.vue'
import type { Account } from '@/types'

const mocks = vi.hoisted(() => ({ create: vi.fn(), update: vi.fn(), preview: vi.fn(), sync: vi.fn(), success: vi.fn(), error: vi.fn() }))
vi.mock('@/api/admin', () => ({ adminAPI: { accounts: {
  create: mocks.create, update: mocks.update, syncUpstreamModelsPreview: mocks.preview, syncUpstreamModels: mocks.sync
} } }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showSuccess: mocks.success, showError: mocks.error }) }))
vi.mock('vue-i18n', async () => ({ ...(await vi.importActual<typeof import('vue-i18n')>('vue-i18n')), useI18n: () => ({ t: (key: string) => key }) }))

const Dialog = defineComponent({ props: { show: Boolean }, template: '<div v-if="show"><slot /><slot name="footer" /></div>' })
function mountModal(account?: Account) {
  return mount(UpstreamAccountModal, { props: { show: true, account, groups: [], proxies: [] }, global: { stubs: { BaseDialog: Dialog, GroupSelector: true, ProxySelector: true } } })
}

describe('UpstreamAccountModal', () => {
  beforeEach(() => { vi.clearAllMocks(); mocks.create.mockResolvedValue({ id: 1 }); mocks.update.mockResolvedValue({ id: 2 }); mocks.preview.mockResolvedValue({ models: ['gpt-4.1', 'o3'] }) })

  it('creates only a custom API-key account and persists discovered models', async () => {
    const wrapper = mountModal()
    const inputs = wrapper.findAll('input')
    await inputs[0].setValue('Vendor A')
    await inputs[1].setValue('https://vendor.example/v1')
    await inputs[2].setValue('secret')
    await wrapper.findAll('button').find(button => button.text() === 'admin.upstream.discover')!.trigger('click')
    await flushPromises()
    expect(mocks.preview).toHaveBeenCalledWith({ platform: 'custom', type: 'apikey', base_url: 'https://vendor.example/v1', api_key: 'secret' })
    expect(wrapper.get('textarea').element.value).toBe('gpt-4.1\no3')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(mocks.create).toHaveBeenCalledWith(expect.objectContaining({
      name: 'Vendor A', platform: 'custom', type: 'apikey',
      credentials: { base_url: 'https://vendor.example/v1', api_key: 'secret', model_mapping: { 'gpt-4.1': 'gpt-4.1', o3: 'o3' } }
    }))
    expect(wrapper.emitted('saved')).toHaveLength(1)
  })

  it('edits a custom account without replacing its redacted API key', async () => {
    const account = { id: 2, name: 'Vendor B', platform: 'custom', type: 'apikey', credentials: { base_url: 'https://old.example/v1', model_mapping: { public: 'actual' } }, concurrency: 5, priority: 30, group_ids: [7], proxy_id: 3 } as Account
    const wrapper = mountModal(account)
    expect(wrapper.findAll('input[type="password"]')[0].element.value).toBe('')
    await wrapper.get('input[type="url"]').setValue('https://new.example/v1')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(mocks.update).toHaveBeenCalledWith(2, expect.objectContaining({
      credentials: { base_url: 'https://new.example/v1', model_mapping: { public: 'actual' } },
      concurrency: 5, priority: 30, group_ids: [7], proxy_id: 3
    }))
    expect(wrapper.emitted('saved')).toHaveLength(1)
  })
})
