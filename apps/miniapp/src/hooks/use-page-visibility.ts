// 页面可见性桥：Taro 页面 useDidShow/useDidHide → core useTaskPolling 的
// subscribeVisibility（不可见即暂停轮询，见请求纪律 §4）。
import { useDidHide, useDidShow } from '@tarojs/taro'
import { useEffect, useRef } from 'react'
import type { SubscribeVisibility } from '@zsm/core'

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
  useDidShow(() => {
    if (firstRef.current) {
      firstRef.current = false
      return
    }
    handler()
  })
}

/** 组件树内的可见性订阅也随组件卸载清理。 */
export function useCleanup(fn: () => void) {
  useEffect(() => fn, [fn])
}
