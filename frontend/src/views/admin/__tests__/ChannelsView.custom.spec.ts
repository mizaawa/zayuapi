import { defineComponent, nextTick } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import type { Channel } from '@/api/admin/channels'
import type { AdminGroup } from '@/types'

const {
  listChannels,
  getAllGroups,
  getWebSearchConfig,
  syncPricingModels,
  updateChannel,
} = vi.hoisted(() => ({
  listChannels: vi.fn(),
  getAllGroups: vi.fn(),
  getWebSearchConfig: vi.fn(),
  syncPricingModels: vi.fn(),
  updateChannel: vi.fn(),
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    channels: {
      list: listChannels,
      create: vi.fn(),
      update: updateChannel,
      remove: vi.fn(),
      syncPricingModels,
    },
    groups: { getAll: getAllGroups },
    settings: { getWebSearchEmulationConfig: getWebSearchConfig },
    accounts: { list: vi.fn(), getById: vi.fn() },
  },
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showError: vi.fn(), showSuccess: vi.fn() }),
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => ({
        'admin.channels.createChannel': 'Create Channel',
        'admin.groups.platforms.anthropic': 'Anthropic',
        'admin.groups.platforms.openai': 'OpenAI',
        'admin.groups.platforms.gemini': 'Gemini',
        'admin.groups.platforms.antigravity': 'Antigravity',
        'admin.groups.platforms.grok': 'Grok',
        'admin.groups.platforms.custom': 'Custom',
      }[key] ?? key),
    }),
  }
})

import ChannelsView from '@/views/admin/ChannelsView.vue'
import PricingEntryCard from '@/components/admin/channel/PricingEntryCard.vue'

const groups = [
  { id: 12, name: 'Custom group', platform: 'composite', rate_multiplier: 1, account_count: 2 },
  { id: 13, name: 'OpenAI group', platform: 'openai', rate_multiplier: 1, account_count: 3 },
] as AdminGroup[]

const customChannel: Channel = {
  id: 77,
  name: 'Custom pricing',
  description: 'Custom upstream pricing',
  status: 'active',
  billing_model_source: 'channel_mapped',
  restrict_models: true,
  features_config: {},
  group_ids: [12],
  model_pricing: [{
    platform: 'custom',
    models: ['videos-mini-480p'],
    billing_mode: 'video',
    input_price: null,
    output_price: null,
    cache_write_price: null,
    cache_read_price: null,
    image_input_price: null,
    image_output_price: null,
    per_request_price: 0.25,
    intervals: [],
  }],
  model_mapping: { custom: { 'public-video': 'videos-mini-480p' } },
  apply_pricing_to_account_stats: false,
  account_stats_pricing_rules: [],
  created_at: '2026-09-17T00:00:00Z',
  updated_at: '2026-09-17T00:00:00Z',
}

const AppLayoutStub = defineComponent({ template: '<main><slot /></main>' })
const TablePageLayoutStub = defineComponent({
  template: '<section><slot name="filters" /><slot name="table" /><slot name="pagination" /></section>',
})
const DataTableStub = defineComponent({
  props: { data: { type: Array, default: () => [] } },
  template: '<div><div v-for="row in data" :key="row.id"><slot name="cell-actions" :row="row" /></div><slot v-if="data.length === 0" name="empty" /></div>',
})
const BaseDialogStub = defineComponent({
  props: { show: { type: Boolean, default: false } },
  template: '<div v-if="show"><slot /><slot name="footer" /></div>',
})
const SelectStub = defineComponent({
  props: {
    modelValue: { type: [String, Number], default: '' },
    options: { type: Array, default: () => [] },
  },
  template: '<select><option v-for="option in options" :key="option.value" :value="option.value">{{ option.label }}</option></select>',
})
const ToggleStub = defineComponent({
  props: { modelValue: { type: Boolean, default: false } },
  template: '<input type="checkbox" :checked="modelValue" />',
})
const PricingEntryCardStub = defineComponent({
  props: {
    entry: { type: Object, required: true },
    platform: { type: String, default: '' },
  },
  template: '<div data-testid="pricing-entry" :data-platform="platform">{{ entry.models.join(",") }}</div>',
})

function mountView(stubPricingEntries = true) {
  return mount(ChannelsView, {
    global: {
      stubs: {
        AppLayout: AppLayoutStub,
        TablePageLayout: TablePageLayoutStub,
        DataTable: DataTableStub,
        Pagination: true,
        BaseDialog: BaseDialogStub,
        ConfirmDialog: true,
        EmptyState: true,
        Select: SelectStub,
        Icon: true,
        PlatformIcon: true,
        Toggle: ToggleStub,
        PricingEntryCard: stubPricingEntries ? PricingEntryCardStub : false,
      },
    },
  })
}

