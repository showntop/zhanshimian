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
import { Image, Text, View } from '@tarojs/components'
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

const FLY: Record<DimKey, [number, number]> = {
  look: [160, 150],
  color: [160, 150],
  waist: [168, 300],
  hair: [168, 64],
}
const FLY_MS = 460
const FLY_MS_ACCEL = 260
const SETTLE_HOLD_MS = 1100

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
  /** 发型素材就绪（use-dress-assets 懒加载结果）；false 时发型维度不入池 */
  hairAvailable?: boolean
  /** 端不支持 CSS filter（M1 渲染探测）→ 换色维度锁基准砖红（spec §4） */
  colorLocked?: boolean
  reduced?: boolean
  /** 收敛轮盖章完成 → 父组件揭晓海报 */
  onSettled?: () => void
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
  hairAvailable = true,
  colorLocked = false,
  reduced = false,
  onSettled,
}: DressShuffleProps) {
  const [figure, setFigure] = useState<FigureState>(INITIAL_FIGURE)
  const [pool, setPool] = useState<PoolState | null>(null)
  const [head, setHead] = useState<{ label: string; badge: string }>({ label: '今天穿什么', badge: '造型' })
  const [chips, setChips] = useState<string[]>([])
  const [fly, setFly] = useState<{ item: ShuffleItem; dim: DimKey; from: [number, number]; go: boolean } | null>(null)
  const [ring, setRing] = useState<[number, number] | null>(null)
  const [stamp, setStamp] = useState(false)
  const [round, setRound] = useState(0)
  const settledRef = useRef(false)
  const timersRef = useRef<ReturnType<typeof setTimeout>[]>([])

  const later = (fn: () => void, ms: number) => {
    timersRef.current.push(setTimeout(fn, ms))
  }
  const clearTimers = () => {
    timersRef.current.forEach(clearTimeout)
    timersRef.current = []
  }

  const asset = (p: string) => `${assetBase}/color/${p}`
  const cardAsset = (key: string) => `${assetBase}/color/card-${key}-v2.jpg`

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

  // ---------- 单维度洗牌驱动 ----------
  const runDim = (dim: RunDim, accel: boolean, done: () => void) => {
    const base = accel ? 30 : 70
    const decay = accel ? 1.18 : 1.35
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
      const blur = d < 110 ? 5.5 : d < 180 ? 3.5 : d < 250 ? 1.5 : 0
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
      later(() => setRing(null), 620)
      setChips((chips) => [...chips, `${CHIP_LABEL[dim.key]}${item.name}`])
      later(done, accel ? 140 : 300)
    }, accel ? FLY_MS_ACCEL : FLY_MS)
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
    if (settling || reduced) return
    setChips([])
    setStamp(false)
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

  // 收敛轮：固定顺序加速锁定 → 盖章 → 揭晓
  useEffect(() => {
    if (!settling) return
    settledRef.current = false
    clearTimers()
    let t = targetRef.current ?? randomTarget()
    if (colorLockRef.current) t = { ...t, color: 1 } // 锁基准砖红（spec §4）
    if (reduced) {
      // 减动效：直接呈现最终搭配，稍候揭晓
      setFigure({ look: t.look, color: t.color, waist: t.waist, hair: t.look === 0 ? t.hair : null })
      setChips([])
      later(() => {
        setStamp(true)
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
    setFigure({ look: t.look, color: 1, waist: null, hair: null })
    const dims = buildRunDims(t, { order: ['look', 'color', 'waist', 'hair'], hairAvailable: hairAvailRef.current })
    runRound(dims, 0, true, () => {
      setStamp(true)
      later(() => {
        if (!settledRef.current) {
          settledRef.current = true
          onSettled?.()
        }
      }, SETTLE_HOLD_MS)
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
  const figureMargin = Math.round(-414 * 0.48)

  return (
    <View className="ds">
      <View className="ds__fig">
        <View className="ds__arch" />
        <View className="ds__clip">
          <Image
            className="ds__figure"
            src={figureSrc}
            style={{
              marginLeft: `${figureMargin}rpx`,
              ...(figureFilter ? { filter: figureFilter } : {}),
            }}
          />
        </View>
        {figure.waist != null ? <View className="ds__waist" style={{ top: `${figure.waist}%` }} /> : null}
        {stamp ? <View className="ds__stamp">今日</View> : null}
        <View className="ds__caption">
          <View className="ds__caption-main">今天穿什么</View>
          <View className="ds__caption-sub">正在为你搭配</View>
        </View>
      </View>

      <View className="ds__pool">
        <View className="ds__pool-head">
          <Text className="ds__dim-label">{head.label}</Text>
          <Text className="ds__badge">{head.badge}</Text>
        </View>
        <View className="ds__grid">
          {pool
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
        <View className="ds__chips">
          {chips.map((c, i) => (
            <Text key={c} className="ds__chip">
              {c}
            </Text>
          ))}
        </View>
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
