<template>
  <div class="relative" ref="containerRef">
    <button
      ref="triggerRef"
      type="button"
      @click="toggle"
      :disabled="disabled"
      :aria-expanded="isOpen"
      aria-haspopup="listbox"
      :aria-controls="isOpen ? instanceId : undefined"
      :aria-invalid="error || undefined"
      :id="id"
      :aria-label="ariaLabel ?? 'Select option'"
      :aria-describedby="ariaDescribedby"
      class="select-trigger"
      @keydown.down.prevent="onTriggerKeyDown"
      @keydown.up.prevent="onTriggerKeyDown"
    >
      <span class="select-value">
        <slot name="selected" :option="selectedOption">
          {{ selectedLabel }}
        </slot>
      </span>
      <span
        v-if="clearable && hasValue && !disabled"
        class="select-clear"
        role="button"
        tabindex="-1"
        aria-label="Clear selection"
        @click.stop="clearSelection"
        @mousedown.stop
        @keydown.enter.stop.prevent="clearSelection"
      >
        <Icon name="x" size="sm" />
      </span>
      <span class="select-icon">
        <Icon
          name="chevronDown"
          size="md"
          :class="['transition-transform duration-200', isOpen && 'rotate-180']"
        />
      </span>
    </button>

    <!-- Teleport dropdown to body to escape stacking context -->
    <Teleport to="body">
      <Transition name="fade">
        <button
          v-if="isOpen && centered"
          type="button"
          class="select-centered-backdrop"
          :aria-label="t('common.close')"
          @click="isOpen = false"
        ></button>
      </Transition>
      <Transition name="select-dropdown">
        <div
          v-if="isOpen"
          ref="dropdownRef"
          class="select-dropdown-portal"
          :class="[instanceId]"
          :style="dropdownStyle"
          :id="instanceId"
          tabindex="-1"
          :aria-label="ariaLabel ?? placeholderText"
          role="listbox"
          @click.stop
          @mousedown.stop
          @keydown="onDropdownKeyDown"
        >
          <!-- Search input -->
          <div v-if="isSearchable" class="select-search">
            <Icon name="search" size="sm" class="text-gray-400" />
            <input
              ref="searchInputRef"
              v-model="searchQuery"
              type="text"
              :placeholder="searchPlaceholderText"
              :aria-label="searchPlaceholderText"
              class="select-search-input"
              @click.stop
            />
          </div>

          <!-- Options list -->
          <div class="select-options" ref="optionsListRef">
            <div
              v-for="(option, index) in filteredOptions"
              :key="`${typeof getOptionValue(option)}:${String(getOptionValue(option) ?? '')}`"
              role="option"
              :aria-selected="isSelected(option)"
              :aria-disabled="isOptionDisabled(option) || isOptionBlocked(option)"
              @click.stop="handleOptionClick(option)"
              @mouseenter="handleOptionMouseEnter(option, index)"
              :class="[
                'select-option',
                isGroupHeaderOption(option) && 'select-option-group',
                isSelected(option) && 'select-option-selected',
                isOptionDisabled(option) && !isGroupHeaderOption(option) && 'select-option-disabled',
                isOptionBlocked(option) && 'select-option-blocked',
                focusedIndex === index && !isGroupHeaderOption(option) && 'select-option-focused'
              ]"
            >
              <slot name="option" :option="option" :selected="isSelected(option)">
                <Icon
                  v-if="option._creatable"
                  name="search"
                  size="sm"
                  class="flex-shrink-0 text-gray-400"
                />
                <span class="select-option-label" :class="option._creatable && 'italic text-gray-500 dark:text-dark-300'">{{ getOptionLabel(option) }}</span>
                <Icon
                  v-if="isSelected(option)"
                  name="check"
                  size="sm"
                  class="shrink-0 text-[var(--select-focus)]"
                  :stroke-width="2"
                />
              </slot>
            </div>

            <!-- Empty state -->
            <div v-if="filteredOptions.length === 0" class="select-empty">
              {{ emptyTextDisplay }}
            </div>
          </div>
        </div>
      </Transition>
    </Teleport>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, watch, onMounted, onUnmounted, nextTick } from 'vue'
import { useElementSize } from '@vueuse/core'
import { usePickerViewport } from '@/composables/usePickerViewport'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'

const { t } = useI18n()

// Instance ID for unique click-outside detection
const instanceId = `select-${Math.random().toString(36).substring(2, 9)}`

export interface SelectOption {
  value: string | number | boolean | null
  label: string
  disabled?: boolean
  blocked?: boolean
  [key: string]: unknown
}

