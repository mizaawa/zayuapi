<template>
  <Teleport to="body">
    <Transition name="kgp" :duration="{ enter: 280, leave: 200 }">
      <div v-if="show" class="kgp-layer" data-test="key-group-picker">
        <div class="kgp-backdrop" aria-hidden="true" @click="close"></div>
        <section
          ref="panelRef"
          class="kgp-panel"
          :class="{ 'is-roomy': options.length > ROOMY_OPTION_COUNT }"
          role="dialog"
          aria-modal="true"
          :aria-labelledby="titleId"
          tabindex="-1"
        >
          <header class="kgp-header">
            <div class="kgp-heading">
              <h2 :id="titleId" class="kgp-title">{{ title || t('keys.selectGroup') }}</h2>
              <p v-if="subtitle" class="kgp-subtitle">{{ subtitle }}</p>
            </div>
            <button
              type="button"
              class="kgp-close"
              :aria-label="t('common.close')"
              data-test="key-group-picker-close"
              @click="close"
            >
              <Icon name="x" size="md" />
            </button>
          </header>

          <div class="kgp-tools">
            <div class="kgp-search">
              <Icon name="search" size="sm" class="kgp-search-icon" />
              <input
                ref="searchRef"
                v-model="query"
                type="text"
                role="combobox"
                class="kgp-search-input"
                :placeholder="t('keys.searchGroup')"
                :aria-label="t('keys.searchGroup')"
                aria-autocomplete="list"
                aria-expanded="true"
                :aria-controls="listId"
                :aria-activedescendant="activeOptionDomId"
                autocomplete="off"
                autocapitalize="off"
                spellcheck="false"
                enterkeyhint="done"
                data-test="key-group-picker-search"
                @input="onQueryInput"
                @keydown="onSearchKeydown"
              />
              <button
                v-if="query"
                type="button"
                class="kgp-search-clear"
                :aria-label="t('keys.groupPicker.clearSearch')"
                data-test="key-group-picker-clear-search"
                @click="clearSearch"
              >
                <Icon name="x" size="sm" />
              </button>
            </div>

            <div class="kgp-platforms" role="group" :aria-label="t('keys.groupPicker.platformFilter')">
              <button
                v-for="chip in chips"
                :key="chip.value"
                type="button"
                class="kgp-chip"
                :class="{ 'is-active': platform === chip.value }"
                :style="chip.platform ? { '--chip-accent': platformAccentColor(chip.platform) } : undefined"
                :disabled="chip.platform !== null && chip.count === 0 && platform !== chip.value"
                :aria-pressed="platform === chip.value"
                :data-test="`key-group-platform-${chip.value}`"
                @click="selectPlatform(chip.value)"
              >
                <PlatformIcon v-if="chip.platform" :platform="chip.platform" size="xs" class="kgp-chip-icon" />
                <span>{{ chip.label }}</span>
                <span class="kgp-chip-count" data-test="key-group-platform-count">{{ chip.count }}</span>
              </button>
            </div>
          </div>

          <div
            ref="listRef"
            class="kgp-list"
            :class="{ 'is-keyboard': keyboardMode }"
            @mousemove="keyboardMode = false"
          >
            <div :id="listId" role="listbox" :aria-labelledby="titleId">
              <button
                v-for="(row, index) in rows"
                :id="optionDomId(row.option)"
                :key="row.option.value"
                type="button"
                role="option"
                tabindex="-1"
                class="kgp-option"
                :class="{
                  'is-selected': row.option.value === selectedId,
                  'is-active': index === activeIndex,
                  'is-blocked': row.option.blocked,
                  'is-shaking': shakingId === row.option.value
                }"
                :style="{ '--kgp-accent': row.accent }"
                :aria-selected="row.option.value === selectedId"
                :aria-disabled="row.option.blocked ? 'true' : undefined"
                :title="row.option.description || undefined"
                :data-test="`key-group-option-${row.option.value}`"
                @click="choose(row.option)"
                @mousemove="activeIndex = index"
              >
                <span class="kgp-option-icon" aria-hidden="true">
                  <PlatformIcon :platform="row.option.platform" size="md" />
                </span>
                <span class="kgp-option-title">
                  <span class="kgp-option-name">{{ row.option.label }}</span>
                  <span v-if="row.option.subscriptionType === 'subscription'" class="kgp-tag">
                    {{ t('groups.subscription') }}
                  </span>
                  <span v-if="row.option.blocked" class="kgp-tag kgp-tag-blocked">{{ t('common.groupBlocked') }}</span>
                </span>
                <span class="kgp-option-side">
                  <span class="kgp-rate">
                    <template v-if="row.customRate">
                      <s class="kgp-rate-old">{{ row.option.rate }}x</s>
                      <span>{{ row.option.userRate }}x</span>
                    </template>
                    <span v-else>{{ row.option.rate }}x</span>
                    <span class="kgp-rate-label">{{ t('admin.groups.rateLabel') }}</span>
                  </span>
                  <span v-if="row.peak" class="kgp-peak" :title="t('common.peakRateTooltip', { window: row.peak })">
                    {{ row.peak }}
                  </span>
                </span>
                <span v-if="row.option.blocked" class="kgp-option-desc kgp-option-blocked-hint">
                  {{ t('keys.groupBlockedToast') }}
                </span>
                <span v-else-if="row.option.description" class="kgp-option-desc">{{ row.option.description }}</span>
                <span class="kgp-check" aria-hidden="true">
                  <Icon name="check" size="xs" :stroke-width="3" />
                </span>
              </button>
            </div>

            <div v-if="rows.length === 0" class="kgp-empty" data-test="key-group-picker-empty">
              <Icon name="search" size="lg" class="kgp-empty-icon" />
              <p>{{ options.length === 0 ? t('keys.groupPicker.empty') : t('keys.groupPicker.noMatch') }}</p>
              <button
                v-if="options.length > 0"
                type="button"
                class="kgp-empty-reset"
                data-test="key-group-picker-reset"
                @click="resetFilters"
              >
                {{ t('keys.groupPicker.clearFilters') }}
              </button>
            </div>
          </div>
        </section>
      </div>
    </Transition>
  </Teleport>
