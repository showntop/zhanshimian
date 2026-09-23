// 换装素材预载（spec §3）+ 弱网对策。
//
// 三条硬约束：
//   ① 首屏只拉「必需」：4 张 hero 人物图 + 4 张核心卡；其余后台补，不抢带宽
//   ② 下载即落本地（downloadFile + saveFile）：二次进入直接读本地文件，
//      弱网/断网也能出人物、能洗牌——这是「一进去就空白」的根本解法
//   ③ 预载中不判失败：停在洗牌自己的静态前奏（hold），到容忍上限才回落旧线
//
// 状态：pending 预载中（hold）/ ready 就绪 / failed 缺素材或超容忍（回落 sketch）
//
// base 为空（未接线）时全部请求必失败 → failed，行为安全。

import { useEffect, useRef, useState } from 'react'
import Taro from '@tarojs/taro'
import { HAIRS, LOOKS } from './presets'

/** 首屏容忍：弱网下 3s 太紧（3G 拉 4 张 60KB 图经常超时），放宽到 6s */
const CORE_TIMEOUT_MS = 6000
/** 并发度：并行两张，串行会把首屏拖到 2~3s */
const CONCURRENCY = 2

/** 首屏必需的 4 张 hero 人物图（原型四类） */
const CORE_LOOK_KEYS = ['outfit', 'ratio', 'fit', 'occasion']
const LOOK_KEYS = LOOKS.map((l) => l.key)
const HAIR_KEYS = HAIRS.map((h) => h.key)

const figureUrl = (base: string, key: string) => `${base}/color/${key}.png`
const cardUrl = (base: string, key: string) => `${base}/color/card-${key}-v2.jpg`

/** 本地缓存索引：远端 URL → 本地文件路径（storage 持久化） */
const CACHE_KEY = 'zsm_dress_assets_v1'
const localOf = new Map<string, string>()
let cacheLoaded = false

function loadCache(): void {
  if (cacheLoaded) return
  cacheLoaded = true
  try {
    const raw = Taro.getStorageSync(CACHE_KEY)
    if (raw && typeof raw === 'object') {
      for (const [url, path] of Object.entries(raw as Record<string, unknown>)) {
        if (typeof path === 'string' && path) localOf.set(url, path)
      }
    }
  } catch {
    // 索引丢了不影响：重新下载即可
  }
}

function persistCache(): void {
  try {
    Taro.setStorageSync(CACHE_KEY, Object.fromEntries(localOf))
  } catch {
    // 存不下就退化成每次重新下载
  }
}

function localUsable(path: string): boolean {
  try {
    const fsm = Taro.getFileSystemManager()
    fsm.accessSync(path)
    return true
  } catch {
    return false
  }
}

/** 优先返回本地文件路径；未缓存时返回远端 URL（渲染同时触发下载） */
export function resolveDressAsset(url: string): string {
  loadCache()
  const local = localOf.get(url)
  return local && localUsable(local) ? local : url
}

async function preloadOne(url: string): Promise<void> {
  await Taro.getImageInfo({ src: url })
}

/** 下载并落本地（失败可容忍：远端 URL 仍可用，只是下次还得重下） */
async function saveLocal(url: string): Promise<void> {
  const downloaded = await Taro.downloadFile({ url })
  const temp = downloaded.tempFilePath
  if (!temp) return
  const fsm = Taro.getFileSystemManager()
  const saved = await new Promise<string>((resolve, reject) => {
    fsm.saveFile({ tempFilePath: temp, success: (r) => resolve(r.savedFilePath), fail: reject })
  })
  localOf.set(url, saved)
  persistCache()
}

export interface DressAssetsState {
  ready: boolean
  failed: boolean
  /** 预载进行中：播洗牌静态前奏，不切旧巡游 */
  pending: boolean
  hairReady: boolean
  /** 远端路径 → 可直接渲染的地址（本地优先） */
  resolve: (url: string) => string
}

