// 序列帧揭晓渲染器（kind=frames）：线稿 → 成形 → 定格。
//
// 帧是普通 <image>（不是 video 原生组件）：背景质感、层级、混合都由前端
// 掌控——这是它比 video 通用的根本原因（原型结论，已按此落素材）。
//
// 三条纪律：
//   · 递交层先行：挂载即淡入暖纸底（帧序列的纸色），草图在它下面淡出，
//     冷纸 → 暖纸的过渡因此是 cross-fade 而不是硬切；
//   · 先预载再开拍：全部帧 getImageInfo 完成才逐帧切换，切 src 不闪；
//     底下永远垫「上一帧」，某帧解码迟到时看到的是连续的前一帧；
//   · 素材问题不能变成黑屏：首帧失败 / 超时 → 直接回调揭晓海报。
// 定格后停一拍（hold_ms）再揭晓——「定住」要有分量。

import { useEffect, useRef, useState } from 'react'
import Taro from '@tarojs/taro'
import { Image, View } from '@tarojs/components'
import type { FramesParams } from '@zsm/core'
import './frames-reveal.scss'

interface FramesRevealProps {
  params: FramesParams
  onSettled?: () => void
  reduced?: boolean
}

/** 预载兜底：CDN 抖一下不能让用户停在空屏上等 */
const PRELOAD_TIMEOUT_MS = 3000
/** 帧太少说明素材大面积缺失，逐帧播没有意义 */
const MIN_PLAYABLE = 3

export default function FramesReveal({ params, onSettled, reduced = false }: FramesRevealProps) {
  // 预载通过后才知道实际可播的帧列表（失败的帧被剔除，索引随列表走）
  const [frames, setFrames] = useState<string[] | null>(null)
  const [index, setIndex] = useState(0)
  const settledRef = useRef(false)

  useEffect(() => {
    let alive = true
    const timers: ReturnType<typeof setTimeout>[] = []
    const finish = () => {
      if (alive && !settledRef.current) {
        settledRef.current = true
        onSettled?.()
      }
    }

    if (reduced) {
      // 减动效：直接定格最后一帧，稍候即揭晓（元素保持可见）
      setFrames(params.urls)
      setIndex(params.urls.length - 1)
      timers.push(setTimeout(finish, 400))
      return () => {
        alive = false
        timers.forEach(clearTimeout)
      }
    }

    const load = Promise.all(
      params.urls.map(
        (url) =>
          new Promise<boolean>((resolve) => {
            Taro.getImageInfo({ src: url, success: () => resolve(true), fail: () => resolve(false) })
          }),
      ),
    )
    const timeout = new Promise<'timeout'>((resolve) => {
      timers.push(setTimeout(() => resolve('timeout'), PRELOAD_TIMEOUT_MS))
    })

    void Promise.race([load, timeout]).then((result) => {
      if (!alive || settledRef.current) return
      if (!Array.isArray(result)) return finish() // 超时：不硬等，直接揭晓
      const playable = params.urls.filter((_, i) => result[i])
      if (playable.length < Math.min(MIN_PLAYABLE, params.urls.length)) return finish()
      setFrames(playable)
      playable.forEach((_, i) => {
        if (i === 0) return
        timers.push(setTimeout(() => setIndex(i), i * params.intervalMS))
      })
      // 定格后再停一拍，然后揭晓
      timers.push(setTimeout(finish, (playable.length - 1) * params.intervalMS + params.holdMS))
    })

    return () => {
      alive = false
      timers.forEach(clearTimeout)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  const list = frames ?? [params.urls[0]]
  const under = Math.max(0, index - 1)
  return (
    <View className="dm-frames">
      {frames ? (
        <>
          <Image className="dm-frames__img dm-frames__img--under" src={list[under] ?? list[0] ?? ""} mode="aspectFill" />
          <Image className="dm-frames__img" src={list[index] ?? list[0] ?? ""} mode="aspectFill" />
        </>
      ) : null}
    </View>
  )
}
