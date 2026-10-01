import { describe, expect, it } from 'vitest'
import { getEndToEndOutputSpeed, getOutputSpeedUnavailableReason } from '../outputSpeed'

describe('end-to-end output delivery speed', () => {
  it('uses the full recorded duration, including the first-token wait', () => {
    const usage = { output_tokens: 1218, duration_ms: 25690, first_token_ms: 10620 }
    expect(getEndToEndOutputSpeed(usage)).toBeCloseTo(47.4114, 4)
  })

  it('supports synchronous and legacy records without first-token timing', () => {
    expect(getEndToEndOutputSpeed({ output_tokens: 50, duration_ms: 500 })).toBe(100)
  })

  it.each([null, undefined, 0, -1, NaN, Infinity])('does not calculate with invalid duration %s', (duration) => {
    expect(getEndToEndOutputSpeed({ output_tokens: 100, duration_ms: duration })).toBeNull()
  })

  it.each([null, undefined, 0, -1, NaN, Infinity])('does not calculate with invalid output %s', (tokens) => {
    expect(getEndToEndOutputSpeed({ output_tokens: tokens, duration_ms: 1000 })).toBeNull()
  })

  it.each([
    { image_count: 1 },
    { image_output_tokens: 200 },
    { billing_mode: 'image' },
    { billing_mode: 'video' },
    { request_type: 'live' },
    { request_type: 'cyber' },
  ])('marks non-text usage as inapplicable: %j', (fields) => {
    const usage = { output_tokens: 100, duration_ms: 1000, ...fields }
    expect(getEndToEndOutputSpeed(usage)).toBeNull()
    expect(getOutputSpeedUnavailableReason(usage)).toBe('usage.performanceNotApplicable')
  })

  it('distinguishes missing timing from no output', () => {
    expect(getOutputSpeedUnavailableReason({ output_tokens: 100 })).toBe('usage.performanceNotRecorded')
    expect(getOutputSpeedUnavailableReason({ output_tokens: 0, duration_ms: 1000 })).toBe('usage.performanceNoOutput')
  })
})