interface Props {
  modelValue: string | number | boolean | null | undefined
  options: SelectOption[] | Array<Record<string, unknown>>
  placeholder?: string
  disabled?: boolean
  error?: boolean
  searchable?: boolean | 'auto'
  searchPlaceholder?: string
  emptyText?: string
  valueKey?: string
  labelKey?: string
  creatable?: boolean
  creatablePrefix?: string
  clearable?: boolean
  id?: string
  ariaLabel?: string
  ariaDescribedby?: string
  centered?: boolean
  /** Preferred minimum menu width in pixels, capped to the padded viewport. */
  dropdownMinWidth?: number
}

interface Emits {
  (e: 'update:modelValue', value: string | number | boolean | null): void
  (e: 'change', value: string | number | boolean | null, option: SelectOption | null): void
  (e: 'blocked', option: SelectOption): void
}

const props = withDefaults(defineProps<Props>(), {
  disabled: false,
  error: false,
  searchable: 'auto',
  creatable: false,
  creatablePrefix: '',
  clearable: false,
  valueKey: 'value',
  labelKey: 'label',
  centered: false
})

const emit = defineEmits<Emits>()

const isOpen = ref(false)
const searchQuery = ref('')
const focusedIndex = ref(-1)
const containerRef = ref<HTMLElement | null>(null)
const triggerRef = ref<HTMLButtonElement | null>(null)
const searchInputRef = ref<HTMLInputElement | null>(null)
const dropdownRef = ref<HTMLElement | null>(null)
const optionsListRef = ref<HTMLElement | null>(null)
const { height: menuHeight } = useElementSize(dropdownRef, undefined, { box: 'border-box' })
const { height: viewportHeight, top: viewportTop } = usePickerViewport(isOpen)
const triggerRect = ref<DOMRect | null>(null)
const dropdownViewportPadding = 8
const dropdownMinimumWidth = 200

// i18n placeholders
const placeholderText = computed(() => props.placeholder ?? t('common.selectOption'))
const searchPlaceholderText = computed(() => props.searchPlaceholder ?? t('common.searchPlaceholder'))
const emptyTextDisplay = computed(() => props.emptyText ?? t('common.noOptionsFound'))

const isSearchable = computed(() => {
  if (props.searchable === 'auto') return props.options.length > 5
  return props.searchable
})

// Computed style for teleported dropdown
const dropdownStyle = computed(() => {
  if (!triggerRect.value) return {}

  const rect = triggerRect.value
  if (props.centered) {
    const width = Math.min(380, Math.max(dropdownViewportPadding * 2, window.innerWidth - dropdownViewportPadding * 2))
    return {
      position: 'fixed',
      top: window.visualViewport ? `${viewportTop.value + viewportHeight.value / 2}px` : '50%',
      maxHeight: `${Math.max(0, viewportHeight.value - dropdownViewportPadding * 2)}px`,
      left: '50%',
      width: `${width}px`,
      minWidth: `${width}px`,
      maxWidth: `${width}px`,
      transform: 'translate(-50%, -50%)',
      zIndex: '100000020'
    }
  }

  const viewportRight = Math.max(dropdownViewportPadding, window.innerWidth - dropdownViewportPadding)
  const left = Math.max(dropdownViewportPadding, rect.left)
  const minWidth = Math.min(
    Math.max(dropdownMinimumWidth, rect.width, props.dropdownMinWidth ?? 0),
    viewportRight - dropdownViewportPadding
  )
  const style: Record<string, string> = {
    position: 'fixed',
    minWidth: `${minWidth}px`,
    zIndex: '100000020'
  }
  if (left + minWidth <= viewportRight) {
    style.left = `${left}px`
    style.maxWidth = `${viewportRight - left}px`
  } else {
    // Open leftward; shift off the trigger edge only if neither alignment fits.
    const right = Math.min(viewportRight, Math.max(rect.right, dropdownViewportPadding + minWidth))
    style.right = `${window.innerWidth - right}px`
    style.maxWidth = `${right - dropdownViewportPadding}px`
  }

  // Measure against the larger side first: a tall list can flip AND scroll on a short screen.
  const start = viewportTop.value + dropdownViewportPadding
  const end = viewportTop.value + viewportHeight.value - dropdownViewportPadding
  const aboveEdge = Math.min(rect.top - 4, end)
  const belowEdge = Math.max(rect.bottom + 4, start)
  const above = Math.max(0, aboveEdge - start)
  const below = Math.max(0, end - belowEdge)
  style.maxHeight = `${Math.max(above, below)}px`
  if (above > below && below < menuHeight.value) {
    style.bottom = `${window.innerHeight - aboveEdge}px`
  } else {
    style.top = `${belowEdge}px`
  }

  return style
})

const getOptionValue = (option: any): any => {
  if (typeof option === 'object' && option !== null) {
    return option[props.valueKey]
  }
  return option
}

