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

export function isGptImage2Model(model: string): boolean {
  return /^gpt-?image-?2(?:[.-]|$)/i.test(model.trim())
}

export function imageResolutionsForModel(model: string): ImageResolution[] {
  return isGptImage2Model(model) ? ['1K', '2K', '4K'] : ['1K', '2K', '4K', '8K']
}

function fitGptImage2Size(ratio: number[], edge: number): ImageSize {
  const landscape = ratio[0]! >= ratio[1]!
  const aspect = Math.max(...ratio) / Math.min(...ratio)
  if (aspect > 3) throw new RangeError('GPT Image 2 aspect ratio must be between 1:3 and 3:1')
  const minPixels = 655360
  const maxPixels = 8294400
  const targetLong = Math.min(3840, Math.sqrt(maxPixels * aspect), Math.max(edge, Math.sqrt(minPixels * aspect)))
  const targetShort = targetLong / aspect
  let best: { long: number; short: number; distance: number } | undefined
  // Choose the closest 16-pixel grid point that meets both area and aspect limits.
  for (let long = 16; long <= 3840; long += 16) {
    const minShort = Math.ceil(Math.max(minPixels / long, long / 3) / 16) * 16
    const maxShort = Math.floor(Math.min(maxPixels / long, long) / 16) * 16
    if (minShort > maxShort) continue
    const short = Math.min(maxShort, Math.max(minShort, Math.round(targetShort / 16) * 16))
    const distance = (long - targetLong) ** 2 + (short - targetShort) ** 2
    if (!best || distance < best.distance) best = { long, short, distance }
  }
  if (!best) throw new RangeError('Invalid GPT Image 2 dimensions')
  const [width, height] = landscape ? [best.long, best.short] : [best.short, best.long]
  return { width, height, size: `${width}x${height}` }
}

export function calculateImageSize(
  aspectRatio: string,
  resolution: ImageResolution,
  customWidth: number,
  customHeight: number,
  model = '',
): ImageSize {
  const edge = resolutionPixels[resolution]
  if (!Number.isFinite(edge)) throw new RangeError('Invalid image resolution')
  if (!imageResolutionsForModel(model).includes(resolution)) throw new RangeError('Unsupported image resolution for this model')
  const ratio = aspectRatio === 'custom' ? [customWidth, customHeight]
    : /^\d+:\d+$/.test(aspectRatio) ? aspectRatio.split(':').map(Number) : []
  if (ratio.length !== 2 || ratio.some(value => !Number.isSafeInteger(value) || value < 1 || value > 8192)) {
    throw new RangeError('Image ratio must contain positive integers from 1 to 8192')
  }
  if (isGptImage2Model(model)) return fitGptImage2Size(ratio, edge)
  const longest = Math.max(...ratio)
  const shortest = Math.min(...ratio)
  if (longest / shortest > 64) throw new RangeError('Image aspect ratio must be between 1:64 and 64:1')
  const [width, height] = ratio.map(value => Math.max(16, Math.round((value / longest) * edge / 16) * 16))
  return { width, height, size: `${width}x${height}` }
}

export function isWorkbenchImageUrl(value: unknown): boolean {
  return typeof value === 'string' && /^\/api\/v1\/image-workbench\/tasks\/imgtask_[a-f0-9]{32}\/images\/\d+$/.test(value)
}

export function safeImageUrl(value: unknown): string {
  if (typeof value !== 'string' || [...value].some(char => char.charCodeAt(0) < 32 || char.charCodeAt(0) === 127)) return ''
  if (isWorkbenchImageUrl(value)) return value
  try {
    const url = new URL(value)
    if (!['http:', 'https:'].includes(url.protocol) || url.username || url.password) return ''
    return url.href
  } catch {
    return ''
  }
}

export function createImageFilename(taskId: string, imageIndex: number, createdAt: number, imageUrl?: string, contentType?: string): string {
  const date = new Date(createdAt * 1000)
  const datePart = Number.isNaN(date.getTime()) ? '19700101'
    : `${date.getFullYear()}${String(date.getMonth() + 1).padStart(2, '0')}${String(date.getDate()).padStart(2, '0')}`
  let hash = 2166136261
  for (const char of `${taskId}:${imageIndex}`) {
    hash = Math.imul(hash ^ char.charCodeAt(0), 16777619)
  }
  const suffix = ((hash >>> 0) % 2176782336).toString(36).padStart(6, '0')
  const safeUrl = safeImageUrl(imageUrl)
  const extension = safeUrl && !isWorkbenchImageUrl(safeUrl) ? /\.(png|jpe?g|webp|avif|gif)$/i.exec(new URL(safeUrl).pathname)?.[1].toLowerCase() : undefined
  const mimeExtension = /^(image)\/(png|jpeg|webp|avif|gif)$/.exec(contentType || '')?.[2]
  return `${datePart}-${suffix}.${mimeExtension === 'jpeg' ? 'jpg' : mimeExtension || extension || 'png'}`
}
