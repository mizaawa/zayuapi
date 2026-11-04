<template>
  <div class="page-picker">
    <button
      ref="triggerRef"
      type="button"
      class="page-picker-trigger"
      :class="{ 'is-open': open }"
      :disabled="disabled || pageCount < 1"
      aria-haspopup="listbox"
      :aria-expanded="open"
      :aria-controls="open ? listId : undefined"
      :title="t('pagination.pageOf', { page, total: totalLabel })"
      data-testid="page-picker-trigger"
      @click="toggle"
      @keydown.down.prevent="openList"
      @keydown.up.prevent="openList"
    >
      <span>{{ t('pagination.pageLabel', { page }) }}</span>
      <Icon name="chevronDown" size="sm" :stroke-width="2" class="page-picker-caret" />
    </button>

    <Teleport to="body">
      <Transition name="page-picker">
        <div
          v-if="open"
          ref="panelRef"
          class="page-picker-panel"
          :style="panelStyle"
          data-testid="page-picker-panel"
        >
          <p class="page-picker-head">{{ t('pagination.totalPages', { total: totalLabel }) }}</p>
          <div
            :id="listId"
            ref="listRef"
            class="page-picker-list"
            role="listbox"
            tabindex="0"
            :aria-label="t('pagination.choosePage')"
            :aria-activedescendant="isRendered(activePage) ? optionId(activePage) : undefined"
            :style="{ height: `${listHeight}px` }"
            @scroll="onListScroll"
            @keydown="onListKeydown"
          >
            <div class="page-picker-track" :style="{ height: `${trackHeight}px` }">
              <div class="page-picker-rows" :style="{ transform: `translateY(${rowsOffset}px)` }">
                <div
                  v-for="n in renderedPages"
                  :id="optionId(n)"
                  :key="n"
                  role="option"
                  class="page-picker-option"
                  :class="{ 'is-current': n === page, 'is-active': n === activePage }"
                  :aria-selected="n === page"
                  :aria-posinset="n"
                  :aria-setsize="totalIsExact ? pageCount : -1"
                  :data-page="n"
                  @click="choose(n)"
                  @mousemove="activePage = n"
                >
                  <span class="page-picker-option-body">
                    <span>{{ t('pagination.pageLabel', { page: n }) }}</span>
                    <Icon v-if="n === page" name="check" size="sm" :stroke-width="2.5" />
                  </span>
                </div>
              </div>
            </div>
          </div>
        </div>
      </Transition>
    </Teleport>
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'

interface Props {
  page: number
  totalPages: number
  /** False while totalPages is only a lower bound and more pages may follow. */
  totalIsExact?: boolean
  disabled?: boolean
}

const props = withDefaults(defineProps<Props>(), {
  totalIsExact: true,
  disabled: false
})

const emit = defineEmits<{
  (e: 'select', page: number): void
}>()

const { t } = useI18n()

// Rows share one height, so only the pages in view are rendered even for tens of thousands of pages.
const ROW_HEIGHT = 40
const VISIBLE_ROWS = 7
const OVERSCAN = 4
const HEAD_HEIGHT = 30
const PANEL_PADDING = 6
const TRIGGER_GAP = 6
const VIEWPORT_MARGIN = 8
const MIN_PANEL_WIDTH = 168
// Browsers stop growing elements at roughly 17M px; longer lists scroll a compressed track instead.
const MAX_TRACK_HEIGHT = 6_000_000

const uid = `page-picker-${Math.random().toString(36).slice(2, 9)}`
const listId = `${uid}-list`
const optionId = (n: number) => `${uid}-option-${n}`

const open = ref(false)
const activePage = ref(1)
const scrollTop = ref(0)
const listHeight = ref(ROW_HEIGHT)
const panelStyle = ref<Record<string, string>>({})
const triggerRef = ref<HTMLButtonElement | null>(null)
const panelRef = ref<HTMLElement | null>(null)
const listRef = ref<HTMLElement | null>(null)

const pageCount = computed(() => Math.max(0, Math.floor(props.totalPages)))
const totalLabel = computed(() => (props.totalIsExact ? String(pageCount.value) : `${pageCount.value}+`))
const clampPage = (n: number) => Math.min(Math.max(1, Math.round(n)), Math.max(1, pageCount.value))

const fullHeight = computed(() => pageCount.value * ROW_HEIGHT)
const trackHeight = computed(() => Math.min(fullHeight.value, MAX_TRACK_HEIGHT))
const maxScrollTop = computed(() => Math.max(0, trackHeight.value - listHeight.value))
const maxOffset = computed(() => Math.max(0, fullHeight.value - listHeight.value))
const compressed = computed(() => fullHeight.value > trackHeight.value)
// Offsets are positions in the full list; on a compressed track they map linearly onto scrollTop.
const toOffset = (top: number) =>
  compressed.value && maxScrollTop.value > 0 ? (top * maxOffset.value) / maxScrollTop.value : top
const toScrollTop = (offset: number) =>
  compressed.value && maxOffset.value > 0 ? (offset * maxScrollTop.value) / maxOffset.value : offset

