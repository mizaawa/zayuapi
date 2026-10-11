<template>
  <AppLayout>
    <section class="video-workbench" :style="{ '--sidebar-width': appStore.sidebarCollapsed ? '84px' : '256px' }" @paste="onPaste">
      <header class="workbench-heading">
        <div class="heading-title"><Icon name="play" size="lg" /><h2>{{ t('videoWorkbench.title') }}</h2></div>
        <span class="activity"><span :class="{ busy: activeCount > 0 }"></span>{{ t('videoWorkbench.activeCount', { count: activeCount, max: settings.max_concurrent }) }}</span>
      </header>
      <p v-if="settings.announcement" class="announcement" role="status"><Icon name="infoCircle" size="sm" /><span>{{ settings.announcement }}</span></p>
      <div v-if="tasksError" class="workbench-message error" role="alert">
        <Icon name="exclamationCircle" size="sm" /><span>{{ t('videoWorkbench.tasksError') }}</span>
        <button type="button" @click="refreshTasks">{{ t('videoWorkbench.retry') }}</button>
      </div>
      <p v-else-if="loaded && !settings.enabled" class="workbench-message">{{ t('videoWorkbench.unavailable') }}</p>

      <form v-show="tab === 'workbench'" id="video-workbench-panel" class="workbench-form" role="tabpanel" aria-labelledby="video-workbench-tab" @submit.prevent="submit">
        <div class="creative-column">
          <div class="field-title"><label for="video-prompt">{{ t('videoWorkbench.prompt') }}</label><span>{{ form.prompt.length.toLocaleString() }} / 32,000</span></div>
          <textarea id="video-prompt" v-model="form.prompt" maxlength="32000" :placeholder="t('videoWorkbench.promptPlaceholder')" class="prompt-input" spellcheck="false"></textarea>
          <div class="key-model-fields">
            <div class="parameter-field">
              <label for="video-key">{{ t('videoWorkbench.apiKey') }}</label>
              <Select id="video-key" v-model="selectedKeyId" :options="keyOptions" :aria-label="t('videoWorkbench.apiKey')" :placeholder="loadingKeys ? t('common.loading') : t('videoWorkbench.chooseKey')" :disabled="loadingKeys || submitting" />
            </div>
            <div class="parameter-field">
              <label for="video-model">{{ t('videoWorkbench.model') }}</label>
              <Select id="video-model" v-model="form.model" :options="modelOptions" :aria-label="t('videoWorkbench.model')" :placeholder="loadingModels ? t('common.loading') : t('videoWorkbench.chooseModel')" :disabled="!selectedKey || loadingModels || submitting" />
            </div>
          </div>
          <div v-if="keysError || (!loadingKeys && !keys.length)" class="inline-state">
            <span>{{ t(keysError ? 'videoWorkbench.keysError' : 'videoWorkbench.noKeys') }}</span>
            <button v-if="keysError" type="button" @click="loadKeys">{{ t('videoWorkbench.retry') }}</button>
            <router-link v-else to="/keys">{{ t('videoWorkbench.manageKeys') }}<Icon name="arrowRight" size="xs" /></router-link>
          </div>
          <div v-else-if="modelsError || (selectedKey && !loadingModels && !models.length)" class="inline-state">
            <span>{{ t(modelsError ? 'videoWorkbench.modelsError' : 'videoWorkbench.noModels') }}</span>
            <button type="button" @click="loadModels">{{ t('videoWorkbench.retry') }}</button>
          </div>
        </div>

        <div class="parameters-column">
          <div v-for="kind in mediaKinds" :key="kind.id" class="reference-field">
            <div class="field-title"><span>{{ t(`videoWorkbench.${kind.id}`) }}</span><span>{{ references[kind.id].length }} / {{ referenceLimit(kind.id) }}</span></div>
            <div class="reference-strip" :data-reference-kind="kind.id" @dragover.prevent @drop.prevent="kind.id === 'images' && onImageDrop($event)">
              <div v-for="(reference, index) in references[kind.id]" :key="reference.id" class="reference-item" :title="reference.file.name">
                <img v-if="kind.id === 'images'" :src="reference.url" :alt="reference.file.name" />
                <template v-else><Icon :name="kind.id === 'videos' ? 'play' : 'document'" size="lg" /><span>{{ reference.file.name }}</span></template>
                <button type="button" :title="t('videoWorkbench.removeReference')" :aria-label="t('videoWorkbench.removeReference')" @click="removeReference(kind.id, index)"><Icon name="x" size="xs" /></button>
              </div>
              <button v-if="references[kind.id].length < referenceLimit(kind.id)" class="reference-add" type="button" :aria-label="t(`videoWorkbench.${kind.addLabel}`)" :title="t(`videoWorkbench.${kind.addLabel}`)" :disabled="submitting" @click="fileInputs[kind.id]?.click()"><Icon name="plus" size="lg" /></button>
              <input :ref="element => setFileInput(kind.id, element)" type="file" multiple :accept="kind.accept" class="sr-only" tabindex="-1" :aria-label="t(`videoWorkbench.${kind.addLabel}`)" @change="onFileChange(kind.id, $event)" />
            </div>
            <p v-if="references[kind.id].length > referenceLimit(kind.id)" class="validation-error">{{ t('videoWorkbench.uploadLimit', { max: referenceLimit(kind.id) }) }}</p>
            <p v-if="kind.id === 'images' && modelLimits?.requiresImage && !references.images.length" class="validation-error">{{ t('videoWorkbench.referenceImageRequired') }}</p>
          </div>
          <fieldset class="parameter-field">
            <legend>{{ t('videoWorkbench.aspectRatio') }}</legend>
            <div class="segmented five ratio-options">
              <button v-for="ratio in ratios" :key="ratio" type="button" :class="{ selected: form.ratio === ratio }" :aria-pressed="form.ratio === ratio" @click="form.ratio = ratio"><span class="ratio-outline" :style="ratioStyle(ratio)"></span>{{ ratio }}</button>
            </div>
            <div class="custom-ratio">
              <button type="button" :class="{ selected: form.ratio === 'custom' }" :aria-pressed="form.ratio === 'custom'" @click="form.ratio = form.ratio === 'custom' ? '16:9' : 'custom'">{{ t('videoWorkbench.custom') }}</button>
              <label for="video-width">{{ t('videoWorkbench.width') }}</label><input id="video-width" v-model.number="form.width" type="number" min="1" max="10000" step="1" :disabled="form.ratio !== 'custom'" />
              <span>:</span><label for="video-height">{{ t('videoWorkbench.height') }}</label><input id="video-height" v-model.number="form.height" type="number" min="1" max="10000" step="1" :disabled="form.ratio !== 'custom'" />
            </div>
            <p v-if="!validRatio" class="validation-error">{{ t('videoWorkbench.invalidRatio') }}</p>
            <p v-else-if="!validModelRatio" class="validation-error">{{ t('videoWorkbench.invalidModelRatio', { ratios: modelLimits?.ratios?.join(', ') }) }}</p>
          </fieldset>
          <fieldset class="parameter-field">
            <legend>{{ t('videoWorkbench.duration') }}</legend>
            <div class="segmented duration-options" :style="{ gridTemplateColumns: `repeat(${durations.length}, minmax(0, 1fr))` }">
              <button v-for="duration in durations" :key="duration" type="button" :class="{ selected: form.duration === duration }" :aria-pressed="form.duration === duration" @click="form.duration = duration">{{ duration === 'custom' ? t('videoWorkbench.custom') : `${duration}s` }}</button>
            </div>
            <div v-if="form.duration === 'custom'" class="custom-duration">
              <label class="sr-only" for="video-custom-duration">{{ t('videoWorkbench.customDuration') }}</label>
              <input id="video-custom-duration" v-model.number="form.customDuration" type="number" :min="modelLimits?.minDuration ?? 1" :max="modelLimits?.maxDuration ?? 86400" step="1" :placeholder="t('videoWorkbench.customDurationPlaceholder')" />
              <span>s</span>
            </div>
            <p v-if="form.duration === 'custom' && form.customDuration !== '' && !validDuration" class="validation-error">{{ modelLimits ? t(modelLimits.fixedDurations ? 'videoWorkbench.fixedModelDuration' : 'videoWorkbench.modelDuration', { min: modelLimits.minDuration, max: modelLimits.maxDuration, durations: modelLimits.durations.join(', ') }) : t('videoWorkbench.invalidDuration') }}</p>
          </fieldset>
          <button class="create-task" type="submit" :disabled="!canSubmit"><Icon :name="submitting ? 'refresh' : 'sparkles'" size="sm" :class="{ 'animate-spin': submitting }" />{{ t(submitting ? 'videoWorkbench.submitting' : 'videoWorkbench.create') }}<Icon name="arrowRight" size="sm" /></button>
        </div>
      </form>

      <div v-show="tab === 'tasks'" id="video-tasks-panel" class="tasks-panel" role="tabpanel" aria-labelledby="video-tasks-tab">
        <div class="task-notice"><Icon name="cloud" size="md" /><p>{{ t('videoWorkbench.retention', { minutes: settings.retention_minutes }) }}</p></div>
        <div class="task-toolbar">
          <div class="status-filter" :aria-label="t('videoWorkbench.tasks')"><button v-for="status in statuses" :key="status" type="button" :class="{ active: filter === status }" :aria-pressed="filter === status" @click="filter = status">{{ t(`videoWorkbench.${status}`) }}<span>{{ statusCount(status) }}</span></button></div>
          <button type="button" class="icon-button" :title="t('videoWorkbench.refresh')" :aria-label="t('videoWorkbench.refresh')" :disabled="loadingTasks || manualRefreshing" @click="refreshManually"><Icon name="refresh" size="sm" :class="{ 'animate-spin': loadingTasks || manualRefreshing }" /></button>
        </div>
        <div v-if="!visibleTasks.length" class="empty-tasks"><Icon name="play" size="xl" /><h3>{{ t(tasks.length ? 'videoWorkbench.emptyFiltered' : 'videoWorkbench.emptyTasks') }}</h3></div>
        <div v-else ref="taskList" class="task-list"><VideoWorkbenchTaskRow v-for="task in pagedTasks" :key="task.id" :task="task" :now="now" :key-name="keys.find(key => key.id === task.workbench.api_key_id)?.name" :downloading="downloading.has(task.id)" @download="download(task)" @delete="deleteTarget = task" /></div>
        <Pagination v-if="visibleTasks.length" v-model:page="taskPage" :total="visibleTasks.length" :page-size="TASKS_PER_PAGE" :show-page-size-selector="false" class="task-pagination" />
      </div>
      <nav class="workbench-tabs" :aria-label="t('videoWorkbench.title')">
        <div class="view-tabs" role="tablist">
          <button v-for="item in tabs" :id="`video-${item.id}-tab`" :key="item.id" type="button" role="tab" :aria-selected="tab === item.id" :aria-controls="`video-${item.id}-panel`" :tabindex="tab === item.id ? 0 : -1" :class="{ active: tab === item.id }" @click="tab = item.id" @keydown="onTabKeydown"><Icon :name="item.icon" size="sm" />{{ t(`videoWorkbench.${item.id}`) }}<span v-if="item.id === 'tasks' && unfinishedCount" class="tab-count">{{ unfinishedCount }}</span></button>
        </div>
        <a v-if="tutorialURL" :href="tutorialURL" target="_blank" rel="noopener noreferrer"><Icon name="book" size="sm" />{{ t('videoWorkbench.guide') }}</a>
        <a v-if="documentationURL" :href="documentationURL" target="_blank" rel="noopener noreferrer"><Icon name="link" size="sm" />{{ t('videoWorkbench.documentation') }}</a>
      </nav>
    </section>
    <BaseDialog :show="!!deleteTarget" :title="t('videoWorkbench.deleteTitle')" width="narrow" @close="!deleting && (deleteTarget = null)">
      <p>{{ t('videoWorkbench.deleteMessage') }}</p>
      <template #footer><button type="button" class="btn btn-secondary" :disabled="deleting" @click="deleteTarget = null">{{ t('common.cancel') }}</button><button type="button" class="btn btn-danger" :disabled="deleting" @click="deleteTask">{{ t('common.delete') }}</button></template>
    </BaseDialog>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, reactive, ref, watch, type ComponentPublicInstance } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import Icon from '@/components/icons/Icon.vue'
