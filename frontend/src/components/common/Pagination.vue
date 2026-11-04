<template>
  <div
    class="pagination-shell flex items-center justify-between border-t px-4 py-3 sm:px-6"
    :class="{ 'gap-3': isPicker }"
  >
    <!-- Picker pagination: previous, a dropdown that lists every page, next — on phones and desktops alike. -->
    <nav
      v-if="isPicker"
      class="pagination-picker sm:order-last"
      :aria-label="t('pagination.choosePage')"
      data-testid="pagination-picker"
    >
      <button
        type="button"
        @click="goToPage(page - 1)"
        :disabled="page <= 1"
        class="pagination-button pagination-step"
        data-testid="pagination-previous"
      >
        <Icon name="chevronLeft" size="sm" class="pagination-step-icon" />
        <span>{{ t('pagination.previous') }}</span>
      </button>
      <PagePicker
        :page="page"
        :total-pages="totalPages"
        :total-is-exact="totalIsExact"
        @select="goToPage"
      />
      <button
        type="button"
        @click="goToPage(page + 1)"
        :disabled="page >= totalPages"
        class="pagination-button pagination-step"
        data-testid="pagination-next"
      >
        <span>{{ t('pagination.next') }}</span>
        <Icon name="chevronRight" size="sm" class="pagination-step-icon" />
      </button>
    </nav>

    <div v-else class="flex flex-1 items-center justify-between sm:hidden">
      <!-- Mobile pagination -->
      <button
        @click="goToPage(page - 1)"
        :disabled="page === 1"
        class="pagination-button relative inline-flex items-center rounded-lg border px-4 py-2 text-sm font-medium disabled:cursor-not-allowed disabled:opacity-50"
      >
        {{ t('pagination.previous') }}
      </button>
      <span class="pagination-copy text-sm">
        {{ t('pagination.pageOf', { page, total: totalPagesDisplay }) }}
      </span>
      <button
        @click="goToPage(page + 1)"
        :disabled="page === totalPages"
        class="pagination-button relative ml-3 inline-flex items-center rounded-lg border px-4 py-2 text-sm font-medium disabled:cursor-not-allowed disabled:opacity-50"
      >
        {{ t('pagination.next') }}
      </button>
    </div>

    <div class="hidden sm:flex sm:flex-1 sm:flex-wrap sm:items-center sm:justify-between sm:gap-3">
      <!-- Desktop pagination info; wraps on narrow tablets instead of pushing the page sideways. -->
      <div class="flex flex-wrap items-center gap-x-4 gap-y-2">
        <p class="pagination-copy text-sm">
          {{ t('pagination.showing') }}
          <span class="font-medium">{{ fromItem }}</span>
          {{ t('pagination.to') }}
          <span class="font-medium">{{ toItem }}</span>
          {{ t('pagination.of') }}
          <span class="font-medium">{{ totalDisplay }}</span>
          {{ t('pagination.results') }}
        </p>

        <!-- Page size selector -->
        <div v-if="showPageSizeSelector" class="flex items-center space-x-2">
          <span class="pagination-copy text-sm"
            >{{ t('pagination.perPage') }}:</span
          >
          <div class="page-size-select w-20">
            <Select
              :model-value="pageSize"
              :options="pageSizeSelectOptions"
              @update:model-value="handlePageSizeChange"
            />
          </div>
        </div>

        <div v-if="showJump" class="flex items-center space-x-2">
          <span class="pagination-copy text-sm">{{ t('pagination.jumpTo') }}</span>
          <input
            v-model="jumpPage"
            type="number"
            min="1"
            :max="totalPages"
            class="input w-20 text-sm"
            :placeholder="t('pagination.jumpPlaceholder')"
            @keyup.enter="submitJump"
          />
          <button type="button" class="btn btn-ghost btn-sm" @click="submitJump">
            {{ t('pagination.jumpAction') }}
          </button>
        </div>
      </div>

      <!-- Desktop pagination buttons -->
      <nav
        v-if="!isPicker"
        class="relative z-0 inline-flex flex-wrap gap-1 rounded-xl"
        aria-label="Pagination"
      >
        <!-- Previous button -->
        <button
          @click="goToPage(page - 1)"
          :disabled="page === 1"
          class="pagination-button relative inline-flex items-center rounded-lg border px-2 py-2 text-sm font-medium disabled:cursor-not-allowed disabled:opacity-50"
          :aria-label="t('pagination.previous')"
        >
          <Icon name="chevronLeft" size="md" />
        </button>

        <!-- Page numbers -->
        <button
          v-for="(pageNum, index) in visiblePages"
          :key="`${pageNum}-${index}`"
          @click="typeof pageNum === 'number' && goToPage(pageNum)"
          :disabled="typeof pageNum !== 'number'"
          :class="[
            'pagination-page relative inline-flex items-center rounded-lg border px-4 py-2 text-sm font-medium',
            pageNum === page
              ? 'pagination-page-active z-10'
              : 'pagination-page-idle',
            typeof pageNum !== 'number' && 'cursor-default'
          ]"
          :aria-label="
            typeof pageNum === 'number' ? t('pagination.goToPage', { page: pageNum }) : undefined
          "
          :aria-current="pageNum === page ? 'page' : undefined"
        >
          {{ pageNum }}
        </button>

        <!-- Next button -->
        <button
          @click="goToPage(page + 1)"
          :disabled="page === totalPages"
          class="pagination-button relative inline-flex items-center rounded-lg border px-2 py-2 text-sm font-medium disabled:cursor-not-allowed disabled:opacity-50"
          :aria-label="t('pagination.next')"
        >
          <Icon name="chevronRight" size="md" />
        </button>
      </nav>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import PagePicker from './PagePicker.vue'
