import { useMemo, useState, type UIEvent } from 'react'

interface VirtualWindowOptions {
  itemCount: number
  itemSize: number
  viewportSize: number
  overscan?: number
}

export interface VirtualWindow {
  startIndex: number
  endIndex: number
  beforeHeight: number
  afterHeight: number
  viewportHeight: number
  onScroll: (event: UIEvent<HTMLElement>) => void
}

export function useVirtualWindow({
  itemCount,
  itemSize,
  viewportSize,
  overscan = 6,
}: VirtualWindowOptions): VirtualWindow {
  const [scrollTop, setScrollTop] = useState(0)

  return useMemo(() => {
    if (itemCount === 0) {
      return {
        startIndex: 0,
        endIndex: 0,
        beforeHeight: 0,
        afterHeight: 0,
        viewportHeight: 0,
        onScroll: (event: UIEvent<HTMLElement>) => setScrollTop(event.currentTarget.scrollTop),
      }
    }

    const viewportHeight = Math.min(viewportSize, itemCount * itemSize)
    const visibleCount = Math.ceil(viewportHeight / itemSize)
    const maxFirstVisible = Math.max(0, itemCount - visibleCount)
    const firstVisible = Math.min(Math.floor(scrollTop / itemSize), maxFirstVisible)
    const startIndex = Math.max(0, firstVisible - overscan)
    const endIndex = Math.min(itemCount, startIndex + visibleCount + overscan * 2 + 1)

    return {
      startIndex,
      endIndex,
      beforeHeight: startIndex * itemSize,
      afterHeight: (itemCount - endIndex) * itemSize,
      viewportHeight,
      onScroll: (event: UIEvent<HTMLElement>) => setScrollTop(event.currentTarget.scrollTop),
    }
  }, [itemCount, itemSize, overscan, scrollTop, viewportSize])
}
