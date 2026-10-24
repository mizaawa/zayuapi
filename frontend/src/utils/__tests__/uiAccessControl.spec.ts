import { describe, expect, it } from 'vitest'
import { normalizeUiBlockedCountries, resolveUiAccessDomainEnabled } from '../uiAccessControl'

describe('normalizeUiBlockedCountries', () => {
  it.each([
    ['', ''],
    [' \t\n ', ''],
    ['CN', 'CN'],
    [' cn, US, cn ,hk ', 'CN,US,HK'],
    ['us,cn,US', 'US,CN'],
    ['AX, BQ, BV, GB, MO, PS, TW, UM, ZW', 'AX,BQ,BV,GB,MO,PS,TW,UM,ZW'],
  ])('normalizes %j to %j', (input, expected) => {
    expect(normalizeUiBlockedCountries(input)).toBe(expected)
  })

  it.each([
    'XX', 'ZZ', 'XK', 'EU', 'UK', 'AN', 'SU', 'CN,,US', 'CN,', ',CN',
    'CN;US', 'CN US', 'CN，US', 'CHN', 'China', 'C N', '1A', '<script>', 'CN\0',
    'ß', 'ſg', 'KR', 'ｃｎ',
  ])('rejects invalid or unassigned country codes in %j', input => {
    expect(normalizeUiBlockedCountries(input)).toBeNull()
  })
})

describe('resolveUiAccessDomainEnabled', () => {
  it.each([
    [{}, false],
    [{ ui_access_domain: '' }, false],
    [{ ui_access_domain: '  ' }, false],
    [{ ui_access_domain: 'example.com' }, true],
    [{ ui_access_domain: 'example.com', ui_access_domain_enabled: false }, false],
    [{ ui_access_domain: 'example.com', ui_access_domain_enabled: true }, true],
    [{ ui_access_domain: '', ui_access_domain_enabled: true }, true],
  ])('resolves legacy or explicit state %j to %j', (settings, expected) => {
    expect(resolveUiAccessDomainEnabled(settings)).toBe(expected)
  })
})
