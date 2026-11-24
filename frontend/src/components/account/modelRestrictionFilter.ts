import { computed, ref, shallowRef, type Ref } from 'vue'

type Mapping = { from: string; to: string }

export function useModelRestrictionFilter(mappings: Ref<Mapping[]>) {
  const modelFilter = ref('')
  const query = computed(() => modelFilter.value.trim().toLowerCase())
  // Keep a row mounted while its matching field is being edited. On blur it
  // immediately participates in the filter again. Save always uses mappings.
  const editingMapping = shallowRef<Mapping | null>(null)
  const visibleMappings = computed(() => mappings.value
    .map((mapping, index) => ({ mapping, index }))
    .filter(({ mapping }) => !query.value || editingMapping.value === mapping ||
      mapping.from.toLowerCase().includes(query.value) || mapping.to.toLowerCase().includes(query.value)))

  function onMappingBlur(event: FocusEvent) {
    if (!(event.relatedTarget instanceof Node) || !(event.currentTarget as Element).contains(event.relatedTarget)) {
      editingMapping.value = null
    }
  }
  return { modelFilter, visibleMappings, editingMapping, onMappingBlur }
}