</template>

<script lang="ts">
let pickerCounter = 0
</script>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import PlatformIcon from '@/components/common/PlatformIcon.vue'
import { useAppStore } from '@/stores/app'
import { isMobileDevice } from '@/utils/device'
import { setDocumentScrollLock } from '@/utils/documentScrollLock'
import { formatPeakRateWindow, serverTimezoneLabel } from '@/utils/peak-rate'
import { platformAccentColor, platformLabel } from '@/utils/platformColors'
import type { GroupPlatform } from '@/types'
import {
  countKeyGroupsByPlatform,
  keyGroupPlatform,
  keyGroupPlatformChips,
  matchesKeyGroupPlatform,
  matchesKeyGroupQuery,
  type KeyGroupOption,
  type KeyGroupPlatformFilter
} from './keyGroupFilters'

const props = withDefaults(defineProps<{
  show: boolean
  options: KeyGroupOption[]
  selectedId?: number | null
  title?: string
  subtitle?: string
}>(), {
  selectedId: null,
  title: '',
  subtitle: ''
})

const emit = defineEmits<{
  select: [option: KeyGroupOption]
  close: []
}>()

const { t } = useI18n()
const appStore = useAppStore()

// Past this many groups the panel keeps its full height, so switching
// platforms or typing does not make it jump around.
const ROOMY_OPTION_COUNT = 4
const SHAKE_MS = 400

const uid = `kgp-${++pickerCounter}`
const titleId = `${uid}-title`
const listId = `${uid}-list`
const optionDomId = (option: KeyGroupOption) => `${uid}-option-${option.value}`

const panelRef = ref<HTMLElement | null>(null)
const searchRef = ref<HTMLInputElement | null>(null)
const listRef = ref<HTMLElement | null>(null)

const query = ref('')
const platform = ref<KeyGroupPlatformFilter>('all')
const activeIndex = ref(-1)
// The active row is only highlighted while the keyboard drives it; the
// pointer already shows where it is.
const keyboardMode = ref(false)
const touchMode = ref(false)
const shakingId = ref<number | null>(null)
let shakeTimer: ReturnType<typeof setTimeout> | null = null
let restoreFocusTo: HTMLElement | null = null
let ownsScrollLock = false

