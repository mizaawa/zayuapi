import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'

import PlatformIcon from '@/components/common/PlatformIcon.vue'

describe('PlatformIcon Custom mark', () => {
  it.each(['custom', 'composite'] as const)(
    'renders the filled jigsaw mark for %s',
    (platform) => {
      const wrapper = mount(PlatformIcon, {
        props: { platform, size: 'lg' },
      })

      const svg = wrapper.get('svg')
      expect(svg.attributes('viewBox')).toBe('0 0 24 24')
      expect(svg.attributes('fill')).toBe('currentColor')
      expect(svg.classes()).toEqual(expect.arrayContaining(['w-5', 'h-5']))
      const paths = svg.findAll('path')
      expect(paths).toHaveLength(1)
      // One closed outline with two outward and two inward knobs.
      const d = paths[0].attributes('d') ?? ''
      expect(d.startsWith('M')).toBe(true)
      expect(d.trim().endsWith('z')).toBe(true)
      expect(d.match(/a2\.6 2\.6 0 1 [01]/g)).toHaveLength(4)
    },
  )
})
