import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent } from 'vue'
import { flushPromises, mount, type DOMWrapper, type VueWrapper } from '@vue/test-utils'

import BackupView from '../BackupView.vue'
import type { UsageCleanupScheduleSettings } from '@/api/admin/usage'

const {
  getS3Config,
  getWebDAVConfig,
  getImageStorageConfig,
  getSchedule,
  listBackups,
  getRetentionSettings,
  updateRetentionSettings,
  getDatabaseStorageStats,
  createRetentionCleanup,
  showSuccess,
  showError,
} = vi.hoisted(() => ({
  getS3Config: vi.fn(),
  getWebDAVConfig: vi.fn(),
  getImageStorageConfig: vi.fn(),
  getSchedule: vi.fn(),
  listBackups: vi.fn(),
  getRetentionSettings: vi.fn(),
  updateRetentionSettings: vi.fn(),
  getDatabaseStorageStats: vi.fn(),
  createRetentionCleanup: vi.fn(),
  showSuccess: vi.fn(),
  showError: vi.fn(),
}))

vi.mock('@/api', () => ({
  adminAPI: {
    backup: {
      getS3Config,
      getWebDAVConfig,
      getImageStorageConfig,
      getSchedule,
      listBackups,
    },
    usage: {
      getRetentionSettings,
      updateRetentionSettings,
      getDatabaseStorageStats,
      createRetentionCleanup,
    },
  },
}))

vi.mock('@/stores', () => ({
  useAppStore: () => ({ showSuccess, showError, showWarning: vi.fn() }),
}))

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string, params?: Record<string, string | number>) =>
      params ? `${key} ${Object.values(params).join(' ')}` : key,
  }),
}))

const BaseDialogStub = defineComponent({
  props: { show: Boolean, title: String },
  emits: ['close'],
  template: '<div v-if="show" role="dialog"><h2>{{ title }}</h2><slot /><slot name="footer" /></div>',
})

const mountedViews: VueWrapper[] = []

function mountView(): VueWrapper {
  const wrapper = mount(BackupView, {
    global: {
      stubs: { BaseDialog: BaseDialogStub, TotpStepUpDialog: true, teleport: true },
    },
  })
  mountedViews.push(wrapper)
  return wrapper
}

function usagePanel(wrapper: VueWrapper): DOMWrapper<Element> {
  const panel = wrapper.findAll('.card').find((card) =>
    card.find('h3').text().includes('admin.backup.usageLogs.title')
  )
  if (!panel) throw new Error('Usage_logs panel not found')
  return panel
}

function databaseStoragePanel(wrapper: VueWrapper): DOMWrapper<Element> {
  const panel = wrapper.findAll('.card').find((card) =>
    card.find('h3').text().includes('admin.backup.databaseStorage.title')
  )
  if (!panel) throw new Error('Database storage panel not found')
  return panel
}

function buttonByText(scope: VueWrapper | DOMWrapper<Element>, text: string): DOMWrapper<HTMLButtonElement> {
  const button = scope.findAll<HTMLButtonElement>('button').find((item) => item.text() === text)
  if (!button) throw new Error(`button not found: ${text}`)
  return button
}

function numberInputByLabel(panel: DOMWrapper<Element>, label: string): DOMWrapper<HTMLInputElement> {
  const input = panel.findAll<HTMLInputElement>('input[type="number"]').find((item) =>
    panel.find(`label[for="${item.element.id}"]`).text() === label
  )
  if (!input) throw new Error(`number input not found: ${label}`)
  return input
}

