<template>
  <AppLayout>
    <section
      class="image-workbench"
      :style="{ '--workbench-sidebar-width': appStore.sidebarCollapsed ? '84px' : '256px' }"
      @paste="onPaste"
    >
      <header class="workbench-heading">
        <div class="heading-title">
          <Icon name="sparkles" size="lg" />
          <h2>{{ t('imageWorkbench.title') }}</h2>
        </div>
        <span class="activity"
          ><span :class="{ busy: activeCount > 0 }"></span
          >{{ t('imageWorkbench.activeCount', { count: activeCount, max: maxConcurrentLabel }) }}</span
        >
      </header>

      <div v-if="tasksError" class="workbench-message message-error" role="alert">
        <Icon name="exclamationCircle" size="sm" /><span>{{ t('imageWorkbench.tasksError') }}</span
        ><button type="button" @click="refreshTasks">{{ t('imageWorkbench.retry') }}</button>
      </div>
      <div v-else-if="capabilitiesLoaded && !enabled" class="workbench-message" role="status">
        <Icon name="infoCircle" size="sm" /><span>{{ t('imageWorkbench.unavailable') }}</span>
      </div>

      <form
        v-show="tab === 'workbench'"
        id="workbench-panel"
        role="tabpanel"
        aria-labelledby="workbench-tab"
        class="workbench-form"
        @submit.prevent="submit"
      >
        <div class="creative-column">
          <div class="field-title">
            <label for="image-prompt">{{ t('imageWorkbench.prompt') }}</label
            ><span class="character-count">{{ form.prompt.length.toLocaleString() }} / 32,000</span>
          </div>
          <textarea
            id="image-prompt"
            v-model="form.prompt"
            maxlength="32000"
            :placeholder="t('imageWorkbench.promptPlaceholder')"
            class="prompt-input"
            spellcheck="false"
          ></textarea>
          <div class="field-title references-title">
            <span>{{ t('imageWorkbench.references') }}</span
            ><span class="reference-counter">{{
              t('imageWorkbench.referenceCount', { count: references.length })
            }}</span>
          </div>
          <div
            class="reference-strip"
            :class="{ 'drag-active': dragging }"
            @dragover.prevent="dragging = true"
            @dragleave.prevent="dragging = false"
            @drop.prevent="onDrop"
          >
            <div
              v-for="(reference, index) in references"
              :key="reference.id"
              class="reference-image"
            >
              <img :src="reference.url" :alt="reference.file.name" />
              <button
                type="button"
                :title="t('imageWorkbench.removeReference')"
                :aria-label="t('imageWorkbench.removeReference')"
                @click="removeReference(index)"
              >
                <Icon name="x" size="xs" />
              </button>
            </div>
            <button
              v-if="references.length < 2"
              type="button"
              class="reference-add"
              :disabled="readingReferences"
              :aria-label="t('imageWorkbench.addReference')"
              :title="t('imageWorkbench.addReference')"
              @click="fileInput?.click()"
            >
              <Icon
                :name="readingReferences ? 'refresh' : 'plus'"
                size="lg"
                :class="{ 'animate-spin': readingReferences }"
              />
            </button>
            <input
              ref="fileInput"
              type="file"
              accept="image/png,image/jpeg,image/webp"
              multiple
              class="sr-only"
              tabindex="-1"
              :aria-label="t('imageWorkbench.addReference')"
              @change="onFileChange"
            />
          </div>
        </div>

        <div class="parameters-column">
          <div class="key-model-fields">
            <div class="parameter-field">
              <label for="image-key">{{ t('imageWorkbench.apiKey') }}</label
              ><Select
                id="image-key"
                v-model="selectedKeyId"
                :options="keyOptions"
                :aria-label="t('imageWorkbench.apiKey')"
                :placeholder="loadingKeys ? t('common.loading') : t('imageWorkbench.chooseKey')"
                :disabled="loadingKeys || submitting"
              />
            </div>
            <div class="parameter-field">
              <label for="image-model">{{ t('imageWorkbench.model') }}</label
              ><Select
                id="image-model"
                v-model="form.model"
                :options="modelOptions"
                :aria-label="t('imageWorkbench.model')"
                :placeholder="loadingModels ? t('common.loading') : t('imageWorkbench.chooseModel')"
                :disabled="!selectedKey || loadingModels || submitting"
              />
            </div>
          </div>
          <div v-if="keysError || (!loadingKeys && !keys.length)" class="inline-state">
            <span>{{ t(keysError ? 'imageWorkbench.keysError' : 'imageWorkbench.noKeys') }}</span
            ><button v-if="keysError" type="button" @click="loadKeys">
              {{ t('imageWorkbench.retry') }}</button
            ><router-link v-else to="/keys"
              >{{ t('imageWorkbench.manageKeys') }}<Icon name="arrowRight" size="xs"
            /></router-link>
          </div>
          <div
            v-else-if="modelsError || (selectedKey && !loadingModels && !models.length)"
            class="inline-state"
          >
            <span>{{
              t(modelsError ? 'imageWorkbench.modelsError' : 'imageWorkbench.noModels')
            }}</span
            ><button type="button" @click="loadModels">{{ t('imageWorkbench.retry') }}</button>
          </div>

          <fieldset>
            <legend>{{ t('imageWorkbench.quality') }}</legend>
            <div class="segmented four">
              <button
                v-for="quality in qualities"
                :key="quality"
                type="button"
                :aria-pressed="form.quality === quality"
                :class="{ selected: form.quality === quality }"
                @click="form.quality = quality"
              >
                {{ t(`imageWorkbench.${quality}`) }}
              </button>
            </div>
          </fieldset>
          <fieldset>
            <legend>{{ t('imageWorkbench.aspectRatio') }}</legend>
            <div class="segmented five ratio-options">
              <button
                v-for="ratio in ratios"
                :key="ratio"
                type="button"
                :aria-pressed="form.aspectRatio === ratio"
                :class="{ selected: form.aspectRatio === ratio }"
                @click="form.aspectRatio = ratio"
              >
                <span class="ratio-outline" :style="ratioStyle(ratio)"></span
                ><span>{{ ratio }}</span>
              </button>
            </div>
            <div class="custom-ratio">
              <button
                type="button"
                class="custom-toggle"
                :class="{ selected: form.aspectRatio === 'custom' }"
                :aria-pressed="form.aspectRatio === 'custom'"
                @click="form.aspectRatio = 'custom'"
              >
                {{ t('imageWorkbench.custom') }}</button
              ><label for="custom-width">{{ t('imageWorkbench.width') }}</label
              ><input
                id="custom-width"
                v-model.number="form.customWidth"
                type="number"
                min="1"
                max="8192"
                step="1"
                inputmode="numeric"
                :disabled="form.aspectRatio !== 'custom'"
              /><span>:</span><label for="custom-height">{{ t('imageWorkbench.height') }}</label
              ><input
                id="custom-height"
                v-model.number="form.customHeight"
                type="number"
                min="1"
                max="8192"
                step="1"
                inputmode="numeric"
                :disabled="form.aspectRatio !== 'custom'"
              />
            </div>
            <p v-if="!outputSize" class="validation-error">
              {{ t('imageWorkbench.invalidRatio') }}
            </p>
          </fieldset>
          <fieldset>
            <legend>{{ t('imageWorkbench.resolution') }}</legend>
            <div class="segmented four">
              <button
                v-for="resolution in resolutions"
                :key="resolution"
                type="button"
                :aria-pressed="form.resolution === resolution"
                :class="{ selected: form.resolution === resolution }"
                @click="form.resolution = resolution"
              >
                {{ resolution }}
              </button>
            </div>
          </fieldset>
          <fieldset>
            <legend>{{ t('imageWorkbench.quantity') }}</legend>
            <div class="segmented five">
              <button
                v-for="quantity in quantities"
                :key="quantity"
                type="button"
                :aria-pressed="form.quantity === quantity"
                :class="{ selected: form.quantity === quantity }"
                @click="form.quantity = quantity"
              >
                {{ t('imageWorkbench.imageCount', { count: quantity }) }}
              </button>
            </div>
          </fieldset>
          <div class="output-summary">
            <span>{{ t('imageWorkbench.outputSize') }}</span
            ><span>{{ outputSize?.size || '-' }} px</span>
          </div>
          <button type="submit" class="create-task" :disabled="!canSubmit">
            <Icon
              :name="submitting ? 'refresh' : 'sparkles'"
              size="md"
              :class="{ 'animate-spin': submitting }"
            />{{ t(submitting ? 'imageWorkbench.submitting' : 'imageWorkbench.create')
            }}<Icon name="arrowRight" size="sm" />
          </button>
        </div>
      </form>

      <div
        v-if="tab === 'tasks'"
        id="tasks-panel"
        role="tabpanel"
        aria-labelledby="tasks-tab"
        class="tasks-panel"
      >
        <div class="task-notice">
          <Icon name="cloud" size="md" />
          <p>{{ t('imageWorkbench.taskNotice', { max: maxConcurrentLabel, minutes: retentionMinutes }) }}</p>
        </div>
        <div class="task-toolbar">
          <div class="status-filter" role="group" :aria-label="t('common.status')">
            <button
              v-for="status in statuses"
              :key="status"
              type="button"
              :class="{ active: taskFilter === status }"
              :aria-pressed="taskFilter === status"
              @click="taskFilter = status"
            >
              {{ t(`imageWorkbench.${status}`) }}<span>{{ statusCount(status) }}</span>
            </button>
          </div>
          <button
            type="button"
            class="toolbar-refresh"
            :disabled="loadingTasks"
            :title="t('imageWorkbench.refresh')"
            :aria-label="t('imageWorkbench.refresh')"
            @click="refreshTasks"
          >
            <Icon name="refresh" :class="{ 'animate-spin': loadingTasks }" size="sm" />
          </button>
        </div>
        <div v-if="loadingTasks && !capabilitiesLoaded" class="empty-tasks">
          <Icon name="refresh" size="lg" class="animate-spin" /><span>{{
            t('common.loading')
          }}</span>
        </div>
        <div v-else-if="!visibleTasks.length" class="empty-tasks">
          <Icon name="grid" size="xl" />
          <h3>
            {{ t(tasks.length ? 'imageWorkbench.emptyFiltered' : 'imageWorkbench.emptyTasks') }}
          </h3>
          <button
            v-if="!tasks.length"
            type="button"
            class="return-workbench"
            @click="tab = 'workbench'"
          >
            <Icon name="plus" size="sm" />{{ t('imageWorkbench.create') }}
          </button>
        </div>
        <div v-else class="task-list">
          <template v-for="task in visibleTasks" :key="task.id"
            ><ImageWorkbenchTaskRow
              v-for="index in Math.max(1, task.result?.data?.length || 0)"
              :key="`${task.id}-${index}`"
              :task="task"
              :image-index="index - 1"
              :now="now"
              :key-name="keys.find((key) => key.id === task.workbench?.api_key_id)?.name"
              :downloading="downloading.has(`${task.id}-${index - 1}`)"
              @download="downloadImage(task, index - 1)"
              @delete="deleteTarget = task"
          /></template>
        </div>
      </div>

      <div
        v-if="tab === 'guide'"
        id="guide-panel"
        role="tabpanel"
        aria-labelledby="guide-tab"
        class="guide-panel"
      >
        <h3>{{ t('imageWorkbench.guideTitle') }}</h3>
        <ol>
          <li v-for="step in 4" :key="step">
            <span class="step-number">{{ String(step).padStart(2, '0') }}</span>
            <div>
              <h4>{{ t(`imageWorkbench.guideStep${step}`) }}</h4>
            <p>{{ t(`imageWorkbench.guideDetail${step}`, { minutes: retentionMinutes }) }}</p>
            </div>
          </li>
        </ol>
        <div class="guide-limits">
          <span>{{ t('imageWorkbench.guideFormats') }}</span
          ><span>{{ t('imageWorkbench.guideLimits', { max: maxConcurrentLabel, minutes: retentionMinutes }) }}</span>
        </div>
      </div>

      <nav class="workbench-tabs" role="tablist" :aria-label="t('imageWorkbench.title')">
        <button
          v-for="item in tabs"
          :id="`${item.id}-tab`"
          :key="item.id"
          type="button"
          role="tab"
          :aria-selected="tab === item.id"
          :aria-controls="`${item.id}-panel`"
          :tabindex="tab === item.id ? 0 : -1"
          :class="{ active: tab === item.id }"
          @click="selectTab(item.id)"
          @keydown="onTabKeydown($event, item.id)"
        >
          <Icon :name="item.icon" size="sm" />{{ t(`imageWorkbench.${item.id}`)
          }}<span v-if="item.id === 'tasks' && activeCount" class="tab-count">{{
            activeCount
          }}</span>
        </button>
      </nav>
    </section>
    <BaseDialog
      :show="!!deleteTarget"
      :title="t('imageWorkbench.deleteTitle')"
      width="narrow"
      @close="!deleting && (deleteTarget = null)"
      ><p>{{ t('imageWorkbench.deleteMessage') }}</p>
      <template #footer
        ><button
          type="button"
          class="btn btn-secondary"
          :disabled="deleting"
          @click="deleteTarget = null"
        >
          {{ t('common.cancel') }}</button
        ><button type="button" class="btn btn-danger" :disabled="deleting" @click="deleteTask">
          {{ t('common.delete') }}
        </button></template
      ></BaseDialog
    >
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import Icon from '@/components/icons/Icon.vue'
import Select from '@/components/common/Select.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import ImageWorkbenchTaskRow from '@/components/user/ImageWorkbenchTaskRow.vue'
import { keysAPI } from '@/api/keys'
import {
  deleteWorkbenchTask,
  downloadWorkbenchImage,
  listImageModels,
  listWorkbenchTasks,
  submitImageTask,
  type ImageModel,
  type ImageTask
} from '@/api/imageWorkbench'
import {
  calculateImageSize,
  createImageFilename as imageFilename,
  safeImageUrl,
  type ImageResolution
} from '@/utils/imageWorkbench'
import { saveBlob } from '@/api/batchImage'
import { useAuthStore } from '@/stores/auth'
import { useAppStore } from '@/stores/app'
import type { ApiKey } from '@/types'

