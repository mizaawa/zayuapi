<template>
  <section class="key-form-section" :aria-labelledby="showHeading ? `${id}-title` : undefined">
    <header v-if="showHeading" class="key-form-section-head">
      <span class="key-form-section-icon" aria-hidden="true"><Icon :name="icon" size="md" /></span>
      <div class="min-w-0 flex-1">
        <label v-if="enabled !== null" :id="`${id}-title`" :for="`${id}-enabled`" class="key-form-section-title">{{ title }}</label>
        <h4 v-else :id="`${id}-title`" class="key-form-section-title">{{ title }}</h4>
        <p v-if="hint" :id="`${id}-summary`" class="key-form-section-hint">{{ hint }}</p>
      </div>
      <Toggle
        v-if="enabled !== null"
        :id="`${id}-enabled`"
        :model-value="enabled"
        :aria-label="title"
        :aria-describedby="hint ? `${id}-summary` : undefined"
        :aria-controls="enabled ? `${id}-controls` : undefined"
        :disabled="disabled"
        :data-test="toggleTest"
        @update:model-value="emit('update:enabled', $event)"
      />
    </header>
    <div v-if="enabled !== false" :id="`${id}-controls`" class="key-form-section-body">
      <slot />
    </div>
  </section>
</template>

<script setup lang="ts">
import Icon from '@/components/icons/Icon.vue'
import Toggle from '@/components/common/Toggle.vue'

withDefaults(defineProps<{
  id: string
  title: string
  icon: InstanceType<typeof Icon>['$props']['name']
  hint?: string
  enabled?: boolean | null
  disabled?: boolean
  showHeading?: boolean
  toggleTest?: string
}>(), { enabled: null, showHeading: true })
const emit = defineEmits<{ 'update:enabled': [enabled: boolean] }>()
</script>

<style>
.key-form-section-head { display: flex; align-items: center; gap: 0.75rem; }
.key-form-section-title { display: block; font-size: 0.875rem; font-weight: 600; }
.key-form-section-head + .key-form-section-body { margin-top: 0.75rem; }
/* The compact create and quick-settings dialogs keep their original single-column hierarchy. */
.key-form-section-icon, .key-form-section-hint { display: none; }
</style>
