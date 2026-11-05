import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import ProfileAvatarCard from '@/components/user/profile/ProfileAvatarCard.vue'
import type { User } from '@/types'

const {
  updateProfileMock,
  showSuccessMock,
  showErrorMock,
  authStoreState
} = vi.hoisted(() => ({
  updateProfileMock: vi.fn(),
  showSuccessMock: vi.fn(),
  showErrorMock: vi.fn(),
  authStoreState: {
    user: null as User | null
  }
}))

vi.mock('@/api', () => ({
  userAPI: {
    updateProfile: updateProfileMock
  }
}))

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => authStoreState
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showSuccess: showSuccessMock,
    showError: showErrorMock
  })
}))

vi.mock('@/utils/apiError', () => ({
  extractApiErrorMessage: (error: unknown) => (error as Error).message || 'request failed'
}))

vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => {
        const messages: Record<string, string> = {
          'profile.avatar.title': 'Profile avatar',
          'profile.avatar.description': 'Upload and manage your avatar',
          'profile.avatar.uploadAction': 'Upload image',
          'profile.avatar.uploadHint': 'The picked image is saved right away',
          'profile.avatar.saveSuccess': 'Avatar updated',
          'profile.avatar.deleteSuccess': 'Avatar removed',
          'profile.avatar.invalidType': 'Please choose an image file',
          'profile.avatar.gifTooLarge': 'GIF avatars must already be 20KB or smaller',
          'profile.avatar.compressTooLarge': 'Unable to compress this image below 20KB',
          'profile.avatar.compressFailed': 'Failed to compress the selected image',
          'profile.avatar.readFailed': 'Failed to read the selected image',
          'profile.avatar.saving': 'Saving',
          'common.delete': 'Delete'
        }
        return messages[key] ?? key
      }
    })
  }
})

const encode = (text: string) => Buffer.from(text).toString('base64')
const WEBP_DATA_URL = 'data:image/webp;base64,' + encode('compressed-avatar')
const JPEG_DATA_URL = 'data:image/jpeg;base64,' + encode('jpeg-avatar')
const PNG_DATA_URL = 'data:image/png;base64,' + encode('original-avatar')
const SAVED_AVATAR = 'data:image/webp;base64,' + encode('saved-avatar')

function createUser(overrides: Partial<User> = {}): User {
  return {
    id: 5,
    username: 'alice',
    email: 'alice@example.com',
    avatar_url: null,
    role: 'user',
    balance: 10,
    concurrency: 2,
    status: 'active',
    allowed_groups: null,
    balance_notify_enabled: true,
    balance_notify_threshold: null,
    balance_notify_extra_emails: [],
    created_at: '2026-04-20T00:00:00Z',
    updated_at: '2026-04-20T00:00:00Z',
    ...overrides
  }
}

const originalFileReader = globalThis.FileReader
const originalImage = globalThis.Image
const originalCreateElement = document.createElement.bind(document)

interface CanvasCall {
  type: string
  width: number
  height: number
}

/**
 * Stubs FileReader, Image and canvas. `webpSupported: false` mimics Safari,
 * whose canvas hands back a PNG when asked for WebP.
 */
function installAvatarCompressionMocks(options: {
  blobSize?: number
  webpSupported?: boolean
  imageWidth?: number
  imageHeight?: number
} = {}) {
  const { blobSize = 8 * 1024, webpSupported = true, imageWidth = 1200, imageHeight = 1200 } = options
  const canvasCalls: CanvasCall[] = []
  const fillRect = vi.fn()

  class MockFileReader {
    result: string | ArrayBuffer | null = null
    onload: ((this: FileReader, ev: ProgressEvent<FileReader>) => unknown) | null = null
    onerror: ((this: FileReader, ev: ProgressEvent<FileReader>) => unknown) | null = null
    error: DOMException | null = null

    readAsDataURL(blob: Blob) {
      if (blob.type === 'image/webp') {
        this.result = WEBP_DATA_URL
      } else if (blob.type === 'image/jpeg') {
        this.result = JPEG_DATA_URL
      } else if (blob.type === 'image/png') {
        this.result = PNG_DATA_URL
      } else {
        this.result = `data:${blob.type};base64,` + encode('exotic-avatar')
      }
      this.onload?.call(this as unknown as FileReader, new ProgressEvent('load'))
    }
  }

  class MockImage {
    naturalWidth = imageWidth
    naturalHeight = imageHeight
    onload: (() => void) | null = null
    onerror: (() => void) | null = null

    set src(_value: string) {
      this.onload?.()
    }
  }

  globalThis.FileReader = MockFileReader as unknown as typeof FileReader
  globalThis.Image = MockImage as unknown as typeof Image
  vi.spyOn(document, 'createElement').mockImplementation(((tagName: string, elementOptions?: ElementCreationOptions) => {
    if (tagName === 'canvas') {
      const canvas = {
        width: 0,
        height: 0,
        getContext: () => ({
          clearRect: vi.fn(),
          fillRect,
          fillStyle: '',
          drawImage: vi.fn()
        }),
        toBlob: (callback: BlobCallback, type: string) => {
          canvasCalls.push({ type, width: canvas.width, height: canvas.height })
          const actualType = type === 'image/webp' && !webpSupported ? 'image/png' : type
          callback(new Blob([new Uint8Array(blobSize)], { type: actualType }))
        }
      }
      return canvas as unknown as HTMLCanvasElement
    }
    return originalCreateElement(tagName, elementOptions)
  }) as typeof document.createElement)

  return { canvasCalls, fillRect }
}