type Tab = 'workbench' | 'tasks' | 'guide'
type Quality = 'auto' | 'low' | 'medium' | 'high'
interface ReferenceImage {
  id: string
  file: File
  url: string
  dataUrl: string
}
const { t } = useI18n()
const authStore = useAuthStore()
const appStore = useAppStore()
const tab = ref<Tab>('workbench')
const tabs = [
  { id: 'workbench', icon: 'sparkles' },
  { id: 'tasks', icon: 'clipboard' },
  { id: 'guide', icon: 'book' }
] as const
const qualities: Quality[] = ['auto', 'low', 'medium', 'high']
const ratios = ['1:1', '4:3', '3:4', '16:9', '9:16']
const resolutions = ['1K', '2K', '4K', '8K'] as const
const quantities = [1, 3, 5, 10, 20]
const statuses = ['all', 'processing', 'completed', 'failed'] as const
const form = reactive({
  prompt: '',
  model: '',
  quality: 'auto' as Quality,
  aspectRatio: '1:1',
  customWidth: 1,
  customHeight: 1,
  resolution: '1K' as ImageResolution,
  quantity: 1
})
const references = ref<ReferenceImage[]>([])
const fileInput = ref<HTMLInputElement | null>(null)
const readingReferences = ref(false)
const dragging = ref(false)
const keys = ref<ApiKey[]>([])
const models = ref<ImageModel[]>([])
const selectedKeyId = ref<number | null>(null)
const loadingKeys = ref(true)
const keysError = ref(false)
const loadingModels = ref(false)
const modelsError = ref(false)
const tasks = ref<ImageTask[]>([])
const loadingTasks = ref(false)
const tasksError = ref(false)
const enabled = ref(false)
const capabilitiesLoaded = ref(false)
const maxConcurrent = ref(5)
const retentionMinutes = ref(15)
const tutorialURL = ref('')
const submitting = ref(false)
const taskFilter = ref<(typeof statuses)[number]>('all')
const now = ref(Math.floor(Date.now() / 1000))
const downloading = reactive(new Set<string>())
const deleteTarget = ref<ImageTask | null>(null)
const deleting = ref(false)
let disposed = false
let modelAbort: AbortController | undefined
const lifecycleAbort = new AbortController()
let pollTimer: ReturnType<typeof setTimeout> | undefined
let clockTimer: ReturnType<typeof setInterval> | undefined
const selectedKey = computed(() => keys.value.find((key) => key.id === selectedKeyId.value))
const keyOptions = computed(() => keys.value.map((key) => ({ value: key.id, label: key.name })))
const modelOptions = computed(() =>
  models.value.map((model) => ({ value: model.id, label: model.id }))
)
const activeCount = computed(
  () =>
    tasks.value.filter((task) => task.status === 'processing' && task.expires_at > now.value).length
)
const maxConcurrentLabel = computed(() => maxConcurrent.value > 0 ? String(maxConcurrent.value) : '∞')
const visibleTasks = computed(() =>
  tasks.value.filter(
    (task) =>
      task.expires_at > now.value &&
      (taskFilter.value === 'all' || task.status === taskFilter.value)
  )
)
const outputSize = computed(() => {
  try {
    return calculateImageSize(
      form.aspectRatio,
      form.resolution,
      form.customWidth,
      form.customHeight
    )
  } catch {
    return null
  }
})
const canSubmit = computed(
  () =>
    !!(
      enabled.value &&
      capabilitiesLoaded.value &&
      !tasksError.value &&
      selectedKey.value &&
      form.model &&
      models.value.some((model) => model.id === form.model) &&
      form.prompt.trim() &&
      outputSize.value &&
      !submitting.value &&
      !readingReferences.value &&
      !loadingModels.value &&
      (maxConcurrent.value === 0 || activeCount.value < maxConcurrent.value)
    )
)

