// 每日内容状态机。
//
//   loading   进入页面，调 prepare（缓存探测）
//   waiting   未命中 → 播通用等待动画；同时调 generate（单次 LLM 选题+成文）
//   settling  generate 返回 → 动画落位（1.2s）→ 海报呈现
//   content   海报呈现
//   offline   网络错误 → 读本地缓存；无缓存也保持静默文案（服务端本身不会空屏）
//
// 客户端只依赖两个契约：cache_hit 决定是否播动画；settle 时机由 generate
// 返回触发。generate 永远 200——客户端没有「生成失败」分支，只有
// source（generated / fallback）字段。选题在服务端的 LLM 调用里完成，
// 客户端不再持有 pick_token。

import { useCallback, useEffect, useRef, useState } from 'react'
import Taro from '@tarojs/taro'
import {
  DAILY_COPY,
  dailyBucketName,
  type CollectionCategory,
  type ContentVisual,
  type ContentType,
  type MotionPresentation,
  type TodayContext,
} from '@zsm/core'
import { peripherals } from '../../app/api/peripherals'
import { addSave, readSaves, syncPendingSaves, type DailySave } from './saves'


// 收敛动画的时长由服务端脚本决定（tempo 档位 + 戏剧停顿），这里只是兜底：
// 万一播放器没回调（脚本异常 / 页面被挂起），也不能永远停在收敛态不进内容。
const SETTLE_FALLBACK_MS = 6000

export type DailyPhase = 'loading' | 'waiting' | 'settling' | 'content' | 'offline'

/** 服务端内容的小程序视图：fit 已在服务端渲染成 fit_text */
export interface DailyContentView {
  id: string
  type: ContentType
  topic: string
  lead: string
  fitText: string
  why: string
  visual: ContentVisual
  asset: CollectionCategory
  dedupeKey: string
  /** generated=AI 基于知识事实生成；fallback=今日精选 */
  source: 'generated' | 'fallback'
}

export interface DailyPickState {
  ctx: TodayContext | null
  saves: DailySave[]
  phase: DailyPhase
  /** 等待动画场景（通用过场；cache_hit 后重进不播） */
  scenario: string
  content: DailyContentView | null
  bucketName: string
  loading: boolean
  /** 巡游脚本（prepare 下发，等待期播） */
  roamScript: MotionPresentation | null
  /** 收敛 + 揭晓脚本（generate 下发，内容到位后播） */
  settleScript: MotionPresentation | null
  /** 收敛播完 → 揭晓海报（由播放器回调） */
  reveal: () => void
  reloadSaves: () => void
  saveCurrent: () => void
}

function toView(generate: {
  source: string
  content: {
    id: string
    type: string
    topic: string
    lead: string
    fit_text: string
    why: string
    visual: { modality: string; spec: Record<string, unknown> | null; alt: string }
    asset: string
    dedupe_key: string
  }
}): DailyContentView {
  return {
    id: generate.content.id,
    type: generate.content.type as ContentType,
    topic: generate.content.topic,
    lead: generate.content.lead,
    fitText: generate.content.fit_text,
    why: generate.content.why,
    visual: {
      modality: generate.content.visual.modality as ContentVisual['modality'],
      spec: generate.content.visual.spec ?? {},
      alt: generate.content.visual.alt,
    },
    asset: generate.content.asset as CollectionCategory,
    dedupeKey: generate.content.dedupe_key,
    source: generate.source === 'generated' ? 'generated' : 'fallback',
  }
}

/** 契约类型是生成的，这里只做形状校验，让播放器与生成代码解耦 */
function scriptOf(value: unknown): MotionPresentation | null {
  if (!value || typeof value !== 'object') return null
  const script = value as MotionPresentation
  return Array.isArray(script.stages) ? script : null
}

/** 当日内容缓存：offline / 重进时直接呈现（防闪屏，不清空已渲染内容） */
interface CachedDaily {
  date: string
  content: DailyContentView
}

function readCachedContent(): DailyContentView | null {
  try {
    const info = Taro.getStorageSync('zsm_daily_today')
    if (info && typeof info === 'object') {
      const cached = info as CachedDaily
      const today = new Date()
      const date = `${today.getFullYear()}-${String(today.getMonth() + 1).padStart(2, '0')}-${String(today.getDate()).padStart(2, '0')}`
      if (cached.date === date && cached.content && cached.content.topic) return cached.content
    }
  } catch {
    // 缓存不可用：走静默文案
  }
  return null
}

function writeCachedContent(content: DailyContentView): void {
  try {
    const now = new Date()
    const date = `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, '0')}-${String(now.getDate()).padStart(2, '0')}`
    Taro.setStorageSync('zsm_daily_today', { date, content } satisfies CachedDaily)
  } catch {
    // 同上：缓存可丢
  }
}

