export const LINE_CENTER_TOLERANCE_PX = 24
export const MAX_LINE_SCROLL_ATTEMPTS = 8

type VerticalRect = Pick<DOMRect, 'top' | 'bottom'>

export function isLineCentered(line: VerticalRect, container: VerticalRect) {
  const lineCenter = (line.top + line.bottom) / 2
  const containerCenter = (container.top + container.bottom) / 2
  return Math.abs(lineCenter - containerCenter) <= LINE_CENTER_TOLERANCE_PX
}