const getOptionLabel = (option: any): string => {
  if (typeof option === 'object' && option !== null) {
    return String(option[props.labelKey] ?? '')
  }
  return String(option ?? '')
}

const isOptionDisabled = (option: any): boolean => {
  if (typeof option === 'object' && option !== null) {
    return !!option.disabled
  }
  return false
}

const isOptionBlocked = (option: any): boolean => {
  return typeof option === 'object' && option !== null && option.blocked === true
}

const handleOptionClick = (option: any) => {
  if (isOptionBlocked(option)) {
    emit('blocked', option as SelectOption)
    return
  }
  if (!isOptionDisabled(option)) {
    selectOption(option)
  }
}

const isGroupHeaderOption = (option: any): boolean => {
  if (typeof option === 'object' && option !== null) {
    return option.kind === 'group'
  }
  return false
}

const selectedOption = computed(() => {
  return props.options.find((opt) => getOptionValue(opt) === props.modelValue) || null
})

const selectedLabel = computed(() => {
  if (selectedOption.value) {
    return getOptionLabel(selectedOption.value)
  }
  // In creatable mode, show the raw value if no matching option
  if (props.creatable && props.modelValue) {
    return String(props.modelValue)
  }
  return placeholderText.value
})

const hasValue = computed(
  () => props.modelValue !== null && props.modelValue !== undefined && props.modelValue !== ''
)

const filteredOptions = computed(() => {
  let opts = props.options as any[]
  if (isSearchable.value && searchQuery.value) {
    const query = searchQuery.value.toLowerCase()
    opts = opts.filter((opt) => {
      // Match label
      if (getOptionLabel(opt).toLowerCase().includes(query)) return true
      // Also match description if present
      if (opt.description && String(opt.description).toLowerCase().includes(query)) return true
      return false
    })
    // In creatable mode, always prepend a fuzzy search option
    if (props.creatable && searchQuery.value.trim()) {
      const trimmed = searchQuery.value.trim()
      const prefix = props.creatablePrefix || t('common.search')
      opts = [{ [props.valueKey]: trimmed, [props.labelKey]: `${prefix} "${trimmed}"`, _creatable: true }, ...opts]
    }
  }
  return opts
})

const isSelected = (option: any): boolean => {
  return getOptionValue(option) === props.modelValue
}

const findNextEnabledIndex = (startIndex: number): number => {
  const opts = filteredOptions.value
  if (opts.length === 0) return -1
  for (let offset = 0; offset < opts.length; offset++) {
    const idx = (startIndex + offset) % opts.length
    if (!isOptionDisabled(opts[idx])) return idx
  }
  return -1
}

const findPrevEnabledIndex = (startIndex: number): number => {
  const opts = filteredOptions.value
  if (opts.length === 0) return -1
  for (let offset = 0; offset < opts.length; offset++) {
    const idx = (startIndex - offset + opts.length) % opts.length
    if (!isOptionDisabled(opts[idx])) return idx
  }
  return -1
}

const handleOptionMouseEnter = (option: any, index: number) => {
  if (isOptionDisabled(option) || isGroupHeaderOption(option)) return
  focusedIndex.value = index
}

// Update trigger rect periodically while open to follow scroll/resize
const updateTriggerRect = () => {
  if (containerRef.value) {
    triggerRect.value = containerRef.value.getBoundingClientRect()
  }
}

const toggle = () => {
  if (props.disabled) return
  isOpen.value = !isOpen.value
}

watch(() => props.disabled, (disabled) => {
  if (disabled) isOpen.value = false
})

watch(isOpen, (open, _, onCleanup) => {
  if (open) {
    updateTriggerRect()
    // Reset focused index to current selection or first item
    if (filteredOptions.value.length === 0) {
      focusedIndex.value = -1
    } else {
      const selectedIdx = filteredOptions.value.findIndex(isSelected)
      const initialIdx = selectedIdx >= 0 ? selectedIdx : 0
      focusedIndex.value = isOptionDisabled(filteredOptions.value[initialIdx])
        ? findNextEnabledIndex(initialIdx + 1)
        : initialIdx
    }

    nextTick(() => {
      // Let touch users browse the options before bringing up a software keyboard.
      const touch = window.matchMedia?.('(pointer: coarse)').matches
      const target = isSearchable.value && !touch ? searchInputRef.value : dropdownRef.value
      target?.focus({ preventScroll: true })
      if (focusedIndex.value > 0) scrollToFocused()
    })
    window.addEventListener('scroll', updateTriggerRect, { capture: true, passive: true })
    window.addEventListener('resize', updateTriggerRect)
    onCleanup(() => {
      window.removeEventListener('scroll', updateTriggerRect, true)
      window.removeEventListener('resize', updateTriggerRect)
    })
  } else {
    searchQuery.value = ''
    focusedIndex.value = -1
  }
})