function message(error: unknown, fallback: string): string {
  return error instanceof Error
    ? error.message
    : typeof error === 'object' && error && 'message' in error && typeof error.message === 'string'
      ? error.message
      : t(fallback)
}
function ratioStyle(ratio: string) {
  const [width = 1, height = 1] = ratio.split(':').map(Number)
  return {
    width: `${(18 * width) / Math.max(width, height)}px`,
    height: `${(18 * height) / Math.max(width, height)}px`
  }
}
function statusCount(status: string) {
  return tasks.value.filter(
    (task) => task.expires_at > now.value && (status === 'all' || task.status === status)
  ).length
}

async function loadKeys() {
  if (disposed) return
  loadingKeys.value = true
  keysError.value = false
  try {
    const available: ApiKey[] = []
    for (let page = 1; ; page++) {
      const response = await keysAPI.list(
        page,
        100,
        { status: 'active' },
        { signal: lifecycleAbort.signal }
      )
      available.push(
        ...response.items.filter(
          (key) =>
            key.status === 'active' &&
            (!key.expires_at || Date.parse(key.expires_at) > Date.now()) &&
            key.group?.allow_image_generation &&
            ['openai', 'grok', 'composite'].includes(key.group.platform)
        )
      )
      if (!response.items.length || page >= response.pages) break
    }
    if (disposed) return
    keys.value = available
    if (!available.some((key) => key.id === selectedKeyId.value))
      selectedKeyId.value = available[0]?.id ?? null
  } catch {
    if (!disposed) keysError.value = true
  } finally {
    if (!disposed) loadingKeys.value = false
  }
}