const searchMatches = computed(() => props.options.filter((option) => matchesKeyGroupQuery(option, query.value)))
const visibleOptions = computed(() =>
  searchMatches.value.filter((option) => matchesKeyGroupPlatform(option, platform.value))
)

const chips = computed(() => {
  const counts = countKeyGroupsByPlatform(searchMatches.value)
  return [
    { value: 'all' as KeyGroupPlatformFilter, platform: null, label: t('keys.groupPicker.all'), count: searchMatches.value.length },
    ...keyGroupPlatformChips(props.options).map((value: GroupPlatform) => ({
      value: value as KeyGroupPlatformFilter,
      platform: value,
      label: platformLabel(value),
      count: counts.get(value) ?? 0
    }))
  ]
})

const timezoneLabel = computed(() => serverTimezoneLabel(appStore.cachedPublicSettings?.server_utc_offset))

const rows = computed(() => visibleOptions.value.map((option) => ({
  option,
  accent: platformAccentColor(keyGroupPlatform(option.platform)),
  customRate: option.userRate !== null && option.userRate !== undefined && option.userRate !== option.rate,
  peak: formatPeakRateWindow(
    {
      peak_rate_enabled: option.peakRateEnabled,
      peak_start: option.peakStart,
      peak_end: option.peakEnd,
      peak_rate_multiplier: option.peakRateMultiplier
    },
    timezoneLabel.value
  )
})))

const activeOptionDomId = computed(() => {
  const option = visibleOptions.value[activeIndex.value]
  return option ? optionDomId(option) : undefined
})

const close = () => emit('close')

const scrollListTo = (element: HTMLElement, center: boolean) => {
  const list = listRef.value
  if (!list) return
  const top = element.offsetTop
  const bottom = top + element.offsetHeight
  if (center) {
    list.scrollTop = Math.max(0, top - (list.clientHeight - element.offsetHeight) / 2)
  } else if (top < list.scrollTop) {
    list.scrollTop = top
  } else if (bottom > list.scrollTop + list.clientHeight) {
    list.scrollTop = bottom - list.clientHeight
  }
}

const revealActive = (center = false) => {
  const id = activeOptionDomId.value
  const element = id ? document.getElementById(id) : null
  if (element) scrollListTo(element, center)
}

// A new search or platform starts again from the top of the shorter list.
const restartList = () => {
  activeIndex.value = visibleOptions.value.length > 0 ? 0 : -1
  if (listRef.value) listRef.value.scrollTop = 0
}

const onQueryInput = () => {
  if (!touchMode.value) keyboardMode.value = true
  restartList()
}

const clearSearch = () => {
  query.value = ''
  restartList()
  if (!touchMode.value) searchRef.value?.focus()
}

const selectPlatform = (value: KeyGroupPlatformFilter) => {
  if (platform.value === value) return
  platform.value = value
  restartList()
}

const resetFilters = () => {
  query.value = ''
  platform.value = 'all'
  restartList()
}

const shake = (id: number) => {
  if (shakeTimer) clearTimeout(shakeTimer)
  // Drop the class for a tick so a second tap replays the animation.
  shakingId.value = null
  void nextTick(() => {
    shakingId.value = id
    shakeTimer = setTimeout(() => {
      shakingId.value = null
      shakeTimer = null
    }, SHAKE_MS)
  })
}

const choose = (option: KeyGroupOption) => {
  // A blocked group explains itself in its row; a page toast would sit under this layer.
  if (option.blocked) {
    shake(option.value)
    return
  }
  emit('select', option)
}

const moveActive = (step: 1 | -1) => {
  const count = visibleOptions.value.length
  if (count === 0) return
  keyboardMode.value = true
  const from = activeIndex.value < 0 ? (step > 0 ? -1 : count) : activeIndex.value
  activeIndex.value = (from + step + count) % count
  void nextTick(() => revealActive())
}

