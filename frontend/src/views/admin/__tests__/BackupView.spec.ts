import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

import BackupView from '../BackupView.vue'

const {
  getS3Config,
  getImageStorageConfig,
  getSchedule,
  updateSchedule,
  deleteBackup,
  listBackups,
  getDownloadURL,
} = vi.hoisted(() => ({
  getS3Config: vi.fn(),
  getImageStorageConfig: vi.fn(),
  getSchedule: vi.fn(),
  updateSchedule: vi.fn(),
  deleteBackup: vi.fn(),
  listBackups: vi.fn(),
  getDownloadURL: vi.fn(),
}))

vi.mock('@/api', () => ({
  adminAPI: {
    backup: {
      getS3Config,
      updateS3Config: vi.fn(),
      testS3Connection: vi.fn(),
      getImageStorageConfig,
      updateImageStorageConfig: vi.fn(),
      testImageStorageConnection: vi.fn(),
      getSchedule,
      updateSchedule,
      createBackup: vi.fn(),
      listBackups,
      getBackup: vi.fn(),
      deleteBackup,
      getDownloadURL,
      restoreBackup: vi.fn(),
    },
  },
}))

vi.mock('@/stores', () => ({
  useAppStore: () => ({
    showError: vi.fn(),
    showSuccess: vi.fn(),
    showWarning: vi.fn(),
  }),
}))

vi.mock('@/composables/useStepUp', () => ({
  useStepUp: () => ({ run: (fn: () => unknown) => fn() }),
  isStepUpBlocked: () => false,
  isStepUpCancelled: () => false,
  stepUpBlockReason: () => '',
}))

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string, params?: Record<string, unknown>) =>
      params?.index !== undefined ? `${key}:${params.index}` : params?.day !== undefined ? `${key}:${params.day}` : key,
  }),
}))

const baseRecord = (id: string, parts?: unknown[]) => ({
  id,
  status: 'completed',
  backup_type: 'postgres',
  file_name: `${id}.sql.gz`,
  s3_key: `backups/${id}.sql.gz`,
  parts,
  size_bytes: 10,
  triggered_by: 'manual',
  started_at: '2026-08-09T00:00:00Z',
})

const wrappers: ReturnType<typeof mount>[] = []

function mountBackupView() {
  const wrapper = mount(BackupView, {
    global: {
      stubs: {
        TotpStepUpDialog: true,
      },
    },
  })
  wrappers.push(wrapper)
  return wrapper
}

describe('admin BackupView', () => {
  beforeEach(() => {
    getS3Config.mockResolvedValue({})
    getImageStorageConfig.mockResolvedValue({ config: {}, secret_configured: false })
    getSchedule.mockResolvedValue({ enabled: false, cron_expr: '', retain_days: 14, retain_count: 10 })
    updateSchedule.mockReset().mockResolvedValue({})
    deleteBackup.mockReset().mockResolvedValue(undefined)
    listBackups.mockResolvedValue({ items: [] })
    getDownloadURL.mockReset()
    vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {})
  })

  afterEach(() => {
    wrappers.splice(0).forEach(wrapper => wrapper.unmount())
    vi.restoreAllMocks()
    document.body.innerHTML = ''
  })

  it.each(['backup', 'restore'])('does not resume %s polling after navigation during initial loading', async (operation) => {
    vi.useFakeTimers()
    let finish!: (value: object) => void
    listBackups.mockImplementationOnce(() => new Promise(resolve => { finish = resolve }))
    const wrapper = mountBackupView()
    try {
      await flushPromises()
      wrapper.unmount()
      finish({ items: [{ ...baseRecord('pending'),
        status: operation === 'backup' ? 'running' : 'completed',
        restore_status: operation === 'restore' ? 'running' : undefined,
      }] })
      await flushPromises()
      expect(vi.getTimerCount()).toBe(0)
    } finally {
      wrapper.unmount()
      vi.clearAllTimers()
      vi.useRealTimers()
    }
  })

})
