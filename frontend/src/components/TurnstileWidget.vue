<template>
  <div v-if="siteKey" ref="wrapper" class="turnstile-wrapper">
    <div ref="container" class="turnstile-container" :data-size="sizeToRender"></div>
    <p v-if="loading" role="status" class="text-sm">{{ t('auth.captchaLoading') }}</p>
    <div v-if="failed || showRetry" class="turnstile-retry" role="status">
      <span v-if="failed">{{ t('auth.turnstileRetryHint') }}</span>
      <button type="button" @click="attempt++">{{ t('auth.retryVerification') }}</button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useElementSize } from '@vueuse/core'
import { useI18n } from 'vue-i18n'
import { loadTurnstile, type TurnstileAPI, type TurnstileSize } from '@/utils/turnstile'

const props = withDefaults(defineProps<{
  siteKey: string
  theme?: 'light' | 'dark' | 'auto'
  size?: TurnstileSize
}>(), { theme: 'auto', size: 'flexible' })
const emit = defineEmits<{
  verify: [token: string]
  expire: []
  error: []
}>()
const { t } = useI18n()
const wrapper = ref<HTMLElement | null>(null)
const container = ref<HTMLElement | null>(null)
const { width } = useElementSize(wrapper)
// Flexible and normal both need at least 300 CSS pixels. Measure the available
// form column, not the viewport; compact is a native 150 × 140 widget.
const sizeToRender = computed(() => width.value <= 0 ? null : width.value < 300 ? 'compact' : props.size)
const loading = ref(true)
const showRetry = ref(false)
const failed = ref(false)
const attempt = ref(0)
let widget: { api: TurnstileAPI; id: string; watchForStall: () => void } | null = null
let hasProof = false

watch([container, sizeToRender, () => props.siteKey, () => props.theme, attempt], async ([el, size, key, theme], _, onCleanup) => {
  if (hasProof) {
    hasProof = false
    emit('expire')
  }
  failed.value = false
  loading.value = true
  showRetry.value = false
  if (!el || !key || !size) return
  let active = true
  let retryTimer: number | undefined
  function watchForStall() {
    window.clearTimeout(retryTimer)
    // Offer help without interrupting a slow network or a human solving the challenge.
    retryTimer = window.setTimeout(() => { if (active && !hasProof) showRetry.value = true }, 15000)
  }
  onCleanup(() => {
    active = false
    window.clearTimeout(retryTimer)
    try { if (widget) widget.api.remove(widget.id) } catch { /* SDK already removed this instance. */ }
    widget = null
  })
  const fail = () => {
    if (active && !failed.value) {
      loading.value = false
      hasProof = false
      failed.value = true
      emit('error')
    }
    return true // We display a retry action; never treat an error as a proof.
  }
  try {
    const api = await loadTurnstile()
    if (!active) return
    loading.value = false
    watchForStall()
    const id = api.render(el, {
      sitekey: key, theme, size,
      callback(token) {
        if (!active) return
        if (!token) { fail(); return }
        failed.value = false
        showRetry.value = false
        hasProof = true
        emit('verify', token)
      },
      'expired-callback'() {
        if (!active) return
        hasProof = false
        showRetry.value = true
        emit('expire')
      },
      'error-callback': fail,
      'timeout-callback': fail
    })
    if (typeof id !== 'string') { fail(); return }
    widget = { api, id, watchForStall }
  } catch { fail() }
}, { flush: 'post' })

function reset() {
  hasProof = false
  if (widget && !failed.value) {
    try {
      showRetry.value = false
      widget.watchForStall()
      widget.api.reset(widget.id)
      return
    } catch { /* Re-render if the SDK lost the instance. */ }
  }
  attempt.value++
}
defineExpose({ reset })
</script>

<style scoped>
.turnstile-wrapper { width: 100%; min-width: 0; }
.turnstile-container { width: 100%; min-height: 65px; }
.turnstile-container[data-size='compact'] { width: 150px; min-height: 140px; margin-inline: auto; }
.turnstile-retry { display: flex; flex-wrap: wrap; align-items: center; gap: .3rem .75rem; font-size: .8125rem; color: var(--md-sys-color-on-surface-variant); }
.turnstile-retry button { min-height: 2.75rem; font-weight: 650; text-decoration: underline; text-underline-offset: .15em; }
</style>
