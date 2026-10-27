export type RechargeStatus = 'paid' | 'unpaid'
export type RechargeChannel = 'code' | 'direct' | 'manual'

export interface RechargeOrder {
  id: string
  user_email: string
  amount: number
  currency: string
  status: RechargeStatus
  channel: RechargeChannel
  created_at: string
}

export interface RechargeRiskEvent {
  id: string
  type: string
  user_email: string
  count: number
  user_balance: number
  total_consumption: number
  first_at: string
  last_at: string
  order_count: number
  result: 'success' | 'failed' | string
}

export interface RechargeOrderPage {
  items: RechargeOrder[]
  total: number
  page: number
  pages: number
}
