import { describe, expect, it } from 'vitest'

import enAdminAccounts from '../locales/en/admin/accounts'
import enAdminAudit from '../locales/en/admin/audit'
import enAdminChannels from '../locales/en/admin/channels'
import enAdminOps from '../locales/en/admin/ops'
import enAdminOverview from '../locales/en/admin/overview'
import enAdminPromptAudit from '../locales/en/admin/promptAudit'
import enAdminResourceLibrary from '../locales/en/admin/resourceLibrary'
import enAdminResources from '../locales/en/admin/resources'
import enAdminSettings from '../locales/en/admin/settings'
import enCommon from '../locales/en/common'
import enIndex from '../locales/en'
import enDashboard from '../locales/en/dashboard'
import enLanding from '../locales/en/landing'
import enMisc from '../locales/en/misc'
import zhAdminAccounts from '../locales/zh/admin/accounts'
import zhAdminAudit from '../locales/zh/admin/audit'
import zhAdminChannels from '../locales/zh/admin/channels'
import zhAdminOps from '../locales/zh/admin/ops'
import zhAdminOverview from '../locales/zh/admin/overview'
import zhAdminPromptAudit from '../locales/zh/admin/promptAudit'
import zhAdminResourceLibrary from '../locales/zh/admin/resourceLibrary'
import zhAdminResources from '../locales/zh/admin/resources'
import zhAdminSettings from '../locales/zh/admin/settings'
import zhCommon from '../locales/zh/common'
import zhIndex from '../locales/zh'
import zhDashboard from '../locales/zh/dashboard'
import zhLanding from '../locales/zh/landing'
import zhMisc from '../locales/zh/misc'

// locales/{zh,en}/index.ts 与 admin/index.ts 使用对象展开聚合各域模块，
// 展开模块之间若出现同名顶层键会静默覆盖。本测试将该风险固化为显式失败。
type Modules = Record<string, Record<string, unknown>>

function collisions(modules: Modules): string[] {
  const seen = new Map<string, string>()
  const out: string[] = []
  for (const [name, mod] of Object.entries(modules)) {
    for (const key of Object.keys(mod)) {
      const prev = seen.get(key)
      if (prev) {
        out.push(`"${key}" in both ${prev} and ${name}`)
      } else {
        seen.set(key, name)
      }
    }
  }
  return out
}

const roots: Record<string, Modules> = {
  zh: { landing: zhLanding, common: zhCommon, dashboard: zhDashboard, misc: zhMisc },
  en: { landing: enLanding, common: enCommon, dashboard: enDashboard, misc: enMisc }
}

const admins: Record<string, Modules> = {
  zh: {
    overview: zhAdminOverview,
    channels: zhAdminChannels,
    accounts: zhAdminAccounts,
    resources: zhAdminResources,
    ops: zhAdminOps,
    settings: zhAdminSettings,
    audit: zhAdminAudit,
    promptAudit: zhAdminPromptAudit,
    resourceLibrary: zhAdminResourceLibrary
  },
  en: {
    overview: enAdminOverview,
    channels: enAdminChannels,
    accounts: enAdminAccounts,
    resources: enAdminResources,
    ops: enAdminOps,
    settings: enAdminSettings,
    audit: enAdminAudit,
    promptAudit: enAdminPromptAudit,
    resourceLibrary: enAdminResourceLibrary
  }
}

describe.each(Object.keys(roots))('locale %s spread assembly', (locale) => {
  it('root modules have no overlapping top-level keys', () => {
    expect(collisions(roots[locale])).toEqual([])
  })

  it('root modules do not shadow the explicit "admin" namespace', () => {
    for (const [name, mod] of Object.entries(roots[locale])) {
      expect(Object.keys(mod), `module ${name} must not define "admin"`).not.toContain('admin')
    }
  })

  it('admin modules have no overlapping top-level keys', () => {
    expect(collisions(admins[locale])).toEqual([])
  })
})

// 资源库模块最初把自身写成 { admin: { resourceLibrary: {...} } }，比其它 admin 模块
// 多套了一层 "admin"。因为 admin/index.ts 用对象展开聚合，这个多出来的顶层键把别的
// 模块的 "admin" 覆盖掉，导致整个后台命名空间只剩 resourceLibrary——所有后台文案在
// 界面上退化成原始 i18n key。
// 断言必须打在"最终被应用消费的聚合对象"上：t('admin.resourceLibrary.title') 走的
// 是 root index 的 admin.* 路径，只测模块局部对象无法发现这类覆盖。
const resourceLibraryKeys = [
  'admin.resourceLibrary.title',
  'admin.resourceLibrary.description',
  'admin.resourceLibrary.tabs.live',
  'admin.resourceLibrary.tabs.saved',
  'admin.resourceLibrary.filters.q',
  'admin.resourceLibrary.fields.createdAt',
  'admin.resourceLibrary.fields.expiresAt',
  'admin.resourceLibrary.fields.model',
  'admin.resourceLibrary.fields.group',
  'admin.resourceLibrary.fields.size',
  'admin.resourceLibrary.fields.cost',
  'admin.resourceLibrary.fields.prompt',
  'admin.resourceLibrary.actions.preview',
  'admin.resourceLibrary.actions.copyPrompt',
  'admin.resourceLibrary.actions.download',
  'admin.resourceLibrary.actions.save',
  'admin.resourceLibrary.kinds.image',
  'admin.resourceLibrary.kinds.video',
  'admin.resourceLibrary.deleteConfirm.title'
]