async function loadModels() {
  modelAbort?.abort()
  const controller = new AbortController()
  modelAbort = controller
  const key = selectedKey.value
  const previous = form.model
  form.model = ''
  models.value = []
  modelsError.value = false
  loadingModels.value = !!key
  if (!key) return
  try {
    const result = await listImageModels(key.key, authStore.user?.email || '', controller.signal)
    if (disposed || controller.signal.aborted) return
    models.value = result
    form.model =
      result.find((model) => model.id === previous)?.id ||
      result.find((model) => /gpt-image-2/i.test(model.id))?.id ||
      result[0]?.id ||
      ''
  } catch {
    if (!controller.signal.aborted && !disposed) modelsError.value = true
  } finally {
    if (!controller.signal.aborted && !disposed) loadingModels.value = false
  }
}
watch(selectedKeyId, loadModels)

async function refreshTasks() {
  if (disposed || loadingTasks.value) return
  clearTimeout(pollTimer)
  loadingTasks.value = true
  try {
    const response = await listWorkbenchTasks(lifecycleAbort.signal)
    if (disposed) return
    tasks.value = [...response.tasks].sort((a, b) => b.created_at - a.created_at)
    enabled.value = response.enabled
    maxConcurrent.value = Number.isFinite(response.max_concurrent) ? response.max_concurrent : 5
    retentionMinutes.value = Math.max(1, Math.round((response.retention_seconds || 900) / 60))
    tutorialURL.value = typeof response.tutorial_url === 'string' ? response.tutorial_url : ''
    capabilitiesLoaded.value = true
    tasksError.value = false
  } catch {
    if (!disposed) tasksError.value = true
  } finally {
    if (!disposed) {
      loadingTasks.value = false
      pollTimer = setTimeout(refreshTasks, activeCount.value ? 3000 : 5000)
    }
  }
}