const onSearchKeydown = (event: KeyboardEvent) => {
  // Let an input method finish composing first (Enter confirms the candidate there).
  if (event.isComposing || event.keyCode === 229) return
  if (event.key === 'ArrowDown') {
    event.preventDefault()
    moveActive(1)
  } else if (event.key === 'ArrowUp') {
    event.preventDefault()
    moveActive(-1)
  } else if (event.key === 'Enter') {
    event.preventDefault()
    // On phones the return key only puts the keyboard away; the user taps a group.
    if (touchMode.value) {
      searchRef.value?.blur()
      return
    }
    const option = visibleOptions.value[activeIndex.value]
    if (option) choose(option)
  }
}

// Capture phase, so Escape closes only this picker and not the key dialog under it.
const onWindowKeydown = (event: KeyboardEvent) => {
  if (event.key !== 'Escape' || event.isComposing) return
  event.preventDefault()
  event.stopPropagation()
  if (query.value) {
    clearSearch()
    return
  }
  close()
}

// The key dialog already locks the page; only the table's picker needs its own lock.
const lockScroll = () => {
  if (ownsScrollLock || document.body.classList.contains('modal-open')) return
  setDocumentScrollLock(true)
  ownsScrollLock = true
}

const unlockScroll = () => {
  if (!ownsScrollLock) return
  ownsScrollLock = false
  setDocumentScrollLock(false)
}

const open = () => {
  restoreFocusTo = document.activeElement instanceof HTMLElement ? document.activeElement : null
  touchMode.value = isMobileDevice()
  keyboardMode.value = false
  query.value = ''
  platform.value = 'all'
  activeIndex.value = visibleOptions.value.findIndex((option) => option.value === props.selectedId)
  if (shakeTimer) clearTimeout(shakeTimer)
  shakeTimer = null
  shakingId.value = null
  lockScroll()
  window.addEventListener('keydown', onWindowKeydown, true)
  void nextTick(() => {
    revealActive(true)
    // A phone keyboard would cover half of the list, so only the sheet takes focus there.
    if (touchMode.value) panelRef.value?.focus({ preventScroll: true })
    else searchRef.value?.focus({ preventScroll: true })
  })
}

const teardown = (restoreFocus: boolean) => {
  window.removeEventListener('keydown', onWindowKeydown, true)
  unlockScroll()
  if (shakeTimer) {
    clearTimeout(shakeTimer)
    shakeTimer = null
  }
  const target = restoreFocusTo
  restoreFocusTo = null
  if (!restoreFocus || !target?.isConnected) return
  // Refocusing a text field on a phone would pop its keyboard back up.
  if (touchMode.value && target.matches('input, textarea, select, [contenteditable="true"]')) return
  target.focus({ preventScroll: true })
}

watch(() => props.show, (show, wasShown) => {
  if (show) open()
  else if (wasShown) teardown(true)
}, { immediate: true })

onBeforeUnmount(() => teardown(false))
</script>

<style scoped>
.kgp-layer {
  position: fixed;
  inset: 0;
  z-index: 100000010;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 2rem 1.5rem;
}

.kgp-backdrop {
  position: absolute;
  inset: 0;
  background: rgb(53 44 28 / 0.58);
  -webkit-backdrop-filter: blur(7px) saturate(1.08);
  backdrop-filter: blur(7px) saturate(1.08);
  touch-action: none;
}

.kgp-panel {
  position: relative;
  display: flex;
  width: min(48rem, 100%);
  max-height: calc(100vh - 4rem);
  max-height: calc(100dvh - 4rem);
  flex-direction: column;
  overflow: hidden;
  border-radius: 1.75rem;
  outline: none;
  color: var(--md-sys-color-on-surface);
  background: var(--md-sys-color-surface);
  box-shadow:
    inset 0 1px 0 rgb(255 255 255 / 0.8),
    0 0 0 1px rgb(215 193 106 / 0.2),
    0 1.75rem 3.5rem -1rem rgb(63 52 32 / 0.34),
    0 0.35rem 1rem rgb(63 52 32 / 0.08);
}

