/** Viewport rectangle the check tooltip points at (one timeline segment). */
export interface MonitorCheckAnchor {
  top: number
  bottom: number
  left: number
  width: number
}

export type MonitorCheckTone = 'operational' | 'degraded' | 'down' | 'empty'

export interface MonitorCheckDetailRow {
  label: string
  value: string
  tone?: 'warn' | 'bad'
}

/** What the tooltip shows for one check in a status timeline. */
export interface MonitorCheckDetail {
  tone: MonitorCheckTone
  title: string
  hint: string
  ago: string
  rows: MonitorCheckDetailRow[]
}