export function useDailyPick(): DailyPickState {
  const [ctx, setCtx] = useState<TodayContext | null>(null)
  const [saves, setSaves] = useState<DailySave[]>(() => readSaves())
  const [phase, setPhase] = useState<DailyPhase>('loading')
  const [scenario, setScenario] = useState('fallback')
  const [content, setContent] = useState<DailyContentView | null>(null)
  const [roamScript, setRoamScript] = useState<MotionPresentation | null>(null)
  const [settleScript, setSettleScript] = useState<MotionPresentation | null>(null)
  const mounted = useRef(true)

  useEffect(() => {
    mounted.current = true
    return () => {
      mounted.current = false
    }
  }, [])

  const settle = useCallback((view: DailyContentView) => {
    setContent(view)
    writeCachedContent(view)
    setPhase('settling')
    setTimeout(() => {
      if (mounted.current) setPhase((current) => (current === 'settling' ? 'content' : current))
    }, SETTLE_FALLBACK_MS)
  }, [])

  // 收敛动画播完 → 揭晓海报。由播放器回调，时长不再写死在客户端。
  const reveal = useCallback(() => {
    if (mounted.current) setPhase('content')
  }, [])

  const run = useCallback(async () => {
    if (!mounted.current) return
    setPhase('loading')
    // 离线缓存先顶上（恢复后重放的入口在 syncPendingSaves）。
    const cached = readCachedContent()
    try {
      const prepare = await peripherals.dailyPrepare()
      if (!mounted.current) return
      setCtx({
        date: prepare.gen_date,
        city: '',
        condition: '',
        temperature: 0,
        day_type: '',
        schedule: '',
      })
      if (prepare.cache_hit) {
        // 当天已生成：不播巡游，直接进收敛（重进也要有揭晓感，只是更快）。
        const result = await peripherals.dailyGenerate()
        if (!mounted.current) return
        setSettleScript(scriptOf(result.presentation))
        settle(toView(result))
        return
      }
      // 未命中：播巡游脚本等 generate 返回（选题在 LLM 调用里，等待期不知道讲什么）。
      setRoamScript(scriptOf(prepare.presentation))
      setPhase('waiting')
      // generate 永远 200：fallback 只是内容来源不同，不是失败分支。
      const result = await peripherals.dailyGenerate()
      if (!mounted.current) return
      setSettleScript(scriptOf(result.presentation))
      settle(toView(result))
    } catch {
      // 网络错误：读本地缓存，无缓存给静默文案（有下一步动作）。
      if (!mounted.current) return
      if (cached) {
        setContent(cached)
      }
      setPhase('offline')
    }
  }, [settle])

  useEffect(() => {
    void run()
    // 离线遗留的收藏在恢复后重放（服务端按 content_key 幂等，重放安全）。
    void syncPendingSaves()
    return () => {
      mounted.current = false
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  const reloadSaves = useCallback(() => {
    setSaves(readSaves())
  }, [])

  const contentKey = content?.dedupeKey ?? ''
  const saved = saves.some((save) => save.key === contentKey)

  const saveCurrent = useCallback(() => {
    if (!content || saved) return
    // 副本：fitText 已由服务端按该用户基因渲染，先本地留底再写穿服务端。
    const next = addSave({
      key: content.dedupeKey,
      contentId: content.id,
      category: content.asset,
      savedAt: new Date().toISOString(),
      status: 'saved',
      note: '',
      pendingSync: true,
      snapshot: {
        id: content.id,
        type: content.type,
        topic: content.topic,
        lead: content.lead,
        fitText: content.fitText,
        why: content.why,
        visual: content.visual,
        category: content.asset,
      },
    })
    setSaves(next)
    Taro.showToast({ title: `${DAILY_COPY.savedToastPrefix}${dailyBucketName(content.asset)}`, icon: 'none' })
  }, [content, saved])

  const loading = phase === 'loading'
  const bucketName = content ? dailyBucketName(content.asset) : ''

  return {
    ctx,
    saves,
    phase,
    scenario,
    content,
    bucketName,
    loading,
    roamScript,
    settleScript,
    reveal,
    reloadSaves,
    saveCurrent,
  }
}

// context 拉取保留给需要天气的页面（今日页顶部语境），失败静默。
export function useTodayContext(): TodayContext | null {
  const [ctx, setCtx] = useState<TodayContext | null>(null)
  useEffect(() => {
    let alive = true
    peripherals
      .getTodayContext()
      .then((value) => {
        if (alive) setCtx(value)
      })
      .catch(() => {})
    return () => {
      alive = false
    }
  }, [])
  return ctx
}
