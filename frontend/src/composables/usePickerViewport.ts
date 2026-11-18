import { ref, watch, type WatchSource } from 'vue'

/** Mobile keyboards can resize/pan the visual viewport without resizing the page. */
export function usePickerViewport(open: WatchSource<boolean>) {
  const height = ref(0)
  const top = ref(0)
  watch(open, (visible, _, onCleanup) => {
    if (!visible || typeof window === 'undefined') return
    const viewport = window.visualViewport
    const measure = () => {
      height.value = viewport?.height ?? window.innerHeight
      top.value = viewport?.offsetTop ?? 0
    }
    measure()
    window.addEventListener('resize', measure)
    viewport?.addEventListener('resize', measure)
    viewport?.addEventListener('scroll', measure)
    onCleanup(() => {
      window.removeEventListener('resize', measure)
      viewport?.removeEventListener('resize', measure)
      viewport?.removeEventListener('scroll', measure)
    })
  }, { immediate: true })
  return { height, top }
}
