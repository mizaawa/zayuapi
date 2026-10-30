<template>
  <Teleport to="body">
    <Transition name="popup-fade">
      <div
        v-if="displayedAnnouncement"
        class="announcement-overlay"
      >
        <div
          role="dialog"
          aria-modal="true"
          aria-labelledby="announcement-popup-title"
          aria-describedby="announcement-popup-body"
          class="announcement-dialog"
          @click.stop
        >
          <span class="announcement-grabber" aria-hidden="true"></span>

          <header class="announcement-head">
            <div class="announcement-tags">
              <span class="announcement-seal" aria-hidden="true">
                <Icon name="megaphone" size="sm" />
              </span>
              <span class="announcement-kind">{{ t('announcements.title') }}</span>
              <span v-if="displayedAnnouncement.is_pinned" class="announcement-pin" data-testid="announcement-pinned">
                <Icon name="pin" size="xs" />
                {{ t('announcements.pinned') }}
              </span>
            </div>

            <h2 id="announcement-popup-title" class="announcement-title">
              {{ displayedAnnouncement.title }}
            </h2>

            <p class="announcement-time">
              <Icon name="clock" size="xs" class="shrink-0" />
              <time :datetime="displayedAnnouncement.created_at">{{ formatRelativeWithDateTime(displayedAnnouncement.created_at) }}</time>
            </p>
          </header>

          <div id="announcement-popup-body" class="announcement-body">
            <div
              class="markdown-body"
              v-html="renderedContent"
            ></div>
          </div>

          <footer class="announcement-foot">
            <p v-if="queuedCount > 0" class="announcement-queue" data-testid="announcement-queue">
              {{ t('announcements.moreQueued', { count: queuedCount }, queuedCount) }}
            </p>
            <button
              ref="dismissButton"
              type="button"
              data-testid="announcement-popup-dismiss"
              class="btn btn-primary announcement-dismiss"
              @click="handleDismiss"
            >
              <Icon :name="preview ? 'x' : 'check'" size="sm" />
              {{ preview ? t('common.close') : t('announcements.markRead') }}
            </button>
          </footer>
        </div>
      </div>
    </Transition>
  </Teleport>
</template>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { marked } from 'marked'
import DOMPurify from 'dompurify'
import { useAnnouncementStore } from '@/stores/announcements'
import { formatRelativeWithDateTime } from '@/utils/format'
import type { Announcement, UserAnnouncement } from '@/types'
import Icon from '@/components/icons/Icon.vue'
import '@/styles/announcement-markdown.css'

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
const dismissButton = ref<HTMLButtonElement | null>(null)
const displayedAnnouncement = computed(() => (
  props.preview ? props.announcement : announcementStore.currentPopup
))
// An admin preview stands alone; only real popups can have more waiting.
const queuedCount = computed(() => (props.preview ? 0 : announcementStore.queuedPopupCount ?? 0))

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
      // Keyboard users land on the one action the dialog offers.
      void nextTick(() => dismissButton.value?.focus({ preventScroll: true }))
    } else {
      restoreBodyOverflow()
    }
  },
  { immediate: true },
)

onBeforeUnmount(restoreBodyOverflow)
</script>

<style scoped>
.announcement-overlay {
  position: fixed;
  inset: 0;
  z-index: 120;
  display: flex;
  align-items: flex-start;
  justify-content: center;
  overflow-y: auto;
  padding: 9vh 1rem 2rem;
  background: var(--theme-scrim);
  backdrop-filter: blur(6px) saturate(1.05);
}

.announcement-dialog {
  position: relative;
  display: flex;
  width: 100%;
  max-width: 40rem;
  flex-direction: column;
  overflow: hidden;
  border-radius: 1.75rem;
  background: var(--md-sys-color-surface);
  color: var(--md-sys-color-on-surface);
  box-shadow:
    inset 0 1px 0 rgb(255 255 255 / 0.8),
    0 0 0 1px rgb(215 193 106 / 0.2),
    0 2rem 4rem -1.25rem rgb(63 52 32 / 0.42),
    0 0.35rem 1rem rgb(63 52 32 / 0.08);
}

.announcement-grabber {
  display: none;
}

