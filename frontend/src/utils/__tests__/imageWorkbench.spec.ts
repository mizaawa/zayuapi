import { describe, expect, it } from 'vitest'
import { calculateImageSize, createImageFilename, safeImageUrl, type ImageResolution } from '@/utils/imageWorkbench'

describe('image workbench dimensions', () => {
  it.each([
    ['1:1', '1K', 1024, 1024],
    ['4:3', '2K', 2048, 1536],
    ['3:4', '4K', 3072, 4096],
    ['16:9', '8K', 8192, 4608],
    ['9:16', '1K', 576, 1024],
  ] as const)('scales %s at %s using the longest edge', (ratio, resolution, width, height) => {
    expect(calculateImageSize(ratio, resolution, 1, 1)).toEqual({ width, height, size: `${width}x${height}` })
  })

  it('rounds a custom ratio to the closest multiple of 16 without exceeding the selected edge', () => {
    expect(calculateImageSize('custom', '1K', 7, 5)).toEqual({ width: 1024, height: 736, size: '1024x736' })
    expect(calculateImageSize('custom', '1K', 5, 7)).toEqual({ width: 736, height: 1024, size: '736x1024' })
  })

  it.each([0, -1, 1.5, NaN, Infinity, 8193])('rejects invalid custom ratios: %s', (value) => {
    expect(() => calculateImageSize('custom', '1K', value, 1)).toThrow(RangeError)
    expect(() => calculateImageSize('custom', '1K', 1, value)).toThrow(RangeError)
  })

  it.each(['1:0', 'NaN:1', '1:1:1', '1.5:1', '1e2:1', '', '65:1'])('rejects invalid aspect ratio %s', ratio => {
    expect(() => calculateImageSize(ratio, '1K', 1, 1)).toThrow(RangeError)
  })

  it.each(['3K', '', 'toString', '__proto__'])('rejects invalid runtime resolutions instead of returning NaN: %s', resolution => {
    expect(() => calculateImageSize('1:1', resolution as ImageResolution, 1, 1)).toThrow(RangeError)
  })
})

describe('safe image URLs', () => {
  it.each(['https://cdn.example.com/image.png?token=abc', 'http://localhost:3000/image.png'])('allows an absolute image URL: %s', value => {
    expect(safeImageUrl(value)).toBe(value)
  })

  it.each([
    'javascript:alert(1)', 'data:image/svg+xml,<svg/>', 'blob:https://example.com/test',
    '//example.com/image.png', '/image.png', 'https://user:secret@example.com/image.png',
    'https://example.com/\nimage.png', null, undefined, {},
  ])('rejects unsafe or malformed image URL: %s', value => {
    expect(safeImageUrl(value)).toBe('')
  })
})

describe('image filenames', () => {
  const createdAt = new Date(2026, 9, 10, 12).getTime() / 1000

  it('uses a stable per-image six-character suffix and the generation date', () => {
    const first = createImageFilename('task-123', 0, createdAt, 'https://cdn.example.com/result.webp?token=1')
    expect(first).toMatch(/^20261010-[a-z0-9]{6}\.webp$/)
    expect(createImageFilename('task-123', 0, createdAt, 'https://cdn.example.com/result.webp?token=2')).toBe(first)
    expect(createImageFilename('task-123', 1, createdAt, 'https://cdn.example.com/result.webp')).not.toBe(first)
  })

  it.each(['https://cdn.example.com/image', 'https://cdn.example.com/image.exe', 'javascript:alert(1)', undefined])(
    'falls back to a PNG filename when an extension is unavailable or unsafe: %s', url => {
      expect(createImageFilename('task-123', 0, createdAt, url)).toMatch(/\.png$/)
    },
  )
})
