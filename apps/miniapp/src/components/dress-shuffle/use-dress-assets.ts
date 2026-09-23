// 换装素材预载（spec §3）。
//
//   核心 4 张 look 人物图 —— 并发拉取，3s 总闸：齐了提前放行，缺/超时 failed
//   其余 look 图 + 卡片缩略图 —— 纯预热，不挡放行
//   发型人物图 + 发型卡 —— 后台补（就绪前发型维度不入池）
//
// 三种状态（home 用它决定播什么）：
//   pending 预载中 —— 播洗牌自己的静态前奏态（不再用旧巡游顶位，避免风格跳变）
//   ready   就绪   —— 洗牌全速
//   failed  缺/超时 —— 回落 sketch 巡游 + 帧揭晓（spec §4 降级矩阵）
//
// 结果与在途请求都在模块级：App 启动就能预热（warmDressAssets），首页挂载时
// 直接复用，把首屏那 1~3s 顶位压掉。
//
// base 为空（未接线）时全部请求必失败 → 恒 failed，行为安全。

import { useEffect, useRef, useState } from 'react'
import Taro from '@tarojs/taro'
import { HAIRS, LOOKS } from './presets'

/** 预载总闸（spec §3：3s 超时放行/回落，不能让用户停在空白上等） */
const CORE_TIMEOUT_MS = 3000
/** 并发度：串行会让首屏顶位拖到 2~3s，两张并行已在 3s 闸内明显更快 */
const CONCURRENCY = 2

/** 核心 4 张：原型四类 hero（首屏必经），其余 look 后台补 */
const CORE_LOOK_KEYS = ['outfit', 'ratio', 'fit', 'occasion']
const LOOK_KEYS = LOOKS.map((l) => l.key)
const HAIR_KEYS = HAIRS.map((h) => h.key)

const figureUrl = (base: string, key: string) => `${base}/color/${key}.png`
const cardUrl = (base: string, key: string) => `${base}/color/card-${key}-v2.jpg`

/** 模块级结果表：App 预热与首页 hook 共用同一份（不重复下载） */
const results = new Map<string, boolean>()

export interface DressAssetsState {
  /** 主素材就绪 → 洗牌全速 */
  ready: boolean
  /** 缺素材/超时 → 父组件回落 sketch */
  failed: boolean
  /** 预载进行中：播洗牌静态前奏，不切旧巡游 */
  pending: boolean
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

/** 并发拉取一批；结果落模块级表（成功 true / 失败 false） */
async function runPool(urls: string[]): Promise<void> {
  const queue = urls.filter((url) => !results.has(url))
  const workers = Array.from({ length: Math.min(CONCURRENCY, queue.length) }, async () => {
    for (;;) {
      const url = queue.shift()
      if (url === undefined) return
      try {
        await preloadOne(url)
        results.set(url, true)
      } catch {
        results.set(url, false)
      }
    }
  })
  await Promise.all(workers)
}

function coreUrls(base: string): string[] {
  return CORE_LOOK_KEYS.filter((k) => LOOK_KEYS.includes(k)).map((k) => figureUrl(base, k))
}

function warmUrls(base: string): string[] {
  return [
    ...LOOK_KEYS.filter((k) => !CORE_LOOK_KEYS.includes(k)).map((k) => figureUrl(base, k)),
    ...LOOK_KEYS.map((k) => cardUrl(base, k)),
    cardUrl(base, 'rule'),
  ]
}

function hairUrls(base: string): string[] {
  return [
    ...HAIR_KEYS.map((k) => figureUrl(base, `outfit-${k}`)),
    ...HAIR_KEYS.map((k) => cardUrl(base, `outfit-${k}`)),
  ]
}

const allOk = (urls: string[]) => urls.every((u) => results.get(u) === true)
const anyDone = (urls: string[]) => urls.some((u) => results.has(u))

/**
 * App 启动预热：上一次服务端给的 variant 是 dress 时，在首页挂载前就开始拉
 * 素材。幂等——模块级结果表 + 队列去重，重复调用不会重复下载。
 */
export function warmDressAssets(base: string): void {
  if (!base) return
  if (anyDone(coreUrls(base))) return
  void runPool(coreUrls(base)).then(() => {
    void runPool([...warmUrls(base), ...hairUrls(base)])
  })
}

export function useDressAssets(base: string, enabled: boolean): DressAssetsState {
  const [ready, setReady] = useState(false)
  const [failed, setFailed] = useState(false)
  const [hairReady, setHairReady] = useState(false)
  // base 变化（脚本下发的 assets.base 接管本地常量）要重新判定：
  // 旧 base 的预载结果对新 base 没有意义。
  const startedBaseRef = useRef<string | null>(null)

  useEffect(() => {
    if (!enabled || startedBaseRef.current === base) return
    startedBaseRef.current = base
    setReady(false)
    setFailed(false)
    setHairReady(false)

    let alive = true
    const core = coreUrls(base)
    const decide = (): void => {
      if (!alive) return
      if (allOk(core)) setReady(true)
      else if (core.some((u) => results.get(u) === false)) setFailed(true)
    }
    // 已被 App 预热拉完 → 直接放行，不再等闸
    if (allOk(core)) {
      setReady(true)
    } else {
      void runPool(core).then(() => {
        if (!alive) return
        decide()
        void runPool([...warmUrls(base), ...hairUrls(base)])
      })
      const gate = setTimeout(decide, CORE_TIMEOUT_MS)
      return () => {
        alive = false
        clearTimeout(gate)
      }
    }
    void runPool([...warmUrls(base), ...hairUrls(base)]).then(() => {
      if (alive && allOk(hairUrls(base))) setHairReady(true)
    })
    return () => {
      alive = false
    }
  }, [enabled, base])

  // 发型可能在别的入口（或预热）已经拉完
  useEffect(() => {
    if (!enabled || !base) return
    if (allOk(hairUrls(base))) setHairReady(true)
  }, [enabled, base, ready, failed])

  return { ready, failed, pending: enabled && !ready && !failed, hairReady }
}