function selectTab(next: Tab): void {
  if (next === 'guide' && tutorialURL.value) {
    window.open(tutorialURL.value, '_blank', 'noopener,noreferrer')
    return
  }
  tab.value = next
}

async function addReferences(files: File[]) {
  if (readingReferences.value) return
  readingReferences.value = true
  try {
    for (const file of files) {
      if (references.value.length >= 2) {
        appStore.showError(t('imageWorkbench.referencesLimit'))
        break
      }
      if (!['image/png', 'image/jpeg', 'image/webp'].includes(file.type)) {
        appStore.showError(t('imageWorkbench.referenceType'))
        continue
      }
      if (file.size > 10 * 1024 * 1024) {
        appStore.showError(t('imageWorkbench.referenceSize'))
        continue
      }
      try {
        const dataUrl = await new Promise<string>((resolve, reject) => {
          const reader = new FileReader()
          reader.onload = () => resolve(String(reader.result))
          reader.onerror = reject
          reader.readAsDataURL(file)
        })
        await new Promise<void>((resolve, reject) => {
          const image = new Image()
          image.onload = () => (image.naturalWidth ? resolve() : reject(new Error('empty')))
          image.onerror = reject
          image.src = dataUrl
        })
        if (disposed) return
        references.value.push({
          id: crypto.randomUUID(),
          file,
          dataUrl,
          url: URL.createObjectURL(file)
        })
      } catch {
        if (!disposed) appStore.showError(t('imageWorkbench.referenceRead'))
      }
    }
  } finally {
    readingReferences.value = false
  }
}
function onFileChange(event: Event) {
  const input = event.target as HTMLInputElement
  void addReferences(Array.from(input.files || []))
  input.value = ''
}
function onPaste(event: ClipboardEvent) {
  const files = Array.from(event.clipboardData?.items || [])
    .filter((item) => item.kind === 'file' && item.type.startsWith('image/'))
    .map((item) => item.getAsFile())
    .filter((file): file is File => !!file)
  if (files.length) {
    event.preventDefault()
    void addReferences(files)
  }
}
function onDrop(event: DragEvent) {
  dragging.value = false
  void addReferences(Array.from(event.dataTransfer?.files || []))
}
function removeReference(index: number) {
  const removed = references.value.splice(index, 1)
  removed.forEach((reference) => URL.revokeObjectURL(reference.url))
}

async function submit() {
  if (!canSubmit.value || !selectedKey.value || !outputSize.value) return
  submitting.value = true
  const key = selectedKey.value
  try {
    const result = await submitImageTask(
      key.key,
      authStore.user?.email || '',
      {
        model: form.model,
        prompt: form.prompt.trim(),
        quality: form.quality,
        size: outputSize.value.size,
        n: form.quantity,
        ...(references.value.length
          ? { images: references.value.map((reference) => ({ image_url: reference.dataUrl })) }
          : {})
      },
      lifecycleAbort.signal
    )
    if (disposed) return
    if (!tasks.value.some((task) => task.id === result.id)) tasks.value.unshift(result)
    tab.value = 'tasks'
    taskFilter.value = 'all'
    appStore.showSuccess(t('imageWorkbench.created'))
    await refreshTasks()
  } catch (error) {
    if (!disposed) appStore.showError(message(error, 'imageWorkbench.submitError'))
  } finally {
    if (!disposed) submitting.value = false
  }
}

async function downloadImage(task: ImageTask, index: number) {
  const url = safeImageUrl(task.result?.data?.[index]?.url)
  const id = `${task.id}-${index}`
  if (!url || downloading.has(id) || task.expires_at <= now.value) return
  downloading.add(id)
  try {
    const blob = await downloadWorkbenchImage(task.id, index, lifecycleAbort.signal)
    if (!disposed) saveBlob(blob, imageFilename(task.id, index, task.created_at, url))
  } catch {
    if (!disposed) appStore.showError(t('imageWorkbench.downloadError'))
  } finally {
    downloading.delete(id)
  }
}

