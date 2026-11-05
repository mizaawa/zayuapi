import { ref } from 'vue'
import { describe, expect, it } from 'vitest'
import { useAvatarImage } from '../useAvatarImage'

describe('useAvatarImage', () => {
  it('trims the URL and treats a blank one as no avatar', () => {
    const url = ref<string | null>('  https://cdn.example.com/a.png  ')
    const { src } = useAvatarImage(() => url.value)

    expect(src.value).toBe('https://cdn.example.com/a.png')

    url.value = '   '
    expect(src.value).toBe('')

    url.value = null
    expect(src.value).toBe('')
  })

  it('drops a URL that failed to load until a different one is set', () => {
    const url = ref('data:image/webp;base64,broken')
    const { src, onError } = useAvatarImage(() => url.value)

    onError()
    expect(src.value).toBe('')

    url.value = 'data:image/png;base64,iVBORw=='
    expect(src.value).toBe('data:image/png;base64,iVBORw==')

    url.value = 'data:image/webp;base64,broken'
    expect(src.value).toBe('')
  })
})