import Select from '@/components/common/Select.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Pagination from '@/components/common/Pagination.vue'
import VideoWorkbenchTaskRow from '@/components/user/VideoWorkbenchTaskRow.vue'
import { keysAPI } from '@/api/keys'
import { saveBlob } from '@/api/batchImage'
import { listVideoTasks, listVideoModels, submitVideoTask, downloadVideoTask, deleteVideoTask, safeWorkbenchLink, miniMaxVideoModelLimits, type VideoTask, type VideoWorkbenchSettings, type VideoPayload } from '@/api/videoWorkbench'
import { useAuthStore } from '@/stores/auth'
import { useAppStore } from '@/stores/app'
import type { ApiKey } from '@/types'

type MediaKind = 'images' | 'videos' | 'audios'
interface Reference { id: string; file: File; url: string }
const { t } = useI18n()
const authStore = useAuthStore(), appStore = useAppStore()
const tab = ref<'workbench' | 'tasks'>('workbench')
const tabs = [{ id: 'workbench', icon: 'sparkles' }, { id: 'tasks', icon: 'clipboard' }] as const
const statuses = ['all', 'queued', 'processing', 'completed', 'failed'] as const
const ratios = ['1:1', '4:3', '3:4', '16:9', '9:16']
const mediaKinds = [
  { id: 'images', addLabel: 'addImages', accept: 'image/png,image/jpeg,image/webp' },
  { id: 'videos', addLabel: 'addVideos', accept: 'video/mp4,video/webm,video/quicktime' },
  { id: 'audios', addLabel: 'addAudios', accept: 'audio/mpeg,audio/mp3,audio/wav,audio/x-wav,audio/ogg,audio/mp4,audio/aac,audio/x-m4a,audio/webm,audio/flac,audio/x-flac' }
] as const
const initialForm = { prompt: '', model: '', ratio: '16:9', width: 16, height: 9, duration: 5 as number | 'custom', customDuration: '' as number | '' }
const form = reactive({ ...initialForm })
const modelLimits = computed(() => miniMaxVideoModelLimits(form.model))
const durations = computed(() => [...(modelLimits.value?.durations ?? [5, 10, 30, 60]), 'custom'] as (number | 'custom')[])
const references = reactive<Record<MediaKind, Reference[]>>({ images: [], videos: [], audios: [] })
const fileInputs: Partial<Record<MediaKind, HTMLInputElement>> = {}
const keys = ref<ApiKey[]>([]), models = ref<{ id: string }[]>([])
const selectedKeyId = ref<number | null>(null)
const loadingKeys = ref(true), loadingModels = ref(false), loadingTasks = ref(false)
const manualRefreshing = ref(false)
const keysError = ref(false), modelsError = ref(false), tasksError = ref(false), loaded = ref(false)
const submitting = ref(false), deleting = ref(false)
const tasks = ref<VideoTask[]>([]), acceptedTasks = new Map<string, VideoTask>()
const deleteTarget = ref<VideoTask | null>(null)
const downloading = reactive(new Set<string>())
const filter = ref<(typeof statuses)[number]>('all')
const TASKS_PER_PAGE = 20
const taskPage = ref(1)
const taskList = ref<HTMLElement | null>(null)
const now = ref(Math.floor(Date.now() / 1000))
const settings = reactive<VideoWorkbenchSettings>({ enabled: false, max_concurrent: 3, max_reference_images: 6, max_reference_videos: 6, max_reference_audios: 6, retention_minutes: 30, tutorial_url: '', documentation_url: '', announcement: '' })
const tutorialURL = computed(() => safeWorkbenchLink(settings.tutorial_url))
const documentationURL = computed(() => safeWorkbenchLink(settings.documentation_url))
let disposed = false, pollTimer: ReturnType<typeof setTimeout> | undefined, clockTimer: ReturnType<typeof setInterval> | undefined
let refreshAnimationTimer: ReturnType<typeof setTimeout> | undefined
let modelAbort: AbortController | undefined
const lifecycle = new AbortController()
const selectedKey = computed(() => keys.value.find(key => key.id === selectedKeyId.value))
const keyOptions = computed(() => keys.value.map(key => ({ value: key.id, label: key.name })))
const modelOptions = computed(() => models.value.map(model => ({ value: model.id, label: model.id })))
const activeCount = computed(() => statusCount('processing'))
const unfinishedCount = computed(() => statusCount('processing') + statusCount('queued'))
const visibleTasks = computed(() => tasks.value.filter(task => task.expires_at > now.value && (filter.value === 'all' || task.status === filter.value)))
const taskPageCount = computed(() => Math.max(1, Math.ceil(visibleTasks.value.length / TASKS_PER_PAGE)))
const pagedTasks = computed(() => visibleTasks.value.slice((taskPage.value - 1) * TASKS_PER_PAGE, taskPage.value * TASKS_PER_PAGE))
watch(filter, () => { taskPage.value = 1 })
watch(taskPageCount, (pages) => { taskPage.value = Math.min(taskPage.value, pages) })
watch(taskPage, () => { if (taskList.value) taskList.value.scrollTop = 0 }, { flush: 'post' })
const seconds = computed(() => form.duration === 'custom' ? Number(form.customDuration) : form.duration)
const validDuration = computed(() => Number.isSafeInteger(seconds.value) && seconds.value >= (modelLimits.value?.minDuration ?? 1) && seconds.value <= (modelLimits.value?.maxDuration ?? 86400) && (!modelLimits.value?.fixedDurations || modelLimits.value.durations.includes(seconds.value)))
const validRatio = computed(() => form.ratio !== 'custom' || [form.width, form.height].every(n => Number.isSafeInteger(n) && n >= 1 && n <= 10000))
const validModelRatio = computed(() => !modelLimits.value?.ratios || modelLimits.value.ratios.includes(form.ratio === 'custom' ? `${form.width}:${form.height}` : form.ratio))
const canSubmit = computed(() => loaded.value && settings.enabled && !tasksError.value && !!selectedKey.value && models.value.some(model => model.id === form.model) && !!form.prompt.trim() && validDuration.value && validRatio.value && validModelRatio.value && (!modelLimits.value?.requiresImage || references.images.length > 0) && !loadingModels.value && !submitting.value && mediaKinds.every(kind => references[kind.id].length <= referenceLimit(kind.id)))
watch(() => form.model, () => {
  if (modelLimits.value && !validDuration.value) form.duration = modelLimits.value.defaultDuration
  else if (form.duration !== 'custom' && !durations.value.includes(form.duration)) {
    form.customDuration = form.duration
    form.duration = 'custom'
  }
})
function referenceLimit(kind: MediaKind) { return Math.min(settings[`max_reference_${kind}`], modelLimits.value?.[kind] ?? Infinity) }
function statusCount(status: string) { return tasks.value.filter(task => task.expires_at > now.value && (status === 'all' || status === task.status)).length }
function ratioStyle(ratio: string) { const [w = 1, h = 1] = ratio.split(':').map(Number); return { width: `${18 * w / Math.max(w, h)}px`, height: `${18 * h / Math.max(w, h)}px` } }
function setFileInput(kind: MediaKind, element: Element | ComponentPublicInstance | null) { if (element instanceof HTMLInputElement) fileInputs[kind] = element }