.kgp-panel.is-roomy {
  height: min(46rem, calc(100vh - 4rem));
  height: min(46rem, calc(100dvh - 4rem));
}

/* ---------- Header ---------- */
.kgp-header {
  display: flex;
  flex-shrink: 0;
  align-items: flex-start;
  justify-content: space-between;
  gap: 1rem;
  padding: 1.25rem 1.5rem 0.875rem;
}

.kgp-heading {
  min-width: 0;
}

.kgp-title {
  font-size: 1.125rem;
  font-weight: 650;
  line-height: 1.5rem;
}

.kgp-subtitle {
  margin-top: 0.125rem;
  overflow-wrap: anywhere;
  font-size: 0.8125rem;
  color: var(--md-sys-color-on-surface-variant);
}

.kgp-close {
  display: inline-flex;
  width: 2.5rem;
  height: 2.5rem;
  flex-shrink: 0;
  align-items: center;
  justify-content: center;
  margin: -0.375rem -0.625rem 0 0;
  border-radius: 0.875rem;
  color: var(--md-sys-color-on-surface-variant);
  transition: background-color 150ms ease, color 150ms ease;
}

.kgp-close:hover {
  color: var(--md-sys-color-on-surface);
  background: var(--md-sys-color-surface-container);
}

/* ---------- Search + platform chips ---------- */
.kgp-tools {
  display: flex;
  flex-shrink: 0;
  flex-direction: column;
  gap: 0.75rem;
  padding: 0 1.5rem 1rem;
}

.kgp-search {
  position: relative;
  display: flex;
  align-items: center;
}

.kgp-search-icon {
  position: absolute;
  left: 1rem;
  color: var(--md-sys-color-placeholder);
  pointer-events: none;
}

.kgp-search-input {
  width: 100%;
  height: 2.875rem;
  padding: 0 2.75rem;
  border: 0;
  border-radius: 1rem;
  font-size: 0.9375rem;
  color: var(--md-sys-color-on-surface);
  background: var(--md-sys-color-surface-container);
  box-shadow: inset 0 1px 2px rgb(63 52 32 / 0.06);
  transition: background-color 160ms ease, box-shadow 160ms ease;
}

.kgp-search-input::placeholder {
  color: var(--md-sys-color-placeholder);
}

.kgp-search-input:focus {
  outline: none;
  background: var(--md-sys-color-surface);
  box-shadow: inset 0 0 0 1.5px rgb(233 184 36 / 0.75), 0 0 0 4px rgb(233 184 36 / 0.14);
}

.kgp-search-clear {
  position: absolute;
  right: 0.4375rem;
  display: inline-flex;
  width: 2rem;
  height: 2rem;
  align-items: center;
  justify-content: center;
  border-radius: 0.75rem;
  color: var(--md-sys-color-on-surface-variant);
  transition: background-color 150ms ease, color 150ms ease;
}

.kgp-search-clear:hover {
  color: var(--md-sys-color-on-surface);
  background: var(--md-sys-color-surface-container-high);
}

.kgp-platforms {
  display: flex;
  flex-wrap: wrap;
  gap: 0.5rem;
}

/* Each chip borrows its platform colour, like the admin group list. */
.kgp-chip {
  --chip-accent: var(--md-sys-color-primary);
  display: inline-flex;
  height: 2.25rem;
  flex-shrink: 0;
  align-items: center;
  gap: 0.4375rem;
  padding: 0 0.4375rem 0 0.875rem;
  border-radius: 999px;
  white-space: nowrap;
  font-size: 0.8125rem;
  font-weight: 550;
  color: var(--md-sys-color-on-surface-variant);
  background: #faf6e8;
  box-shadow: inset 0 0 0 1px rgb(215 193 106 / 0.32);
  transition: background-color 160ms ease, color 160ms ease, box-shadow 160ms ease, opacity 160ms ease;
}

.kgp-chip-icon {
  color: var(--chip-accent);
}

.kgp-chip-count {
  display: inline-flex;
  min-width: 1.375rem;
  height: 1.375rem;
  align-items: center;
  justify-content: center;
  padding: 0 0.375rem;
  border-radius: 999px;
  font-size: 0.6875rem;
  font-weight: 700;
  font-variant-numeric: tabular-nums;
  background: rgb(255 255 255 / 0.85);
}