import Select from './Select.vue'
import { getConfiguredTablePageSizeOptions, normalizeTablePageSize } from '@/utils/tablePreferences'
import { setPersistedPageSize } from '@/composables/usePersistedPageSize'

const { t } = useI18n()

interface Props {
  total: number
  totalIsExact?: boolean
  page: number
  pageSize: number
  pageSizeOptions?: number[]
  showPageSizeSelector?: boolean
  showJump?: boolean
  /** "picker" swaps the numbered buttons for previous / page dropdown / next. */
  variant?: 'numbers' | 'picker'
}

interface Emits {
  (e: 'update:page', page: number): void
  (e: 'update:pageSize', pageSize: number): void
}

const props = withDefaults(defineProps<Props>(), {
  totalIsExact: true,
  pageSizeOptions: () => getConfiguredTablePageSizeOptions(),
  showPageSizeSelector: true,
  showJump: false,
  variant: 'numbers'
})

const emit = defineEmits<Emits>()

const isPicker = computed(() => props.variant === 'picker')
const totalPages = computed(() => Math.ceil(props.total / props.pageSize))
const totalDisplay = computed(() => props.totalIsExact ? props.total : `${props.total}+`)
const totalPagesDisplay = computed(() => props.totalIsExact ? totalPages.value : `${totalPages.value}+`)

const fromItem = computed(() => {
  if (props.total === 0) return 0
  return (props.page - 1) * props.pageSize + 1
})

const toItem = computed(() => {
  const to = props.page * props.pageSize
  return to > props.total ? props.total : to
})

const pageSizeSelectOptions = computed(() => {
  const options = Array.from(
    new Set([
      ...getConfiguredTablePageSizeOptions(),
      normalizeTablePageSize(props.pageSize)
    ])
  ).sort((a, b) => a - b)

  return options.map((size) => ({
    value: size,
    label: String(size)
  }))
})

const jumpPage = ref('')

