import { apiClient } from '../client'

export interface UpstreamBalanceSummary {
  total_usd: number
  known_count: number
  unknown_count: number
}

export interface UpstreamBalanceSnapshot {
  account_id: number
  balance_usd: number | null
  updated_at?: string
}

export async function getUpstreamBalanceSummary(): Promise<UpstreamBalanceSummary> {
  const { data } = await apiClient.get<UpstreamBalanceSummary>('/admin/accounts/upstream-balances')
  return data
}

export async function setUpstreamBalance(accountId: number, balance: number | null): Promise<UpstreamBalanceSnapshot> {
  const { data } = await apiClient.put<UpstreamBalanceSnapshot>(`/admin/accounts/${accountId}/upstream-balance`, { balance_usd: balance })
  return data
}