function mountCard(user: User | null, embedded = true) {
  authStoreState.user = user
  return mount(ProfileAvatarCard, {
    props: { user, embedded },
    global: { stubs: { Icon: true } }
  })
}

async function pickFile(wrapper: ReturnType<typeof mountCard>, file: File) {
  const fileInput = wrapper.get('[data-testid="profile-avatar-file-input"]')
  Object.defineProperty(fileInput.element, 'files', { value: [file], configurable: true })
  await fileInput.trigger('change')
  await flushPromises()
}

describe('ProfileAvatarCard', () => {
  beforeEach(() => {
    updateProfileMock.mockReset()
    showSuccessMock.mockReset()
    showErrorMock.mockReset()
    authStoreState.user = null
  })

  afterEach(() => {
    globalThis.FileReader = originalFileReader
    globalThis.Image = originalImage
    vi.restoreAllMocks()
  })

  it('does not render a manual avatar input field or a separate save step', () => {
    const wrapper = mountCard(createUser(), false)

    expect(wrapper.find('[data-testid="profile-avatar-input"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="profile-avatar-save"]').exists()).toBe(false)
  })

  it('compresses an oversized image and saves it as soon as it is picked', async () => {
    installAvatarCompressionMocks()
    const updatedUser = createUser({ avatar_url: WEBP_DATA_URL })
    updateProfileMock.mockResolvedValue(updatedUser)
    const wrapper = mountCard(createUser())

    await pickFile(wrapper, new File([new Uint8Array(220 * 1024)], 'avatar.png', { type: 'image/png' }))

    expect(updateProfileMock).toHaveBeenCalledTimes(1)
    expect(updateProfileMock).toHaveBeenCalledWith({ avatar_url: WEBP_DATA_URL })
    expect(authStoreState.user).toBe(updatedUser)
    expect(showSuccessMock).toHaveBeenCalledWith('Avatar updated')
    expect(showErrorMock).not.toHaveBeenCalled()
  })

  it('falls back to JPEG on a white backdrop when the browser cannot encode WebP', async () => {
    const { canvasCalls, fillRect } = installAvatarCompressionMocks({ webpSupported: false })
    updateProfileMock.mockResolvedValue(createUser({ avatar_url: JPEG_DATA_URL }))
    const wrapper = mountCard(createUser())

    await pickFile(wrapper, new File([new Uint8Array(220 * 1024)], 'avatar.heic.png', { type: 'image/png' }))

    expect(canvasCalls.map((call) => call.type)).toEqual(['image/webp', 'image/jpeg'])
    expect(fillRect).toHaveBeenCalled()
    expect(updateProfileMock).toHaveBeenCalledWith({ avatar_url: JPEG_DATA_URL })
    expect(showErrorMock).not.toHaveBeenCalled()
  })

  it('caps the compression canvas at 512px on the longest side', async () => {
    const { canvasCalls } = installAvatarCompressionMocks({ imageWidth: 8064, imageHeight: 6048 })
    updateProfileMock.mockResolvedValue(createUser({ avatar_url: WEBP_DATA_URL }))
    const wrapper = mountCard(createUser())

    await pickFile(wrapper, new File([new Uint8Array(900 * 1024)], 'photo.jpg', { type: 'image/jpeg' }))

    expect(canvasCalls[0]).toEqual({ type: 'image/webp', width: 512, height: 384 })
  })

  it('uploads a small PNG unchanged', async () => {
    const { canvasCalls } = installAvatarCompressionMocks()
    updateProfileMock.mockResolvedValue(createUser({ avatar_url: PNG_DATA_URL }))
    const wrapper = mountCard(createUser())

    await pickFile(wrapper, new File([new Uint8Array(4 * 1024)], 'small.png', { type: 'image/png' }))

    expect(canvasCalls).toHaveLength(0)
    expect(updateProfileMock).toHaveBeenCalledWith({ avatar_url: PNG_DATA_URL })
  })

  it('redraws a small image in a format browsers may not display', async () => {
    const { canvasCalls } = installAvatarCompressionMocks()
    updateProfileMock.mockResolvedValue(createUser({ avatar_url: WEBP_DATA_URL }))
    const wrapper = mountCard(createUser())

    await pickFile(wrapper, new File([new Uint8Array(4 * 1024)], 'small.tiff', { type: 'image/tiff' }))

    expect(canvasCalls).not.toHaveLength(0)
    expect(updateProfileMock).toHaveBeenCalledWith({ avatar_url: WEBP_DATA_URL })
  })

  it('shows the picked image while it is saved and locks the controls', async () => {
    installAvatarCompressionMocks()
    let resolveSave: (user: User) => void = () => {}
    updateProfileMock.mockReturnValue(new Promise<User>((resolve) => { resolveSave = resolve }))
    const wrapper = mountCard(createUser({ avatar_url: SAVED_AVATAR }))

    await pickFile(wrapper, new File([new Uint8Array(220 * 1024)], 'avatar.png', { type: 'image/png' }))

    expect(wrapper.get('[data-testid="profile-avatar-preview"]').attributes('src')).toBe(WEBP_DATA_URL)
    expect(wrapper.find('[data-testid="profile-avatar-saving"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid="profile-avatar-file-input"]').attributes('disabled')).toBeDefined()
    expect(wrapper.get('[data-testid="profile-avatar-delete"]').attributes('disabled')).toBeDefined()

    const savedUser = createUser({ avatar_url: WEBP_DATA_URL })
    resolveSave(savedUser)
    await flushPromises()
    await wrapper.setProps({ user: savedUser })

    expect(wrapper.find('[data-testid="profile-avatar-saving"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="profile-avatar-preview"]').attributes('src')).toBe(WEBP_DATA_URL)
    expect(wrapper.get('[data-testid="profile-avatar-file-input"]').attributes('disabled')).toBeUndefined()
  })

  it('keeps the saved avatar when saving fails', async () => {
    installAvatarCompressionMocks()
    updateProfileMock.mockRejectedValue(new Error('upload rejected'))
    const original = createUser({ avatar_url: SAVED_AVATAR })
    const wrapper = mountCard(original)

    await pickFile(wrapper, new File([new Uint8Array(220 * 1024)], 'avatar.png', { type: 'image/png' }))

    expect(showErrorMock).toHaveBeenCalledWith('upload rejected')
    expect(showSuccessMock).not.toHaveBeenCalled()
    expect(authStoreState.user).toBe(original)
    expect(wrapper.get('[data-testid="profile-avatar-preview"]').attributes('src')).toBe(SAVED_AVATAR)
    expect(wrapper.find('[data-testid="profile-avatar-saving"]').exists()).toBe(false)
  })

  it('reports an image that cannot be compressed below 20KB without saving', async () => {
    installAvatarCompressionMocks({ blobSize: 64 * 1024 })
    const wrapper = mountCard(createUser())

    await pickFile(wrapper, new File([new Uint8Array(220 * 1024)], 'huge.png', { type: 'image/png' }))

    expect(updateProfileMock).not.toHaveBeenCalled()
    expect(showErrorMock).toHaveBeenCalledWith('Unable to compress this image below 20KB')
  })

  it('falls back to the initial when the saved avatar cannot be displayed', async () => {
    const wrapper = mountCard(createUser({ avatar_url: 'https://cdn.example.com/blocked.png' }))

    await wrapper.get('[data-testid="profile-avatar-preview"]').trigger('error')

    expect(wrapper.find('[data-testid="profile-avatar-preview"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="profile-avatar-initial"]').text()).toBe('A')
  })

  it('deletes the current avatar', async () => {
    const updatedUser = createUser({ avatar_url: null })
    updateProfileMock.mockResolvedValue(updatedUser)
    const wrapper = mountCard(createUser({ avatar_url: 'https://cdn.example.com/old.png' }), false)

    await wrapper.get('[data-testid="profile-avatar-delete"]').trigger('click')
    await flushPromises()

    expect(updateProfileMock).toHaveBeenCalledWith({ avatar_url: '' })
    expect(authStoreState.user?.avatar_url).toBeNull()
    expect(showSuccessMock).toHaveBeenCalledWith('Avatar removed')
  })
})
