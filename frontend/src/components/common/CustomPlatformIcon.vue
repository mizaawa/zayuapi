<template>
  <!-- Custom platform mark: a jigsaw piece, since a Custom account plugs any
       compatible upstream into the site. Filled like the other platform logos
       so it stays legible at 12px. -->
  <svg
    :width="size"
    :height="size"
    viewBox="0 0 24 24"
    fill="currentColor"
    xmlns="http://www.w3.org/2000/svg"
    aria-hidden="true"
  >
    <path
      d="M5 7h3.2a2.6 2.6 0 1 1 3.6 0H15a2 2 0 0 1 2 2v3.2a2.6 2.6 0 1 1 0 3.6V19a2 2 0 0 1-2 2h-3.2a2.6 2.6 0 1 0-3.6 0H5a2 2 0 0 1-2-2v-3.2a2.6 2.6 0 1 0 0-3.6V9a2 2 0 0 1 2-2z"
    />
  </svg>
</template>

<script setup lang="ts">
defineProps<{
  size?: number | string
}>()
</script>
