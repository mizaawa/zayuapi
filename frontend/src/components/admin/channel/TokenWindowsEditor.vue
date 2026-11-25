<template>
  <section class="mt-4 space-y-2" data-testid="token-windows">
    <div class="flex flex-wrap items-center justify-between gap-2">
      <h4 class="text-xs font-semibold">{{ t('admin.channels.form.timeWindows') }}</h4>
      <button type="button" class="btn btn-secondary btn-sm" @click="emit('update', [...windows, emptyWindow()])">{{ t('admin.channels.form.addInterval') }}</button>
    </div>
    <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.channels.form.windowHint') }}</p>
    <fieldset v-for="(w, i) in windows" :key="i" class="min-w-0 space-y-2 rounded-lg border p-3 dark:border-dark-600">
      <legend class="px-1 text-xs">{{ t('admin.channels.form.windowNumber', { n: i+1 }) }}</legend>
      <div class="flex flex-wrap items-center gap-x-2 gap-y-1">
        <template v-for="side in sides" :key="side">
          <span v-if="side === 'end'" aria-hidden="true">～</span>
          <div class="inline-flex items-center gap-1">
            <template v-for="(part, k) in parts" :key="part">
              <span v-if="k" aria-hidden="true">:</span>
              <input type="text" inputmode="numeric" maxlength="2" autocomplete="off" class="input w-11 px-1 text-center tabular-nums"
                :aria-label="`${t('admin.channels.form.' + side)} · ${t('admin.channels.form.' + part)}`" :placeholder="k ? 'mm' : 'HH'"
                :value="w[side].split(':')[k] || ''" @input="setClock(i, side, k, ($event.target as HTMLInputElement).value)"
                @blur="padClock(i, side, k)" />
            </template>
          </div>
        </template>
        <button type="button" class="btn btn-secondary btn-sm ml-auto" @click="emit('update', windows.filter((_, j) => i !== j))">{{ t('common.delete') }}</button>
      </div>
      <TokenPriceFields :value="w" @change="(key, value) => change(i, { [key]: value })" />
    </fieldset>
  </section>
</template>
<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { emptyWindow, type TokenWindow } from '@/utils/tokenPriceWindows'
import TokenPriceFields from './TokenPriceFields.vue'
type WindowForm = TokenWindow<string | number | null>
const props = defineProps<{ windows: WindowForm[] }>()
const emit = defineEmits<{ update: [windows: WindowForm[]] }>()
const { t } = useI18n()
const sides = ['start', 'end'] as const, parts = ['hour', 'minute'] as const
function change(i: number, patch: Partial<WindowForm>) { emit('update', props.windows.map((w,j) => j === i ? { ...w, ...patch } : w)) }
function setClock(i: number, side: typeof sides[number], k: number, value: string) {
  const clock = props.windows[i][side].split(':'); clock[k] = value
  change(i, { [side]: (clock[0] || '') + ':' + (clock[1] || '') })
}
function padClock(i: number, side: typeof sides[number], k: number) {
  const part = props.windows[i][side].split(':')[k]; if (/^\d$/.test(part || '')) setClock(i, side, k, '0' + part)
}
</script>