describe('admin BackupView Usage_logs management', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    getS3Config.mockResolvedValue({})
    getWebDAVConfig.mockResolvedValue({})
    getImageStorageConfig.mockResolvedValue({ config: {}, secret_configured: false })
    getSchedule.mockResolvedValue({ enabled: false })
    listBackups.mockResolvedValue({ items: [] })
    getRetentionSettings.mockResolvedValue({ enabled: false, interval_days: 7, retention_days: 45, delete_all: false })
    updateRetentionSettings.mockImplementation(async (settings: UsageCleanupScheduleSettings) => ({ ...settings }))
    getDatabaseStorageStats.mockResolvedValue({
      database_name: 'app',
      database_bytes: 2 * 1024 ** 3,
      tables: [
        { table_name: 'public.usage_logs', table_bytes: 1024 ** 3, index_bytes: 1024 ** 2, total_bytes: 1024 ** 3 + 1024 ** 2 },
        { table_name: 'public.users', table_bytes: 1024 ** 2, index_bytes: 1024 ** 2, total_bytes: 2 * 1024 ** 2 },
      ],
      table_bytes: 1024 ** 3,
      index_bytes: 1024 ** 2,
      total_bytes: 1024 ** 3 + 2 * 1024 ** 2,
      measured_at: '2026-10-02T00:00:00Z',
    })
    createRetentionCleanup.mockResolvedValue({ id: 11, status: 'pending' })
  })

  afterEach(() => {
    mountedViews.splice(0).forEach((wrapper) => wrapper.unmount())
  })

  it('measures all database storage only on demand and permits another measurement', async () => {
    const wrapper = mountView()
    await flushPromises()
    const panel = databaseStoragePanel(wrapper)

    expect(getRetentionSettings).toHaveBeenCalledTimes(1)
    expect(getDatabaseStorageStats).not.toHaveBeenCalled()
    expect(panel.text()).not.toContain('admin.backup.databaseStorage.databaseSize')

    await buttonByText(panel, 'admin.backup.databaseStorage.measure').trigger('click')
    await flushPromises()

    expect(getDatabaseStorageStats).toHaveBeenCalledTimes(1)
    expect(panel.text()).toContain('public.usage_logs')
    expect(panel.text()).toContain('public.users')
    expect(panel.text()).toContain('admin.backup.databaseStorage.databaseSize')
    expect(panel.text()).toContain('admin.backup.databaseStorage.indexSizeTotal')
    expect(panel.text()).toContain('admin.backup.databaseStorage.businessTotal')
    expect(updateRetentionSettings).not.toHaveBeenCalled()
    expect(createRetentionCleanup).not.toHaveBeenCalled()

    await buttonByText(panel, 'admin.backup.databaseStorage.remeasure').trigger('click')
    await flushPromises()
    expect(getDatabaseStorageStats).toHaveBeenCalledTimes(2)
  })

  it('saves the cleanup interval while preserving the stored cleanup range', async () => {
    const wrapper = mountView()
    await flushPromises()
    const panel = usagePanel(wrapper)

    expect(panel.text()).not.toContain('admin.backup.usageLogs.intervalDays')
    expect(panel.text()).not.toContain('admin.backup.usageLogs.retentionDays')
    expect(panel.text()).not.toContain('admin.backup.usageLogs.deleteAll')
    expect(panel.text()).not.toContain('admin.backup.usageLogs.retentionHint')
    await panel.get('input[type="checkbox"]').setValue(true)
    await numberInputByLabel(panel, 'admin.backup.usageLogs.intervalDays').setValue(14)
    expect(panel.find('#usage-cleanup-retention').exists()).toBe(false)
    expect(panel.findAll('input[type="checkbox"]')).toHaveLength(1)
    await buttonByText(panel, 'common.save').trigger('click')
    await flushPromises()

    expect(updateRetentionSettings).toHaveBeenCalledWith({
      enabled: true,
      interval_days: 14,
      retention_days: 45,
      delete_all: false,
    })
    expect(showSuccess).toHaveBeenCalledWith('admin.backup.usageLogs.settingsSaved')
  })

  it('does not queue cleanup when the confirmation is cancelled', async () => {
    const wrapper = mountView()
    await flushPromises()
    await buttonByText(usagePanel(wrapper), 'admin.backup.usageLogs.manualCleanup').trigger('click')

    expect(wrapper.get('[role="dialog"]').text()).toContain('admin.backup.usageLogs.confirmMessage 45')
    expect(createRetentionCleanup).not.toHaveBeenCalled()

    await buttonByText(wrapper.get('[role="dialog"]'), 'common.cancel').trigger('click')
    await flushPromises()

    expect(createRetentionCleanup).not.toHaveBeenCalled()
    expect(wrapper.find('[role="dialog"]').exists()).toBe(false)
  })

  it('allows a separate manual retention period without enabling the schedule', async () => {
    const wrapper = mountView()
    await flushPromises()
    const panel = usagePanel(wrapper)
    const manualCleanup = buttonByText(panel, 'admin.backup.usageLogs.manualCleanup')
    expect(manualCleanup.classes()).toContain('btn-primary')
    expect(manualCleanup.classes()).not.toContain('btn-danger')
    await manualCleanup.trigger('click')

    expect(createRetentionCleanup).not.toHaveBeenCalled()
    expect(wrapper.get<HTMLInputElement>('#manual-usage-retention').element.value).toBe('45')
    await wrapper.get('#manual-usage-retention').setValue(30)
    expect(wrapper.get('[role="dialog"]').text()).toContain('admin.backup.usageLogs.confirmMessage 30')
    const confirmCleanup = buttonByText(wrapper.get('[role="dialog"]'), 'admin.backup.usageLogs.confirmCleanup')
    expect(confirmCleanup.classes()).toContain('btn-danger')
    await confirmCleanup.trigger('click')
    await flushPromises()

    expect(createRetentionCleanup).toHaveBeenCalledTimes(1)
    expect(createRetentionCleanup).toHaveBeenCalledWith(30, false)
    expect(showSuccess).toHaveBeenCalledWith('admin.backup.usageLogs.cleanupQueued 11')
    expect(wrapper.find('[role="dialog"]').exists()).toBe(false)
    expect(updateRetentionSettings).not.toHaveBeenCalled()
    await manualCleanup.trigger('click')
    expect(wrapper.get<HTMLInputElement>('#manual-usage-retention').element.value).toBe('45')
  })

  it('keeps cleanup unavailable when persisted settings cannot be loaded', async () => {
    getRetentionSettings.mockRejectedValueOnce(new Error('settings unavailable'))
    const wrapper = mountView()
    await flushPromises()
    const panel = usagePanel(wrapper)

    expect(panel.get('input[type="checkbox"]').attributes('disabled')).toBeDefined()
    expect(buttonByText(panel, 'common.save').attributes('disabled')).toBeDefined()
    expect(buttonByText(panel, 'admin.backup.usageLogs.manualCleanup').attributes('disabled')).toBeDefined()
    expect(showError).toHaveBeenCalledWith('settings unavailable')
    expect(createRetentionCleanup).not.toHaveBeenCalled()
  })

  it('preserves the stored delete-all setting when the manual range is changed', async () => {
    getRetentionSettings.mockResolvedValueOnce({ enabled: false, interval_days: 7, retention_days: 45, delete_all: true })
    const wrapper = mountView()
    await flushPromises()
    const panel = usagePanel(wrapper)
    await buttonByText(panel, 'admin.backup.usageLogs.manualCleanup').trigger('click')
    const dialog = wrapper.get('[role="dialog"]')
    const deleteAll = dialog.get('input[type="checkbox"]')
    const days = dialog.get<HTMLInputElement>('#manual-usage-retention')

    expect(days.element.disabled).toBe(true)
    await deleteAll.setValue(false)
    expect(days.element.disabled).toBe(false)
    expect(days.element.value).toBe('45')
    await days.setValue(30)
    await buttonByText(dialog, 'admin.backup.usageLogs.confirmCleanup').trigger('click')
    await flushPromises()
    expect(createRetentionCleanup).toHaveBeenCalledWith(30, false)

    await buttonByText(panel, 'common.save').trigger('click')
    await flushPromises()
    expect(updateRetentionSettings).toHaveBeenCalledWith({
      enabled: false, interval_days: 7, retention_days: 45, delete_all: true,
    })
  })

  it('requires confirmation before enabling scheduled delete-all cleanup', async () => {
    getRetentionSettings.mockResolvedValueOnce({ enabled: false, interval_days: 7, retention_days: 45, delete_all: true })
    const wrapper = mountView()
    await flushPromises()
    const panel = usagePanel(wrapper)
    const enabled = panel.get<HTMLInputElement>('input[type="checkbox"]')

    await enabled.setValue(true)
    await buttonByText(panel, 'common.save').trigger('click')
    await flushPromises()

    expect(wrapper.get('[role="dialog"]').text()).toContain('admin.backup.usageLogs.confirmScheduledDeleteAllMessage')
    expect(updateRetentionSettings).not.toHaveBeenCalled()
    await buttonByText(wrapper.get('[role="dialog"]'), 'common.cancel').trigger('click')
    await flushPromises()
    expect(updateRetentionSettings).not.toHaveBeenCalled()

    await buttonByText(panel, 'common.save').trigger('click')
    const dialog = wrapper.get('[role="dialog"]')
    await buttonByText(dialog, 'admin.backup.usageLogs.confirmScheduledDeleteAll').trigger('click')
    await flushPromises()

    expect(updateRetentionSettings).toHaveBeenCalledWith({
      enabled: true, interval_days: 7, retention_days: 45, delete_all: true,
    })
    expect(showSuccess).toHaveBeenCalledWith('admin.backup.usageLogs.settingsSaved')
    expect(wrapper.find('[role="dialog"]').exists()).toBe(false)
  })

  it('requires confirmation before queuing manual delete-all cleanup', async () => {
    const wrapper = mountView()
    await flushPromises()
    await buttonByText(usagePanel(wrapper), 'admin.backup.usageLogs.manualCleanup').trigger('click')
    const dialog = wrapper.get('[role="dialog"]')
    await dialog.get('input[type="checkbox"]').setValue(true)

    expect(wrapper.get<HTMLInputElement>('#manual-usage-retention').element.disabled).toBe(true)
    expect(dialog.text()).toContain('admin.backup.usageLogs.confirmMessageAll')
    expect(createRetentionCleanup).not.toHaveBeenCalled()
    await buttonByText(dialog, 'admin.backup.usageLogs.confirmCleanup').trigger('click')
    await flushPromises()
    expect(createRetentionCleanup).toHaveBeenCalledWith(45, true)
    expect(updateRetentionSettings).not.toHaveBeenCalled()
  })

  it('rejects an invalid manual retention period before queuing cleanup', async () => {
    const wrapper = mountView()
    await flushPromises()
    await buttonByText(usagePanel(wrapper), 'admin.backup.usageLogs.manualCleanup').trigger('click')
    await wrapper.get('#manual-usage-retention').setValue(0)
    await buttonByText(wrapper.get('[role="dialog"]'), 'admin.backup.usageLogs.confirmCleanup').trigger('click')
    await flushPromises()

    expect(createRetentionCleanup).not.toHaveBeenCalled()
    expect(showError).toHaveBeenCalledWith('admin.backup.usageLogs.invalidRetentionDays')
    expect(wrapper.find('[role="dialog"]').exists()).toBe(true)
  })
})
