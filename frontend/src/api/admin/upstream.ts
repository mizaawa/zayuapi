import { apiClient } from '../client'

export type ProviderKind = 'sub2api' | 'newapi'
export type ProviderAuthMode = 'password' | 'token'

export interface ProviderInput {
  id?: string
  name: string
  site_type: ProviderKind
  base_url: string
  auth_mode: ProviderAuthMode
  account?: string
  password?: string
  /** Sub2API dashboard session token, never a model API key. */
  access_token?: string
  refresh_token?: string
  token_type?: string
  /** New API dashboard account id and system access token. */
  user_id?: string
  system_access_token?: string
  exchange_rate: number
  notes?: string
  suppress_low_balance_alert?: boolean
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
  user_id?: string
  token_type?: string
  exchange_rate: number
  notes?: string
  status: 'connected' | 'disconnected'
  metrics: ProviderMetrics
  updated_at?: string
  last_error?: string
  suppress_low_balance_alert?: boolean
  low_balance_alert?: boolean
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
export async function deleteProvider(id: string): Promise<void> {
  await apiClient.delete('/admin/providers/' + encodeURIComponent(id))
}
export async function probeProvider(input: ProviderInput): Promise<Provider> {
  const { data } = await apiClient.post<Provider>('/admin/providers/probe', input)
  return data
}
export async function refreshProvider(id: string): Promise<Provider> {
  const { data } = await apiClient.post<Provider>('/admin/providers/' + encodeURIComponent(id) + '/refresh')
  return data
}

/** A provider offered as an account import source. */
export interface ProviderOption {
  id: string
  name: string
  site_type: ProviderKind
  base_url: string
  status: 'connected' | 'disconnected' | ''
  last_error?: string
  updated_at?: string
}

/** Model API key metadata. The key itself never leaves the server. */
export interface ProviderKey {
  id: string
  name: string
  group_id: string
  group_name: string
  group_platform?: string
  group_multiplier: number | null
  status: 'active' | 'disabled' | 'expired' | 'quota_exhausted' | 'unknown' | string
  available: boolean
}

export interface ProviderKeyList {
  provider_id: string
  site_type: ProviderKind
  endpoint: string
  keys: ProviderKey[]
  synced_at: string
}

export interface ProviderKeyGroup {
  id: string
  name: string
  platform?: string
  description?: string
  multiplier: number | null
  /** The name the server gives a key created for this group. */
  key_name: string
}

export interface ProviderKeyGroupList {
  provider_id: string
  site_type: ProviderKind
  groups: ProviderKeyGroup[]
}

export interface ProviderKeyCreated {
  provider_id: string
  created: boolean
  name: string
  group_id: string
  /** Empty when the provider acknowledged the key without identifying it. */
  key_id: string
}

export async function listProviderOptions(): Promise<ProviderOption[]> {
  const { data } = await apiClient.get<ProviderOption[]>('/admin/providers/options')
  return data ?? []
}
export async function listProviderKeys(id: string): Promise<ProviderKeyList> {
  const { data } = await apiClient.get<ProviderKeyList>('/admin/providers/' + encodeURIComponent(id) + '/keys')
  return data
}
export async function listProviderKeyGroups(id: string): Promise<ProviderKeyGroupList> {
  const { data } = await apiClient.get<ProviderKeyGroupList>('/admin/providers/' + encodeURIComponent(id) + '/groups')
  return data
}
export async function createProviderKey(id: string, groupId: string): Promise<ProviderKeyCreated> {
  const { data } = await apiClient.post<ProviderKeyCreated>('/admin/providers/' + encodeURIComponent(id) + '/keys', { group_id: groupId })
  return data
}
