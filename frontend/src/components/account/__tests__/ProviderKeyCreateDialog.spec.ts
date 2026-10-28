import { defineComponent } from 'vue'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

const mocks = vi.hoisted(() => ({
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

import ProviderKeyCreateDialog from '../ProviderKeyCreateDialog.vue'

const BaseDialogStub = defineComponent({
  name: 'BaseDialog',
  props: { show: { type: Boolean, default: false } },
  emits: ['close'],
  template: '<div v-if="show" data-testid="dialog"><slot /><slot name="footer" /></div>'
})

const provider = { id: 'p1', name: '杂鱼AI', site_type: 'sub2api', base_url: 'https://www.zayuapi.com', status: 'connected' }
const groups = [
  { id: '12', name: 'Gemini-默认通道', platform: 'gemini', multiplier: 0.25, key_name: '[杂鱼]Gemini-默认通道' },
  { id: '13', name: 'CodexPro', platform: 'openai', multiplier: 0.18, key_name: '[杂鱼]CodexPro' }
]

function mountDialog() {
  return mount(ProviderKeyCreateDialog, {
    props: { show: true, provider },
    global: { stubs: { teleport: true, BaseDialog: BaseDialogStub } }
  })
}

async function chooseGroup(wrapper: VueWrapper, index: number) {
  await wrapper.get('[data-testid="provider-key-group-picker"] button').trigger('click')
  await wrapper.get('[data-testid="provider-key-group-picker"]').findAll('[role="option"]')[index].trigger('click')
}

const submit = (wrapper: VueWrapper) => wrapper.get('[data-testid="provider-key-create-submit"]')
const cancel = (wrapper: VueWrapper) => wrapper.findAll('button').find((button) => button.text() === 'common.cancel')!

describe('ProviderKeyCreateDialog', () => {
  beforeEach(() => {
    mocks.listProviderKeyGroups.mockReset().mockResolvedValue({ provider_id: 'p1', site_type: 'sub2api', groups })
    mocks.createProviderKey.mockReset()
  })

  it('loads the provider groups and previews the prefixed key name', async () => {
    const wrapper = mountDialog()
    await flushPromises()

    expect(mocks.listProviderKeyGroups).toHaveBeenCalledWith('p1')
    expect(wrapper.text()).toContain('杂鱼AI')
    expect(wrapper.get('[data-testid="provider-key-name-preview"]').text()).toBe('admin.providerImport.keyNamePending')
    expect(submit(wrapper).attributes('disabled')).toBeDefined()

    await chooseGroup(wrapper, 1)
    expect(wrapper.get('[data-testid="provider-key-name-preview"]').text()).toBe('[杂鱼]CodexPro')
    expect(submit(wrapper).attributes('disabled')).toBeUndefined()
  })

  it('emits the created key', async () => {
    const created = { provider_id: 'p1', created: true, name: '[杂鱼]Gemini-默认通道', group_id: '12', key_id: '99' }
    mocks.createProviderKey.mockResolvedValue(created)
    const wrapper = mountDialog()
    await flushPromises()
    await chooseGroup(wrapper, 0)
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    expect(mocks.createProviderKey).toHaveBeenCalledWith('p1', '12')
    expect(wrapper.emitted('created')).toEqual([[created]])
  })

  it('refuses another creation after an uncertain result and asks for a refresh', async () => {
    mocks.createProviderKey.mockRejectedValue({ status: 502, reason: 'PROVIDER_KEY_CREATE_UNCERTAIN', message: 'gateway timeout' })
    const wrapper = mountDialog()
    await flushPromises()
    await chooseGroup(wrapper, 0)
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    expect(wrapper.get('[data-testid="provider-key-create-error"]').text()).toContain('admin.providerImport.createUncertain')
    expect(submit(wrapper).attributes('disabled')).toBeDefined()
    expect(wrapper.find('[data-testid="provider-key-refresh-groups"]').exists()).toBe(false)
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(mocks.createProviderKey).toHaveBeenCalledTimes(1)

    await cancel(wrapper).trigger('click')
    expect(wrapper.emitted('close')).toEqual([[true]])
  })

  it('treats a lost response as uncertain', async () => {
    mocks.createProviderKey.mockRejectedValue({ status: 0, message: 'Network error. Please check your connection.' })
    const wrapper = mountDialog()
    await flushPromises()
    await chooseGroup(wrapper, 0)
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    expect(wrapper.get('[data-testid="provider-key-create-error"]').text()).toContain('admin.providerImport.createUncertain')
    expect(submit(wrapper).attributes('disabled')).toBeDefined()
  })

  it('allows a retry after a definite refusal', async () => {
    const created = { provider_id: 'p1', created: true, name: '[杂鱼]CodexPro', group_id: '13', key_id: '7' }
    mocks.createProviderKey
      .mockRejectedValueOnce({ status: 400, reason: 'PROVIDER_REMOTE_FAILED', message: 'Provider rejected the request: 令牌名称过长' })
      .mockResolvedValueOnce(created)
    const wrapper = mountDialog()
    await flushPromises()
    await chooseGroup(wrapper, 1)
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    const error = wrapper.get('[data-testid="provider-key-create-error"]').text()
    expect(error).toContain('admin.providerImport.createFailed')
    expect(error).toContain('令牌名称过长')
    expect(submit(wrapper).attributes('disabled')).toBeUndefined()

    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(wrapper.emitted('created')).toEqual([[created]])

    await cancel(wrapper).trigger('click')
    expect(wrapper.emitted('close')).toEqual([[false]])
  })

  it('shows why the groups could not be read', async () => {
    mocks.listProviderKeyGroups.mockReset().mockRejectedValue({ status: 502, message: 'Provider login failed' })
    const wrapper = mountDialog()
    await flushPromises()

    expect(wrapper.get('[data-testid="provider-key-create-error"]').text()).toContain('Provider login failed')
    expect(submit(wrapper).attributes('disabled')).toBeDefined()
  })
})
