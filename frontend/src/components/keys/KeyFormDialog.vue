<template>
  <BaseDialog
    :show="show"
    :title="editingKey ? t('keys.editKey') : t('keys.createKey')"
    :width="editingKey ? 'wide' : 'normal'"
    :panel-class="editingKey ? 'key-editor-panel' : undefined"
    @close="emit('close')"
  >
    <template v-if="editingKey" #heading="{ titleId }">
      <div class="key-editor-heading">
        <span class="key-editor-mark" aria-hidden="true"><Icon name="key" size="lg" /></span>
        <div class="min-w-0">
          <p class="key-editor-eyebrow">{{ t('keys.apiKey') }} <span aria-hidden="true">/</span> <b>#{{ editingKey.id }}</b></p>
          <div class="key-editor-title-line">
            <h3 :id="titleId" class="modal-title">{{ t('keys.editKey') }}</h3>
            <span class="key-editor-name" :title="editingKey.name">{{ editingKey.name }}</span>
          </div>
        </div>
      </div>
    </template>
    <template v-if="editingKey" #navigation>
      <div class="key-editor-tabs" role="tablist" :aria-label="t('keys.editKey')">
        <button
          v-for="(tab, index) in tabs" :id="`key-tab-${tab.id}`" :key="tab.id"
          type="button" role="tab" :aria-selected="active === tab.id"
          :aria-controls="`key-panel-${tab.id}`" :tabindex="active === tab.id ? 0 : -1"
          @click="active = tab.id" @keydown="onTabKey($event, index)"
        >
          <Icon :name="tab.icon" size="sm" aria-hidden="true" />{{ t(`keys.editor.${tab.id}`) }}
        </button>
      </div>
    </template>

    <form id="key-form" ref="form" :class="{ 'space-y-5': !editingKey }" :novalidate="!!editingKey" @submit.prevent="submit">
      <div
        v-for="tab in tabs" v-show="!editingKey || active === tab.id" :id="`key-panel-${tab.id}`" :key="tab.id"
        :role="editingKey ? 'tabpanel' : undefined" :aria-labelledby="editingKey ? `key-tab-${tab.id}` : undefined"
        :data-key-panel="tab.id" class="key-form-panel space-y-5"
      >
        <slot :name="tab.id" />
      </div>
    </form>
    <template #footer>
      <div class="flex w-full items-center justify-end gap-3">
        <span v-if="editingKey?.created_at" class="key-editor-created">
          <Icon name="clock" size="sm" aria-hidden="true" />{{ t('keys.created') }}
          <time :datetime="editingKey.created_at">{{ formatDateOnly(editingKey.created_at) }}</time>
        </span>
        <button type="button" class="btn btn-secondary" @click="emit('close')">{{ t('common.cancel') }}</button>
        <button form="key-form" type="submit" class="btn btn-primary" :disabled="submitting" data-tour="key-form-submit">
          <Icon v-if="submitting" name="refresh" size="sm" class="animate-spin" aria-hidden="true" />
          <Icon v-else-if="editingKey" name="check" size="sm" :stroke-width="2" aria-hidden="true" />
          {{ submitting ? t('keys.saving') : editingKey ? t('keys.editor.save') : t('common.create') }}
        </button>
      </div>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { nextTick, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import type { ApiKey } from '@/types'
import { formatDateOnly } from '@/utils/format'

const props = defineProps<{ show: boolean; editingKey: ApiKey | null; submitting: boolean }>()
const emit = defineEmits<{ close: []; submit: [] }>()
const { t } = useI18n()
const tabs = [
  { id: 'general', icon: 'cog' }, { id: 'requests', icon: 'shield' }, { id: 'limits', icon: 'chartBar' }
] as const
type TabId = typeof tabs[number]['id']
const active = ref<TabId>('general')
const form = ref<HTMLFormElement | null>(null)
watch(() => props.show, (open) => { if (open) active.value = 'general' })
watch(active, () => { form.value?.closest('.modal-body')?.scrollTo?.(0, 0) }, { flush: 'post' })

function onTabKey(event: KeyboardEvent, index: number) {
  const next = { ArrowLeft: (index + tabs.length - 1) % tabs.length, ArrowRight: (index + 1) % tabs.length,
    Home: 0, End: tabs.length - 1 }[event.key]
  if (next === undefined) return
  event.preventDefault()
  active.value = tabs[next].id
  document.getElementById(`key-tab-${active.value}`)?.focus()
}

async function reveal(field: HTMLElement) {
  const panel = field.closest<HTMLElement>('[data-key-panel]')
  const tab = tabs.find(({ id }) => id === panel?.dataset.keyPanel)
  if (tab) active.value = tab.id
  await nextTick()
  field.focus()
}

function focusField(id: string) {
  const field = form.value?.querySelector<HTMLElement>(`#${id}`)
  if (field) void reveal(field)
}
defineExpose({ focusField })

async function submit() {
  if (props.submitting) return
  // Hidden tab panels retain their drafts and constraints. Reveal the first invalid
  // control before asking the browser to show its native validation message.
  if (props.editingKey) {
    const invalid = form.value?.querySelector<HTMLInputElement | HTMLTextAreaElement>('input:invalid, textarea:invalid, select:invalid')
    if (invalid) {
      await reveal(invalid)
      invalid.reportValidity()
      return
    }
  }
  emit('submit')
}
</script>

<style src="@/styles/key-editor.css"></style>
