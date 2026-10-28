<template>
  <div class="provider-picker" :data-testid="testId">
    <button
      ref="triggerRef"
      type="button"
      class="provider-picker-trigger"
      :class="{ 'is-open': open, 'is-placeholder': !selected, 'is-loading': loading }"
      role="combobox"
      aria-haspopup="listbox"
      :aria-label="label"
      :aria-expanded="open"
      :aria-controls="listId"
      :aria-activedescendant="open && activeIndex >= 0 ? optionId(activeIndex) : undefined"
      :aria-busy="loading || undefined"
      :disabled="inactive"
      @click="toggle"
      @keydown="onKeydown"
    >
      <span class="provider-picker-icon" aria-hidden="true">
        <span v-if="loading" class="provider-picker-spinner" />
        <Icon v-else :name="icon" size="md" />
      </span>
      <span class="provider-picker-copy">
        <strong>{{ loading ? loadingText : (selected?.name ?? placeholder) }}</strong>
        <small>{{ selected?.detail || hint }}</small>
      </span>
      <Icon name="chevronDown" size="sm" class="provider-picker-chevron" aria-hidden="true" />
    </button>
    <Teleport to="body">
      <Transition name="provider-picker-menu">
        <div
          v-if="open"
          :id="listId"
          ref="menuRef"
          class="provider-picker-menu"
          :class="{ 'is-cards': cards }"
          :style="menuStyle"
          role="listbox"
          :aria-label="label"
        >
          <div class="provider-picker-heading" role="presentation" @mousedown.prevent>
            <span>{{ label }}</span>
            <span>{{ countText }}</span>
          </div>
          <div
            v-for="(entry, index) in entries"
            :id="optionId(index)"
            :key="entry.action ? 'action' : `option-${entry.id}`"
            :data-index="index"
            :data-testid="entry.action ? 'provider-picker-action' : 'provider-picker-option'"
            role="option"
            :aria-selected="isSelected(entry)"
            :aria-disabled="!entry.available"
            class="provider-picker-option"
            :class="{
              'is-active': index === activeIndex,
              'is-selected': isSelected(entry),
              'is-unavailable': !entry.available,
              'is-action': entry.action
            }"
            @mousedown.prevent
            @mousemove="hover(index)"
            @click="choose(index)"
          >
            <span class="provider-picker-option-icon" aria-hidden="true">
              <Icon :name="entry.action ? 'plus' : icon" size="sm" />
            </span>
            <span class="provider-picker-copy">
              <strong>{{ entry.name }}</strong>
              <small v-if="entry.detail">{{ entry.detail }}</small>
            </span>
            <span
              v-if="!entry.action && entry.badge"
              class="provider-picker-badge"
              :class="`tone-${entry.badgeTone ?? 'honey'}`"
            >{{ entry.badge }}</span>
            <Icon v-if="isSelected(entry)" name="check" size="sm" class="provider-picker-check" aria-hidden="true" />
          </div>
          <p v-if="!options.length && emptyText" class="provider-picker-empty">{{ emptyText }}</p>
        </div>
      </Transition>
    </Teleport>
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref, useId, watch } from 'vue'
import Icon from '@/components/icons/Icon.vue'
import type { ProviderPickerOption } from './providerImport'
import '@/styles/provider-import.css'

interface PickerEntry extends ProviderPickerOption {
  available: boolean
  action?: boolean
}

const props = withDefaults(defineProps<{
  modelValue: string
  options: ProviderPickerOption[]
  icon: 'server' | 'key'
  label: string
  placeholder: string
  hint: string
  countText: string
  loading?: boolean
  loadingText?: string
  disabled?: boolean
  /** An extra first row, such as "create a key", that emits `action`. */
  action?: { name: string; detail: string } | null
  emptyText?: string
  /** Render the rows as outlined cards, as the key list does. */
  cards?: boolean
  testId?: string
}>(), {
  loading: false,
  loadingText: '',
  disabled: false,
  action: null,
  emptyText: '',
  cards: false,
  testId: undefined
})

const emit = defineEmits<{
  'update:modelValue': [value: string]
  action: []
}>()

const listId = `provider-picker-${useId()}`
const triggerRef = ref<HTMLButtonElement | null>(null)
const menuRef = ref<HTMLElement | null>(null)
const open = ref(false)
const activeIndex = ref(-1)
const menuStyle = ref<Record<string, string>>({})

const entries = computed<PickerEntry[]>(() => {
  const options = props.options.map((option) => ({ ...option, available: option.available !== false }))
  if (!props.action) return options
  return [{ id: '__action__', name: props.action.name, detail: props.action.detail, available: true, action: true }, ...options]
})
const selected = computed(() => props.options.find((option) => option.id === props.modelValue))
const inactive = computed(() => props.disabled || props.loading || entries.value.length === 0)

