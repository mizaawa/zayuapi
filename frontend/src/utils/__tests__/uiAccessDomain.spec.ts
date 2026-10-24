import { describe, expect, it } from 'vitest'
import { normalizeUiAccessDomain } from '../uiAccessDomain'

describe('normalizeUiAccessDomain', () => {
  it.each([
    ['', ''],
    [' \t\n ', ''],
    ['example.com', 'example.com'],
    ['  EXAMPLE.COM.  ', 'example.com'],
    ['sub-domain.example.com', 'sub-domain.example.com'],
    ['例子.测试', 'xn--fsqu00a.xn--0zwm56d'],
    ['例子.中国', 'xn--fsqu00a.xn--fiqs8s'],
    ['EXAMPLE。COM。', 'example.com'],
    ['BÜCHER.example.', 'xn--bcher-kva.example'],
  ])('normalizes %j to %j', (input, expected) => {
    expect(normalizeUiAccessDomain(input)).toBe(expected)
  })

  it.each([
    'https://example.com', 'http://example.com', '//example.com', 'example.com:443',
    'example.com/path', 'example.com\\path', 'user@example.com', '*.example.com',
    'example.com?x=1', 'example.com#hash', 'example.com%2fpath', 'exam ple.com',
    'example.com..', 'example.com。。', '.example.com', 'example..com', '-example.com', 'example-.com',
    'exa_mple.com', 'localhost', '127.0.0.1', '[::1]', '::1', '123.456', '127.1', '0x7f.1', 'foo.0x1',
    'evil".com', '<script>.com', 'a\x00.example',
    `${'a'.repeat(64)}.example`, `${'a'.repeat(63)}.${'b'.repeat(63)}.${'c'.repeat(63)}.${'d'.repeat(62)}`,
  ])('rejects a non-domain input %j', input => {
    expect(normalizeUiAccessDomain(input)).toBeNull()
  })
})