async function deleteTask() {
  if (!deleteTarget.value || deleting.value) return
  deleting.value = true
  const id = deleteTarget.value.id
  try {
    await deleteWorkbenchTask(id)
    tasks.value = tasks.value.filter((task) => task.id !== id)
    deleteTarget.value = null
  } catch (error) {
    appStore.showError(message(error, 'imageWorkbench.deleteError'))
  } finally {
    deleting.value = false
  }
}
function onTabKeydown(event: KeyboardEvent, current: Tab) {
  const index = tabs.findIndex((item) => item.id === current)
  let target = index
  if (event.key === 'ArrowRight') target = (index + 1) % tabs.length
  else if (event.key === 'ArrowLeft') target = (index + tabs.length - 1) % tabs.length
  else if (event.key === 'Home') target = 0
  else if (event.key === 'End') target = tabs.length - 1
  else return
  event.preventDefault()
  selectTab(tabs[target]!.id)
  void nextTick(() => document.getElementById(`${tab.value}-tab`)?.focus())
}
onMounted(() => {
  void loadKeys()
  void refreshTasks()
  clockTimer = setInterval(() => {
    now.value = Math.floor(Date.now() / 1000)
  }, 1000)
})
onUnmounted(() => {
  disposed = true
  lifecycleAbort.abort()
  modelAbort?.abort()
  clearTimeout(pollTimer)
  clearInterval(clockTimer)
  references.value.forEach((reference) => URL.revokeObjectURL(reference.url))
})
</script>