const viewOffset = computed(() => toOffset(scrollTop.value))
const firstIndex = computed(() => Math.max(0, Math.floor(viewOffset.value / ROW_HEIGHT) - OVERSCAN))
const renderedPages = computed(() => {
  const last = Math.min(pageCount.value, Math.ceil((viewOffset.value + listHeight.value) / ROW_HEIGHT) + OVERSCAN)
  const pages: number[] = []
  for (let n = firstIndex.value + 1; n <= last; n++) pages.push(n)
  return pages
})
// Rendered rows sit where the full list would put them relative to the current scroll position.
const rowsOffset = computed(() => scrollTop.value + firstIndex.value * ROW_HEIGHT - viewOffset.value)
const isRendered = (n: number) => n > firstIndex.value && n <= firstIndex.value + renderedPages.value.length

const place = () => {
  const trigger = triggerRef.value
  if (!trigger) return
  const rect = trigger.getBoundingClientRect()
  const viewportWidth = document.documentElement.clientWidth || window.innerWidth
  const viewportHeight = window.innerHeight
  const width = Math.min(Math.max(rect.width, MIN_PANEL_WIDTH), viewportWidth - VIEWPORT_MARGIN * 2)
  const left = Math.min(
    Math.max(rect.left + (rect.width - width) / 2, VIEWPORT_MARGIN),
    viewportWidth - VIEWPORT_MARGIN - width
  )
  const chrome = HEAD_HEIGHT + PANEL_PADDING * 2
  const wanted = Math.min(pageCount.value, VISIBLE_ROWS) * ROW_HEIGHT
  const below = viewportHeight - rect.bottom - TRIGGER_GAP - VIEWPORT_MARGIN - chrome
  const above = rect.top - TRIGGER_GAP - VIEWPORT_MARGIN - chrome
  // Pagers sit under long lists, so near the bottom of the screen the panel opens upward.
  const downward = below >= wanted || below >= above
  listHeight.value = Math.max(Math.min(wanted, downward ? below : above), Math.min(wanted, ROW_HEIGHT * 2))
  const style: Record<string, string> = { left: `${Math.round(left)}px`, width: `${Math.round(width)}px` }
  if (downward) style.top = `${Math.round(rect.bottom + TRIGGER_GAP)}px`
  else style.bottom = `${Math.round(viewportHeight - rect.top + TRIGGER_GAP)}px`
  panelStyle.value = style
}

const scrollToPage = (n: number, center = false) => {
  const top = (n - 1) * ROW_HEIGHT
  let offset = viewOffset.value
  if (center) offset = top - (listHeight.value - ROW_HEIGHT) / 2
  else if (top < offset) offset = top
  else if (top + ROW_HEIGHT > offset + listHeight.value) offset = top + ROW_HEIGHT - listHeight.value
  const next = Math.min(Math.max(toScrollTop(Math.min(Math.max(offset, 0), maxOffset.value)), 0), maxScrollTop.value)
  scrollTop.value = next
  if (listRef.value) listRef.value.scrollTop = next
}

const onListScroll = () => {
  scrollTop.value = listRef.value?.scrollTop ?? 0
}

const onOutsidePointer = (event: Event) => {
  const target = event.target as Node | null
  if (target && (triggerRef.value?.contains(target) || panelRef.value?.contains(target))) return
  closeList()
}

const onViewportChange = (event: Event) => {
  if (event.target === listRef.value) return
  place()
}

const openList = async () => {
  if (open.value || props.disabled || pageCount.value < 1) return
  activePage.value = clampPage(props.page)
  place()
  open.value = true
  document.addEventListener('pointerdown', onOutsidePointer, true)
  window.addEventListener('scroll', onViewportChange, { capture: true, passive: true })
  window.addEventListener('resize', onViewportChange)
  await nextTick()
  scrollToPage(activePage.value, true)
  listRef.value?.focus({ preventScroll: true })
}

const closeList = (restoreFocus = false) => {
  if (!open.value) return
  open.value = false
  document.removeEventListener('pointerdown', onOutsidePointer, true)
  window.removeEventListener('scroll', onViewportChange, true)
  window.removeEventListener('resize', onViewportChange)
  if (restoreFocus) triggerRef.value?.focus({ preventScroll: true })
}

const toggle = () => {
  if (open.value) closeList()
  else void openList()
}

const choose = (n: number) => {
  closeList(true)
  if (n !== props.page) emit('select', n)
}

const moveTo = (n: number, center = false) => {
  activePage.value = clampPage(n)
  scrollToPage(activePage.value, center)
}

let typedDigits = ''
let typedTimer: ReturnType<typeof setTimeout> | undefined

