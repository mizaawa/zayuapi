/**
 * Account concurrency input helpers.
 *
 * A blank field means "unlimited". The backend stores that as 0, so the form
 * keeps `null` while editing and sends 0 on save.
 */

/** Keep blank input blank; otherwise coerce to a positive whole number. */
export function normalizeConcurrencyInput(value: unknown): number | null {
  if (value === '' || value === null || value === undefined) return null
  const parsed = typeof value === 'number' ? value : Number(value)
  if (!Number.isFinite(parsed)) return null
  return Math.max(1, Math.floor(parsed))
}

/** Value sent to the API: 0 (unlimited) for blank input. */
export function concurrencyPayload(value: number | null | undefined): number {
  return typeof value === 'number' && Number.isFinite(value) && value >= 1 ? Math.floor(value) : 0
}

/** True when an account has no concurrency limit. */
export function isUnlimitedConcurrency(value: number | null | undefined): boolean {
  return !(typeof value === 'number' && value > 0)
}
