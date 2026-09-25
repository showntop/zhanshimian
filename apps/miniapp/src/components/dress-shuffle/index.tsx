// 换装洗牌播放器（原型 daily-dressup-asset.html 的小程序移植）。
//
// 两阶段：
//   waiting  = 循环轮：每轮随机子集洗 3~4 维（target 保底），轮间无限循环
//   settling = 收敛轮：固定顺序 look→color→waist→hair 加速锁定，
//              飞卡连击 + 盖章「今日」→ onSettled（父组件揭晓海报）
//
// 与原型的差异：
//   · WAAPI 飞卡 → CSS transition + timer（小程序无 WAAPI）
//   · 发型维度只在 target.look === 0（hero look）时入列——发型素材只做了
//     hero look；其他 look 的日子该维度自动跳过（presets.buildRunDims）
//   · 减动效：塌缩为直接展示四张 target 卡 + 盖章
//
// 降级（父组件职责）：素材未就绪时父组件不应挂载本组件（sketch 巡游顶位，
// 见设计文档 §4 降级矩阵）。

import { useEffect, useRef, useState } from 'react'
import Taro from '@tarojs/taro'
import { Image, Text, View } from '@tarojs/components'
import { DAILY_COPY } from '@zsm/core'
import {
  buildRunDims,
  COLORS,
  HAIRS,
  LOOKS,
  randomTarget,
  type DimKey,
  type DressTarget,
  type RunDim,
  type ShuffleItem,
} from './presets'
import './index.scss'

/** 下标访问断言：调用点保证下标合法（pickPool 保底 / 取模循环） */
const at = <T,>(arr: T[], i: number): T => arr[i] as T
const LOOKS_IDX: Record<string, number> = Object.fromEntries(LOOKS.map((l, i) => [l.key, i]))
const COLOR_IDX: Record<string, number> = Object.fromEntries(COLORS.map((c, i) => [c.name, i]))
const HAIR_IDX: Record<string, number> = Object.fromEntries(HAIRS.map((h, i) => [h.key, i]))

// 飞卡落点 = 人物身上的部位（融合式构图下人物在画面右侧：身体 x≈426~750，
// 头 y≈40~150、腰 y≈330）。旧坐标是左栏坐标，已随构图作废。
const FLY: Record<DimKey, [number, number]> = {
  look: [576, 300],
  color: [612, 236],
  waist: [578, 344],
  hair: [586, 110],
}
const FLY_MS = 460
const FLY_MS_ACCEL = 260
const SETTLE_HOLD_MS = 1100

// 收敛结束 → 揭晓的时序（原型 daily-dressup-asset.html 的 run() 尾部）：
// caption 先变「今天这一身」，820ms 后文字层淡入（层级逐级上浮）。
const CAPTION_SETTLE_MS = 200
/** 定格：池子消失与文字层出现落在同一帧（原子切换，见 index.scss） */
const REVEAL_IN_MS = 820
/** 面板停留（原型停在面板上；我们随后交棒给内容海报，留足看清的时间） */
const REVEAL_HOLD_MS = 1600

interface FigureState {
  look: number
  color: number
  /** null = 腰线未上；数值 = 腰线 top 百分比 */
  waist: number | null
  /** null = 默认长直发；有值 = HAIRS 下标（仅 hero look 生效） */
  hair: number | null
}

const INITIAL_FIGURE: FigureState = { look: 0, color: 1, waist: null, hair: null }

export interface DressShuffleProps {
  /** 收敛轮锁定目标（服务端 dress_lock 下发；未传 = 循环轮随机） */
  target?: DressTarget
  /** false = 循环轮（等待期）；true = 收敛轮 */
  settling: boolean
  /** 素材基地址（M4 接 CDN；本地传 '' 走相对路径） */
  assetBase?: string
  /** 素材未就绪：停在洗牌自己的静态前奏（壁龛 + 骨架卡位），不洗牌、不顶旧巡游 */
  hold?: boolean
  /** 远端路径 → 可直接渲染的地址（本地缓存优先；不传则直接用远端） */
  resolveAsset?: (url: string) => string
  /** 发型素材就绪（use-dress-assets 懒加载结果）；false 时发型维度不入池 */
  hairAvailable?: boolean
  /** 端不支持 CSS filter（M1 渲染探测）→ 换色维度锁基准砖红（spec §4） */
  colorLocked?: boolean
  reduced?: boolean
  /** 收敛轮盖章完成 → 父组件揭晓海报 */
  onSettled?: () => void
  /** 当天建议的选题与导语：揭晓后直接在面板里亮出来（建议与定格同屏，不再另起引导行） */
  topic?: string
  lead?: string
  /** 建议归格（color/fit/.../hair/makeup/accessory）：决定面板标题（讲发型不叫「今天这一身」） */
  category?: string
  /** 当天日期编号（MM.DD）：右下角大号衬线数字（editorial 主视觉 + 「今日」语义） */
  seq?: string
  /** 面板底部的一行行动入口（如「看今日详情 ›」）；整卡点击由外层承担 */
  cta?: string
}