async function loadKeys() {
  loadingKeys.value = true; keysError.value = false
  try {
    const available: ApiKey[] = []
    for (let page = 1; ; page++) {
      const result = await keysAPI.list(page, 100, { status: 'active' }, { signal: lifecycle.signal })
      available.push(...result.items.filter(key => key.status === 'active' && (!key.expires_at || Date.parse(key.expires_at) > Date.now()) && ['custom', 'composite'].includes(key.group?.platform || '') && key.group?.video_workbench_enabled === true))
      if (!result.items.length || page >= result.pages) break
    }
    if (disposed) return
    keys.value = available
    if (!available.some(key => key.id === selectedKeyId.value)) selectedKeyId.value = available[0]?.id ?? null
  } catch { if (!disposed) keysError.value = true }
  finally { if (!disposed) loadingKeys.value = false }
}
async function loadModels() {
  modelAbort?.abort()
  const controller = new AbortController(); modelAbort = controller
  const key = selectedKey.value, previous = form.model
  form.model = ''; models.value = []; modelsError.value = false; loadingModels.value = !!key
  if (!key) return
  try {
    const result = await listVideoModels(key.key, authStore.user?.email || '', controller.signal)
    if (disposed || controller.signal.aborted) return
    models.value = result; form.model = result.find(model => model.id === previous)?.id || result[0]?.id || ''
  } catch { if (!disposed && !controller.signal.aborted) modelsError.value = true }
  finally { if (!disposed && !controller.signal.aborted) loadingModels.value = false }
}
watch(selectedKeyId, loadModels)
async function refreshTasks() {
  if (disposed || loadingTasks.value) return
  clearTimeout(pollTimer); loadingTasks.value = true
  try {
    const result = await listVideoTasks(lifecycle.signal)
    if (disposed) return
    const ids = new Set(result.tasks.map(task => task.id))
    for (const [id, task] of acceptedTasks) if (ids.has(id) || task.expires_at <= now.value) acceptedTasks.delete(id)
    tasks.value = [...result.tasks, ...acceptedTasks.values()].sort((a, b) => b.created_at - a.created_at)
    Object.assign(settings, result.settings); loaded.value = true; tasksError.value = false
  } catch { if (!disposed) tasksError.value = true }
  finally { if (!disposed) { loadingTasks.value = false; pollTimer = setTimeout(refreshTasks, unfinishedCount.value ? 3000 : 5000) } }
}
function refreshManually() {
  if (loadingTasks.value || manualRefreshing.value) return
  manualRefreshing.value = true
  refreshAnimationTimer = setTimeout(() => { manualRefreshing.value = false }, 1000)
  void refreshTasks()
}
function addFiles(kind: MediaKind, files: File[]) {
  if (submitting.value) return
  const accept = mediaKinds.find(item => item.id === kind)!.accept.split(',')
  for (const file of files) {
    if (references[kind].length >= referenceLimit(kind)) { appStore.showError(t('videoWorkbench.uploadLimit', { max: referenceLimit(kind) })); break }
    if (!accept.includes(file.type) || !file.size) { appStore.showError(t('videoWorkbench.uploadType')); continue }
    const total = Object.values(references).flat().reduce((sum, ref) => sum + ref.file.size, 0)
    if (total + file.size > 47 * 1024 * 1024) { appStore.showError(t('videoWorkbench.uploadSize')); break }
    references[kind].push({ id: crypto.randomUUID(), file, url: kind === 'images' ? URL.createObjectURL(file) : '' })
  }
}
function onFileChange(kind: MediaKind, event: Event) { const input = event.target as HTMLInputElement; addFiles(kind, Array.from(input.files || [])); input.value = '' }
function onImageDrop(event: DragEvent) { addFiles('images', Array.from(event.dataTransfer?.files || [])) }
function onPaste(event: ClipboardEvent) {
  const files = Array.from(event.clipboardData?.items || []).filter(item => item.kind === 'file' && item.type.startsWith('image/')).map(item => item.getAsFile()).filter((file): file is File => !!file)
  if (files.length) { event.preventDefault(); addFiles('images', files) }
}
function removeReference(kind: MediaKind, index: number) { references[kind].splice(index, 1).forEach(ref => { if (ref.url) URL.revokeObjectURL(ref.url) }) }
function readReference(file: File): Promise<string> {
  return new Promise((resolve, reject) => { const reader = new FileReader(); reader.onload = () => resolve(String(reader.result)); reader.onerror = () => reject(new Error(t('videoWorkbench.uploadRead'))); reader.readAsDataURL(file) })
}
async function submit() {
  if (!canSubmit.value || !selectedKey.value) return
  submitting.value = true
  const key = selectedKey.value.key, email = authStore.user?.email || ''
  const draft = { ...form }
  const payload: VideoPayload = { model: form.model, prompt: form.prompt.trim(), ratio: form.ratio === 'custom' ? `${form.width}:${form.height}` : form.ratio, duration: seconds.value }
  const snapshot = { images: [...references.images], videos: [...references.videos], audios: [...references.audios] }
  try {
    for (const [kind, field] of [['images', 'referenceImages'], ['videos', 'referenceVideos'], ['audios', 'referenceAudios']] as const) {
      if (snapshot[kind].length) payload[field] = await Promise.all(snapshot[kind].map(ref => readReference(ref.file)))
    }
    const result = await submitVideoTask(key, email, payload)
    if (disposed) return
    acceptedTasks.set(result.id, result)
    if (!tasks.value.some(task => task.id === result.id)) tasks.value.unshift(result)
    if (Object.entries(draft).every(([field, value]) => form[field as keyof typeof form] === value) &&
      mediaKinds.every(({ id }) => references[id].length === snapshot[id].length && references[id].every((reference, index) => reference.id === snapshot[id][index]?.id))) {
      Object.assign(form, initialForm, { model: draft.model, duration: modelLimits.value?.defaultDuration ?? initialForm.duration })
      for (const { id } of mediaKinds) {
        references[id].forEach(reference => { if (reference.url) URL.revokeObjectURL(reference.url) })
        references[id] = []
      }
    }
    appStore.showSuccess(t('videoWorkbench.created')); void refreshTasks()
  } catch { if (!disposed) { appStore.showError(t('videoWorkbench.submitError')); void refreshTasks() } }
  finally { if (!disposed) submitting.value = false }
}
async function download(task: VideoTask) {
  if (downloading.has(task.id)) return
  downloading.add(task.id)
  try { const blob = await downloadVideoTask(task.id, lifecycle.signal); const ext = task.content_type === 'video/webm' ? 'webm' : task.content_type === 'video/quicktime' ? 'mov' : 'mp4'; if (!disposed) saveBlob(blob, `video-${task.id.slice(-8)}.${ext}`) }
  catch { if (!disposed) appStore.showError(t('videoWorkbench.downloadError')) }
  finally { downloading.delete(task.id) }
}
async function deleteTask() {
  const target = deleteTarget.value
  if (!target || deleting.value) return
  deleting.value = true
  try { await deleteVideoTask(target.id); acceptedTasks.delete(target.id); tasks.value = tasks.value.filter(task => task.id !== target.id); deleteTarget.value = null; void refreshTasks() }
  catch { appStore.showError(t('videoWorkbench.deleteError')) }
  finally { deleting.value = false }
}
async function onTabKeydown(event: KeyboardEvent) {
  if (!['ArrowLeft', 'ArrowRight', 'Home', 'End'].includes(event.key)) return
  event.preventDefault(); tab.value = event.key === 'Home' ? 'workbench' : event.key === 'End' ? 'tasks' : tab.value === 'workbench' ? 'tasks' : 'workbench'
  await nextTick(); document.getElementById(`video-${tab.value}-tab`)?.focus()
}
onMounted(() => { void loadKeys(); void refreshTasks(); clockTimer = setInterval(() => { now.value = Math.floor(Date.now() / 1000) }, 1000) })
onUnmounted(() => { disposed = true; lifecycle.abort(); modelAbort?.abort(); clearTimeout(pollTimer); clearInterval(clockTimer); clearTimeout(refreshAnimationTimer); references.images.forEach(ref => URL.revokeObjectURL(ref.url)) })
</script>

