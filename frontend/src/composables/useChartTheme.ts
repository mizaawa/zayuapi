import { computed } from 'vue'

export function useChartTheme() {
  // Match the app's light-only brand palette, including stale dark preferences.
  return {
    distributionBorderColor: computed(() => '#e9b824'),
    distributionHoverBorderColor: computed(() => '#976800')
  }
}
