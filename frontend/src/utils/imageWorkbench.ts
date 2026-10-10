export type ImageResolution = '1K' | '2K' | '4K' | '8K'

export interface ImageSize {
  width: number
  height: number
  size: string
}

const resolutionPixels: Record<ImageResolution, number> = {
  '1K': 1024,
  '2K': 2048,
  '4K': 4096,
  '8K': 8192,
}

export function calculateImageSize(
  aspectRatio: string,
  resolution: ImageResolution,
  customWidth: number,
  customHeight: number,
): ImageSize {
  const edge = resolutionPixels[resolution]
  if (!Number.isFinite(edge)) throw new RangeError('Invalid image resolution')
  const ratio = aspectRatio === 'custom' ? [customWidth, customHeight]
    : /^\d+:\d+$/.test(aspectRatio) ? aspectRatio.split(':').map(Number) : []
  if (ratio.length !== 2 || ratio.some(value => !Number.isSafeInteger(value) || value < 1 || value > 8192)) {
    throw new RangeError('Image ratio must contain positive integers from 1 to 8192')
  }
  const longest = Math.max(...ratio)
  const shortest = Math.min(...ratio)
  if (longest / shortest > 64) throw new RangeError('Image aspect ratio must be between 1:64 and 64:1')
  const [width, height] = ratio.map(value => Math.max(16, Math.round((value / longest) * edge / 16) * 16))
  return { width, height, size: `${width}x${height}` }
}

export function safeImageUrl(value: unknown): string {
  if (typeof value !== 'string' || [...value].some(char => char.charCodeAt(0) < 32 || char.charCodeAt(0) === 127)) return ''
  try {
    const url = new URL(value)
    if (!['http:', 'https:'].includes(url.protocol) || url.username || url.password) return ''
    return url.href
  } catch {
    return ''
  }
}

export function createImageFilename(taskId: string, imageIndex: number, createdAt: number, imageUrl?: string): string {
  const date = new Date(createdAt * 1000)
  const datePart = Number.isNaN(date.getTime()) ? '19700101'
    : `${date.getFullYear()}${String(date.getMonth() + 1).padStart(2, '0')}${String(date.getDate()).padStart(2, '0')}`
  let hash = 2166136261
  for (const char of `${taskId}:${imageIndex}`) {
    hash = Math.imul(hash ^ char.charCodeAt(0), 16777619)
  }
  const suffix = ((hash >>> 0) % 2176782336).toString(36).padStart(6, '0')
  const safeUrl = safeImageUrl(imageUrl)
  const extension = safeUrl ? /\.(png|jpe?g|webp|avif|gif)$/i.exec(new URL(safeUrl).pathname)?.[1].toLowerCase() : undefined
  return `${datePart}-${suffix}.${extension || 'png'}`
}
