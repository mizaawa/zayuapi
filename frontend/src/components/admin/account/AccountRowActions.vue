<template>
  <div class="account-row-actions">
    <button type="button" :title="t('common.edit')" @click="$emit('edit')">
      <Icon name="edit" size="sm" /><span>{{ t('common.edit') }}</span>
    </button>
    <button type="button" :title="t('common.more')" @click="$emit('more', $event)">
      <Icon name="more" size="sm" /><span>{{ t('common.more') }}</span>
    </button>
    <button type="button" class="is-danger" :title="t('common.delete')" @click="$emit('delete')">
      <Icon name="trash" size="sm" /><span>{{ t('common.delete') }}</span>
    </button>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'

const { t } = useI18n()
defineEmits<{ edit: []; more: [event: MouseEvent]; delete: [] }>()
</script>

<style scoped>
.account-row-actions {
  display: inline-flex;
  align-items: center;
  gap: 0.125rem;
}
.account-row-actions button {
  @apply inline-flex shrink-0 flex-col items-center justify-center gap-0.5 rounded-lg text-xs;
  min-width: 2.75rem;
  min-height: 2.75rem;
  color: var(--md-sys-color-on-surface-variant);
}
.account-row-actions button:hover,
.account-row-actions button:focus-visible {
  background: var(--md-sys-color-surface-container);
  color: var(--md-sys-color-on-surface);
}
.account-row-actions .is-danger:hover,
.account-row-actions .is-danger:focus-visible {
  color: var(--monitor-down-ink);
  background: color-mix(in srgb, var(--monitor-down) 10%, var(--md-sys-color-surface));
}
@media (min-width: 1024px) {
  .account-row-actions button { min-width: 2rem; min-height: 2rem; }
  .account-row-actions span { @apply sr-only; }
}
</style>
