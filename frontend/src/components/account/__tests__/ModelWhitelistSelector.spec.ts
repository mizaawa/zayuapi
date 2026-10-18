import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

const copyToClipboard = vi.fn().mockResolvedValue(true)

const { syncUpstreamModels, syncUpstreamModelsPreview } = vi.hoisted(() => ({
  syncUpstreamModels: vi.fn(),
  syncUpstreamModelsPreview: vi.fn()
}))

vi.mock('@/api/admin/accounts', () => ({
  accountsAPI: { syncUpstreamModels, syncUpstreamModelsPreview }
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => (key === 'common.copy' ? '复制' : key)
    })
  }
})

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError: vi.fn(),
    showSuccess: vi.fn(),
    showInfo: vi.fn()
  })
}))

vi.mock('@/composables/useClipboard', () => ({
  useClipboard: () => ({
    copyToClipboard
  })
}))

import ModelWhitelistSelector from '../ModelWhitelistSelector.vue'

function mountSelector(props: Record<string, unknown> = {}) {
  return mount(ModelWhitelistSelector, {
    props: {
      modelValue: [],
      platform: 'openai',
      ...props
    },
    global: {
      stubs: {
        ModelIcon: true
      }
    }
  })
}

function findModelRow(wrapper: ReturnType<typeof mountSelector>, modelId: string) {
  const row = wrapper
    .findAll('[data-testid="model-option"]')
    .find(candidate => candidate.text().includes(modelId))

  if (!row) {
    throw new Error(`Model row not found: ${modelId}`)
  }

  return row
}

describe('ModelWhitelistSelector', () => {
  beforeEach(() => {
    copyToClipboard.mockClear()
    syncUpstreamModels.mockReset().mockResolvedValue({ models: ['upstream-model'] })
    syncUpstreamModelsPreview.mockReset().mockResolvedValue({ models: ['preview-model'] })
  })

  it('copies a model ID without selecting the model', async () => {
    const wrapper = mountSelector()
    await wrapper.get('div.cursor-pointer').trigger('click')

    const row = findModelRow(wrapper, 'gpt-5.6-sol')

    const copyButton = row.get('[data-testid="copy-model-id"]')
    expect(copyButton.attributes('aria-label')).toBe('复制 gpt-5.6-sol')

    await copyButton.trigger('click')
    await flushPromises()

    expect(copyToClipboard).toHaveBeenCalledWith('gpt-5.6-sol')
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
  })

  it('keeps the existing model selection behavior', async () => {
    const wrapper = mountSelector()
    await wrapper.get('div.cursor-pointer').trigger('click')

    const row = findModelRow(wrapper, 'gpt-5.6-sol')
    await row.get('[data-testid="select-model"]').trigger('click')

    expect(wrapper.emitted('update:modelValue')).toEqual([[['gpt-5.6-sol']]])
    expect(copyToClipboard).not.toHaveBeenCalled()
  })

  it('passes current overrides on each saved-account model request', async () => {
    const wrapper = mountSelector({
      accountId: 7,
      syncOverrides: { base_url: 'https://first.example/v1', api_key: 'first-key' }
    })
    const syncButton = wrapper.findAll('button').find(button => button.text() === 'admin.accounts.syncUpstreamModels')!

    await syncButton.trigger('click')
    await flushPromises()

    expect(syncUpstreamModels).toHaveBeenLastCalledWith(7, {
      base_url: 'https://first.example/v1',
      api_key: 'first-key'
    })
    expect(wrapper.emitted('update:modelValue')).toEqual([[['upstream-model']]])

    await wrapper.setProps({
      syncOverrides: { base_url: 'https://second.example/v1', api_key: 'second-key' }
    })
    await syncButton.trigger('click')
    await flushPromises()

    expect(syncUpstreamModels).toHaveBeenLastCalledWith(7, {
      base_url: 'https://second.example/v1',
      api_key: 'second-key'
    })
    expect(syncUpstreamModelsPreview).not.toHaveBeenCalled()
  })

  it('keeps existing saved-account model requests without credential overrides', async () => {
    const wrapper = mountSelector({ accountId: 7 })
    const syncButton = wrapper.findAll('button').find(button => button.text() === 'admin.accounts.syncUpstreamModels')!

    await syncButton.trigger('click')
    await flushPromises()

    expect(syncUpstreamModels).toHaveBeenCalledWith(7, undefined)
    expect(syncUpstreamModelsPreview).not.toHaveBeenCalled()
  })

  it('preserves model preview using unsaved new-account credentials', async () => {
    const credentials = {
      platform: 'openai',
      type: 'apikey',
      base_url: 'https://new.example/v1',
      api_key: 'new-key'
    }
    const wrapper = mountSelector({ syncCredentials: credentials })
    const syncButton = wrapper.findAll('button').find(button => button.text() === 'admin.accounts.syncUpstreamModels')!

    await syncButton.trigger('click')
    await flushPromises()

    expect(syncUpstreamModelsPreview).toHaveBeenCalledWith(credentials)
    expect(syncUpstreamModels).not.toHaveBeenCalled()
    expect(wrapper.emitted('update:modelValue')).toEqual([[['preview-model']]])
  })
})
