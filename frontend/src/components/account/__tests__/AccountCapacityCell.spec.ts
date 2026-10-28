import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import AccountCapacityCell from '../AccountCapacityCell.vue'
import type { Account } from '@/types'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return { ...actual, useI18n: () => ({ t: (key: string) => key }) }
})

const account = (concurrency: number, current = 1) => ({
  id: 1, name: 'fixture', platform: 'openai', type: 'apikey', concurrency, current_concurrency: current
}) as unknown as Account

describe('AccountCapacityCell concurrency', () => {
  it('shows current / max for a limited account', () => {
    const wrapper = mount(AccountCapacityCell, { props: { account: account(5000) } })
    expect(wrapper.get('[data-testid="account-concurrency-capacity"]').text()).toBe('1/5000')
  })

  it('shows an infinity sign when concurrency is unlimited', () => {
    const wrapper = mount(AccountCapacityCell, { props: { account: account(0, 3) } })
    const badge = wrapper.get('[data-testid="account-concurrency-capacity"]')
    expect(badge.text()).toBe('3/∞')
    expect(badge.find('[title="admin.accounts.capacity.concurrencyUnlimited"]').exists()).toBe(true)
    // An unlimited account is never shown as full.
    expect(badge.classes().join(' ')).not.toContain('bg-red-100')
  })
})
