// 换装素材预载（spec §3）：
//   链 1  核心 4 张 look 人物图 —— 3s 总闸放行；齐了提前放行，缺/超时置 failed
//   链 2  其余 look 图 + 全部卡片缩略图 —— 纯预热，不挡放行
//   链 3  发型人物图 + 发型卡 —— 懒加载（失败/未就绪 → 发型维度不入池）
//
// 降级纪律（spec §4）：failed → 父组件回落 sketch 巡游；任何一级失败都不
// 该让用户看到空图，宁可不演。
//
// base 为空（M4 CDN 未接线）时全部请求必失败 → 恒回落 sketch，行为安全。

import { useEffect, useRef, useState } from 'react'
import Taro from '@tarojs/taro'
import { HAIRS, LOOKS } from './presets'

/** 预载总闸（spec §3：3s 超时放行/回落，不能让用户停在空白上等） */
const CORE_TIMEOUT_MS = 3000

/** 核心 4 张：原型四类 hero（首屏等待轮必经），其余 look 后台补 */
const CORE_LOOK_KEYS = ['outfit', 'ratio', 'fit', 'occasion']
const LOOK_KEYS = LOOKS.map((l) => l.key)
const HAIR_KEYS = HAIRS.map((h) => h.key)

const figureUrl = (base: string, key: string) => `${base}/color/${key}.png`
const cardUrl = (base: string, key: string) => `${base}/color/card-${key}-v2.jpg`

export interface DressAssetsState {
  /** 主素材就绪 → 可挂载洗牌播放器 */
  ready: boolean
  /** 核心素材缺失/超时 → 父组件回落 sketch */
  failed: boolean
  /** 发型素材就绪；false 时发型维度不入池（洗牌照常） */
  hairReady: boolean
}

function preloadOne(src: string): Promise<void> {
  return Taro.getImageInfo({ src })
    .then(() => undefined)
    .catch(() => {
      throw new Error(`dress asset load failed: ${src}`)
    })
}

export function useDressAssets(base: string, enabled: boolean): DressAssetsState {
  const [ready, setReady] = useState(false)
  const [failed, setFailed] = useState(false)
  const [hairReady, setHairReady] = useState(false)
  // base 变化（脚本下发的 assets.base 接管本地常量）要整体重载：
  // 旧 base 的预载结果对新 base 没有意义。
  const startedBaseRef = useRef<string | null>(null)

  useEffect(() => {
    if (!enabled || startedBaseRef.current === base) return
    startedBaseRef.current = base
    setReady(false)
    setFailed(false)
    setHairReady(false)

    let alive = true
    const coreKeys = CORE_LOOK_KEYS.filter((k) => LOOK_KEYS.includes(k))
    const restKeys = LOOK_KEYS.filter((k) => !coreKeys.includes(k))
    const results = new Map<string, boolean>()

    const runChain = (urls: string[], onDone?: () => void): void => {
      const queue = [...urls]
      const next = (): void => {
        if (!alive) return
        const url = queue.shift()
        if (url === undefined) {
          onDone?.()
          return
        }
        preloadOne(url)
          .then(() => {
            results.set(url, true)
          })
          .catch(() => {
            results.set(url, false)
          })
          .finally(next)
      }
      next()
    }

    const allOk = (urls: string[]) => urls.every((u) => results.get(u) === true)
    const coreUrls = coreKeys.map((k) => figureUrl(base, k))
    // 链 2：其余 look 图 + 全部卡片缩略图（纯预热，不挡放行）
    const warmUrls = [
      ...restKeys.map((k) => figureUrl(base, k)),
      ...LOOK_KEYS.map((k) => cardUrl(base, k)),
      cardUrl(base, 'rule'),
    ]
    // 链 3：发型人物图 + 发型卡（懒加载；就绪前发型维度不入池）
    const hairUrls = [
      ...HAIR_KEYS.map((k) => figureUrl(base, `outfit-${k}`)),
      ...HAIR_KEYS.map((k) => cardUrl(base, `outfit-${k}`)),
    ]

    // 核心 4 张：齐了立即放行并启动后台链；3s 闸兜底（放行或回落，二选一）
    const decide = (): void => {
      if (!alive) return
      if (allOk(coreUrls)) setReady(true)
      else setFailed(true)
    }
    let backgroundStarted = false
    const startBackground = (): void => {
      if (!alive || backgroundStarted) return
      backgroundStarted = true
      runChain(warmUrls)
      runChain(hairUrls, () => {
        if (alive && allOk(hairUrls)) setHairReady(true)
      })
    }
    runChain(coreUrls, () => {
      decide()
      startBackground()
    })
    const gate = setTimeout(decide, CORE_TIMEOUT_MS)
    const kick = setTimeout(startBackground, CORE_TIMEOUT_MS + 1500)

    return () => {
      alive = false
      clearTimeout(gate)
      clearTimeout(kick)
    }
  }, [enabled, base])

  return { ready, failed, hairReady }
}
