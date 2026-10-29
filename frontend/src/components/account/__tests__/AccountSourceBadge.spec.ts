import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import AccountSourceBadge from '../AccountSourceBadge.vue'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, params?: Record<string, unknown>) =>
        params ? `${key}:${Object.values(params).join(',')}` : key
    })
  }
})

const mountBadge = (account: Record<string, unknown>) =>
  mount(AccountSourceBadge, {
    props: { account: account as never },
    global: { stubs: { Icon: true } }
  })

describe('AccountSourceBadge', () => {
  it('names the provider an imported account came from', () => {
    const wrapper = mountBadge({
      type: 'apikey',
      extra: {
        provider_binding: {
          provider_id: 'p-1',
          provider_name: '小卖部',
          site_type: 'sub2api',
          key_id: '42',
          key_name: '[杂鱼] 主力',
          group_name: 'Claude 特价'
        }
      }
    })

    const badge = wrapper.get('[data-testid="account-source"]')
    expect(badge.attributes('data-source')).toBe('provider')
    expect(badge.text()).toContain('admin.providerImport.sourceProvider')
    expect(badge.text()).toContain('小卖部')
    expect(badge.attributes('title')).toContain('[杂鱼] 主力')
    expect(badge.attributes('title')).toContain('Claude 特价')
  })

  it('marks hand-entered API key accounts', () => {
    const wrapper = mountBadge({ type: 'apikey', extra: {} })

    const badge = wrapper.get('[data-testid="account-source"]')
    expect(badge.attributes('data-source')).toBe('api')
    expect(badge.text()).toContain('API Key')
  })

  it('ignores malformed bindings and non-key accounts', () => {
    expect(mountBadge({ type: 'apikey', extra: { provider_binding: 'oops' } })
      .get('[data-testid="account-source"]').attributes('data-source')).toBe('api')
    expect(mountBadge({ type: 'oauth', extra: {} }).find('[data-testid="account-source"]').exists()).toBe(false)
  })
})