const selectOption = (option: any) => {
  const value = getOptionValue(option) ?? null
  emit('update:modelValue', value)
  emit('change', value, option)
  isOpen.value = false
  triggerRef.value?.focus()
}

const clearSelection = () => {
  if (props.disabled) return
  emit('update:modelValue', null)
  emit('change', null, null)
}

// Keyboards
const onTriggerKeyDown = () => {
  if (!isOpen.value) {
    isOpen.value = true
  }
}

const onDropdownKeyDown = (e: KeyboardEvent) => {
  if (e.isComposing) return
  switch (e.key) {
    case 'ArrowDown':
      e.preventDefault()
      focusedIndex.value = findNextEnabledIndex(focusedIndex.value + 1)
      if (focusedIndex.value >= 0) scrollToFocused()
      break
    case 'ArrowUp':
      e.preventDefault()
      focusedIndex.value = findPrevEnabledIndex(focusedIndex.value - 1)
      if (focusedIndex.value >= 0) scrollToFocused()
      break
    case 'Enter':
      e.preventDefault()
      if (focusedIndex.value >= 0 && focusedIndex.value < filteredOptions.value.length) {
        const opt = filteredOptions.value[focusedIndex.value]
        handleOptionClick(opt)
      }
      break
    case 'Escape':
      e.preventDefault()
      e.stopPropagation()
      isOpen.value = false
      triggerRef.value?.focus()
      break
    case 'Tab':
      isOpen.value = false
      triggerRef.value?.focus({ preventScroll: true })
      break
  }
}

const scrollToFocused = () => {
  nextTick(() => {
    const list = optionsListRef.value
    if (!list) return
    const focusedEl = list.children[focusedIndex.value] as HTMLElement
    if (!focusedEl) return

    if (focusedEl.offsetTop < list.scrollTop) {
      list.scrollTop = focusedEl.offsetTop
    } else if (focusedEl.offsetTop + focusedEl.offsetHeight > list.scrollTop + list.offsetHeight) {
      list.scrollTop = focusedEl.offsetTop + focusedEl.offsetHeight - list.offsetHeight
    }
  })
}

const handleClickOutside = (event: MouseEvent) => {
  const target = event.target as HTMLElement
  // Check if click is inside THIS specific instance's dropdown or trigger
  const isInDropdown = !!target.closest(`.${instanceId}`)
  const isInTrigger = containerRef.value?.contains(target)

  if (!isInDropdown && !isInTrigger && isOpen.value) {
    isOpen.value = false
  }
}

onMounted(() => {
  document.addEventListener('click', handleClickOutside)
})

onUnmounted(() => {
  document.removeEventListener('click', handleClickOutside)
})
</script>

<style scoped>
.select-clear {
  @apply flex flex-shrink-0 cursor-pointer items-center justify-center;
  @apply rounded text-gray-400 transition-colors;
  @apply hover:text-gray-600 dark:hover:text-gray-200;
}
</style>

<style>
.select-dropdown-portal {
  @apply flex w-max min-w-[200px] flex-col overflow-hidden rounded-xl;
  border: 1px solid var(--select-line);
  background: var(--select-surface);
  color: var(--md-sys-color-on-surface);
  box-shadow: var(--theme-elevation-2);
  outline: none;
  pointer-events: auto !important;
}

.select-dropdown-portal .select-search {
  @apply flex shrink-0 items-center gap-2 px-3 py-2;
  border-bottom: 1px solid var(--select-line);
}

.select-dropdown-portal .select-options {
  @apply relative min-h-0 max-h-80 overflow-y-auto overscroll-contain p-1;
}

.select-dropdown-portal .select-option {
  @apply flex cursor-pointer items-center justify-between gap-2 rounded-lg px-3 py-2 text-sm transition-colors;
}

.select-dropdown-portal .select-option-disabled {
  @apply cursor-not-allowed opacity-50;
}

.select-dropdown-portal .select-option-blocked { color: var(--monitor-down); }

.select-dropdown-portal .select-option-group {
  @apply cursor-default select-none text-[11px] font-bold uppercase tracking-wider;
  color: var(--md-sys-color-on-surface-variant);
  background: var(--select-hover);
}

.select-dropdown-portal .select-option-label {
  @apply min-w-0 flex-1 break-words text-left md:truncate;
}

.select-dropdown-portal .select-empty {
  @apply px-4 py-8 text-center text-sm;
  color: var(--md-sys-color-on-surface-variant);
}

.select-dropdown-enter-active,
.select-dropdown-leave-active {
  transition: all 0.2s ease;
}

.select-dropdown-enter-from,
.select-dropdown-leave-to {
  opacity: 0;
  transform: translateY(-8px);
}
</style>