<style scoped>
.image-workbench {
  max-width: 1320px;
  min-height: calc(100vh - 164px);
  margin: 0 auto;
  display: flex;
  flex-direction: column;
  padding-bottom: calc(88px + env(safe-area-inset-bottom));
  color: var(--md-sys-color-on-surface);
  letter-spacing: 0;
}
.workbench-heading {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  padding: 4px 0 26px;
}
.heading-title {
  display: flex;
  align-items: center;
  gap: 12px;
}
.heading-title > svg {
  color: var(--md-sys-color-primary);
}
h2 {
  font-size: 21px;
  font-weight: 600;
}
.activity {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 12px;
  white-space: nowrap;
  color: var(--md-sys-color-on-surface-variant);
}
.activity > span {
  width: 6px;
  height: 6px;
  background: var(--md-sys-color-outline);
  border-radius: 50%;
}
.activity > .busy {
  background: var(--md-sys-color-secondary);
}
.workbench-form {
  display: grid;
  grid-template-columns: minmax(0, 1.13fr) minmax(0, 1fr);
  gap: 42px;
  padding: 0 0 30px;
}
.creative-column,
.parameters-column {
  min-width: 0;
}
.creative-column {
  display: flex;
  flex-direction: column;
}
.field-title {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  font-size: 13px;
  font-weight: 600;
  margin-bottom: 12px;
}
.character-count,
.reference-counter {
  font-size: 11px;
  font-weight: 400;
  color: var(--md-sys-color-on-surface-variant);
  font-variant-numeric: tabular-nums;
}
.prompt-input {
  display: block;
  width: 100%;
  min-height: 310px;
  flex: 1;
  resize: vertical;
  padding: 22px;
  background: var(--md-sys-color-surface);
  border: 1px solid var(--md-sys-color-outline);
  border-radius: 1.5rem;
  font-size: 14px;
  line-height: 1.9;
  color: var(--md-sys-color-on-surface);
}
.prompt-input::placeholder {
  color: var(--md-sys-color-placeholder) !important;
}
.prompt-input:focus,
.custom-ratio input:focus {
  outline: 2px solid var(--md-sys-color-primary);
  outline-offset: 2px;
}
.references-title {
  margin-top: 22px;
}
.reference-strip {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 12px;
  min-height: 114px;
  padding: 13px;
  border: 1px dashed var(--md-sys-color-outline);
  border-radius: 1.5rem;
  background: var(--md-sys-color-surface);
}
.drag-active {
  background: var(--md-sys-color-surface-container);
  border-color: var(--md-sys-color-primary);
}
.reference-add,
.reference-image {
  width: 86px;
  height: 86px;
  border-radius: 1rem;
  position: relative;
  flex-shrink: 0;
}
.reference-add {
  display: grid;
  place-items: center;
  color: var(--md-sys-color-on-surface-variant);
  background: var(--md-sys-color-surface-container);
  border: 1px dashed var(--md-sys-color-outline);
}
.reference-add:hover {
  background: var(--md-sys-color-surface-container-high);
  color: var(--md-sys-color-on-surface);
}
.reference-image {
  background: var(--md-sys-color-surface);
}
.reference-image img {
  width: 100%;
  height: 100%;
  border-radius: 1rem;
  object-fit: contain;
}
.reference-image button {
  position: absolute;
  right: 3px;
  top: 3px;
  border-radius: 50%;
  width: 23px;
  height: 23px;
  display: grid;
  place-items: center;
  color: white;
  background: color-mix(in srgb, var(--md-sys-color-on-surface) 85%, transparent);
}
.parameters-column {
  display: flex;
  flex-direction: column;
  gap: 24px;
}
.key-model-fields {
  display: grid;
  grid-template-columns: minmax(0, 0.9fr) minmax(0, 1.1fr);
  gap: 16px;
}
.parameter-field {
  min-width: 0;
}
.parameter-field label,
legend {
  display: block;
  margin-bottom: 12px;
  font-size: 13px;
  font-weight: 600;
}
fieldset {
  min-width: 0;
}
.segmented {
  display: grid;
  gap: 4px;
  padding: 4px;
  background: var(--md-sys-color-surface-container);
  border-radius: 1.25rem;
}
.four {
  grid-template-columns: repeat(4, minmax(0, 1fr));
}
.five {
  grid-template-columns: repeat(5, minmax(0, 1fr));
}
.segmented button {
  min-width: 0;
  min-height: 38px;
  font-size: 13px;
  border-radius: 1rem;
  color: var(--md-sys-color-on-surface-variant);
  transition:
    background 0.15s,
    color 0.15s;
}
.segmented button:hover {
  color: var(--md-sys-color-on-surface);
  background: var(--md-sys-color-surface-container-high);
}
.segmented button.selected {
  color: var(--md-sys-color-on-surface);
  background: var(--md-sys-color-surface);
  box-shadow: 0 1px 4px rgb(63 52 32 / 0.1);
  font-weight: 600;
}
.ratio-options button {
  display: flex;
  min-height: 56px;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 6px;
  font-size: 12px;
}
.ratio-outline {
  display: block;
  box-sizing: border-box;
  border: 1px solid currentColor;
  border-radius: 2px;
  opacity: 0.7;
}
.custom-ratio {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 10px;
  margin-top: 12px;
  font-size: 12px;
  color: var(--md-sys-color-on-surface-variant);
}
.custom-toggle {
  border: 1px solid var(--md-sys-color-outline);
  border-radius: 1rem;
  min-height: 32px;
  padding: 0 13px;
  margin-right: auto;
  color: var(--md-sys-color-on-surface-variant);
}
.custom-toggle.selected {
  background: var(--md-sys-color-surface-container-high);
  border-color: var(--md-sys-color-primary);
  color: var(--md-sys-color-on-surface);
}
.custom-ratio input {
  width: 52px;
  height: 32px;
  min-width: 0;
  text-align: center;
  border: 1px solid var(--md-sys-color-outline);
  background: var(--md-sys-color-surface);
  border-radius: 1rem;
  color: var(--md-sys-color-on-surface);
  padding: 3px;
}
.custom-ratio input:disabled {
  opacity: 0.45;
}
.output-summary {
  display: flex;
  justify-content: space-between;
  font-size: 11px;
  color: var(--md-sys-color-on-surface-variant);
  margin: -7px 0 -10px;
}
.create-task {
  width: 100%;
  min-height: 48px;
  display: flex;
  justify-content: center;
  align-items: center;
  gap: 10px;
  border-radius: 1rem;
  background: var(--md-sys-color-primary);
  color: var(--md-sys-color-on-primary);
  font-size: 14px;
  font-weight: 600;
}
.create-task > svg:last-child {
  margin-left: auto;
}
.create-task > svg:first-child {
  margin-left: auto;
}
.create-task {
  padding-right: 18px;
}
.create-task:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}
.create-task:hover:not(:disabled) {
  background: color-mix(in srgb, var(--md-sys-color-primary) 88%, var(--md-sys-color-on-primary));
}
.inline-state {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  align-items: center;
  font-size: 12px;
  color: var(--md-sys-color-on-surface-variant);
  margin: -10px 0;
}
.inline-state a,
.inline-state button {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  color: var(--md-sys-color-on-surface);
  text-decoration: underline;
}
.validation-error {
  font-size: 11px;
  color: #b84646;
  margin-top: 8px;
}
.workbench-message {
  padding: 12px 16px;
  margin-bottom: 22px;
  background: var(--md-sys-color-surface-container);
  border-radius: 1rem;
  font-size: 12px;
  color: var(--md-sys-color-on-surface-variant);
  display: flex;
  align-items: center;
  gap: 10px;
}
.workbench-message > svg {
  flex-shrink: 0;
}
.message-error {
  color: #a74646;
  background: #f9eaea;
}
.workbench-message button {
  margin-left: auto;
  white-space: nowrap;
  text-decoration: underline;
}
.workbench-tabs {
  position: fixed;
  right: 16px;
  bottom: calc(20px + env(safe-area-inset-bottom));
  left: 16px;
  z-index: 20;
  display: flex;
  width: max-content;
  max-width: calc(100% - 32px);
  gap: 6px;
  padding: 6px;
  margin: 0 auto;
  background: var(--md-sys-color-surface);
  box-shadow: 0 8px 28px rgb(63 52 32 / 0.14);
  border: 1px solid color-mix(in srgb, var(--md-sys-color-outline) 45%, transparent);
  border-radius: 1.5rem;
}
.workbench-tabs button {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 9px;
  min-height: 43px;
  min-width: 112px;
  padding: 0 18px;
  border-radius: 1.125rem;
  color: var(--md-sys-color-on-surface-variant);
  font-size: 13px;
}
.workbench-tabs button.active {
  background: var(--md-sys-color-surface-container-high);
  color: var(--md-sys-color-on-surface);
  font-weight: 600;
}
.tab-count {
  background: var(--md-sys-color-primary);
  border-radius: 999px;
  color: var(--md-sys-color-on-primary);
  min-width: 17px;
  font-size: 10px;
  padding: 1px 4px;
}
.tasks-panel {
  padding-bottom: 40px;
}
.task-list {
  max-height: max(280px, calc(100dvh - 360px));
  overflow-y: auto;
  scrollbar-gutter: stable;
  padding-right: 8px;
}
.task-notice {
  display: flex;
  align-items: flex-start;
  gap: 12px;
  padding: 14px 16px;
  background: var(--md-sys-color-surface-container);
  border-radius: 1rem;
  color: var(--md-sys-color-on-surface-variant);
  font-size: 12px;
  line-height: 1.8;
}
.task-notice svg {
  flex-shrink: 0;
  margin-top: 2px;
}
.task-toolbar {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 8px;
  padding-top: 24px;
  padding-bottom: 12px;
  border-bottom: 1px solid var(--md-sys-color-outline);
}
.status-filter {
  display: flex;
  flex-wrap: wrap;
  gap: 12px;
}
.status-filter button {
  display: flex;
  gap: 7px;
  align-items: center;
  min-height: 30px;
  font-size: 12px;
  color: var(--md-sys-color-on-surface-variant);
  padding: 0 5px;
}
.status-filter button.active {
  color: var(--md-sys-color-on-surface);
  font-weight: 600;
}
.status-filter button > span {
  font-size: 10px;
  font-variant-numeric: tabular-nums;
  background: var(--md-sys-color-surface-container);
  padding: 1px 6px;
  border-radius: 999px;
}
.toolbar-refresh {
  flex-shrink: 0;
  width: 32px;
  height: 32px;
  display: grid;
  place-items: center;
  color: var(--md-sys-color-on-surface-variant);
  border-radius: 1rem;
}
.toolbar-refresh:hover {
  background: var(--md-sys-color-surface-container);
}
.empty-tasks {
  min-height: 380px;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 18px;
  color: var(--md-sys-color-on-surface-variant);
}
.empty-tasks h3 {
  font-size: 14px;
  font-weight: 400;
}
.return-workbench {
  display: inline-flex;
  gap: 6px;
  align-items: center;
  color: var(--md-sys-color-on-surface);
  font-size: 12px;
}
.guide-panel {
  max-width: 760px;
  padding: 8px 0 38px;
  margin: 0 auto;
}
.guide-panel h3 {
  font-size: 18px;
  font-weight: 600;
  margin-bottom: 24px;
}
.guide-panel li {
  display: flex;
  gap: 22px;
  padding: 23px 0;
  border-bottom: 1px solid var(--md-sys-color-outline);
}
.step-number {
  color: var(--md-sys-color-on-surface-variant);
  font-size: 16px;
  font-variant-numeric: tabular-nums;
}
.guide-panel h4 {
  font-size: 14px;
  font-weight: 600;
  margin-bottom: 9px;
}
.guide-panel p {
  color: var(--md-sys-color-on-surface-variant);
  font-size: 13px;
  line-height: 1.9;
}
.guide-limits {
  display: flex;
  flex-wrap: wrap;
  gap: 10px 24px;
  margin-top: 24px;
  color: var(--md-sys-color-on-surface-variant);
  font-size: 11px;
}
@media (min-width: 1600px) {
  .workbench-form {
    gap: 56px;
  }
  .prompt-input {
    min-height: 390px;
  }
}
@media (min-width: 1024px) {
  .workbench-tabs {
    left: calc(var(--workbench-sidebar-width) + 16px);
    max-width: calc(100% - var(--workbench-sidebar-width) - 32px);
  }
}
@media (max-width: 1100px) {
  .workbench-form {
    gap: 24px;
  }
  .key-model-fields {
    grid-template-columns: minmax(0, 1fr);
    gap: 18px;
  }
  .parameters-column {
    gap: 20px;
  }
  .workbench-heading {
    padding-bottom: 22px;
  }
}
@media (max-width: 767px) {
  .image-workbench {
    min-height: calc(100vh - 135px);
  }
  .workbench-form {
    grid-template-columns: minmax(0, 1fr);
    gap: 26px;
  }
  .workbench-heading {
    padding-top: 10px;
    padding-bottom: 22px;
  }
  h2 {
    font-size: 18px;
  }
  .heading-title {
    gap: 8px;
  }
  .heading-title > svg {
    width: 20px;
  }
  .activity {
    font-size: 11px;
    gap: 5px;
  }
  .prompt-input {
    min-height: 245px;
    padding: 16px;
  }
  .key-model-fields {
    grid-template-columns: minmax(0, 0.9fr) minmax(0, 1.1fr);
    gap: 12px;
  }
  .parameters-column {
    gap: 24px;
  }
  .workbench-tabs {
    gap: 3px;
    padding: 5px;
  }
  .workbench-tabs button {
    min-width: 0;
    min-height: 42px;
    padding: 0 13px;
    gap: 7px;
    font-size: 12px;
  }
  .reference-strip {
    min-height: 100px;
  }
  .reference-add,
  .reference-image {
    height: 74px;
    width: 74px;
  }
  .status-filter {
    gap: 6px;
  }
  .status-filter button {
    font-size: 11px;
    gap: 4px;
  }
  .status-filter button > span {
    padding: 1px 4px;
  }
  .task-notice {
    padding: 12px;
  }
  .task-toolbar {
    padding-top: 18px;
  }
  .guide-panel li {
    gap: 16px;
  }
}
@media (prefers-reduced-motion: reduce) {
  .segmented button {
    transition: none;
  }
}
</style>
