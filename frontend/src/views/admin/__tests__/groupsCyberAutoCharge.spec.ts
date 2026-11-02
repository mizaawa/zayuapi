import { describe, expect, it } from 'vitest'
import {
  cyberAutoChargePayload,
  normalizeCyberAutoChargeAmount,
  validateCyberAutoChargeFormState,
} from '../groupsCyberAutoCharge'

describe('groupsCyberAutoCharge', () => {
  it('treats empty, negative and invalid amounts as zero', () => {
    expect(normalizeCyberAutoChargeAmount('')).toBe(0)
    expect(normalizeCyberAutoChargeAmount(null)).toBe(0)
    expect(normalizeCyberAutoChargeAmount(-1)).toBe(0)
    expect(normalizeCyberAutoChargeAmount('abc')).toBe(0)
    expect(normalizeCyberAutoChargeAmount('0.5')).toBe(0.5)
  })

  it('requires an amount above zero only while the charge is on', () => {
    expect(validateCyberAutoChargeFormState({ cyber_auto_charge_enabled: true, cyber_auto_charge_amount: '' })).toBe('amountRequired')
    expect(validateCyberAutoChargeFormState({ cyber_auto_charge_enabled: true, cyber_auto_charge_amount: 0 })).toBe('amountRequired')
    expect(validateCyberAutoChargeFormState({ cyber_auto_charge_enabled: true, cyber_auto_charge_amount: 0.2 })).toBeNull()
    expect(validateCyberAutoChargeFormState({ cyber_auto_charge_enabled: false, cyber_auto_charge_amount: '' })).toBeNull()
  })

  it('submits the switch as set and keeps the amount', () => {
    expect(cyberAutoChargePayload({ cyber_auto_charge_enabled: true, cyber_auto_charge_amount: 0.5 })).toEqual({ cyber_auto_charge_enabled: true, cyber_auto_charge_amount: 0.5 })
    expect(cyberAutoChargePayload({ cyber_auto_charge_enabled: false, cyber_auto_charge_amount: 0.5 })).toEqual({ cyber_auto_charge_enabled: false, cyber_auto_charge_amount: 0.5 })
    expect(cyberAutoChargePayload({ cyber_auto_charge_enabled: false, cyber_auto_charge_amount: '' })).toEqual({ cyber_auto_charge_enabled: false, cyber_auto_charge_amount: 0 })
  })
})
