import { computed, ref } from 'vue'

/**
 * Avatar image source that gives up on a URL once it fails to load.
 *
 * Without this a broken avatar (unsupported format, hotlink-protected host,
 * corrupt data) renders as a broken-image icon plus its alt text, which looks
 * like the user's name squeezed into the avatar box. Callers render initials
 * whenever `src` is empty; a new URL is tried again.
 */
export function useAvatarImage(source: () => string | null | undefined) {
  const failedUrl = ref('')
  const url = computed(() => source()?.trim() || '')
  const src = computed(() => (url.value && url.value !== failedUrl.value ? url.value : ''))

  function onError(): void {
    failedUrl.value = url.value
  }

  return { src, onError }
}
