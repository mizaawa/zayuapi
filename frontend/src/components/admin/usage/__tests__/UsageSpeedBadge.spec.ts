import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import UsageSpeedBadge from '../UsageSpeedBadge.vue'

describe('usage speed bill snapshot', () => {
  it.each([
    ['priority', 2, 'Fast×2'], ['ultrafast', 6.25, 'UltraFast×6.25'],
    ['ultrafast', null, ''], ['default', 2, ''], ['flex', null, ''], [null, null, ''],
  ] as const)('%s with stored rate %s', (tier, multiplier, text) => {
    expect(mount(UsageSpeedBadge, { props: { tier, multiplier } }).text()).toBe(text)
  })
})
