import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

import { describe, expect, it } from 'vitest'

const componentPath = resolve(dirname(fileURLToPath(import.meta.url)), '../AppSidebar.vue')
const componentSource = readFileSync(componentPath, 'utf8')
const headerPath = resolve(dirname(fileURLToPath(import.meta.url)), '../AppHeader.vue')
const headerSource = readFileSync(headerPath, 'utf8')
const layoutPath = resolve(dirname(fileURLToPath(import.meta.url)), '../AppLayout.vue')
const layoutSource = readFileSync(layoutPath, 'utf8')
const stylePath = resolve(dirname(fileURLToPath(import.meta.url)), '../../../style.css')
const styleSource = readFileSync(stylePath, 'utf8')

describe('AppSidebar custom SVG styles', () => {
  it('does not override uploaded SVG fill or stroke colors', () => {
    expect(componentSource).toContain('.sidebar-svg-icon {')
    expect(componentSource).toContain('color: currentColor;')
    expect(componentSource).toContain('display: block;')
    expect(componentSource).not.toContain('stroke: currentColor;')
    expect(componentSource).not.toContain('fill: none;')
  })
})

describe('AppSidebar scroll position persistence', () => {
  it('binds a template ref to the sidebar nav element', () => {
    expect(componentSource).toContain('ref="sidebarNavRef"')
    expect(componentSource).toContain('sidebar-nav')
  })

  it('declares sidebarNavRef in script setup', () => {
    expect(componentSource).toContain("const sidebarNavRef = ref<HTMLElement | null>(null)")
  })

  it('saves scroll position on beforeUnmount', () => {
    expect(componentSource).toContain('onBeforeUnmount')
    expect(componentSource).toContain('appStore.sidebarScrollTop')
    expect(componentSource).toContain('sidebarNavRef.value.scrollTop')
  })

  it('restores scroll position on mount', () => {
    expect(componentSource).toContain('onMounted')
    expect(componentSource).toContain('appStore.sidebarScrollTop')
    expect(componentSource).toContain('nextTick')
  })
})

describe('AppSidebar header styles', () => {
  it('does not clip the version badge dropdown', () => {
    const sidebarHeaderBlockMatch = styleSource.match(/\.sidebar-header\s*\{[\s\S]*?\n {2}\}/)
    const sidebarBrandBlockMatch = componentSource.match(/\.sidebar-brand\s*\{[\s\S]*?\n\}/)

    expect(sidebarHeaderBlockMatch).not.toBeNull()
    expect(sidebarBrandBlockMatch).not.toBeNull()
    expect(sidebarHeaderBlockMatch?.[0]).not.toContain('@apply overflow-hidden;')
    expect(sidebarBrandBlockMatch?.[0]).not.toContain('overflow: hidden;')
  })
})

describe('AppSidebar grouped navigation', () => {
  it('keeps administrator navigation collapsed behind a persistent admin-only panel', () => {
    expect(componentSource).toContain("const adminPanelStorageKey = 'sub2api-admin-panel-expanded'")
    expect(componentSource).toContain('v-if="isAdmin"')
    expect(componentSource).toContain("t('nav.adminPanel')")
    expect(componentSource).toContain('v-show="adminPanelExpanded && !sidebarCollapsed"')
    expect(componentSource).toContain('localStorage.setItem(adminPanelStorageKey')
  })

  it('wraps personal navigation and layers the mobile backdrop above the header', () => {
    expect(componentSource).toContain('personal-panel-section')
    expect(componentSource).toContain('mobile-sidebar-backdrop fixed inset-0 z-40')
    expect(styleSource).toContain('@apply fixed inset-y-0 left-0 z-50;')
  })
})

describe('AppSidebar feature management navigation', () => {
  it('does not expose feature management in the administrator sidebar', () => {
    expect(componentSource).not.toContain("path: '/admin/features', label: t('nav.featureManagement'), icon: CogIcon")
  })
})

