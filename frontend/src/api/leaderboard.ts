import { apiClient } from './client'

export interface LeaderboardEntry {
  rank: number
  usage_band: 'under_1' | '1_to_10' | '10_to_100' | '100_to_1000' | 'over_1000'
}

export interface LeaderboardResponse {
  period: 'today' | 'week' | 'month'
  participating: boolean
  period_days: number
  entries: LeaderboardEntry[]
  my_rank: number | null
  my_actual_cost: number
  my_requests: number
  my_tokens: number
}

export async function getLeaderboard(period: 'today' | 'week' | 'month' = 'today', options?: { signal?: AbortSignal }): Promise<LeaderboardResponse> {
  const { data } = await apiClient.get<LeaderboardResponse>('/leaderboard', {
    params: { period },
    signal: options?.signal,
  })
  return data
}

export async function setLeaderboardParticipation(participating: boolean): Promise<{ participating: boolean }> {
  const { data } = await apiClient.patch<{ participating: boolean }>('/leaderboard/participation', { participating })
  return data
}

export const leaderboardAPI = { get: getLeaderboard, setParticipation: setLeaderboardParticipation }
export default leaderboardAPI
