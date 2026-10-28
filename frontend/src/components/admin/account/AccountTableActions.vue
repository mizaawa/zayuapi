<template>
  <!-- On phones the row spans the toolbar: square icon buttons, and the
       create button takes the remaining width. -->
  <div class="flex flex-wrap items-center gap-3 max-sm:w-full">
    <slot name="before"></slot>
    <button
      @click="$emit('refresh')"
      :disabled="loading"
      class="btn btn-secondary max-sm:h-11 max-sm:w-11 max-sm:shrink-0 max-sm:p-0"
      :title="t('common.refresh')"
      :aria-label="t('common.refresh')"
    >
      <Icon name="refresh" size="md" :class="[loading ? 'animate-spin' : '']" />
    </button>
    <slot name="after"></slot>
    <slot name="beforeCreate"></slot>
    <button @click="$emit('create')" class="btn btn-primary max-sm:flex-1">{{ t('admin.accounts.createAccount') }}</button>
    <slot name="afterCreate"></slot>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'

defineProps(['loading'])
defineEmits(['refresh', 'create'])

const { t } = useI18n()
</script>
