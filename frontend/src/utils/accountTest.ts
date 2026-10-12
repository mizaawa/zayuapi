import { safeImageUrl } from '@/utils/imageWorkbench'

export function isAccountTestImageModel(value: unknown): boolean {
  if (typeof value !== 'string') return false
  const model = value.trim().toLowerCase()
  return model.startsWith('gpt-image-') || model.startsWith('grok-imagine-image') ||
    (model.startsWith('gemini-') && model.includes('-image')) ||
    ['gpt-2.0', 'gpt-2.5-fast', 'gpt-2.5-pro', 'gpt-2.0-4k', 'banana-2', 'banana-pro',
      'grok-imagine', 'grok-imagine-edit', 'dall-e-2', 'dall-e-3'].includes(model)
}

export function safeAccountTestImageUrl(value: unknown): string {
  if (typeof value !== 'string') return ''
  if (/^data:image\/(png|jpeg|webp|gif);base64,[A-Za-z0-9+/]+={0,2}$/.test(value)) return value
  return safeImageUrl(value)
}