// 与资源库同屏出现的其它后台文案：必须与资源库共存，不能被覆盖掉。
const siblingAdminKeys = [
  'admin.dashboard.title',
  'admin.settings.title',
  'admin.accounts.title'
]

function resolve(mod: unknown, path: string): unknown {
  return path.split('.').reduce<unknown>((acc, part) => {
    if (acc && typeof acc === 'object') return (acc as Record<string, unknown>)[part]
    return undefined
  }, mod)
}

describe.each([
  ['zh', zhIndex, zhAdminResourceLibrary],
  ['en', enIndex, enAdminResourceLibrary]
])('assembled locale %s', (_locale, rootIndex, resourceLibrary) => {
  const root = rootIndex as Record<string, unknown>

  it('exports a flat section like every other admin module', () => {
    // 多套一层 "admin" 会覆盖同级模块，这里直接锁死形状。
    expect(Object.keys(resourceLibrary)).toContain('resourceLibrary')
    expect(Object.keys(resourceLibrary)).not.toContain('admin')
  })

  it('resolves every resource library key through the root index', () => {
    for (const key of resourceLibraryKeys) {
      const value = resolve(root, key)
      expect(typeof value, `root index must resolve ${key}`).toBe('string')
      expect(value, `${key} must not fall back to the raw key`).not.toBe(key)
      expect(String(value).trim().length, `${key} must not be empty`).toBeGreaterThan(0)
    }
  })

  it('keeps sibling admin sections alongside the resource library', () => {
    for (const key of siblingAdminKeys) {
      const value = resolve(root, key)
      expect(typeof value, `root index must resolve ${key}`).toBe('string')
      expect(value, `${key} must not fall back to the raw key`).not.toBe(key)
    }
  })

  it('resolves the sidebar entry through the root index', () => {
    const value = resolve(root, 'nav.resourceLibrary')
    expect(typeof value, 'root index must resolve nav.resourceLibrary').toBe('string')
    expect(value, 'nav.resourceLibrary must not fall back to the raw key').not.toBe('nav.resourceLibrary')
  })
})

// 资源库页面实际调用的全部 key：任一缺失都会在界面上退化成原始 key。
const viewKeys = [
  'admin.resourceLibrary.actions.copyPrompt',
  'admin.resourceLibrary.actions.download',
  'admin.resourceLibrary.actions.preview',
  'admin.resourceLibrary.actions.save',
  'admin.resourceLibrary.actions.saved',
  'admin.resourceLibrary.alreadySaved',
  'admin.resourceLibrary.archivedBadge',
  'admin.resourceLibrary.billing.pending',
  'admin.resourceLibrary.billing.refunded',
  'admin.resourceLibrary.deleteConfirm.liveMessage',
  'admin.resourceLibrary.deleteConfirm.savedMessage',
  'admin.resourceLibrary.deleteConfirm.title',
  'admin.resourceLibrary.deleteFailed',
  'admin.resourceLibrary.deleted',
  'admin.resourceLibrary.description',
  'admin.resourceLibrary.disabled',
  'admin.resourceLibrary.downloadFailed',
  'admin.resourceLibrary.emptyLive',
  'admin.resourceLibrary.emptySaved',
  'admin.resourceLibrary.fields.billingId',
  'admin.resourceLibrary.fields.contentType',
  'admin.resourceLibrary.fields.cost',
  'admin.resourceLibrary.fields.createdAt',
  'admin.resourceLibrary.fields.expiresAt',
  'admin.resourceLibrary.fields.fileName',
  'admin.resourceLibrary.fields.group',
  'admin.resourceLibrary.fields.model',
  'admin.resourceLibrary.fields.prompt',
  'admin.resourceLibrary.fields.savedAt',
  'admin.resourceLibrary.fields.savedBy',
  'admin.resourceLibrary.fields.sha256',
  'admin.resourceLibrary.fields.size',
  'admin.resourceLibrary.fields.sourceExpiresAt',
  'admin.resourceLibrary.fields.taskId',
  'admin.resourceLibrary.fields.userEmail',
  'admin.resourceLibrary.fields.userId',
  'admin.resourceLibrary.filters.allGroups',
  'admin.resourceLibrary.filters.allKinds',
  'admin.resourceLibrary.filters.allModels',
  'admin.resourceLibrary.filters.group',
  'admin.resourceLibrary.filters.kind',
  'admin.resourceLibrary.filters.model',
  'admin.resourceLibrary.filters.q',
  'admin.resourceLibrary.filters.qPlaceholder',
  'admin.resourceLibrary.kinds.image',
  'admin.resourceLibrary.kinds.video',
  'admin.resourceLibrary.loadFailed',
  'admin.resourceLibrary.previewUnavailable',
  'admin.resourceLibrary.promptCopied',
  'admin.resourceLibrary.saveFailed',
  'admin.resourceLibrary.saveTooLarge',
  'admin.resourceLibrary.saved',
  'admin.resourceLibrary.savedBadge',
  'admin.resourceLibrary.thumbLoading',
  'admin.resourceLibrary.title',
  'admin.resourceLibrary.truncated',
  'common.cancel',
  'common.close',
  'common.delete',
  'common.loading',
  'common.refresh',
  'common.reset',
  'common.search'
]

describe.each([
  ['zh', zhIndex],
  ['en', enIndex]
])('resource library view keys (%s)', (_locale, rootIndex) => {
  const root = rootIndex as Record<string, unknown>

  it('every key the view calls resolves to a translation', () => {
    const missing: string[] = []
    for (const key of viewKeys) {
      const value = resolve(root, key)
      if (typeof value !== 'string' || value === '' || value === key) missing.push(key)
    }
    expect(missing).toEqual([])
  })
})