describe('Model Plaza navigation', () => {
  it('keeps the entry out of the header and places it between channel status and subscriptions', () => {
    expect(headerSource).not.toContain('model-plaza')
    expect(headerSource).not.toContain('modelPlazaEnabled')

    const channelStatusIndex = componentSource.indexOf("{ path: '/monitor'")
    const modelPlazaIndex = componentSource.indexOf("{ path: '/model-plaza'")
    const subscriptionsIndex = componentSource.indexOf("{ path: '/subscriptions'")

    expect(channelStatusIndex).toBeGreaterThanOrEqual(0)
    expect(modelPlazaIndex).toBeGreaterThan(channelStatusIndex)
    expect(subscriptionsIndex).toBeGreaterThan(modelPlazaIndex)
  })

  it('opens the standalone page in a new tab behind the model plaza feature flag', () => {
    expect(componentSource).toContain('const flagModelPlaza = makeSidebarFlag(FeatureFlags.modelPlaza)')
    expect(componentSource).toContain("path: '/model-plaza', label: t('nav.modelPlaza'), icon: ModelPlazaIcon, openInNewWindow: true, featureFlag: flagModelPlaza")
    expect(componentSource).toContain(":href=\"item.openInNewWindow ? resolveNavHref(item.path, item.externalUrl) : undefined\"")
    expect(componentSource).toContain(":target=\"item.openInNewWindow ? '_blank' : undefined\"")
    expect(componentSource).toContain(":rel=\"item.openInNewWindow ? 'noopener noreferrer' : undefined\"")
  })

  it('uses the supplied interlocking-hexagon icon geometry', () => {
    expect(componentSource).toContain("viewBox: '0 0 48 48'")
    expect(componentSource).toContain("d: 'M17 40L4 33V17L17 10L30 17V29'")
    expect(componentSource).toContain("d: 'M30 8L43 15V31L30 38L17 31V19'")
    expect(componentSource).toContain("stroke: 'currentColor'")
    expect(componentSource).toContain("'stroke-width': '4'")
  })
})

describe('AppSidebar console appearance', () => {
  it('removes the console theme toggle without changing the saved Home preference', () => {
    expect(componentSource).not.toContain('@click="toggleTheme"')
    expect(componentSource).not.toContain("localStorage.setItem('theme'")
    expect(layoutSource).toContain("document.documentElement.classList.remove('dark')")
    expect(layoutSource).not.toContain("localStorage.removeItem('theme')")
  })

  it('centers every icon inside the collapsed sidebar width', () => {
    const collapsedBlock = componentSource.match(/\.sidebar-link-collapsed\s*\{[\s\S]*?\n\}/)?.[0]

    expect(collapsedBlock).toContain('width: 100%;')
    expect(collapsedBlock).toContain('justify-content: center;')
    expect(collapsedBlock).toContain('padding-left: 0;')
    expect(collapsedBlock).toContain('padding-right: 0;')
  })

  it('floats matching brand and collapse panels above the scrolling navigation surface', () => {
    expect(componentSource).toContain('class="sidebar-footer"')
    expect(componentSource).toContain("'sidebar-footer-collapsed': sidebarCollapsed")
    expect(componentSource).toContain('.sidebar-footer {')
    expect(componentSource).toContain('.sidebar-footer-collapsed {')
    expect(componentSource).toContain('top: 0.75rem;')
    expect(componentSource).toContain('bottom: 0.75rem;')
    expect(componentSource).toContain('backdrop-filter: blur(14px) saturate(135%);')
    expect(styleSource).toContain('background: transparent;')
    expect(styleSource).toContain('position: absolute;')
    expect(styleSource).toContain('padding: 6.3rem 0.75rem 5rem;')
    expect(componentSource).toContain("sidebarCollapsed ? 'w-[84px]' : 'w-64'")
    expect(layoutSource).toContain("sidebarCollapsed ? 'lg:ml-[84px]' : 'lg:ml-64'")
  })
})
