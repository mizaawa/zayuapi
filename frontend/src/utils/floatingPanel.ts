export interface FloatingPanelPosition {
  top: number | null
  bottom: number | null
  left: number
  width: number
  maxHeight: number
}

export interface FloatingPanelOptions {
  viewportPadding?: number
  gap?: number
  maxWidth?: number
  maxHeightRatio?: number
  mobileBreakpoint?: number
  minComfortableHeight?: number
}

/**
 * 计算挂载到 body 的浮层位置，避免触发按钮靠近视口边缘时浮层被挤到屏幕外。
 */
export const getFloatingPanelPosition = (
  triggerRect: Pick<DOMRect, 'top' | 'right' | 'bottom'>,
  viewportWidth: number,
  viewportHeight: number,
  options: FloatingPanelOptions = {}
): FloatingPanelPosition => {
  const viewportPadding = options.viewportPadding ?? 16
  const gap = options.gap ?? 8
  const maxWidth = options.maxWidth ?? 320
  const maxHeightRatio = options.maxHeightRatio ?? 0.7
  const mobileBreakpoint = options.mobileBreakpoint ?? 768
  const minComfortableHeight = options.minComfortableHeight ?? 240

  const availableWidth = Math.max(0, viewportWidth - viewportPadding * 2)
  const width = Math.min(maxWidth, availableWidth)
  const left = viewportWidth < mobileBreakpoint
    ? viewportPadding
    : Math.max(
        viewportPadding,
        Math.min(triggerRect.right - width, viewportWidth - width - viewportPadding)
      )

  const preferredMaxHeight = Math.max(0, Math.floor(viewportHeight * maxHeightRatio))
  const spaceBelow = Math.max(0, viewportHeight - triggerRect.bottom - gap - viewportPadding)
  const spaceAbove = Math.max(0, triggerRect.top - gap - viewportPadding)
  const openAbove = spaceBelow < Math.min(minComfortableHeight, preferredMaxHeight) && spaceAbove > spaceBelow
  const maxHeight = Math.min(preferredMaxHeight, openAbove ? spaceAbove : spaceBelow)

  return {
    top: openAbove ? null : triggerRect.bottom + gap,
    bottom: openAbove ? viewportHeight - triggerRect.top + gap : null,
    left,
    width,
    maxHeight
  }
}

export interface AnchoredTooltipPosition {
  top: number
  left: number
  placement: 'top' | 'bottom'
  /** 箭头尖端相对提示框左边缘的水平偏移。 */
  arrowLeft: number
}

export interface AnchoredTooltipOptions {
  viewportPadding?: number
  gap?: number
  /** 箭头离提示框左右边缘的最小距离，避免落到圆角上。 */
  arrowInset?: number
}

/**
 * 计算贴着一个小锚点（例如状态时间线里的一格）显示的提示框位置：
 * 默认在锚点正上方居中，上方放不下时翻到下方；水平方向夹在视口内，
 * 箭头始终指向锚点中心。
 */
export const getAnchoredTooltipPosition = (
  anchor: Pick<DOMRect, 'top' | 'bottom' | 'left' | 'width'>,
  tooltip: { width: number; height: number },
  viewportWidth: number,
  viewportHeight: number,
  options: AnchoredTooltipOptions = {}
): AnchoredTooltipPosition => {
  const viewportPadding = options.viewportPadding ?? 8
  const gap = options.gap ?? 8
  const arrowInset = options.arrowInset ?? 12

  const center = anchor.left + anchor.width / 2
  const maxLeft = Math.max(viewportPadding, viewportWidth - tooltip.width - viewportPadding)
  const left = Math.min(Math.max(center - tooltip.width / 2, viewportPadding), maxLeft)

  const spaceAbove = anchor.top - gap - viewportPadding
  const spaceBelow = viewportHeight - anchor.bottom - gap - viewportPadding
  const placement = spaceAbove >= tooltip.height || spaceAbove >= spaceBelow ? 'top' : 'bottom'
  const top = placement === 'top' ? anchor.top - gap - tooltip.height : anchor.bottom + gap

  const arrowLeft = Math.min(
    Math.max(center - left, arrowInset),
    Math.max(arrowInset, tooltip.width - arrowInset)
  )

  return { top, left, placement, arrowLeft }
}