.kgp-chip:hover:not(:disabled) {
  color: var(--md-sys-color-on-surface);
  background: color-mix(in srgb, var(--chip-accent) 14%, var(--md-sys-color-surface));
}

.kgp-chip.is-active {
  font-weight: 650;
  color: color-mix(in srgb, var(--chip-accent) 68%, #3f3420);
  background: color-mix(in srgb, var(--chip-accent) 18%, var(--md-sys-color-surface));
  box-shadow: inset 0 0 0 1.5px color-mix(in srgb, var(--chip-accent) 60%, transparent);
}

.kgp-chip:disabled {
  cursor: not-allowed;
  opacity: 0.45;
}

.kgp-close:focus-visible,
.kgp-search-clear:focus-visible,
.kgp-chip:focus-visible,
.kgp-empty-reset:focus-visible {
  outline: none;
  box-shadow: 0 0 0 3px rgb(233 184 36 / 0.5);
}

/* ---------- Group list ---------- */
.kgp-list {
  position: relative;
  min-height: 0;
  flex: 1 1 auto;
  overflow-y: auto;
  overscroll-behavior: contain;
  padding: 0.375rem 0.875rem 1rem;
  border-top: 1px solid rgb(215 193 106 / 0.3);
  scrollbar-color: rgb(215 193 106 / 0.75) transparent;
  scrollbar-width: thin;
}

.kgp-option {
  --kgp-accent: var(--md-sys-color-primary);
  position: relative;
  display: grid;
  width: 100%;
  grid-template-columns: auto minmax(0, 1fr) auto auto;
  grid-template-areas:
    'icon title side check'
    'icon desc side check';
  align-items: start;
  column-gap: 0.875rem;
  margin-top: 0.375rem;
  padding: 0.875rem 1rem;
  border-radius: 1.125rem;
  text-align: left;
  cursor: pointer;
  -webkit-tap-highlight-color: transparent;
  transition: background-color 140ms ease, box-shadow 140ms ease;
}

.kgp-option-icon {
  display: inline-flex;
  width: 2.5rem;
  height: 2.5rem;
  grid-area: icon;
  align-items: center;
  justify-content: center;
  border-radius: 0.875rem;
  color: var(--kgp-accent);
  background: color-mix(in srgb, var(--kgp-accent) 13%, #fff);
}

.kgp-option-title {
  display: flex;
  min-width: 0;
  grid-area: title;
  flex-wrap: wrap;
  align-items: center;
  gap: 0.25rem 0.5rem;
  padding-top: 0.125rem;
}

.kgp-option-name {
  min-width: 0;
  overflow-wrap: anywhere;
  font-size: 0.9375rem;
  font-weight: 650;
  line-height: 1.375rem;
  color: var(--md-sys-color-on-surface);
}

.kgp-tag {
  display: inline-flex;
  align-items: center;
  padding: 0.0625rem 0.5rem;
  border-radius: 999px;
  font-size: 0.6875rem;
  font-weight: 650;
  line-height: 1.125rem;
  color: color-mix(in srgb, var(--kgp-accent) 70%, #3f3420);
  background: color-mix(in srgb, var(--kgp-accent) 14%, #fff);
}

.kgp-tag-blocked {
  color: #b91c1c;
  background: #fee2e2;
}

.kgp-option-desc {
  display: -webkit-box;
  grid-area: desc;
  margin-top: 0.25rem;
  overflow: hidden;
  -webkit-box-orient: vertical;
  -webkit-line-clamp: 3;
  white-space: pre-line;
  overflow-wrap: anywhere;
  font-size: 0.8125rem;
  line-height: 1.25rem;
  color: var(--md-sys-color-on-surface-variant);
}

.kgp-option-blocked-hint {
  color: #b91c1c;
}

.kgp-option-side {
  display: flex;
  grid-area: side;
  flex-direction: column;
  align-items: flex-end;
  gap: 0.3125rem;
}

.kgp-rate {
  display: inline-flex;
  align-items: baseline;
  gap: 0.25rem;
  padding: 0.25rem 0.6875rem;
  border-radius: 999px;
  white-space: nowrap;
  font-size: 0.8125rem;
  font-weight: 700;
  font-variant-numeric: tabular-nums;
  color: color-mix(in srgb, var(--kgp-accent) 72%, #3f3420);
  background: color-mix(in srgb, var(--kgp-accent) 12%, #fff);
}

.kgp-rate-old {
  font-weight: 500;
  opacity: 0.55;
}

.kgp-rate-label {
  font-size: 0.6875rem;
  font-weight: 600;
  opacity: 0.8;
}

.kgp-peak {
  padding: 0.125rem 0.5rem;
  border-radius: 999px;
  white-space: nowrap;
  font-size: 0.6875rem;
  font-weight: 600;
  color: #92400e;
  background: #fef3c7;
}

.kgp-check {
  display: inline-flex;
  width: 1.375rem;
  height: 1.375rem;
  grid-area: check;
  align-items: center;
  justify-content: center;
  margin-top: 0.1875rem;
  border-radius: 999px;
  color: transparent;
  box-shadow: inset 0 0 0 1.5px rgb(215 193 106 / 0.75);
  transition: background-color 140ms ease, color 140ms ease, box-shadow 140ms ease;
}

@media (hover: hover) {
  .kgp-option:hover {
    background: color-mix(in srgb, var(--kgp-accent) 7%, var(--md-sys-color-surface));
  }
}

.kgp-list.is-keyboard .kgp-option.is-active {
  background: color-mix(in srgb, var(--kgp-accent) 8%, var(--md-sys-color-surface));
  box-shadow: inset 0 0 0 2px color-mix(in srgb, var(--kgp-accent) 55%, transparent);
}

.kgp-option:active:not(.is-blocked) {
  background: color-mix(in srgb, var(--kgp-accent) 13%, var(--md-sys-color-surface));
}

.kgp-option.is-selected {
  background: color-mix(in srgb, var(--kgp-accent) 11%, var(--md-sys-color-surface));
  box-shadow: inset 0 0 0 1.5px color-mix(in srgb, var(--kgp-accent) 50%, transparent);
}

.kgp-option.is-selected .kgp-check {
  color: #fff;
  background: var(--kgp-accent);
  box-shadow: none;
}

/* The tinted row would swallow the tinted pills, so lift them onto white. */
.kgp-option.is-selected .kgp-option-icon,
.kgp-option.is-selected .kgp-rate,
.kgp-option.is-selected .kgp-tag:not(.kgp-tag-blocked) {
  background: rgb(255 255 255 / 0.9);
}

.kgp-option.is-blocked {
  cursor: not-allowed;
}

.kgp-option.is-blocked .kgp-option-icon,
.kgp-option.is-blocked .kgp-option-name,
.kgp-option.is-blocked .kgp-option-side {
  opacity: 0.55;
}

.kgp-option.is-blocked .kgp-check {
  visibility: hidden;
}

.kgp-option.is-shaking {
  animation: kgp-shake 360ms ease;
}

@keyframes kgp-shake {
  0%,
  100% {
    transform: translateX(0);
  }

  20%,
  60% {
    transform: translateX(-4px);
  }

  40%,
  80% {
    transform: translateX(4px);
  }
}

.kgp-empty {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 0.625rem;
  padding: 3rem 1rem;
  text-align: center;
  font-size: 0.875rem;
  color: var(--md-sys-color-on-surface-variant);
}

.kgp-empty-icon {
  color: var(--md-sys-color-outline);
}

.kgp-empty-reset {
  padding: 0.5rem 1rem;
  border-radius: 999px;
  font-size: 0.8125rem;
  font-weight: 600;
  color: #7e5600;
  background: #fff3bd;
  transition: background-color 150ms ease;
}

.kgp-empty-reset:hover {
  background: #ffe78a;
}

/* ---------- Enter / leave ---------- */
.kgp-enter-active .kgp-backdrop {
  transition: opacity 240ms ease-out;
}

.kgp-leave-active .kgp-backdrop {
  transition: opacity 200ms ease-in;
}

.kgp-enter-from .kgp-backdrop,
.kgp-leave-to .kgp-backdrop {
  opacity: 0;
}

.kgp-enter-active .kgp-panel {
  transition: transform 280ms var(--theme-ease-out), opacity 220ms ease-out;
}

.kgp-leave-active .kgp-panel {
  transition: transform 200ms ease-in, opacity 180ms ease-in;
}

.kgp-enter-from .kgp-panel,
.kgp-leave-to .kgp-panel {
  opacity: 0;
  transform: translateY(0.75rem) scale(0.98);
}

/* ---------- Phones: a bottom sheet with thumb-sized targets ---------- */
@media (max-width: 639.98px) {
  .kgp-layer {
    align-items: flex-end;
    padding: 0;
  }

  .kgp-panel {
    width: 100%;
    max-height: calc(100vh - 3.5rem);
    max-height: calc(100dvh - 3.5rem);
    border-radius: 1.5rem 1.5rem 0 0;
  }

  .kgp-panel.is-roomy {
    height: calc(100vh - 3.5rem);
    height: calc(100dvh - 3.5rem);
  }

  .kgp-header {
    padding: 1.125rem 1.125rem 0.75rem;
  }

  .kgp-tools {
    gap: 0.625rem;
    padding: 0 1.125rem 0.875rem;
  }

  .kgp-search-input {
    height: 3rem;
    /* 16px keeps iOS Safari from zooming into the field on focus. */
    font-size: 1rem;
  }

  .kgp-platforms {
    flex-wrap: nowrap;
    margin: 0 -1.125rem;
    padding: 0.125rem 1.125rem;
    overflow-x: auto;
    overscroll-behavior-x: contain;
    scrollbar-width: none;
    -webkit-mask-image: linear-gradient(90deg, transparent, #000 1.125rem, #000 calc(100% - 2rem), transparent);
    mask-image: linear-gradient(90deg, transparent, #000 1.125rem, #000 calc(100% - 2rem), transparent);
  }

  .kgp-platforms::-webkit-scrollbar {
    display: none;
  }

  .kgp-chip {
    height: 2.5rem;
  }

  .kgp-list {
    padding: 0.25rem 0.625rem calc(1rem + env(safe-area-inset-bottom, 0px));
  }

  .kgp-option {
    grid-template-columns: auto minmax(0, 1fr) auto;
    grid-template-areas:
      'icon title check'
      'icon side check'
      'icon desc check';
    column-gap: 0.75rem;
    padding: 0.875rem 0.75rem;
  }

  .kgp-option-icon {
    width: 2.25rem;
    height: 2.25rem;
    border-radius: 0.75rem;
  }

  .kgp-option-side {
    flex-direction: row;
    flex-wrap: wrap;
    align-items: center;
    justify-content: flex-start;
    margin-top: 0.375rem;
  }

  .kgp-enter-from .kgp-panel,
  .kgp-leave-to .kgp-panel {
    opacity: 1;
    transform: translateY(100%);
  }
}

/* Landscape phones and short windows: give the height to the rows. */
@media (min-width: 640px) and (max-height: 560px) {
  .kgp-layer {
    padding: 0.75rem;
  }

  .kgp-panel {
    max-height: calc(100vh - 1.5rem);
    max-height: calc(100dvh - 1.5rem);
  }

  .kgp-panel.is-roomy {
    height: calc(100vh - 1.5rem);
    height: calc(100dvh - 1.5rem);
  }

  .kgp-header {
    padding-top: 0.875rem;
    padding-bottom: 0.5rem;
  }

  .kgp-subtitle {
    display: none;
  }

  .kgp-tools {
    padding-bottom: 0.625rem;
  }
}

@media (prefers-reduced-motion: reduce) {
  .kgp-enter-active .kgp-panel,
  .kgp-leave-active .kgp-panel {
    transition: opacity 150ms ease;
  }

  .kgp-enter-from .kgp-panel,
  .kgp-leave-to .kgp-panel {
    opacity: 0;
    transform: none;
  }

  .kgp-option.is-shaking {
    animation: none;
  }
}
</style>
