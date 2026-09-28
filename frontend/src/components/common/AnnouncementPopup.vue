<template>
  <Teleport to="body">
    <Transition name="popup-fade">
      <div
        v-if="displayedAnnouncement"
        class="fixed inset-0 z-[120] flex items-start justify-center overflow-y-auto bg-black/50 p-4 pt-[8vh] backdrop-blur-sm"
      >
        <div
          role="dialog"
          aria-modal="true"
          :aria-label="displayedAnnouncement.title"
          class="announcement-surface w-full max-w-[680px] overflow-hidden rounded-3xl shadow-2xl ring-1 ring-black/5"
          @click.stop
        >
          <div class="announcement-header px-5 py-5 sm:px-8 sm:py-6">
            <div>
              <!-- Icon and badge -->
              <div class="mb-3 flex flex-wrap items-center gap-2">
                <div class="announcement-icon flex h-10 w-10 shrink-0 items-center justify-center rounded-lg">
                  <Icon name="bell" size="md" />
                </div>
                <span class="announcement-badge">{{ t('announcements.title') }}</span>
                <span v-if="displayedAnnouncement.is_pinned" class="announcement-badge" data-testid="announcement-pinned">
                  <Icon name="pin" size="xs" />
                  {{ t('announcements.pinned') }}
                </span>
              </div>

              <!-- Title -->
              <h2 class="mb-2 break-words text-xl font-bold leading-snug text-gray-900 sm:text-2xl">
                {{ displayedAnnouncement.title }}
              </h2>

              <!-- Time -->
              <div class="flex items-center gap-1.5 text-sm text-gray-600">
                <Icon name="clock" size="sm" class="shrink-0" />
                <time>{{ formatRelativeWithDateTime(displayedAnnouncement.created_at) }}</time>
              </div>
            </div>
          </div>

          <!-- Body -->
          <div class="max-h-[50vh] overflow-y-auto px-5 py-6 sm:px-8 sm:py-8">
            <div class="relative">
              <div class="announcement-accent absolute bottom-0 left-0 top-0 w-1 rounded-full"></div>
              <div class="pl-4 sm:pl-6">
                <div
                  class="markdown-body prose prose-sm max-w-none dark:prose-invert"
                  v-html="renderedContent"
                ></div>
              </div>
            </div>
          </div>

          <!-- Footer -->
          <div class="announcement-footer px-5 py-4 sm:px-8 sm:py-5">
            <div class="flex items-center justify-end">
              <button
                @click="handleDismiss"
                data-testid="announcement-popup-dismiss"
                class="btn btn-primary"
              >
                <span class="flex items-center gap-2">
                  <Icon :name="preview ? 'x' : 'check'" size="sm" />
                  {{ preview ? t('common.close') : t('announcements.markRead') }}
                </span>
              </button>
            </div>
          </div>
        </div>
      </div>
    </Transition>
  </Teleport>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { marked } from 'marked'
import DOMPurify from 'dompurify'
import { useAnnouncementStore } from '@/stores/announcements'
import { formatRelativeWithDateTime } from '@/utils/format'
import type { Announcement, UserAnnouncement } from '@/types'
import Icon from '@/components/icons/Icon.vue'
import '@/styles/announcement-markdown.css'
import '@/styles/announcements.css'

type PreviewAnnouncement = Pick<Announcement | UserAnnouncement, 'title' | 'content' | 'created_at' | 'is_pinned'>

const props = withDefaults(defineProps<{
  announcement?: PreviewAnnouncement | null
  preview?: boolean
}>(), {
  announcement: null,
  preview: false,
})

const emit = defineEmits<{
  close: []
}>()

const { t } = useI18n()
const announcementStore = useAnnouncementStore()
const displayedAnnouncement = computed(() => (
  props.preview ? props.announcement : announcementStore.currentPopup
))

marked.setOptions({
  breaks: true,
  gfm: true,
})

const renderedContent = computed(() => {
  const content = displayedAnnouncement.value?.content
  if (!content) return ''
  const html = marked.parse(content) as string
  return DOMPurify.sanitize(html)
})

function handleDismiss() {
  if (props.preview) {
    emit('close')
    return
  }
  announcementStore.dismissPopup()
}

let previousBodyOverflow: string | null = null

function restoreBodyOverflow() {
  if (previousBodyOverflow === null) return
  document.body.style.overflow = previousBodyOverflow
  previousBodyOverflow = null
}

watch(
  displayedAnnouncement,
  (popup) => {
    if (popup) {
      previousBodyOverflow ??= document.body.style.overflow
      document.body.style.overflow = 'hidden'
    } else {
      restoreBodyOverflow()
    }
  },
  { immediate: true },
)

onBeforeUnmount(restoreBodyOverflow)
</script>

<style scoped>
.popup-fade-enter-active {
  transition: all 0.3s cubic-bezier(0.16, 1, 0.3, 1);
}

.popup-fade-leave-active {
  transition: all 0.2s cubic-bezier(0.4, 0, 1, 1);
}

.popup-fade-enter-from,
.popup-fade-leave-to {
  opacity: 0;
}

.popup-fade-enter-from > div {
  transform: scale(0.94) translateY(-12px);
  opacity: 0;
}

.popup-fade-leave-to > div {
  transform: scale(0.96) translateY(-8px);
  opacity: 0;
}

/* Scrollbar Styling */
.overflow-y-auto::-webkit-scrollbar {
  width: 8px;
}

.overflow-y-auto::-webkit-scrollbar-track {
  background: transparent;
}

.overflow-y-auto::-webkit-scrollbar-thumb {
  background: var(--md-sys-color-outline);
  border-radius: 4px;
}
</style>
