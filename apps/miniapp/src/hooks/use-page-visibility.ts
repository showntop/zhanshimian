// 页面可见性桥：Taro 页面 useDidShow/useDidHide → core useTaskPolling 的
// subscribeVisibility（不可见即暂停轮询，见请求纪律 §4）。
// 切 tab / 回页防闪屏：useShowOnce 跳过首次 onShow；usePageClass 在入场动画
// 播完后加 page--settled，避免 fade-up 从透明重播把已渲染图片藏掉。
import { useDidHide, useDidShow } from '@tarojs/taro'
import { useEffect, useRef, useState } from 'react'
import type { SubscribeVisibility } from '@zsm/core'
import { durations } from '@zsm/design'

/** fade-up 520ms + delay-3 240ms，入场结束后才钉住，避免中途切走再回来重播。 */
const SETTLE_MS = durations.fadeUp + 240

export function usePageVisibility(): SubscribeVisibility {
  const listenersRef = useRef(new Set<(visible: boolean) => void>())

  useDidShow(() => {
    listenersRef.current.forEach((cb) => cb(true))
  })
  useDidHide(() => {
    listenersRef.current.forEach((cb) => cb(false))
  })

  return (cb) => {
    listenersRef.current.add(cb)
    return () => {
      listenersRef.current.delete(cb)
    }
  }
}

/** tab 页防重复加载：onShow 时跳过首次（onLoad 后紧跟的 show）。 */
export function useShowOnce(handler: () => void) {
  const firstRef = useRef(true)
  const handlerRef = useRef(handler)
  handlerRef.current = handler
  useDidShow(() => {
    if (firstRef.current) {
      firstRef.current = false
      return
    }
    handlerRef.current()
  })
}

/** 组件树内的可见性订阅也随组件卸载清理。 */
export function useCleanup(fn: () => void) {
  useEffect(() => fn, [fn])
}

/** 跨 tab 重挂也记住已入场，避免 Taro 重挂页面时 fade-up 再从透明播。 */
const settledKeys = new Set<string>()

/** 内容首次上屏并播完入场后为 true；骨架/空载期间不开始计时。 */
export function usePageSettled(ready: boolean, persistKey = ''): boolean {
  const [settled, setSettled] = useState(() => Boolean(persistKey && settledKeys.has(persistKey)))
  useEffect(() => {
    if (settled) {
      if (persistKey) settledKeys.add(persistKey)
      return
    }
    if (!ready) return
    const timer = setTimeout(() => {
      if (persistKey) settledKeys.add(persistKey)
      setSettled(true)
    }, SETTLE_MS)
    return () => clearTimeout(timer)
  }, [ready, settled, persistKey])
  return settled
}

/** 页面根 class：`page` + 可选修饰 + 入场结束后的 `page--settled`。 */
export function usePageClass(ready: boolean, extra = '', persistKey = extra || 'page'): string {
  return usePageShell(ready, extra, persistKey).pageClass
}

/**
 * 入场 class 必须在播完后从 DOM 拿掉。微信切 tab 会重播仍挂在节点上的
 * CSS animation（both + from 透明），只靠覆盖 animation:none 压不住。
 */
export function usePageShell(ready: boolean, extra = '', persistKey = extra || 'page') {
  const settled = usePageSettled(ready, persistKey)
  const pageClass = ['page', extra, settled ? 'page--settled' : ''].filter(Boolean).join(' ')
  const enter = (delay?: 1 | 2 | 3) => {
    if (settled) return ''
    return delay ? `fade-up delay-${delay}` : 'fade-up'
  }
  return { pageClass, enter, settled }
}
