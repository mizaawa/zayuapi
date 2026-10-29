import { describe, expect, it, vi } from 'vitest'

import { warmRouteComponents, warmRoutePath } from '../warmRoute'

describe('warmRouteComponents', () => {
  it('starts lazy page loaders and leaves loaded components alone', () => {
    const lazy = vi.fn(() => Promise.resolve({ default: {} }))
    const loaded = { name: 'Loaded', setup: () => null }
    const functional = Object.assign(() => null, { displayName: 'Functional' })

    warmRouteComponents({
      matched: [
        { components: { default: lazy } },
        { components: { default: loaded, side: functional } },
      ] as never,
    })

    expect(lazy).toHaveBeenCalledTimes(1)
  })

  it('swallows loader failures so a later navigation reports them itself', async () => {
    const rejected = vi.fn(() => Promise.reject(new Error('chunk gone')))
    const throwing = vi.fn(() => { throw new Error('sync failure') })

    expect(() => warmRouteComponents({ matched: [{ components: { a: rejected, b: throwing } }] as never })).not.toThrow()
    await Promise.resolve()
    expect(rejected).toHaveBeenCalled()
    expect(throwing).toHaveBeenCalled()
  })

  it('tolerates missing routes and records without components', () => {
    expect(() => warmRouteComponents(null)).not.toThrow()
    expect(() => warmRouteComponents({ matched: [{ components: null }] as never })).not.toThrow()
  })
})

describe('warmRoutePath', () => {
  it('only resolves in-app paths, once each', () => {
    const resolve = vi.fn(() => ({ matched: [] }))
    warmRoutePath({ resolve } as never, 'https://example.com/x')
    warmRoutePath({ resolve } as never, '/keys')
    warmRoutePath({ resolve } as never, '/keys')
    expect(resolve).toHaveBeenCalledTimes(1)
    expect(resolve).toHaveBeenCalledWith('/keys')
  })

  it('ignores paths the router cannot resolve', () => {
    const resolve = vi.fn(() => { throw new Error('no match') })
    expect(() => warmRoutePath({ resolve } as never, '/custom/unknown')).not.toThrow()
  })
})
