export const followBottomThreshold = 48

export function isAtScrollBottom({ scrollHeight, scrollTop, clientHeight }: Pick<HTMLElement, 'scrollHeight' | 'scrollTop' | 'clientHeight'>) {
  return scrollHeight - scrollTop - clientHeight < followBottomThreshold
}