/* A honey letterhead that fades into the page. */
.announcement-head {
  padding: 1.5rem 1.75rem 1.125rem;
  background:
    radial-gradient(32rem 10rem at 0% 0%, rgb(255 231 138 / 0.75), transparent 75%),
    linear-gradient(180deg, #fff3bd 0%, rgb(255 243 189 / 0) 100%);
}

.announcement-tags {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 0.5rem;
}

.announcement-seal {
  display: inline-flex;
  width: 2.25rem;
  height: 2.25rem;
  flex-shrink: 0;
  align-items: center;
  justify-content: center;
  border-radius: 999px;
  background: #e9b824;
  color: #3f3420;
  box-shadow: 0 0 0 4px rgb(255 254 248 / 0.75), 0 8px 16px -8px rgb(151 104 0 / 0.6);
  transform: rotate(-10deg);
}

.announcement-kind {
  font-size: 0.8125rem;
  font-weight: 700;
  color: #7e5600;
}

.announcement-pin {
  display: inline-flex;
  align-items: center;
  gap: 0.25rem;
  padding: 0 0.5rem;
  border-radius: 999px;
  background: #3f3420;
  font-size: 0.6875rem;
  font-weight: 700;
  line-height: 1.375rem;
  color: #fff3bd;
}

.announcement-title {
  margin-top: 0.875rem;
  font-size: 1.5rem;
  font-weight: 800;
  line-height: 1.3;
  letter-spacing: -0.01em;
  overflow-wrap: anywhere;
  color: #3f3420;
}

.announcement-time {
  display: flex;
  align-items: center;
  gap: 0.375rem;
  margin-top: 0.5rem;
  font-size: 0.8125rem;
  font-variant-numeric: tabular-nums;
  color: #6b6045;
}

/* Scroll shadows: the covers move with the content, the shadows stay put. */
.announcement-body {
  max-height: 52vh;
  overflow-y: auto;
  overscroll-behavior: contain;
  padding: 0.5rem 1.75rem 1.5rem;
  background:
    linear-gradient(var(--md-sys-color-surface) 30%, rgb(255 254 248 / 0)) top / 100% 2.5rem no-repeat local,
    linear-gradient(rgb(255 254 248 / 0), var(--md-sys-color-surface) 70%) bottom / 100% 2.5rem no-repeat local,
    radial-gradient(farthest-side at 50% 0, rgb(63 52 32 / 0.12), transparent) top / 100% 0.75rem no-repeat scroll,
    radial-gradient(farthest-side at 50% 100%, rgb(63 52 32 / 0.12), transparent) bottom / 100% 0.75rem no-repeat scroll;
}

.announcement-body::-webkit-scrollbar {
  width: 8px;
}

.announcement-body::-webkit-scrollbar-track {
  background: transparent;
}

.announcement-body::-webkit-scrollbar-thumb {
  border-radius: 4px;
  background: rgb(215 193 106 / 0.6);
}

.announcement-foot {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: flex-end;
  gap: 0.75rem 1rem;
  padding: 1rem 1.75rem 1.25rem;
  background: #fffbe8;
}

.announcement-queue {
  margin-right: auto;
  font-size: 0.8125rem;
  font-weight: 600;
  color: #6b6045;
}

.announcement-dismiss {
  min-width: 8.5rem;
}

.popup-fade-enter-active {
  transition: opacity 0.32s ease;
}

.popup-fade-leave-active {
  transition: opacity 0.2s ease-in;
}

.popup-fade-enter-active .announcement-dialog {
  transition: transform 0.32s var(--theme-ease-out), opacity 0.28s ease;
}

.popup-fade-leave-active .announcement-dialog {
  transition: transform 0.2s ease-in, opacity 0.2s ease-in;
}

.popup-fade-enter-from,
.popup-fade-leave-to {
  opacity: 0;
}

.popup-fade-enter-from .announcement-dialog {
  opacity: 0;
  transform: translateY(-0.75rem) scale(0.96);
}

.popup-fade-leave-to .announcement-dialog {
  opacity: 0;
  transform: translateY(-0.5rem) scale(0.97);
}

/* Phones get a bottom sheet so the action sits under the thumb. */
@media (max-width: 639px) {
  .announcement-overlay {
    align-items: flex-end;
    padding: 2.5rem 0 0;
  }

  .announcement-dialog {
    max-width: none;
    max-height: 88vh;
    max-height: 88dvh;
    border-radius: 1.75rem 1.75rem 0 0;
  }

  .announcement-grabber {
    position: absolute;
    top: 0.5rem;
    left: 50%;
    display: block;
    width: 2.5rem;
    height: 0.3125rem;
    border-radius: 999px;
    background: rgb(126 86 0 / 0.25);
    transform: translateX(-50%);
  }

  .announcement-head {
    padding: 1.5rem 1.25rem 1rem;
  }

  .announcement-title {
    margin-top: 0.75rem;
    font-size: 1.25rem;
  }

  .announcement-body {
    max-height: none;
    flex: 1 1 auto;
    min-height: 0;
    padding: 0.25rem 1.25rem 1.25rem;
  }

  .announcement-foot {
    padding: 0.875rem 1.25rem calc(1rem + env(safe-area-inset-bottom));
  }

  .announcement-queue {
    width: 100%;
    margin-right: 0;
    text-align: center;
  }

  .announcement-dismiss {
    width: 100%;
    min-height: 3rem;
  }

  .popup-fade-enter-from .announcement-dialog,
  .popup-fade-leave-to .announcement-dialog {
    opacity: 1;
    transform: translateY(100%);
  }
}

@media (prefers-reduced-motion: reduce) {
  .popup-fade-enter-active .announcement-dialog,
  .popup-fade-leave-active .announcement-dialog {
    transition: opacity 0.2s ease;
  }

  .popup-fade-enter-from .announcement-dialog,
  .popup-fade-leave-to .announcement-dialog {
    transform: none;
  }
}
</style>
