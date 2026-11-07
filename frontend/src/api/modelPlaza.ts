/**
 * Model Plaza API（公开端点，可匿名访问）
 * 以分组为中心的模型价目：分组信息 + 实付计算基价 + 渠道配置原价。
 * 带 token 请求时后端会额外返回专属分组与用户专属倍率。
 */

import { apiClient } from './client'
import type { UserSupportedModelPricing } from './channels'
import type { PlatformPricingCurrency } from '@/utils/channelPricingCurrency'

/** Channel price before group overrides or multipliers; persisted USD units.
 * Optional extra fields keep legacy token-only responses readable.
 */
export type PlazaOfficialPricing = Pick<UserSupportedModelPricing,
  'input_price' | 'output_price' | 'cache_write_price' | 'cache_read_price'>
  & Partial<UserSupportedModelPricing>

export interface PlazaModel {
  name: string
  platform: string
  pricing: UserSupportedModelPricing | null
  official_pricing: PlazaOfficialPricing | null
  /** Effective billing base source; original prices never use reference fallback. */
  pricing_source?: 'channel' | 'group' | 'reference' | 'unavailable'
  pricing_currency?: PlatformPricingCurrency
}

export interface ModelPlazaGroup {
  id: number
  name: string
  description: string
  platform: string
  /** 'standard' | 'subscription' */
  subscription_type: string
  rate_multiplier: number
  /** OpenAI Fast token billing: standard model price × group/personal rate × this factor. */
  fast_rate_enabled?: boolean
  fast_rate_multiplier?: number
  /** 登录且管理员为该用户配了专属倍率时返回；生效倍率 = user_rate ?? rate_multiplier。 */
  user_rate_multiplier?: number
  peak_rate_enabled: boolean
  peak_start: string
  peak_end: string
  peak_rate_multiplier: number
  is_exclusive: boolean
  /** 分组管理里设定的展示顺序（升序）；广场按它排列分组。 */
  sort_order?: number
  /** 生图独立倍率：true 时图片计费模型的实付倍率取 image_rate_multiplier，不取分组/专属倍率。 */
  image_rate_independent: boolean
  image_rate_multiplier: number
  video_rate_independent: boolean
  video_rate_multiplier: number
  models: PlazaModel[]
}

export interface ModelPlazaResponse {
  /** 管理员配置的全局价格说明（Markdown）。 */
  description: string
  groups: ModelPlazaGroup[]
}

/** 获取模型广场数据。开关未启用时后端返回 404。 */
export async function getModelPlaza(options?: { signal?: AbortSignal }): Promise<ModelPlazaResponse> {
  const { data } = await apiClient.get<ModelPlazaResponse>('/model-plaza', {
    signal: options?.signal
  })
  return data
}

export const modelPlazaAPI = { getModelPlaza }

export default modelPlazaAPI