async function openCreateDialog() {
  const wrapper = mountView()
  await flushPromises()
  const button = wrapper.findAll('button').find(item => item.text().includes('Create Channel'))
  if (!button) throw new Error('Create Channel button not found')
  await button.trigger('click')
  await flushPromises()
  return wrapper
}

async function enableCustomPlatform(wrapper: ReturnType<typeof mountView>) {
  const label = wrapper.findAll('label').find(item => item.text().includes('Custom'))
  if (!label) throw new Error('Custom platform option not found')
  await label.get('input[type="checkbox"]').setValue(true)
  await nextTick()

  const tab = wrapper.findAll('button.channel-tab').find(item => item.text().includes('Custom'))
  if (!tab) throw new Error('Custom platform tab not found')
  await tab.trigger('click')
  await nextTick()
}

async function enableOpenAIPlatform(wrapper: ReturnType<typeof mountView>) {
  const label = wrapper.findAll('label').find(item => item.text().includes('OpenAI'))
  if (!label) throw new Error('OpenAI platform option not found')
  await label.get('input[type="checkbox"]').setValue(true)
  await nextTick()

  const tab = wrapper.findAll('button.channel-tab').find(item => item.text().includes('OpenAI'))
  if (!tab) throw new Error('OpenAI platform tab not found')
  await tab.trigger('click')
  await nextTick()
}

