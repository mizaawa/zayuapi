<template>
  <div class="mb-3">
    <div class="relative">
      <Icon name="search" size="sm" aria-hidden="true" class="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-gray-400" />
      <input
        v-model="keyword"
        type="search"
        class="input w-full pl-9 text-sm"
        :aria-label="t('admin.accounts.modelFilterPlaceholder')"
        :placeholder="t('admin.accounts.modelFilterPlaceholder')"
        @keydown.enter.prevent
      />
    </div>
    <p v-if="noMatches" role="status" class="input-hint">{{ t('admin.accounts.noMatchingModels') }}</p>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'

const keyword = defineModel<string>({ required: true })
defineProps<{ noMatches?: boolean }>()
const { t } = useI18n()
</script>