const visiblePages = computed(() => {
  const pages: (number | string)[] = []
  const maxVisible = 7
  const total = totalPages.value

  if (total <= maxVisible) {
    // Show all pages if total is small
    for (let i = 1; i <= total; i++) {
      pages.push(i)
    }
  } else {
    // Always show first page
    pages.push(1)

    const start = Math.max(2, props.page - 2)
    const end = Math.min(total - 1, props.page + 2)

    // Add ellipsis before if needed
    if (start > 2) {
      pages.push('...')
    }

    // Add middle pages
    for (let i = start; i <= end; i++) {
      pages.push(i)
    }

    // Add ellipsis after if needed
    if (end < total - 1) {
      pages.push('...')
    }

    // Always show last page
    pages.push(total)
  }

  return pages
})

const goToPage = (newPage: number) => {
  if (newPage >= 1 && newPage <= totalPages.value && newPage !== props.page) {
    emit('update:page', newPage)
  }
}

const handlePageSizeChange = (value: string | number | boolean | null) => {
  if (value === null || typeof value === 'boolean') return
  const newPageSize = normalizeTablePageSize(typeof value === 'string' ? parseInt(value, 10) : value)
  setPersistedPageSize(newPageSize)
  emit('update:pageSize', newPageSize)
}

const submitJump = () => {
  const value = jumpPage.value.trim()
  if (!value) return
  const pageNum = Number.parseInt(value, 10)
  if (Number.isNaN(pageNum)) return
  const nextPage = Math.min(Math.max(pageNum, 1), totalPages.value)
  jumpPage.value = ''
  goToPage(nextPage)
}
</script>

<style scoped>
.pagination-shell {
  border-color: transparent;
  border-radius: 0 0 1.25rem 1.25rem;
  background: var(--md-sys-color-surface-container);
  color: var(--md-sys-color-on-surface);
}

.pagination-copy {
  color: var(--md-sys-color-on-surface-variant);
}

.pagination-button,
.pagination-page-idle {
  border-color: transparent;
  background: var(--md-sys-color-surface);
  color: var(--md-sys-color-on-surface-variant);
  transition: background-color 160ms ease, border-color 160ms ease, color 160ms ease;
}

.pagination-button:hover:not(:disabled),
.pagination-page-idle:hover {
  border-color: transparent;
  background: var(--md-sys-color-surface-container-high);
  color: var(--md-sys-color-on-surface);
}

.pagination-page-active {
  border-color: transparent;
  background: var(--md-sys-color-primary);
  color: var(--md-sys-color-on-primary);
}

.page-size-select :deep(.select-trigger) {
  @apply px-3 py-1.5 text-sm;
  background: var(--md-sys-color-surface);
}

/* Phones: previous and next hug the edges with the page picker centred between them. */
.pagination-picker {
  display: grid;
  width: 100%;
  grid-template-columns: minmax(0, 1fr) auto minmax(0, 1fr);
  align-items: center;
  gap: 0.5rem;
}

.pagination-picker > :first-child {
  justify-self: start;
}

.pagination-picker > :last-child {
  justify-self: end;
}

.pagination-step {
  display: inline-flex;
  min-height: 2.5rem;
  max-width: 100%;
  align-items: center;
  justify-content: center;
  gap: 0.25rem;
  padding: 0 0.75rem;
  border-radius: 0.75rem;
  font-size: 0.875rem;
  font-weight: 500;
  white-space: nowrap;
}

.pagination-step:disabled {
  cursor: not-allowed;
  opacity: 0.5;
}

/* Phones keep plain words so five-digit page numbers still fit; arrows join them once the bar has room. */
.pagination-step-icon {
  display: none;
}

@media (max-width: 359.98px) {
  .pagination-picker {
    gap: 0.375rem;
  }

  .pagination-step {
    padding: 0 0.5rem;
  }
}

@media (min-width: 640px) {
  .pagination-picker {
    display: flex;
    width: auto;
    flex: none;
  }

  .pagination-step-icon {
    display: block;
  }
}
</style>