describe('ChannelsView Custom pricing', () => {
  beforeEach(() => {
    listChannels.mockReset().mockResolvedValue({ items: [], total: 0 })
    getAllGroups.mockReset().mockResolvedValue(groups)
    getWebSearchConfig.mockReset().mockResolvedValue({ enabled: false, providers: [] })
    syncPricingModels.mockReset().mockResolvedValue({ models: [] })
    updateChannel.mockReset().mockResolvedValue(customChannel)
  })

  it('offers Custom and binds it only to composite-backed Custom groups', async () => {
    const wrapper = await openCreateDialog()
    await enableCustomPlatform(wrapper)

    expect(wrapper.text()).toContain('Custom group')
    expect(wrapper.text()).not.toContain('OpenAI group')
  })

  it('does not expose composite-backed Custom groups to OpenAI pricing', async () => {
    const wrapper = await openCreateDialog()
    await enableOpenAIPlatform(wrapper)

    expect(wrapper.text()).toContain('OpenAI group')
    expect(wrapper.text()).not.toContain('Custom group')
  })

  it('does not offer model synchronization for Custom pricing', async () => {
    const wrapper = await openCreateDialog()
    await enableCustomPlatform(wrapper)

    expect(wrapper.findAll('button').filter(button => (
      button.text().includes('admin.channels.form.syncLatestModels')
    ))).toHaveLength(0)
    expect(syncPricingModels).not.toHaveBeenCalled()
  })

  it('keeps model sync scoped to its original platform and disables other sync buttons', async () => {
    let resolveSync!: (value: { models: string[] }) => void
    syncPricingModels.mockReturnValue(new Promise(resolve => { resolveSync = resolve }))
    const wrapper = await openCreateDialog()
    await enableOpenAIPlatform(wrapper)
    const syncButton = () => wrapper.findAll('button').find(button => button.isVisible() && (
      button.text() === 'admin.channels.form.syncLatestModels' || button.text() === 'admin.channels.form.syncingModels'
    ))!
    await syncButton().trigger('click')

    const geminiLabel = wrapper.findAll('label').find(label => label.text() === 'Gemini')!
    await geminiLabel.get('input[type="checkbox"]').setValue(true)
    await wrapper.findAll('button.channel-tab').find(button => button.text() === 'Gemini')!.trigger('click')
    expect(syncButton().attributes('disabled')).toBeDefined()
    await syncButton().trigger('click')
    expect(syncPricingModels).toHaveBeenCalledTimes(1)
    expect(syncPricingModels).toHaveBeenCalledWith('openai')

    resolveSync({ models: ['gpt-latest'] })
    await flushPromises()
    expect(wrapper.findAll('[data-testid="pricing-entry"]')).toHaveLength(1)
    expect(wrapper.get('[data-testid="pricing-entry"]').attributes('data-platform')).toBe('openai')
    expect(wrapper.get('[data-testid="pricing-entry"]').text()).toBe('gpt-latest')
    expect(syncButton().attributes('disabled')).toBeUndefined()
    wrapper.unmount()
  })

  it('discards a model sync response after the channel form has been replaced', async () => {
    let resolveSync!: (value: { models: string[] }) => void
    syncPricingModels.mockReturnValue(new Promise(resolve => { resolveSync = resolve }))
    const wrapper = await openCreateDialog()
    await enableOpenAIPlatform(wrapper)
    await wrapper.findAll('button').find(button => button.text() === 'admin.channels.form.syncLatestModels')!.trigger('click')
    await wrapper.findAll('button').find(button => button.text() === 'common.cancel')!.trigger('click')
    await wrapper.findAll('button').find(button => button.text() === 'Create Channel')!.trigger('click')
    await flushPromises()
    await enableOpenAIPlatform(wrapper)

    resolveSync({ models: ['old-channel-model'] })
    await flushPromises()
    expect(wrapper.findAll('[data-testid="pricing-entry"]')).toHaveLength(0)
    wrapper.unmount()
  })

  it('round-trips Custom group mapping and video pricing when editing', async () => {
    listChannels.mockResolvedValue({ items: [customChannel], total: 1 })
    const wrapper = mountView()
    await flushPromises()

    const editButton = wrapper.findAll('button').find(button => button.text().includes('common.edit'))
    if (!editButton) throw new Error('Edit button not found')
    await editButton.trigger('click')
    await flushPromises()

    const customTab = wrapper.findAll('button.channel-tab').find(button => button.text().includes('Custom'))
    if (!customTab) throw new Error('Custom tab not found')
    await customTab.trigger('click')
    await nextTick()

    expect(wrapper.get('[data-testid="pricing-entry"]').attributes('data-platform')).toBe('custom')
    expect(wrapper.get('[data-testid="pricing-entry"]').text()).toContain('videos-mini-480p')

    await wrapper.get('#channel-form').trigger('submit')
    await flushPromises()

    expect(updateChannel).toHaveBeenCalledWith(77, expect.objectContaining({
      group_ids: [12],
      model_mapping: { custom: { 'public-video': 'videos-mini-480p' } },
      model_pricing: [expect.objectContaining({
        platform: 'custom',
        models: ['videos-mini-480p'],
        billing_mode: 'video',
      })],
    }))
  })

  it('prepends expanded pricing entries and preserves existing card state when editing', async () => {
    listChannels.mockResolvedValue({ items: [customChannel], total: 1 })
    const wrapper = mountView(false)
    await flushPromises()

    const editButton = wrapper.findAll('button').find(button => button.text().includes('common.edit'))
    if (!editButton) throw new Error('Edit button not found')
    await editButton.trigger('click')
    await flushPromises()

    const customTab = wrapper.findAll('button.channel-tab').find(button => button.text().includes('Custom'))
    if (!customTab) throw new Error('Custom tab not found')
    await customTab.trigger('click')

    const existingCard = wrapper.getComponent(PricingEntryCard)
    expect(existingCard.get('.collapsible-content').classes()).toContain('collapsible-content--collapsed')

    const pricingSection = wrapper.findAll('div').find(element => (
      element.element.firstElementChild?.textContent?.trim() === 'admin.channels.form.modelPricing'
    ))
    if (!pricingSection) throw new Error('Model pricing section not found')
    const addButton = pricingSection.get('button')
    await addButton.trigger('click')

    let cards = wrapper.findAllComponents(PricingEntryCard)
    expect(cards.map(card => card.props('entry').models)).toEqual([[], ['videos-mini-480p']])
    expect(cards[0].get('.collapsible-content').classes()).not.toContain('collapsible-content--collapsed')
    expect(cards[1].element).toBe(existingCard.element)
    expect(cards[1].get('.collapsible-content').classes()).toContain('collapsible-content--collapsed')

    const firstAddedCard = cards[0]
    await firstAddedCard.get('input[type="text"]').setValue('draft-model')
    await addButton.trigger('click')

    cards = wrapper.findAllComponents(PricingEntryCard)
    expect(cards.map(card => card.props('entry').models)).toEqual([[], [], ['videos-mini-480p']])
    expect((cards[0].get('input[type="text"]').element as HTMLInputElement).value).toBe('')
    expect(cards[1].element).toBe(firstAddedCard.element)
    expect((cards[1].get('input[type="text"]').element as HTMLInputElement).value).toBe('draft-model')

    await cards[1].get('input[type="number"]').setValue('1.5')
    expect(wrapper.findAllComponents(PricingEntryCard)[1].element).toBe(firstAddedCard.element)
    expect(wrapper.findAllComponents(PricingEntryCard)[1].props('entry').input_price).toBe('1.5')
    expect(existingCard.props('entry').per_request_price).toBe(0.25)

    await cards[0].get('button').trigger('click')
    cards = wrapper.findAllComponents(PricingEntryCard)
    expect(cards).toHaveLength(2)
    expect(cards[0].element).toBe(firstAddedCard.element)
    expect(cards[1].element).toBe(existingCard.element)
    wrapper.unmount()
  })
})
