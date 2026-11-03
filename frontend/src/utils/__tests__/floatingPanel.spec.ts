import { describe, expect, it } from 'vitest'
import { getAnchoredTooltipPosition, getFloatingPanelPosition } from '@/utils/floatingPanel'

describe('getFloatingPanelPosition', () => {
  it('移动端使用视口安全边距，不再从靠左按钮向屏幕外展开', () => {
    const position = getFloatingPanelPosition(
      { top: 160, right: 148, bottom: 200 },
      393,
      844
    )

    expect(position).toMatchObject({
      top: 208,
      bottom: null,
      left: 16,
      width: 320
    })
    expect(position.left + position.width).toBeLessThanOrEqual(393 - 16)
  })

  it('桌面端与按钮右侧对齐', () => {
    const position = getFloatingPanelPosition(
      { top: 100, right: 1000, bottom: 140 },
      1280,
      900
    )

    expect(position.left).toBe(680)
    expect(position.width).toBe(320)
  })

  it('按钮下方空间不足时改为向上展开', () => {
    const position = getFloatingPanelPosition(
      { top: 700, right: 1000, bottom: 740 },
      1280,
      800
    )

    expect(position.top).toBeNull()
    expect(position.bottom).toBe(108)
    expect(position.maxHeight).toBe(560)
  })
})

describe('getAnchoredTooltipPosition', () => {
  const tooltip = { width: 180, height: 90 }

  it('居中显示在锚点上方，箭头指向锚点中心', () => {
    const position = getAnchoredTooltipPosition(
      { top: 400, bottom: 422, left: 500, width: 6 },
      tooltip,
      1440,
      900
    )

    expect(position).toEqual({ top: 302, left: 413, placement: 'top', arrowLeft: 90 })
  })

  it('靠近视口边缘时收进视口，箭头仍对准锚点', () => {
    const right = getAnchoredTooltipPosition(
      { top: 400, bottom: 422, left: 1384, width: 6 },
      tooltip,
      1440,
      900
    )
    expect(right.left).toBe(1252)
    expect(right.arrowLeft).toBe(135)

    const left = getAnchoredTooltipPosition(
      { top: 400, bottom: 422, left: 2, width: 4 },
      tooltip,
      1440,
      900
    )
    expect(left.left).toBe(8)
    expect(left.arrowLeft).toBe(12)
  })

  it('上方放不下时翻到锚点下方', () => {
    const position = getAnchoredTooltipPosition(
      { top: 40, bottom: 62, left: 200, width: 6 },
      tooltip,
      390,
      844
    )

    expect(position.placement).toBe('bottom')
    expect(position.top).toBe(70)
  })
})