const onListKeydown = (event: KeyboardEvent) => {
  const pageStep = Math.max(1, Math.floor(listHeight.value / ROW_HEIGHT) - 1)
  switch (event.key) {
    case 'ArrowDown':
      moveTo(activePage.value + 1)
      break
    case 'ArrowUp':
      moveTo(activePage.value - 1)
      break
    case 'PageDown':
      moveTo(activePage.value + pageStep)
      break
    case 'PageUp':
      moveTo(activePage.value - pageStep)
      break
    case 'Home':
      moveTo(1)
      break
    case 'End':
      moveTo(pageCount.value)
      break
    case 'Enter':
    case ' ':
      choose(activePage.value)
      break
    case 'Escape':
    case 'Tab':
      closeList(true)
      break
    default: {
      if (!/^[0-9]$/.test(event.key)) return
      // Typing a page number jumps straight to it.
      const appended = `${typedDigits}${event.key}`
      typedDigits = Number(appended) <= pageCount.value ? appended : event.key
      clearTimeout(typedTimer)
      typedTimer = setTimeout(() => {
        typedDigits = ''
      }, 800)
      if (Number(typedDigits) >= 1) moveTo(Number(typedDigits), true)
    }
  }
  event.preventDefault()
}

watch(() => props.page, () => closeList())
watch(() => props.disabled, (disabled) => {
  if (disabled) closeList()
})
watch(pageCount, () => {
  if (!open.value) return
  activePage.value = clampPage(activePage.value)
  place()
})

onBeforeUnmount(() => {
  closeList()
  clearTimeout(typedTimer)
})
</script>

<style scoped>
.page-picker {
  display: inline-flex;
  min-width: 0;
}

.page-picker-trigger {
  display: inline-flex;
  min-height: 2.5rem;
  align-items: center;
  justify-content: center;
  gap: 0.375rem;
  padding: 0 0.75rem 0 0.875rem;
  border-radius: 0.75rem;
  background: var(--md-sys-color-primary);
  color: var(--md-sys-color-on-primary);
  font-size: 0.875rem;
  font-weight: 600;
  font-variant-numeric: tabular-nums;
  white-space: nowrap;
  transition: background-color 160ms ease, box-shadow 160ms ease;
}

.page-picker-trigger:focus-visible {
  outline: none;
  box-shadow: 0 0 0 3px color-mix(in srgb, var(--md-sys-color-primary) 40%, transparent);
}

.page-picker-trigger:disabled {
  cursor: not-allowed;
  opacity: 0.5;
}

.page-picker-caret {
  flex: none;
  transition: transform 160ms ease;
}

.page-picker-trigger.is-open .page-picker-caret {
  transform: rotate(180deg);
}

@media (max-width: 359.98px) {
  .page-picker-trigger {
    gap: 0.25rem;
    padding: 0 0.625rem 0 0.75rem;
  }
}

@media (hover: hover) {
  .page-picker-trigger:hover:not(:disabled) {
    background: color-mix(in srgb, var(--md-sys-color-primary) 86%, var(--md-sys-color-on-primary));
  }
}

.page-picker-panel {
  position: fixed;
  z-index: 100000020;
  display: flex;
  flex-direction: column;
  padding: 6px;
  border-radius: 1rem;
  background: var(--md-sys-color-surface);
  color: var(--md-sys-color-on-surface);
  box-shadow: var(--theme-highlight), 0 0 0 1px rgb(215 193 106 / 0.45), var(--theme-elevation-3);
}

.page-picker-head {
  display: flex;
  height: 30px;
  flex: none;
  align-items: center;
  overflow: hidden;
  padding: 0 0.625rem;
  font-size: 0.75rem;
  font-weight: 600;
  font-variant-numeric: tabular-nums;
  white-space: nowrap;
  text-overflow: ellipsis;
  color: var(--md-sys-color-on-surface-variant);
}

.page-picker-list {
  overflow-x: hidden;
  overflow-y: auto;
  overscroll-behavior: contain;
  border-radius: 0.625rem;
  outline: none;
  scrollbar-width: thin;
}

.page-picker-list:focus-visible {
  box-shadow: inset 0 0 0 2px color-mix(in srgb, var(--md-sys-color-primary) 55%, transparent);
}

.page-picker-track {
  position: relative;
  overflow: hidden;
}

.page-picker-rows {
  position: absolute;
  inset: 0 0 auto;
  will-change: transform;
}

.page-picker-option {
  height: 40px;
  padding: 2px 0;
  cursor: pointer;
  user-select: none;
}

.page-picker-option-body {
  display: flex;
  height: 100%;
  align-items: center;
  justify-content: space-between;
  gap: 0.5rem;
  padding: 0 0.75rem;
  border-radius: 0.625rem;
  font-size: 0.875rem;
  font-variant-numeric: tabular-nums;
  white-space: nowrap;
}

.page-picker-option.is-active .page-picker-option-body {
  background: var(--md-sys-color-surface-container);
}

.page-picker-option.is-current .page-picker-option-body {
  background: var(--md-sys-color-primary);
  color: var(--md-sys-color-on-primary);
  font-weight: 600;
}

.page-picker-enter-active,
.page-picker-leave-active {
  transition: opacity 140ms ease, transform 140ms var(--theme-ease-out);
}

.page-picker-enter-from,
.page-picker-leave-to {
  opacity: 0;
  transform: translateY(4px) scale(0.98);
}

@media (prefers-reduced-motion: reduce) {
  .page-picker-enter-active,
  .page-picker-leave-active {
    transition: none;
  }
}
</style>