interface PoolState {
  dim: RunDim
  items: ShuffleItem[]
  order: number[]
  blur: number
  /** 快切结束、target 高亮后为 true */
  picked: boolean
}

export default function DressShuffle({
  target: targetProp,
  settling,
  assetBase = '',
  hold = false,
  resolveAsset,
  hairAvailable = true,
  colorLocked = false,
  reduced = false,
  onSettled,
  topic,
  lead,
  category,
  seq,
  cta,
}: DressShuffleProps) {
  const toUrl = resolveAsset ?? ((url: string) => url)
  const [figure, setFigure] = useState<FigureState>(INITIAL_FIGURE)
  const [pool, setPool] = useState<PoolState | null>(null)
  const [head, setHead] = useState<{ label: string; badge: string }>({ label: '今天穿什么', badge: '造型' })
  const [chips, setChips] = useState<string[]>([])
  const [fly, setFly] = useState<{ item: ShuffleItem; dim: DimKey; from: [number, number]; go: boolean } | null>(null)
  const [ring, setRing] = useState<[number, number] | null>(null)
  /** 揭晓面板滑入（原型：右栏变海报，人物保持亮着） */
  const [revealed, setRevealed] = useState(false)
  /** 上身时的一次性摆身（pop），与常驻 sway 叠加 */
  const [pop, setPop] = useState(false)
  const [caption, setCaption] = useState<string>(DAILY_COPY.dressCaptionIdle)
  const [round, setRound] = useState(0)
  /** 收敛轮：池子先淡出，文字层等它走完再升——左栏一次只发生一件事 */
  const [poolOut, setPoolOut] = useState(false)
  /**
   * 揭晓快照：revealed 翻真那一刻把左栏内容冻结下来。
   *
   * 定格之后父组件还会因种种原因重渲染（阶段切换、素材后台预热完成、
   * 内容晚到……），任何一次晚到的 props 都会让左栏「再刷一下」。
   * 有了这份快照，已定格的字只能呈现一次。
   */
  const [revealSnap, setRevealSnap] = useState<{
    topic?: string
    lead?: string
    cta?: string
    chips: string[]
  } | null>(null)
  // 低端机能力位（一次性探测）：benchmarkLevel 是微信给的机器档位，
  // 低于阈值就关掉运动模糊这类离屏合成开销；探测失败按「支持」处理。
  const lowEndRef = useRef(false)
  const probedRef = useRef(false)
  const settledRef = useRef(false)
  const timersRef = useRef<ReturnType<typeof setTimeout>[]>([])
  // 快照在 timer 回调里执行：闭包拿到的是 effect 首次运行那一刻的值，
  // 所以全程走 ref 读最新 props/state。
  const chipsRef = useRef<string[]>([])
  chipsRef.current = chips
  const topicRef = useRef<string | undefined>(topic)
  topicRef.current = topic
  const leadRef = useRef<string | undefined>(lead)
  leadRef.current = lead
  const ctaRef = useRef<string | undefined>(cta)
  ctaRef.current = cta

  const freezeReveal = () =>
    setRevealSnap({
      topic: topicRef.current,
      lead: leadRef.current,
      cta: ctaRef.current,
      chips: chipsRef.current,
    })

  if (!probedRef.current) {
    probedRef.current = true
    try {
      const info = Taro.getSystemInfoSync() as { benchmarkLevel?: number }
      lowEndRef.current = typeof info.benchmarkLevel === 'number' && info.benchmarkLevel > 0 && info.benchmarkLevel < 8
    } catch {
      lowEndRef.current = false
    }
  }

  const later = (fn: () => void, ms: number) => {
    timersRef.current.push(setTimeout(fn, ms))
  }
  const clearTimers = () => {
    timersRef.current.forEach(clearTimeout)
    timersRef.current = []
  }

  // 本地缓存优先：弱网/断网时人物与卡面走本地文件，不会空白
  const asset = (p: string) => toUrl(`${assetBase}/color/${p}`)
  const cardAsset = (key: string) => toUrl(`${assetBase}/color/card-${key}-v2.jpg`)

  // ---------- 应用候选到人物 ----------
  const applyItem = (dim: RunDim, item: ShuffleItem) => {
    setFigure((f) => {
      switch (dim.key) {
        case 'look':
          // 换 look 重置发型（发型素材只对 hero look 存在；hero→hero 保发型）
          return { ...f, look: LOOKS_IDX[item.key ?? ''] ?? 0, hair: item.key === 'outfit' ? f.hair : null }
        case 'color':
          return { ...f, color: COLOR_IDX[item.name] ?? 1 }
        case 'waist':
          return { ...f, waist: item.pct ?? null }
        case 'hair':
          return { ...f, hair: HAIR_IDX[item.key ?? ''] ?? null }
        default:
          return f
      }
    })
  }

  // ---------- 池布局（自适应列数，永远排满；数值即 rpx） ----------
  const poolPos = (dim: RunDim, poolIdx: number): [number, number] => {
    const n = dim.items.length
    const cols = n <= 3 ? n : n === 4 ? 2 : 3
    const rows = Math.ceil(n / cols)
    const gw = cols * 104 + (cols - 1) * 16
    const gh = rows * 124 + (rows - 1) * 16
    return [
      Math.round((390 - gw) / 2 + (poolIdx % cols) * 120),
      Math.round((300 - gh) / 2 + Math.floor(poolIdx / cols) * 140),
    ]
  }

    /** 静态前奏的骨架卡位（4 张 2×2，与 poolPos 的排布同款） */
  const idlePos = (idleIdx: number): [number, number] => [
    Math.round((390 - (2 * 104 + 16)) / 2 + (idleIdx % 2) * 120),
    Math.round((300 - (2 * 124 + 16)) / 2 + Math.floor(idleIdx / 2) * 140),
  ]

  // ---------- 单维度洗牌驱动 ----------
  // 步长下限 46ms：30ms 一步是 33fps，低端机上「快切」会变成明显的卡顿，
  // 而洗牌的爽感来自「快而不卡」，不是「步频高」。低端机同时关掉运动模糊
  // （blur 要离屏合成，是这一屏最贵的一项）。
  const runDim = (dim: RunDim, accel: boolean, done: () => void) => {
    const base = accel ? 46 : 78
    const decay = accel ? 1.22 : 1.35
    const pickAt = 330
    let d = base
    let steps = 0
    setHead({ label: dim.label, badge: dim.badge })
    const cut = () => {
      const n = dim.items.length
      const order = [...Array(n).keys()]
      for (let i = n - 1; i > 0; i--) {
        const j = Math.floor(Math.random() * (i + 1))
        const a = at(order, i)
        order[i] = at(order, j)
        order[j] = a
      }
      const blur = lowEndRef.current || reduced ? 0 : d < 110 ? 5.5 : d < 180 ? 3.5 : d < 250 ? 1.5 : 0
      setPool({ dim, items: dim.items, order, blur, picked: false })
      const idx = d >= pickAt ? dim.target : steps % n
      // 快切段：候选实时上身（试衣闪切/闪染）
      if (d < pickAt) applyItem(dim, at(dim.items, idx))
      steps++
      d = Math.round(d * decay)
      if (d >= pickAt || steps > 16) {
        // 快切结束：顺序归位 + 清模糊，target 高亮 → 飞卡
        setPool({ dim, items: dim.items, order: [...Array(n).keys()], blur: 0, picked: true })
        later(() => flyPick(dim, accel, done), accel ? 120 : 200)
        return
      }
      later(cut, d)
    }
    cut()
  }

  // ---------- 飞卡上身（transition 版：无 WAAPI） ----------
  const flyPick = (dim: RunDim, accel: boolean, done: () => void) => {
    const item = at(dim.items, dim.target)
    const from = poolPos(dim, dim.target)
    const [tx, ty] = FLY[dim.key]
    setFly({ item, dim: dim.key, from, go: false })
    later(() => setFly((f) => (f ? { ...f, go: true } : f)), 30)
    later(() => {
      setFly(null)
      applyItem(dim, item)
      setRing(FLY[dim.key])
      // 上身带一下摆身（原型 .figstage.pop）：让「穿上了」有分量
      setPop(true)
      later(() => setPop(false), 520)
      later(() => setRing(null), 620)
      setChips((chips) => [...chips, `${CHIP_LABEL[dim.key]}${item.name}`])
      later(done, accel ? 140 : 300)
    }, accel ? FLY_MS_ACCEL : FLY_MS)
  }
  // 标注行渲染：chips 串形如「造型 · 衬衫+半裙」——拆成 维度（绿）+ 取值 + ↑（朱红）
  const renderChip = (chip: string) => {
    const cut = chip.indexOf(' · ')
    const label = cut >= 0 ? chip.slice(0, cut) : ''
    const value = cut >= 0 ? chip.slice(cut + 3) : chip
    return (
      <View key={chip} className="ds__chip">
        {label ? <Text className="ds__chip-label">{label}</Text> : null}
        <Text className="ds__chip-value">{value}</Text>
        <Text className="ds__chip-arrow">↑</Text>
      </View>
    )
  }
  const CHIP_LABEL: Record<DimKey, string> = {
    look: '造型 · ',
    color: '配色 · ',
    waist: '比例 · ',
    hair: '发型 · ',
  }

  // ---------- 轮次递归 ----------
  const runRound = (dims: RunDim[], i: number, accel: boolean, done: () => void) => {
    if (i >= dims.length) {
      done()
      return
    }
    runDim(at(dims, i), accel, () => runRound(dims, i + 1, accel, done))
  }

  // ---------- 两阶段驱动 ----------
  // target 走 ref：父组件每次渲染传新对象，不能进依赖数组（会重启收敛轮）。
  // hairAvailable/colorLocked 同理：预载/探测是渐进信号，不该打断播放中的轮次。
  const targetRef = useRef(targetProp)
  targetRef.current = targetProp
  const hairAvailRef = useRef(hairAvailable)
  hairAvailRef.current = hairAvailable
  const colorLockRef = useRef(colorLocked)
  colorLockRef.current = colorLocked

  // 循环轮：每轮随机 target，跑完稍歇进下一轮
  useEffect(() => {
    if (settling || reduced || hold) return
    setChips([])
    setRevealed(false)
    setPoolOut(false)
    setCaption(DAILY_COPY.dressCaptionIdle)
    setFigure({ ...INITIAL_FIGURE })
    const t = randomTarget()
    if (colorLockRef.current) t.color = 1 // 端不支持 filter：锁基准砖红（spec §4）
    const dims = buildRunDims(t, { hairAvailable: hairAvailRef.current })
    let cancelled = false
    runRound(dims, 0, false, () => {
      if (cancelled) return
      later(() => setRound((r) => r + 1), 800)
    })
    return () => {
      cancelled = true
      clearTimers()
    }
  }, [settling, round, reduced])

  // 收敛轮：固定顺序加速锁定 → 盖章 → 池子淡出 → 揭晓
  useEffect(() => {
    if (!settling) return
    settledRef.current = false
    clearTimers()
    let t = targetRef.current ?? randomTarget()
    if (colorLockRef.current) t = { ...t, color: 1 } // 锁基准砖红（spec §4）
    setRevealed(false)
    setPoolOut(false)
    if (reduced) {
      // 减动效：直接呈现最终搭配，稍候揭晓（面板滑入也归零，直接出现在位）
      setFigure({ look: t.look, color: t.color, waist: t.waist, hair: t.look === 0 ? t.hair : null })
      setChips([])
      setPoolOut(true)
      setCaption(DAILY_COPY.dressCaptionSettled)
      later(() => {
        freezeReveal()
        setRevealed(true)
        later(() => {
          if (!settledRef.current) {
            settledRef.current = true
            onSettled?.()
          }
        }, 900)
      }, 400)
      return () => clearTimers()
    }
    setChips([])
    setCaption(DAILY_COPY.dressCaptionIdle)
    setFigure({ look: t.look, color: 1, waist: null, hair: null })
    const dims = buildRunDims(t, { order: ['look', 'color', 'waist', 'hair'], hairAvailable: hairAvailRef.current })
    runRound(dims, 0, true, () => {
      // 原型序列：caption 先落「今天这一身」→ 定格。
      // 定格是原子切换：池子消失、文字层出现同帧发生，中间没有任何交叠过渡——
      // 之前池子淡出与文字分层升起叠在同一块左栏区域，就是「定格后左侧再刷」的来源
      later(() => setCaption(DAILY_COPY.dressCaptionSettled), CAPTION_SETTLE_MS)
      later(() => {
        setPoolOut(true)
        freezeReveal()
        setRevealed(true)
        later(() => {
          if (!settledRef.current) {
            settledRef.current = true
            onSettled?.()
          }
        }, Math.max(REVEAL_HOLD_MS, SETTLE_HOLD_MS))
      }, REVEAL_IN_MS)
    })
    return () => clearTimers()
  }, [settling, reduced, onSettled])

  // ---------- 渲染 ----------
  const look = at(LOOKS, figure.look)
  const color = at(COLORS, figure.color)
  const hairKey = figure.hair != null && figure.look === 0 ? at(HAIRS, figure.hair).key : null
  const figureSrc = hairKey ? asset(`outfit-${hairKey}.png`) : asset(`${look.key}.png`)
  // 端不支持 filter（colorLocked）：人物保持基准砖红，洗牌照常（spec §4）
  const figureFilter = !colorLocked && color.filter !== 'none' ? color.filter : undefined
  // 左栏内容：定格后优先读快照，快照某项为空才回退实时 props（内容真晚到仍要显示）
  const view = {
    topic: revealSnap?.topic || topic,
    lead: revealSnap?.lead || lead,
    cta: revealSnap?.cta || cta,
    chips: revealSnap?.chips ?? chips,
  }

  return (
    <View className="ds">
      <View className="ds__fig">
        <View className="ds__arch" />
        <View className="ds__clip">
          {/* figstage：常驻微摆（sway）+ 上身一次性摆身（pop）。人物对位在 SCSS 里
              按拱门内边距解算（原型 750 宽 + focus48% 会让人物上下溢出拱门） */}
          <View className={`ds__figstage${pop ? ' ds__figstage--pop' : ''}`}>
            {hold ? null : (
              <Image
                className="ds__figure"
                src={figureSrc}
                style={figureFilter ? { filter: figureFilter } : undefined}
              />
            )}
            {figure.waist != null ? <View className="ds__waist" style={{ top: `${figure.waist}%` }} /> : null}
          </View>
        </View>
        {/* 揭晓后左下 caption 隐藏：「今天这一身」只在右栏面板出现一次 */}
        {!revealed ? (
          <View className="ds__caption">
            <View className="ds__caption-main">{caption}</View>
            <View className="ds__caption-sub">{DAILY_COPY.dressCaptionSub}</View>
          </View>
        ) : null}
      </View>

      {/* 揭晓时池子淡出（不卸载：淡出更干净），把画面让给人物与文字 */}
      <View className={`ds__pool${poolOut ? ' ds__pool--out' : ''}`}>
        <View className="ds__pool-head">
          <Text className="ds__dim-label">{head.label}</Text>
          <Text className="ds__badge">{head.badge}</Text>
        </View>
        <View className="ds__grid">
          {hold
            ? // 静态前奏：只占位不洗牌（素材未就绪，切回旧巡游会风格跳变）
              [0, 1, 2, 3].map((i) => (
                <View
                  key={`idle-${i}`}
                  className="ds__cand ds__cand--idle"
                  style={{ transform: `translate(${idlePos(i)[0]}rpx,${idlePos(i)[1]}rpx)` }}
                />
              ))
            : pool
              ? pool.items.map((it, i) => {
                const pos = poolPos(pool.dim, i)
                const picked = pool.picked
                const cls =
                  picked && i === pool.dim.target ? ' ds__cand--pick' : picked ? ' ds__cand--rest' : ''
                return (
                  <View
                    key={`${it.name}-${i}`}
                    className={`ds__cand${cls}`}
                    style={{ transform: `translate(${pos[0]}rpx,${pos[1]}rpx)`, filter: pool.blur ? `blur(${pool.blur}px)` : undefined }}
                  >
                    <View className="ds__cand-inner">
                      {it.kind === 'swatch' ? (
                        <View className="ds__swatch" style={{ background: it.hex }} />
                      ) : (
                        <Image
                          className="ds__cand-img"
                          src={
                            it.kind === 'rule'
                              ? cardAsset('rule')
                              : it.kind === 'hair'
                                ? cardAsset(`outfit-${it.key ?? ''}`) // 发型卡命名：card-outfit-{key}-v2.jpg（原型同款）
                                : cardAsset(it.key ?? '')
                          }
                        />
                      )}
                    </View>
                    <View className="ds__tag">{it.name}</View>
                    </View>
                  )
                })
              : null}
        </View>
        <View className="ds__chips">{chips.map(renderChip)}</View>
      </View>

      {/* 揭晓：文字层压在人物之上（无面板、同底色），层级照参照稿——
          小标注（竖线）→ 大标题（当天选题）→ 细线 → 正文 → 标注行 → 胶囊按钮，
          右下角大号衬线日期数字；竖排英文压在人物肩侧做注脚 */}
      <View className={`ds__reveal${revealed ? ' ds__reveal--on' : ''}`}>
        <Text className="ds__reveal-ghost">{DAILY_COPY.dressRevealGhost}</Text>
        <View className="ds__reveal-topic">
          {(category && DAILY_COPY.dressRevealTitleByCategory[category]) || DAILY_COPY.dressRevealTitle}
        </View>
        {view.topic ? <View className="ds__reveal-title">{view.topic}</View> : null}
        <View className="ds__reveal-rule" />
        {view.lead ? (
          <View className="ds__reveal-lead">{view.lead}</View>
        ) : (
          <View className="ds__reveal-sub">{DAILY_COPY.dressRevealSub}</View>
        )}
        <View className="ds__reveal-chips">{view.chips.map(renderChip)}</View>
        {/* 行动区 = 布样行 + 按钮，包在 fit-content 的列里互相居中：
            布样永远对按钮同轴（列宽 = 按钮宽，布样在列内水平居中）。
            margin-top:auto 把整组顶到卡底；空间不足时 auto margin 先归零，
            配合 flex-shrink:0 按钮不会被压扁贴底（此前「按钮与卡底重合」的教训） */}
        {view.cta || seq ? (
          <View className="ds__reveal-actions">
            <View className="ds__reveal-fabrics">
              <View className="ds__reveal-fabric ds__reveal-fabric--1" />
              <View className="ds__reveal-fabric ds__reveal-fabric--2" />
              <View className="ds__reveal-fabric ds__reveal-fabric--3" />
            </View>
            {view.cta ? (
              <View className="ds__reveal-cta-row">
                <Text className="ds__reveal-cta">{view.cta}</Text>
              </View>
            ) : null}
          </View>
        ) : null}
        {seq ? (
          <View className="ds__index">
            <Text className="ds__index-num serif">{seq.slice(3)}</Text>
            <Text className="ds__index-label">{DAILY_COPY.dressStamp}</Text>
          </View>
        ) : null}
      </View>

      {fly ? (
        <View
          className={`ds__fly${fly.go ? ' ds__fly--go' : ''}`}
          style={{
            left: `${fly.from[0]}rpx`,
            top: `${fly.from[1]}rpx`,
            transform: fly.go
              ? `translate(${FLY[fly.dim][0] - fly.from[0]}rpx,${FLY[fly.dim][1] - fly.from[1]}rpx) rotate(-10deg) scale(.2)`
              : 'none',
          }}
        >
          {fly.item.kind === 'swatch' ? (
            <View className="ds__swatch" style={{ background: fly.item.hex }} />
          ) : (
            <Image
              className="ds__fly-img"
              src={fly.item.kind === 'hair' ? cardAsset(`outfit-${fly.item.key ?? ''}`) : cardAsset(fly.item.key ?? '')}
            />
          )}
        </View>
      ) : null}
      {ring ? (
        <View className="ds__ring" style={{ left: `${ring[0]}rpx`, top: `${ring[1]}rpx` }} />
      ) : null}
    </View>
  )
}