const optionId = (index: number) => `${listId}-option-${index}`
const isSelected = (entry: PickerEntry) => !entry.action && entry.id === props.modelValue

function nextAvailable(from: number, step: 1 | -1): number {
  const count = entries.value.length
  for (let offset = 1; offset <= count; offset++) {
    const index = (((from + step * offset) % count) + count) % count
    if (entries.value[index].available) return index
  }
  return -1
}

function initialIndex(): number {
  const current = entries.value.findIndex((entry) => isSelected(entry) && entry.available)
  return current >= 0 ? current : nextAvailable(-1, 1)
}

// The menu is teleported to <body> so the dialog's scroll area cannot clip
// it; it follows the trigger and opens upward when there is more room above.
function position() {
  const trigger = triggerRef.value
  if (!trigger || !open.value) return
  const rect = trigger.getBoundingClientRect()
  const viewportWidth = window.innerWidth
  const viewportHeight = window.innerHeight
  if (rect.bottom < 0 || rect.top > viewportHeight) {
    close()
    return
  }
  const margin = 12
  const gap = 6
  const spaceBelow = viewportHeight - rect.bottom - margin - gap
  const spaceAbove = rect.top - margin - gap
  const desired = Math.min(360, entries.value.length * 72 + 48)
  const upward = spaceBelow < Math.min(desired, 240) && spaceAbove > spaceBelow
  const maxHeight = Math.max(120, Math.min(desired, upward ? spaceAbove : spaceBelow))
  const width = Math.min(Math.max(rect.width, 260), viewportWidth - margin * 2)
  const left = Math.min(Math.max(margin, rect.left), viewportWidth - width - margin)
  menuStyle.value = {
    position: 'fixed',
    left: `${left}px`,
    width: `${width}px`,
    maxHeight: `${maxHeight}px`,
    zIndex: '100000020',
    ...(upward ? { bottom: `${viewportHeight - rect.top + gap}px` } : { top: `${rect.bottom + gap}px` })
  }
}

function onPointerDown(event: PointerEvent) {
  const target = event.target as Node | null
  if (target && (triggerRef.value?.contains(target) || menuRef.value?.contains(target))) return
  close()
}

function scrollActiveIntoView() {
  if (activeIndex.value < 0) return
  menuRef.value?.querySelector<HTMLElement>(`[data-index="${activeIndex.value}"]`)?.scrollIntoView?.({ block: 'nearest' })
}

function show(index = initialIndex()) {
  if (inactive.value || open.value) return
  open.value = true
  activeIndex.value = index
  position()
  window.addEventListener('resize', position)
  window.addEventListener('scroll', position, true)
  document.addEventListener('pointerdown', onPointerDown, true)
  void nextTick(scrollActiveIntoView)
}

function close() {
  if (!open.value) return
  open.value = false
  activeIndex.value = -1
  window.removeEventListener('resize', position)
  window.removeEventListener('scroll', position, true)
  document.removeEventListener('pointerdown', onPointerDown, true)
}

function toggle() {
  if (open.value) close()
  else show()
}

function hover(index: number) {
  if (entries.value[index]?.available) activeIndex.value = index
}

function choose(index: number) {
  const entry = entries.value[index]
  if (!entry?.available) return
  close()
  triggerRef.value?.focus()
  if (entry.action) emit('action')
  else if (entry.id !== props.modelValue) emit('update:modelValue', entry.id)
}

function onKeydown(event: KeyboardEvent) {
  if (inactive.value) return
  switch (event.key) {
    case 'ArrowDown':
    case 'ArrowUp':
      event.preventDefault()
      if (!open.value) show()
      else activeIndex.value = nextAvailable(activeIndex.value, event.key === 'ArrowDown' ? 1 : -1)
      void nextTick(scrollActiveIntoView)
      break
    case 'Home':
    case 'End':
      if (!open.value) return
      event.preventDefault()
      activeIndex.value = event.key === 'Home' ? nextAvailable(-1, 1) : nextAvailable(0, -1)
      void nextTick(scrollActiveIntoView)
      break
    case 'Enter':
    case ' ':
      event.preventDefault()
      if (open.value) choose(activeIndex.value)
      else show()
      break
    case 'Escape':
      if (!open.value) return
      // Close only this menu, not the dialog that contains the picker.
      event.preventDefault()
      event.stopPropagation()
      close()
      break
    case 'Tab':
      close()
      break
  }
}

watch(inactive, (value) => {
  if (value) close()
})
watch(() => entries.value.length, () => {
  if (open.value) void nextTick(position)
})
onBeforeUnmount(close)
</script>
