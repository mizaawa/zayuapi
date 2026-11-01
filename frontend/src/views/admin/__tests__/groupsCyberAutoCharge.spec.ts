import { describe, expect, it } from 'vitest'
import {
  cyberAutoChargePayload,
  isCyberAutoChargePlatform,
  normalizeCyberAutoChargeAmount,
  validateCyberAutoChargeFormState,
} from '../groupsCyberAutoCharge'

describe('groupsCyberAutoCharge', () => {
  it('offers the charge only on platforms served by the OpenAI gateway', () => {
    expect(isCyberAutoChargePlatform('openai')).toBe(true)
    expect(isCyberAutoChargePlatform('composite')).toBe(true)
    expect(isCyberAutoChargePlatform('custom')).toBe(true)
    expect(isCyberAutoChargePlatform('anthropic')).toBe(false)
    expect(isCyberAutoChargePlatform('gemini')).toBe(false)
  })

  it('treats empty, negative and invalid amounts as zero', () => {
    expect(normalizeCyberAutoChargeAmount('')).toBe(0)
    expect(normalizeCyberAutoChargeAmount(null)).toBe(0)
    expect(normalizeCyberAutoChargeAmount(-1)).toBe(0)
    expect(normalizeCyberAutoChargeAmount('abc')).toBe(0)
    expect(normalizeCyberAutoChargeAmount('0.5')).toBe(0.5)
  })

  it('requires an amount above zero only while the charge is on', () => {
    expect(validateCyberAutoChargeFormState({ platform: 'openai', cyber_auto_charge_enabled: true, cyber_auto_charge_amount: '' })).toBe('amountRequired')
    expect(validateCyberAutoChargeFormState({ platform: 'openai', cyber_auto_charge_enabled: true, cyber_auto_charge_amount: 0 })).toBe('amountRequired')
    expect(validateCyberAutoChargeFormState({ platform: 'openai', cyber_auto_charge_enabled: true, cyber_auto_charge_amount: 0.2 })).toBeNull()
    expect(validateCyberAutoChargeFormState({ platform: 'openai', cyber_auto_charge_enabled: false, cyber_auto_charge_amount: '' })).toBeNull()
    expect(validateCyberAutoChargeFormState({ platform: 'anthropic', cyber_auto_charge_enabled: true, cyber_auto_charge_amount: '' })).toBeNull()
  })

  it('turns the charge off for other platforms and keeps the amount', () => {
    expect(cyberAutoChargePayload({ platform: 'openai', cyber_auto_charge_enabled: true, cyber_auto_charge_amount: 0.5 })).toEqual({ cyber_auto_charge_enabled: true, cyber_auto_charge_amount: 0.5 })
    expect(cyberAutoChargePayload({ platform: 'gemini', cyber_auto_charge_enabled: true, cyber_auto_charge_amount: 0.5 })).toEqual({ cyber_auto_charge_enabled: false, cyber_auto_charge_amount: 0.5 })
    expect(cyberAutoChargePayload({ platform: 'openai', cyber_auto_charge_enabled: false, cyber_auto_charge_amount: '' })).toEqual({ cyber_auto_charge_enabled: false, cyber_auto_charge_amount: 0 })
  })
})