function coreUrls(base: string): string[] {
  return CORE_LOOK_KEYS.filter((k) => LOOK_KEYS.includes(k)).map((k) => figureUrl(base, k))
}

/** 首屏卡面：核心 4 张 + 腰线规则卡（其余卡按需加载，都是十几 KB） */
function coreCardUrls(base: string): string[] {
  return [...CORE_LOOK_KEYS.filter((k) => LOOK_KEYS.includes(k)).map((k) => cardUrl(base, k)), cardUrl(base, 'rule')]
}

function warmUrls(base: string): string[] {
  return [
    ...LOOK_KEYS.filter((k) => !CORE_LOOK_KEYS.includes(k)).map((k) => figureUrl(base, k)),
    ...LOOK_KEYS.filter((k) => !CORE_LOOK_KEYS.includes(k)).map((k) => cardUrl(base, k)),
  ]
}

function hairUrls(base: string): string[] {
  return [
    ...HAIR_KEYS.map((k) => figureUrl(base, `outfit-${k}`)),
    ...HAIR_KEYS.map((k) => cardUrl(base, `outfit-${k}`)),
  ]
}

const allCached = (urls: string[]) => urls.every((u) => {
  const local = localOf.get(u)
  return Boolean(local && localUsable(local))
})

async function runPool(urls: string[], withLocalSave: boolean): Promise<void> {
  const queue = urls.filter((u) => !allCached([u]))
  const workers = Array.from({ length: Math.min(CONCURRENCY, queue.length) }, async () => {
    for (;;) {
      const url = queue.shift()
      if (url === undefined) return
      try {
        await preloadOne(url)
      } catch {
        continue // 预热失败不阻塞；判定交给下面的 allCached/超时
      }
      if (withLocalSave) {
        try {
          await saveLocal(url)
        } catch {
          // 落本地失败：仍然可用（远端 URL 已能渲染）
        }
      }
    }
  })
  await Promise.all(workers)
}

/**
 * App 启动预热：上一次服务端给的 variant 是 dress 时，在首页挂载前就开始拉
 * 素材。幂等（本地已缓存的直接跳过）。
 */
export function warmDressAssets(base: string): void {
  if (!base) return
  loadCache()
  void runPool([...coreUrls(base), ...coreCardUrls(base)], true).then(() => {
    void runPool([...warmUrls(base), ...hairUrls(base)], true)
  })
}

export function useDressAssets(base: string, enabled: boolean): DressAssetsState {
  const [ready, setReady] = useState(false)
  const [failed, setFailed] = useState(false)
  const [hairReady, setHairReady] = useState(false)
  const startedBaseRef = useRef<string | null>(null)
  loadCache()

  useEffect(() => {
    if (!enabled || startedBaseRef.current === base) return
    startedBaseRef.current = base
    setReady(false)
    setFailed(false)
    setHairReady(false)

    let alive = true
    const core = coreUrls(base)
    const cards = coreCardUrls(base)

    // 本地已全部命中 → 立即就绪，不走网络
    if (allCached(core)) {
      setReady(true)
      void runPool([...cards, ...warmUrls(base)], true).then(() => {
        void runPool(hairUrls(base), true).then(() => {
          if (alive && allCached(hairUrls(base))) setHairReady(true)
        })
      })
      if (allCached(hairUrls(base))) setHairReady(true)
      return () => {
        alive = false
      }
    }

    const decide = (): void => {
      if (!alive) return
      if (allCached(core)) setReady(true)
      else setFailed(true)
    }
    void runPool([...core, ...cards], true).then(() => {
      if (!alive) return
      decide()
      void runPool(warmUrls(base), true).then(() => {
        void runPool(hairUrls(base), true).then(() => {
          if (alive && allCached(hairUrls(base))) setHairReady(true)
        })
      })
    })
    const gate = setTimeout(decide, CORE_TIMEOUT_MS)
    return () => {
      alive = false
      clearTimeout(gate)
    }
  }, [enabled, base])

  return { ready, failed, pending: enabled && !ready && !failed, hairReady, resolve: resolveDressAsset }
}
