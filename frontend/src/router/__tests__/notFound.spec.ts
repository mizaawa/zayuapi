import { createMemoryHistory, createRouter } from 'vue-router'
import { describe, expect, it, vi } from 'vitest'
import { notFoundLocation } from '../notFound'

describe('protected page invisibility', () => {
  it('renders the ordinary unknown-page record at the original URL without loading a protected page', async () => {
    const protectedLoader = vi.fn(async () => ({ template: '<div>Payment</div>' }))
    const unknownPage = { template: '<div>404</div>' }
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [
        { name: 'Payment', path: '/purchase', component: protectedLoader },
        { name: 'NotFound', path: '/:pathMatch(.*)*', component: unknownPage, meta: { requiresAuth: false } },
      ],
    })
    router.beforeEach(to => to.name === 'Payment' ? notFoundLocation(to) : true)
    await router.push('/purchase?resume_token=opaque#result')
    expect(router.currentRoute.value.name).toBe('NotFound')
    expect(router.currentRoute.value.fullPath).toBe('/purchase?resume_token=opaque#result')
    expect(protectedLoader).not.toHaveBeenCalled()
    const deniedRecord = router.currentRoute.value.matched[0]
    await router.push('/an-unknown-path')
    expect(router.currentRoute.value.matched[0]).toBe(deniedRecord)
  })
})
