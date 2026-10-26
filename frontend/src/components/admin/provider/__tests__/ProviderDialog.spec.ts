import { defineComponent } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import ProviderDialog from '../ProviderDialog.vue'

const mocks = vi.hoisted(() => ({ create: vi.fn(), update: vi.fn(), probe: vi.fn() }))
vi.mock('@/api/admin/upstream', () => ({ createProvider: mocks.create, updateProvider: mocks.update, probeProvider: mocks.probe }))
vi.mock('vue-i18n', async () => ({
  ...(await vi.importActual<typeof import('vue-i18n')>('vue-i18n')),
  useI18n: () => ({ t: (key: string) => key })
}))
const Dialog = defineComponent({ props: { show: Boolean }, template: '<div v-if="show"><slot /><slot name="footer" /></div>' })
const mountDialog = () => mount(ProviderDialog, { props: { show: true }, global: { stubs: { BaseDialog: Dialog, Icon: true } } })

describe('ProviderDialog', () => {
  beforeEach(() => { vi.clearAllMocks(); mocks.create.mockResolvedValue({ id: 'one' }); mocks.probe.mockResolvedValue({ status: 'connected' }) })

  it('logs into a Sub2API site and saves only provider fields', async () => {
    const wrapper = mountDialog()
    await wrapper.get('input[placeholder="admin.upstream.namePlaceholder"]').setValue('Vendor A')
    await wrapper.get('input[type="url"]').setValue('https://vendor.example.com')
    await wrapper.get('input[autocomplete="username"]').setValue('demo@example.com')
    await wrapper.get('input[autocomplete="new-password"]').setValue('example-password')
    await wrapper.findAll('button').find(button => button.text() === 'admin.upstream.probe')!.trigger('click')
    await flushPromises()
    expect(mocks.probe).toHaveBeenCalledWith(expect.objectContaining({
      site_type: 'sub2api', auth_mode: 'password', base_url: 'https://vendor.example.com', account: 'demo@example.com'
    }))
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(mocks.create).toHaveBeenCalledWith(expect.objectContaining({ name: 'Vendor A', exchange_rate: 1 }))
    expect(wrapper.emitted('saved')).toHaveLength(1)
  })

  it('switches to New API user-key login without account fields', async () => {
    const wrapper = mountDialog()
    await wrapper.get('input[value="newapi"]').setValue()
    await wrapper.get('input[value="api_key"]').setValue()
    expect(wrapper.find('input[autocomplete="username"]').exists()).toBe(false)
    expect(wrapper.find('input[autocomplete="off"]').exists()).toBe(true)
  })
})
