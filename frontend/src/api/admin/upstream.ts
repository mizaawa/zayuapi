import { apiClient } from '../client'

export type ProviderKind = 'sub2api' | 'newapi'
export type ProviderAuthMode = 'password' | 'api_key'

export interface ProviderInput {
  id?: string
  name: string
  site_type: ProviderKind
  base_url: string
  auth_mode: ProviderAuthMode
  account?: string
  password?: string
  api_key?: string
  exchange_rate: number
  notes?: string
}

export interface ProviderMetrics {
  balance: number | null
  today_spend: number | null
  total_recharge: number | null
  total_spend: number | null
  currency: string
}

export interface Provider {
  id: string
  name: string
  site_type: ProviderKind
  base_url: string
  auth_mode: ProviderAuthMode
  account?: string
  exchange_rate: number
  notes?: string
  status: 'connected' | 'disconnected'
  metrics: ProviderMetrics
  updated_at?: string
  last_error?: string
}

export interface ProviderList {
  items: Provider[]
  total: number
  page: number
  pages: number
  overview: { provider_count: number; connected_count: number; upstream_total_balance: number }
}

export async function listProviders(page = 1, search = '', status = ''): Promise<ProviderList> {
  const { data } = await apiClient.get<ProviderList>('/admin/providers', { params: { page, search, status } })
  return data
}
export async function createProvider(input: ProviderInput): Promise<Provider> {
  const { data } = await apiClient.post<Provider>('/admin/providers', input)
  return data
}
export async function updateProvider(id: string, input: ProviderInput): Promise<Provider> {
  const { data } = await apiClient.put<Provider>('/admin/providers/' + encodeURIComponent(id), input)
  return data
}
export async function probeProvider(input: ProviderInput): Promise<Provider> {
  const { data } = await apiClient.post<Provider>('/admin/providers/probe', input)
  return data
}
export async function refreshProvider(id: string): Promise<Provider> {
  const { data } = await apiClient.post<Provider>('/admin/providers/' + encodeURIComponent(id) + '/refresh')
  return data
}
