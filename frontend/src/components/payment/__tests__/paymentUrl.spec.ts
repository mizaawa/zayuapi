import { describe, expect, it } from 'vitest'
import { sanitizePaymentUrl } from '../paymentUrl'

describe('sanitizePaymentUrl', () => {
  it.each([
    'https://pay.example.com/order/42?return=https%3A%2F%2Fapp.example.com',
    'http://pay.example.com/order/42',
    'weixin://wxpay/bizpayurl?pr=42',
    'wxp://payment-token',
    'alipays://platformapi/startapp?saId=10000007',
    'alipay://platformapi/startapp?saId=10000007',
  ])('preserves supported payment link %s', (url) => {
    expect(sanitizePaymentUrl(url)).toBe(url)
    expect(sanitizePaymentUrl(`  ${url}  `)).toBe(url)
  })

  it.each([
    'javascript:alert(document.domain)',
    'JaVaScRiPt:alert(1)',
    'java\tscript:alert(1)',
    'java\nscript:alert(1)',
    'java\u0000script:alert(1)',
    'data:text/html,<script>alert(1)</script>',
    'vbscript:msgbox(1)',
    'file:///C:/Windows/system.ini',
    'blob:https://app.example.com/42',
    'intent://pay/#Intent;scheme=javascript;end',
    '//attacker.example.com/pay',
    '/payment?payee=attacker',
    'not a url',
    '',
    undefined,
    ['https://pay.example.com'],
  ])('rejects unsafe or unbound link %s', (url) => {
    expect(sanitizePaymentUrl(url)).toBe('')
  })
})
