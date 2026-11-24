<template>
  <div class="border-t pt-4">
    <label :for="fieldId" class="input-label">{{ t('admin.groups.fastRate.force') }}</label>
    <Select
      :id="fieldId"
      v-model="mode"
      :options="options"
      :searchable="false"
      :aria-label="t('admin.groups.fastRate.force')"
      :aria-describedby="`${fieldId}-hint`"
      data-testid="forced-fast-mode"
    />
    <p :id="`${fieldId}-hint`" class="input-hint">{{ t('admin.groups.fastRate.forceHint') }}</p>
  </div>
</template>

<script setup lang="ts">
import { computed, useId } from 'vue'
import { useI18n } from 'vue-i18n'
import Select from '@/components/common/Select.vue'
import type { ForcedFastMode } from '@/types'

const mode = defineModel<ForcedFastMode>({ required: true })
const { t } = useI18n()
const fieldId = useId()
const options = computed<{ value: ForcedFastMode; label: string }[]>(() => [
  { value: 'off', label: t('admin.groups.fastRate.off') },
  { value: 'fast', label: 'Fast' },
  { value: 'ultrafast', label: 'UltraFast' }
])
</script>
