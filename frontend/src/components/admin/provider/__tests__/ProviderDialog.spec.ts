import { defineComponent } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import ProviderDialog from '../ProviderDialog.vue'

const mocks = vi.hoisted(() => ({ create: vi.fn(), update: vi.fn(), probe: vi.fn(), success: vi.fn(), error: vi.fn() }))
vi.mock('@/api/admin/upstream', () => ({ createProvider: mocks.create, updateProvider: mocks.update, probeProvider: mocks.probe }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showSuccess: mocks.success, showError: mocks.error }) }))
vi.mock('vue-i18n', async () => ({
  ...(await vi.importActual<typeof import('vue-i18n')>('vue-i18n')),
  useI18n: () => ({ t: (key: string) => key })
}))
const Dialog = defineComponent({ props: { show: Boolean }, template: '<div v-if="show"><slot /><slot name="footer" /></div>' })
const mountDialog = () => mount(ProviderDialog, { props: { show: true }, global: { stubs: { BaseDialog: Dialog, Icon: true } } })

describe('ProviderDialog login forms', () => {
  beforeEach(() => { vi.clearAllMocks(); mocks.create.mockResolvedValue({ id: 'one' }); mocks.probe.mockResolvedValue({ status: 'connected' }) })

  it('sends Sub2API password login and shows success toast', async () => {
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
    expect(mocks.success).toHaveBeenCalledWith('admin.upstream.probeSuccess')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(mocks.create).toHaveBeenCalledWith(expect.objectContaining({ name: 'Vendor A', exchange_rate: 1 }))
    expect(wrapper.emitted('saved')).toHaveLength(1)
    wrapper.unmount()
  })

  it('sends Sub2API dashboard tokens rather than a model API key', async () => {
    const wrapper = mountDialog()
    await wrapper.get('input[name="provider-auth"][value="token"]').setValue()
    await wrapper.get('input[placeholder="admin.upstream.accessTokenPlaceholder"]').setValue('dashboard-access')
    await wrapper.get('input[placeholder="admin.upstream.refreshTokenPlaceholder"]').setValue('dashboard-refresh')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(mocks.create).toHaveBeenCalledWith(expect.objectContaining({
      site_type: 'sub2api', auth_mode: 'token', access_token: 'dashboard-access', refresh_token: 'dashboard-refresh', token_type: 'Bearer'
    }))
    expect(mocks.create.mock.calls[0][0]).not.toHaveProperty('api_key')
    wrapper.unmount()
  })

  it('shows New API password account and token fields separately', async () => {
    const wrapper = mountDialog()
    await wrapper.get('input[name="provider-kind"][value="newapi"]').setValue()
    expect(wrapper.find('input[autocomplete="username"]').exists()).toBe(true)
    await wrapper.get('input[autocomplete="username"]').setValue('newapi-user')
    await wrapper.get('input[autocomplete="new-password"]').setValue('newapi-password')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(mocks.create).toHaveBeenCalledWith(expect.objectContaining({
      site_type: 'newapi', auth_mode: 'password', account: 'newapi-user', password: 'newapi-password'
    }))
    mocks.create.mockClear()
    await wrapper.get('input[name="provider-auth"][value="token"]').setValue()
    expect(wrapper.find('input[autocomplete="username"]').exists()).toBe(false)
    await wrapper.get('input[pattern="[1-9][0-9]*"]').setValue('123')
    await wrapper.get('input[placeholder="admin.upstream.systemTokenPlaceholder"]').setValue('dashboard-system-token')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(mocks.create).toHaveBeenCalledWith(expect.objectContaining({ user_id: '123', system_access_token: 'dashboard-system-token' }))
    wrapper.unmount()
  })

  it('renders the server failure reason inline and in a toast', async () => {
    mocks.create.mockRejectedValueOnce({ message: 'TURNSTILE_VERIFICATION_FAILED' })
    const wrapper = mountDialog()
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(wrapper.get('[role="status"]').text()).toBe('TURNSTILE_VERIFICATION_FAILED')
    expect(mocks.error).toHaveBeenCalledWith('TURNSTILE_VERIFICATION_FAILED')
    expect(wrapper.emitted('saved')).toBeUndefined()
    wrapper.unmount()
  })
})
