interface OutputSpeedUsage {
  output_tokens?: number | null
  duration_ms?: number | null
  image_count?: number | null
  image_output_tokens?: number | null
  billing_mode?: string | null
  request_type?: string | null
}

const isNonTextUsage = (usage: OutputSpeedUsage): boolean =>
  (usage.image_count ?? 0) > 0 ||
  (usage.image_output_tokens ?? 0) > 0 ||
  usage.billing_mode === 'image' ||
  usage.billing_mode === 'video' ||
  usage.request_type === 'live' ||
  usage.request_type === 'cyber'

export function getEndToEndOutputSpeed(usage: OutputSpeedUsage): number | null {
  if (isNonTextUsage(usage)) return null
  const tokens = usage.output_tokens
  const duration = usage.duration_ms
  if (tokens == null || !Number.isFinite(tokens) || tokens <= 0) return null
  if (duration == null || !Number.isFinite(duration) || duration <= 0) return null

  // Include first-token latency in the recorded total forwarding duration.
  const speed = tokens / (duration / 1000)
  return Number.isFinite(speed) ? speed : null
}

export function getOutputSpeedUnavailableReason(usage: OutputSpeedUsage): string {
  if (isNonTextUsage(usage)) return 'usage.performanceNotApplicable'
  if (usage.output_tokens === 0) return 'usage.performanceNoOutput'
  return 'usage.performanceNotRecorded'
}
