import { describe, expect, it } from 'vitest'
import { calculateImageSize, createImageFilename, imageResolutionsForModel, isGptImage2Model, safeImageUrl, type ImageResolution } from '@/utils/imageWorkbench'

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

describe('GPT Image 2 dimensions', () => {
  it.each(['gpt-image-2', 'gptimage2', 'GPT-IMAGE2', 'gpt-image-2-2026-04-21', 'gpt-image-2.5'])('recognizes the model and its aliases: %s', model => {
    expect(isGptImage2Model(model)).toBe(true)
    expect(imageResolutionsForModel(model)).toEqual(['1K', '2K', '4K'])
    expect(() => calculateImageSize('1:1', '8K', 1, 1, model)).toThrow(RangeError)
  })

  it.each(['gpt-image-1.5', 'gpt-image-20', 'grok-imagine', 'banana-2', 'custom-image'])('preserves resolution support for other models: %s', model => {
    expect(isGptImage2Model(model)).toBe(false)
    expect(imageResolutionsForModel(model)).toEqual(['1K', '2K', '4K', '8K'])
    expect(calculateImageSize('16:9', '8K', 1, 1, model).size).toBe('8192x4608')
  })

  it.each([
    ['1:1', '1K', '1024x1024'],
    ['16:9', '1K', '1088x608'],
    ['9:16', '1K', '608x1088'],
    ['16:9', '2K', '2048x1152'],
    ['1:1', '4K', '2880x2880'],
    ['16:9', '4K', '3840x2160'],
    ['9:16', '4K', '2160x3840'],
  ] as const)('fits %s at %s into the supported pixel range', (ratio, resolution, size) => {
    expect(calculateImageSize(ratio, resolution, 1, 1, 'gpt-image-2').size).toBe(size)
  })

  it('keeps every preset and custom boundary within the protocol constraints', () => {
    for (const resolution of imageResolutionsForModel('gpt-image-2')) {
      for (const [ratio, customWidth, customHeight] of [
        ['1:1', 1, 1], ['4:3', 1, 1], ['3:4', 1, 1], ['16:9', 1, 1], ['9:16', 1, 1],
        ['custom', 3, 1], ['custom', 1, 3], ['custom', 7, 5], ['custom', 8192, 8191],
      ] as const) {
        const { width, height } = calculateImageSize(ratio, resolution, customWidth, customHeight, 'gptimage2')
        expect(width % 16).toBe(0)
        expect(height % 16).toBe(0)
        expect(Math.max(width, height)).toBeLessThanOrEqual(3840)
        expect(Math.max(width, height) / Math.min(width, height)).toBeLessThanOrEqual(3)
        expect(width * height).toBeGreaterThanOrEqual(655360)
        expect(width * height).toBeLessThanOrEqual(8294400)
      }
    }
  })

  it.each([[4, 1], [1, 4], [8192, 1]])('rejects unsupported custom aspect ratios %s:%s', (width, height) => {
    expect(() => calculateImageSize('custom', '1K', width, height, 'gptimage2')).toThrow(RangeError)
  })
})

describe('safe image URLs', () => {
  it('accepts only the authenticated workbench image route for relative URLs', () => {
    const url = '/api/v1/image-workbench/tasks/imgtask_' + 'a'.repeat(32) + '/images/0'
    expect(safeImageUrl(url)).toBe(url)
    expect(safeImageUrl(url + '?redirect=https://example.com')).toBe('')
    expect(safeImageUrl(url + '/../1')).toBe('')
    expect(createImageFilename('task', 0, 0, url)).toMatch(/\.png$/)
  })
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

  it.each([['image/png', 'png'], ['image/jpeg', 'jpg'], ['image/webp', 'webp']])('keeps the real %s extension for protected image URLs', (contentType, extension) => {
    const url = '/api/v1/image-workbench/tasks/imgtask_' + 'a'.repeat(32) + '/images/0'
    expect(createImageFilename('task', 0, createdAt, url, contentType)).toMatch(new RegExp(`\\.${extension}$`))
  })

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
