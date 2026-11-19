<template>
  <div class="border-t pt-4">
    <label class="block">
      <span class="input-label">{{ t('admin.groups.fastRate.force') }}</span>
      <select v-model="mode" class="input" data-testid="forced-fast-mode">
        <option value="off">{{ t('admin.groups.fastRate.off') }}</option>
        <option value="fast">Fast</option>
        <option value="ultrafast">UltraFast</option>
      </select>
    </label>
    <p class="input-hint">{{ t('admin.groups.fastRate.forceHint') }}</p>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type { ForcedFastMode } from '@/types'
const mode = defineModel<ForcedFastMode>({ required: true })
const { t } = useI18n()
</script>