<style scoped>
.video-workbench { display: flex; flex-direction: column; max-width: 1320px; min-height: calc(100vh - 164px); margin: 0 auto; padding-bottom: calc(88px + env(safe-area-inset-bottom)); color: var(--md-sys-color-on-surface); letter-spacing: 0; }
.workbench-heading,.heading-title,.activity { display: flex; align-items: center; }
.workbench-heading { flex-shrink: 0; justify-content: space-between; gap: 16px; padding: 4px 0 26px; }
.heading-title { gap: 12px; } .heading-title svg { color: var(--md-sys-color-primary); }
h2 { font-size: 21px; font-weight: 600; } .activity { gap: 8px; font-size: 12px; color: var(--md-sys-color-on-surface-variant); white-space: nowrap; }
.activity > span { width: 6px; height: 6px; border-radius: 50%; background: var(--md-sys-color-outline); } .activity .busy { background: var(--md-sys-color-secondary); }
.announcement,.workbench-message { display: flex; gap: 10px; align-items: flex-start; padding: 12px 0; margin-bottom: 18px; font-size: 13px; line-height: 1.7; border-bottom: 1px solid var(--md-sys-color-outline-variant); }
.announcement { flex-shrink: 0; max-height: 120px; overflow-y: auto; } .announcement span { white-space: pre-wrap; overflow-wrap: anywhere; } .announcement svg { flex-shrink: 0; margin-top: 3px; } .error { color: var(--md-sys-color-error, #b3261e); } .workbench-message button { margin-left: auto; text-decoration: underline; }
.workbench-form { display: grid; padding: 0 0 30px; grid-template-columns: minmax(0,1.13fr) minmax(0,1fr); gap: 42px; }
.creative-column,.parameters-column { min-width: 0; display: flex; flex-direction: column; } .parameters-column { gap: 16px; }
.field-title { display: flex; justify-content: space-between; gap: 12px; align-items: center; margin-bottom: 12px; font-size: 13px; font-weight: 600; }
.field-title > span:last-child { font-size: 11px; font-weight: 400; color: var(--md-sys-color-on-surface-variant); }
.prompt-input { display: block; resize: none; aspect-ratio: 6 / 5; min-height: 0; flex: none; overflow-y: auto; width: 100%; padding: 22px; border: 1px solid var(--md-sys-color-outline); background: var(--md-sys-color-surface); border-radius: 24px; font-size: 14px; line-height: 1.9; }
.prompt-input:focus { outline: 2px solid var(--md-sys-color-primary); outline-offset: 2px; } .prompt-input::placeholder { color: var(--md-sys-color-on-surface-variant); }
.key-model-fields { display: grid; grid-template-columns: minmax(0,.9fr) minmax(0,1.1fr); gap: 16px; margin-top: 25px; }
.parameter-field { min-width: 0; border: 0; padding: 0; } .parameter-field > label,.parameter-field > legend { display: block; margin-bottom: 12px; font-size: 13px; font-weight: 600; }
.reference-strip { display: flex; flex-wrap: wrap; align-items: center; gap: 10px; min-height: 78px; padding: 7px; border: 1px dashed var(--md-sys-color-outline); border-radius: 18px; background: var(--md-sys-color-surface); }
.reference-add,.reference-item { width: 62px; height: 62px; flex: 0 0 62px; border-radius: 8px; }
.reference-add { display: grid; place-items: center; border: 1px dashed var(--md-sys-color-outline); background: var(--md-sys-color-surface-container); color: var(--md-sys-color-on-surface-variant); }
.reference-add:hover { background: var(--md-sys-color-surface-container-high); }
.reference-item { position: relative; display: flex; flex-direction: column; justify-content: center; align-items: center; gap: 7px; background: var(--md-sys-color-surface-container); }
.reference-item > img { width: 100%; height: 100%; object-fit: cover; border-radius: 8px; }
.reference-item > span { max-width: 100%; padding: 0 5px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-size: 10px; }
.reference-item > button { position: absolute; top: -5px; right: -5px; display: grid; place-items: center; width: 22px; height: 22px; background: var(--md-sys-color-surface); border: 1px solid var(--md-sys-color-outline); border-radius: 50%; }
.segmented { display: grid; padding: 4px; gap: 4px; border-radius: 19px; background: var(--md-sys-color-surface-container); } .five { grid-template-columns: repeat(5,minmax(0,1fr)); }
.segmented button { min-width: 0; min-height: 38px; font-size: 13px; border-radius: 16px; color: var(--md-sys-color-on-surface-variant); }
.segmented button.selected { background: var(--md-sys-color-surface); color: var(--md-sys-color-on-surface); box-shadow: 0 1px 4px rgb(0 0 0 / .08); font-weight: 600; }
.ratio-options button { display: flex; flex-direction: column; align-items: center; justify-content: center; gap: 6px; min-height: 56px; font-size: 12px; }
.ratio-outline { border: 1px solid currentColor; border-radius: 2px; opacity: .7; }
.custom-ratio { display: flex; flex-wrap: wrap; align-items: center; gap: 10px; margin-top: 12px; font-size: 12px; color: var(--md-sys-color-on-surface-variant); }
.custom-ratio > button { margin-right: auto; padding: 0 13px; min-height: 32px; border: 1px solid var(--md-sys-color-outline); border-radius: 16px; } .custom-ratio > button.selected { background: var(--md-sys-color-surface-container); }
.custom-ratio input { width: 55px; height: 32px; min-width: 0; text-align: center; border: 1px solid var(--md-sys-color-outline); background: var(--md-sys-color-surface); border-radius: 16px; padding: 3px; }
.custom-ratio input:disabled { opacity: .45; }
.custom-duration { display: flex; align-items: center; gap: 10px; margin-top: 12px; } .custom-duration input { min-width: 0; width: 100%; min-height: 42px; padding: 9px 12px; font-size: 13px; background: var(--md-sys-color-surface); border: 1px solid var(--md-sys-color-outline); border-radius: 8px; }
.create-task { display: flex; align-items: center; justify-content: center; gap: 10px; width: 100%; min-height: 48px; padding: 10px 18px; border-radius: 16px; background: var(--md-sys-color-primary); color: var(--md-sys-color-on-primary); font-size: 14px; font-weight: 600; }
.create-task > svg:first-child { margin-left: auto; } .create-task > svg:last-child { margin-left: auto; } .create-task:disabled { opacity: .5; cursor: not-allowed; }
.validation-error { margin-top: 8px; font-size: 12px; color: var(--md-sys-color-error, #b3261e); }
.inline-state { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; margin-top: 14px; font-size: 12px; color: var(--md-sys-color-on-surface-variant); } .inline-state a,.inline-state button { display: inline-flex; align-items: center; gap: 4px; text-decoration: underline; }
.workbench-tabs { position: fixed; bottom: calc(20px + env(safe-area-inset-bottom)); left: 16px; right: 16px; display: flex; justify-content: center; width: max-content; max-width: calc(100% - 32px); margin: 0 auto; gap: 6px; padding: 6px; z-index: 20; border-radius: 24px; background: var(--md-sys-color-surface); border: 1px solid color-mix(in srgb, var(--md-sys-color-outline) 45%, transparent); box-shadow: 0 8px 28px rgb(63 52 32 / .14); }
.view-tabs { display: flex; gap: 5px; min-width: 0; } .workbench-tabs button,.workbench-tabs a { position: relative; display: flex; align-items: center; justify-content: center; gap: 8px; min-height: 43px; padding: 0 18px; min-width: 0; border-radius: 18px; font-size: 13px; color: var(--md-sys-color-on-surface-variant); white-space: nowrap; }
.workbench-tabs button.active { background: var(--md-sys-color-surface-container-high); color: var(--md-sys-color-on-surface); font-weight: 600; } .tab-count { min-width: 16px; padding: 1px 4px; border-radius: 20px; font-size: 10px; background: var(--md-sys-color-primary); color: var(--md-sys-color-on-primary); }
.task-notice { display: flex; gap: 12px; align-items: flex-start; padding: 14px 16px; background: var(--md-sys-color-surface-container); border-radius: 1rem; font-size: 12px; line-height: 1.8; color: var(--md-sys-color-on-surface-variant); }
.task-notice svg { flex-shrink: 0; margin-top: 2px; }
.task-toolbar { display: flex; justify-content: space-between; align-items: center; gap: 8px; padding: 22px 0 12px; border-bottom: 1px solid var(--md-sys-color-outline); }
.status-filter { display: flex; flex-wrap: wrap; gap: 12px; } .status-filter button { display: flex; align-items: center; gap: 7px; min-height: 30px; font-size: 12px; color: var(--md-sys-color-on-surface-variant); }
.status-filter button.active { color: var(--md-sys-color-on-surface); font-weight: 600; } .status-filter button > span { font-size: 10px; padding: 1px 6px; border-radius: 20px; background: var(--md-sys-color-surface-container); font-variant-numeric: tabular-nums; }
.icon-button { display: grid; place-items: center; flex-shrink: 0; width: 32px; height: 32px; border-radius: 8px; } .icon-button:hover { background: var(--md-sys-color-surface-container); }
.empty-tasks { min-height: 350px; display: flex; flex-direction: column; align-items: center; justify-content: center; gap: 18px; color: var(--md-sys-color-on-surface-variant); } .empty-tasks h3 { font-size: 14px; font-weight: 400; }
.tasks-panel { padding-bottom: 40px; }
.task-list { max-height: max(240px, calc(100dvh - 480px)); overflow-y: auto; scrollbar-gutter: stable; padding-right: 8px; }
.task-pagination { margin-top: 16px; border-radius: 1rem; }
@media (min-width: 1600px) { .workbench-form { gap: 56px; } }
@media (min-width: 1024px) { .workbench-tabs { left: calc(var(--sidebar-width) + 16px); max-width: calc(100% - var(--sidebar-width) - 32px); } }
@media (max-width: 1100px) { .workbench-form { gap: 24px; } .key-model-fields { grid-template-columns: minmax(0,1fr); } }
@media (max-width: 767px) { .video-workbench { min-height: calc(100vh - 135px); } .workbench-form { grid-template-columns: minmax(0,1fr); gap: 26px; } .prompt-input { padding: 16px; } .key-model-fields { grid-template-columns: minmax(0,.9fr) minmax(0,1.1fr); gap: 12px; } h2 { font-size: 18px; } .heading-title { gap: 8px; } .activity { font-size: 11px; gap: 5px; } .workbench-heading { gap: 10px; } .workbench-tabs { gap: 3px; padding: 5px; } .view-tabs { gap: 3px; } .workbench-tabs button,.workbench-tabs a { flex-direction: column; gap: 4px; min-height: 42px; padding: 5px 9px; font-size: 11px; } .tab-count { position: absolute; top: 2px; right: 2px; } .status-filter { gap: 9px; } .status-filter button { font-size: 11px; gap: 4px; } }
@media (max-width: 359px) { .workbench-tabs button,.workbench-tabs a { padding: 5px 8px; } .custom-ratio { gap: 6px; } .heading-title { gap: 5px; } h2 { font-size: 17px; } }
</style>
