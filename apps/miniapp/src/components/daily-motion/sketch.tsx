// 巡游渲染器（kind=sketch_tour）：把每个分类的形状当场「画」出来。
//
// 移植自原型 daily-combo-sim.html 的 SVG 描边动画——小程序没有内联 SVG，
// 也没有 stroke-dashoffset，所以在这里用 Canvas 2d 复刻同一套语言：
//   · 三层笔法：main 粗轮廓 / guide 虚线辅助 / note 强调标注
//   · 回描：先画轻的试探线，再画重的肯定线（手绘感的来源）
//   · 笔尖沿真实路径走（「正在画」看得见）
//   · 每个主题换 seed 的手绘抖动（低频平滑噪声，替代 feTurbulence 位移）
//   · 人物画完轻微呼吸（画面是活的）
// 零素材、可无限循环、可随时中断（unmount 即停）。
//
// 网格底、进度点、标签、日期编号不在 canvas 里：它们是静态装饰，
// View/Text 一行样式就够，canvas 只做它擅长的「线条」。

import { useEffect, useRef, useState } from 'react'
import Taro from '@tarojs/taro'
import { Canvas, Text, View } from '@tarojs/components'
import './sketch.scss'

// ---------- 设计空间 ----------
// 原型按 375×300 布 point，这里原样保留坐标（保真），绘制时 contain 缩放居中。

interface Pt {
  x: number
  y: number
}

/** SVG path（M/L/Q/T/Z，绝对坐标）→ 采样点列。原型路径直接复用。 */
function parsePath(d: string): Pt[] {
  const cmds = d.match(/[MLQTZ][^MLQTZ]*/g) ?? []
  const pts: Pt[] = []
  let cur: Pt | null = null
  let ctrl: Pt | null = null
  const nums = (s: string) => (s.slice(1).trim().match(/-?\d+(?:\.\d+)?/g) ?? []).map(Number)
  const quad = (p0: Pt, c: Pt, p1: Pt) => {
    for (let i = 1; i <= 8; i++) {
      const t = i / 8
      const u = 1 - t
      pts.push({
        x: u * u * p0.x + 2 * u * t * c.x + t * t * p1.x,
        y: u * u * p0.y + 2 * u * t * c.y + t * t * p1.y,
      })
    }
  }
  for (const cmd of cmds) {
    const v = nums(cmd)
    switch (cmd[0]) {
      case 'M':
      case 'L':
        for (let i = 0; i + 1 < v.length; i += 2) {
          const x = v[i]
          const y = v[i + 1]
          if (x === undefined || y === undefined) continue
          cur = { x, y }
          pts.push(cur)
        }
        ctrl = null
        break
      case 'Q':
        if (cur && v.length >= 4) {
          const cx = v[0]
          const cy = v[1]
          const px = v[2]
          const py = v[3]
          if (cx === undefined || cy === undefined || px === undefined || py === undefined) break
          const c = { x: cx, y: cy }
          const p1 = { x: px, y: py }
          quad(cur, c, p1)
          cur = p1
          ctrl = c
        }
        break
      case 'T':
        if (cur && v.length >= 2) {
          const px = v[0]
          const py = v[1]
          if (px === undefined || py === undefined) break
          // 平滑二次曲线：控制点关于上一控制点反射
          const c: Pt = ctrl ? { x: 2 * cur.x - ctrl.x, y: 2 * cur.y - ctrl.y } : { ...cur }
          const p1 = { x: px, y: py }
          quad(cur, c, p1)
          cur = p1
          ctrl = c
        }
        break
      case 'Z': {
        const head = pts[0]
        if (head) pts.push({ ...head })
        break
      }
    }
  }
  return pts
}

/** 圆（人物头部）：整圆采样，从左侧起笔画一整圈 */
function circlePts(cx: number, cy: number, r: number): Pt[] {
  const pts: Pt[] = []
  const steps = 30
  for (let i = 0; i <= steps; i++) {
    const a = Math.PI + (i / steps) * Math.PI * 2
    pts.push({ x: cx + Math.cos(a) * r, y: cy + Math.sin(a) * r })
  }
  return pts
}

/** 长段加密：原型靠 feTurbulence 让长直线也带抖动，这里靠重采样接住 */
function densify(pts: Pt[], maxStep = 12): Pt[] {
  const head = pts[0]
  if (!head) return pts
  const out: Pt[] = [head]
  for (let i = 1; i < pts.length; i++) {
    const a = pts[i - 1]
    const b = pts[i]
    if (!a || !b) break
    const dist = Math.hypot(b.x - a.x, b.y - a.y)
    const steps = Math.max(1, Math.ceil(dist / maxStep))
    for (let s = 1; s <= steps; s++) out.push({ x: a.x + ((b.x - a.x) * s) / steps, y: a.y + ((b.y - a.y) * s) / steps })
  }
  return out
}

// ---------- 画法数据（与原型同源） ----------

type Layer = 'main' | 'light' | 'guide' | 'note' | 'hanger'

interface GarmentSpec {
  main: string[]
  guide: string[]
  note: string[]
}

// 兜底画法单独立成常量：Record 的索引访问带 undefined，
// 兜底分支必须是「确定有」的值，不能兜到另一个 possibly-undefined 上。
const OUTFIT_SPEC: GarmentSpec = {
  main: ['M120 54 L255 54 L255 118 L120 118 Z', 'M128 128 L247 128 L254 252 L121 252 Z'],
  guide: ['M110 123 L265 123'],
  note: ['M282 54 L282 118', 'M282 128 L282 252', 'M276 54 L288 54', 'M276 118 L288 118', 'M276 128 L288 128', 'M276 252 L288 252'],
}

const OUTLINES: Record<string, GarmentSpec> = {
  color: {
    main: ['M118 90 L156 90 L156 242 L118 242 Z', 'M166 58 L204 58 L204 242 L166 242 Z', 'M214 128 L252 128 L252 242 L214 242 Z'],
    guide: ['M100 248 L275 248', 'M100 58 L275 58'],
    note: ['M118 76 L156 76', 'M166 44 L204 44', 'M214 114 L252 114'],
  },
  fit: {
    main: ['M142 52 L233 52 L252 244 L123 244 Z'],
    guide: ['M187 38 L187 256', 'M136 116 L239 116', 'M132 180 L243 180'],
    note: ['M142 42 L233 42', 'M132 184 L243 184'],
  },
  proportion: {
    main: ['M126 56 L249 56 L249 134 L126 134 Z', 'M126 144 L249 144 L249 252 L126 252 Z'],
    guide: ['M110 139 L265 139'],
    note: ['M276 56 L276 134', 'M276 144 L276 252', 'M270 56 L282 56', 'M270 134 L282 134', 'M270 144 L282 144', 'M270 252 L282 252'],
  },
  fabric: {
    main: ['M88 86 Q150 60 212 86 T312 86', 'M88 144 Q150 118 212 144 T312 144', 'M88 202 Q150 176 212 202 T312 202'],
    guide: ['M88 64 L312 64', 'M88 224 L312 224'],
    note: ['M330 86 L330 202', 'M324 86 L336 86', 'M324 202 L336 202'],
  },
  occasion: {
    main: ['M88 68 L287 68 L287 224 L88 224 Z'],
    guide: ['M88 146 L287 146', 'M187 68 L187 224'],
    note: ['M112 104 L166 104 L166 198 L112 198 Z', 'M238 94 L272 94', 'M272 94 L262 84', 'M272 94 L262 102'],
  },
  howto: {
    main: ['M120 68 L255 68 L255 228 L120 228 Z'],
    guide: ['M112 136 L263 136', 'M112 176 L263 176'],
    note: ['M106 132 L267 128', 'M106 180 L267 176'],
  },
  outfit: OUTFIT_SPEC,
}

// 内置主题（脚本未到时立即开画）没有 theme 字段，按 form 反查分类。
// 与 core copy 的 motionRoamThemes 及 forms.tsx 注册表同名对齐。
export const FORM_CATEGORY: Record<string, string> = {
  swatch_bars: 'color',
  silhouette_shape: 'fit',
  ratio_blocks: 'proportion',
  texture_lines: 'fabric',
  scene_panel: 'occasion',
  fold_lines: 'howto',
  outfit_blocks: 'outfit',
}

// 人物：极简线条，圆头 + 直线躯干，无五官、无曲线。
// 有「人」在场，画面才像顾问在为你比划；不画细节是为了不碰身材红线。
const FIGURE_HEAD = { cx: 86.5, cy: 92, r: 8.5 }
const FIGURE_BODY = ['M86 101 L86 158', 'M86 158 L72 232', 'M86 158 L100 232']
// 每个分类的手臂姿态不同——「互动」发生在手的动作里
const ARMS: Record<string, string[]> = {
  color: ['M86 114 L56 148', 'M86 114 L124 96'], // 举起比色
  fit: ['M86 114 L56 146', 'M86 114 L122 130'],
  proportion: ['M86 114 L58 150', 'M86 114 L128 118'],
  fabric: ['M86 114 L54 132', 'M86 114 L128 124'], // 手托布料
  occasion: ['M86 114 L58 152', 'M86 114 L118 134'],
  howto: ['M86 114 L64 94', 'M86 114 L116 140'], // 抬手卷袖
  outfit: ['M86 114 L56 148', 'M86 114 L126 124'],
}
// 衣架：所有草图都挂在它上面——画面一眼是「服装」的事
const HANGER = ['M187 34 L187 52', 'M187 52 L118 78', 'M187 52 L256 78', 'M108 78 L264 78']
// 视线：从手指向衣服，「她在看这一身」
const GAZE = 'M124 118 L156 112'

// ---------- 笔画计划 ----------

interface Stroke {
  pts: Pt[]
  lens: number[] // 前缀弧长：partial 绘制与笔尖定位共用
  layer: Layer
  start: number // 相对主题开始的 ms（自然时基，未缩放）
  dur: number
  fade: boolean // guide 类整条淡入，不逐段画
  figure: boolean // 属于人物组（呼吸偏移）
}

/** 每主题换 seed 的手绘抖动：低频平滑噪声（周期插值），模拟起稿的手不稳 */
function makeJitter(seed: number) {
  const hash = (n: number) => {
    let h = Math.imul(n ^ seed, 0x9e3779b9)
    h = Math.imul(h ^ (h >>> 13), 0xc2b2ae35)
    h ^= h >>> 16
    return (h >>> 0) / 4294967296
  }
  const period = 26
  const phase = hash(0) * period
  const at = (t: number, channel: number) => {
    const pos = (t + phase + channel * 9.6) / period
    const i = Math.floor(pos)
    const f = pos - i
    const a = hash(i * 2 + channel + 1)
    const b = hash((i + 1) * 2 + channel + 1)
    const w = 0.5 - 0.5 * Math.cos(f * Math.PI)
    return (a + (b - a) * w - 0.5) * 3.2
  }
  return (t: number) => ({ dx: at(t, 0), dy: at(t, 1) })
}

/** 衣架与衣服缩到右侧，给人物留位置（原型同款变换） */
const GARMENT_TRANSFORM = { tx: 78, ty: -2, scale: 0.76 }

function withLens(pts: Pt[]): { pts: Pt[]; lens: number[] } {
  const lens: number[] = [0]
  for (let i = 1; i < pts.length; i++) {
    const a = pts[i - 1]
    const b = pts[i]
    const prev = lens[i - 1]
    if (!a || !b || prev === undefined) break
    lens.push(prev + Math.hypot(b.x - a.x, b.y - a.y))
  }
  return { pts, lens }
}

/** 一个主题的完整笔画计划：顺序即叙事（先有人 → 立衣架 → 起形 → 辅助线 → 标注 → 视线） */
function buildPlan(category: string, slot: number): { strokes: Stroke[]; natural: number; aliveAt: number } {
  const spec = OUTLINES[category] ?? OUTFIT_SPEC
  const arms = ARMS[category] ?? (ARMS.outfit ?? ['M86 114 L56 148', 'M86 114 L126 124'])
  const jitter = makeJitter(3 + slot * 7)
  const strokes: Stroke[] = []
  let at = 0

  /** 画一笔：raw 是设计空间点列，garment=true 时套右侧变换 */
  const push = (raw: Pt[], layer: Layer, dur: number, delay: number, fade: boolean, figure: boolean, garment: boolean) => {
    const source = garment
      ? raw.map((p) => ({ x: GARMENT_TRANSFORM.tx + p.x * GARMENT_TRANSFORM.scale, y: GARMENT_TRANSFORM.ty + p.y * GARMENT_TRANSFORM.scale }))
      : raw
    const dense = densify(source)
    const pts = dense.map((p, i) => {
      // 试探线换噪声相位：与肯定线略有错位，才是「先轻后重两笔」
      const off = jitter(i * 11 + (layer === 'light' ? 150 : 0))
      return { x: p.x + off.dx, y: p.y + off.dy }
    })
    const withJitter = withLens(pts)
    strokes.push({ ...withJitter, layer, start: delay, dur, fade, figure })
  }

  // 人物（不缩放：她是画面主体，保持正常比例）
  push(circlePts(FIGURE_HEAD.cx, FIGURE_HEAD.cy, FIGURE_HEAD.r), 'main', 140, at, false, true, false)
  at += 80
  FIGURE_BODY.forEach((d) => {
    push(parsePath(d), 'main', 110, at, false, true, false)
    at += 30
  })
  arms.forEach((d) => {
    push(parsePath(d), 'main', 90, at, false, true, false)
    at += 30
  })
  // 衣架
  HANGER.forEach((d) => {
    push(parsePath(d), 'hanger', 130, at, true, false, true)
    at += 18
  })
  // 回描：先试探线（轻）再肯定线（重），两线略有错位，像真的起稿
  spec.main.forEach((d) => {
    const base = parsePath(d)
    push(base, 'light', 150, at, false, false, true)
    push(base, 'main', 200, at + 130, false, false, true)
    at += 165
  })
  const aliveAt = 620 // 人物画完「活」起来的时刻
  spec.guide.forEach((d) => {
    push(parsePath(d), 'guide', 120, at, true, false, true)
    at += 15
  })
  spec.note.forEach((d) => {
    push(parsePath(d), 'note', 100, at, false, false, true)
    at += 18
  })
  push(parsePath(GAZE), 'guide', 220, at, true, false, false)

  const natural = Math.max(...strokes.map((s) => s.start + s.dur))
  return { strokes, natural, aliveAt }
}

// ---------- 渲染 ----------

const INK = '#39492E'
const MOSS = '#587344'
const LAYER_STYLE: Record<Layer, { color: string; width: number; alpha: number; dash?: number[] }> = {
  main: { color: INK, width: 2.6, alpha: 1 },
  light: { color: INK, width: 1.4, alpha: 0.4 },
  guide: { color: INK, width: 1.3, alpha: 0.55, dash: [5, 5] },
  note: { color: MOSS, width: 2, alpha: 1 },
  hanger: { color: INK, width: 1.3, alpha: 0.5 },
}

export interface SketchTheme {
  theme: string
  label: string
}

interface SketchTourProps {
  themes: SketchTheme[]
  perThemeMS: number
  /** 左上角日期编号（与海报的日期编号同一语言） */
  seq?: string
  /** true = 揭晓开始：停笔定格、整体淡出（递交给序列帧） */
  fading?: boolean
  reduced?: boolean
}

const CROSSFADE_MS = 280
const DESIGN_W = 375
const DESIGN_H = 300
/** 画完的图保留在缓存里的幅数上限（抖动按 slot 种子，旧幅淡出后即弃） */
const PLAN_CACHE = 8

export default function SketchTour({ themes, perThemeMS, seq, fading = false, reduced = false }: SketchTourProps) {
  const canvasId = useRef(`dm-sketch-${Math.random().toString(36).slice(2, 8)}`)
  const [index, setIndex] = useState(0)
  // 画布状态全部走 ref：rAF 每帧自取，不与 React 渲染耦合
  const stateRef = useRef({
    slot: 0, // 绝对主题序号（抖动 seed 用，跨轮不重复）
    startAt: 0, // 当前主题开始的时间戳
    fading: false,
    plans: new Map<number, { strokes: Stroke[]; natural: number; aliveAt: number; speed: number }>(),
  })

  const per = perThemeMS > 0 ? perThemeMS : 4000

  useEffect(() => {
    stateRef.current.fading = fading
  }, [fading])

  // 主题轮换（label/进度点跟随；canvas 从 ref 里读同一序号）
  useEffect(() => {
    if (fading || reduced || themes.length <= 1) return
    const timer = setInterval(() => {
      stateRef.current.slot += 1
      stateRef.current.startAt = Date.now()
      setIndex((i) => (i + 1) % themes.length)
    }, per)
    return () => clearInterval(timer)
  }, [fading, reduced, themes.length, per])

  useEffect(() => {
    let alive = true
    let retry = 0

    // canvas 节点偶发晚于挂载（模板渲染时序），查不到就退避重试，
    // 兜底比空白好：巡游首屏绝不能是一张空纸。
    const attach = () => {
      if (!alive) return
      Taro.createSelectorQuery()
        .select(`#${canvasId.current}`)
        .fields({ node: true, size: true })
        .exec((res) => {
          const rect = (Array.isArray(res) ? res[0] : res) as
            | { node?: unknown; width?: number; height?: number }
            | undefined
          const node = rect?.node as
            | {
                getContext: (id: '2d') => CanvasRenderingContext2D
                requestAnimationFrame: (cb: () => void) => number
                width: number
                height: number
              }
            | undefined
          if (!alive) return
          if (!node || !rect || (rect.width ?? 0) <= 0) {
            if (retry < 5) {
              retry += 1
              setTimeout(attach, 90 * retry)
            }
            return
          }
        const dpr = Taro.getSystemInfoSync().pixelRatio || 2
        const w = rect.width ?? 0
        const h = rect.height ?? 0
        node.width = Math.round(w * dpr)
        node.height = Math.round(h * dpr)
        const ctx = node.getContext('2d')
        // contain 缩放居中：设计空间不裁切，四周留给纸面网格
        const fit = Math.min(w / DESIGN_W, h / DESIGN_H)
        const offX = (w - DESIGN_W * fit) / 2
        const offY = (h - DESIGN_H * fit) / 2
        stateRef.current.startAt = Date.now()

        const planOf = (slot: number) => {
          const st = stateRef.current
          let plan = st.plans.get(slot)
          if (!plan) {
            const category = themes[slot % themes.length]?.theme ?? 'outfit'
            const built = buildPlan(category, slot)
            // 描绘占每主题停留的 ~72%，其余留给「画完端详」；
            // 自然时长不足时加速补齐，超长时压到 85% 上限（必能画完才谈切换）
            const speed = Math.min(Math.max((per * 0.72) / built.natural, 1), (per * 0.85) / built.natural)
            plan = { ...built, speed }
            st.plans.set(slot, plan)
            if (st.plans.size > PLAN_CACHE) {
              const oldest = st.plans.keys().next().value
              if (oldest !== undefined && oldest !== slot) st.plans.delete(oldest)
            }
          }
          return plan
        }

        const drawPartial = (stroke: Stroke, progress: number, alpha: number, breatheY: number) => {
          const style = LAYER_STYLE[stroke.layer]
          const yOff = stroke.figure ? breatheY : 0
          ctx.globalAlpha = Math.min(1, style.alpha * alpha)
          ctx.strokeStyle = style.color
          ctx.lineWidth = style.width
          ctx.lineCap = 'round'
          ctx.lineJoin = 'round'
          ctx.setLineDash(style.dash ?? [])
          ctx.beginPath()
          const target = (stroke.lens[stroke.lens.length - 1] ?? 0) * progress
          const first = stroke.pts[0]
          if (!first) {
            ctx.stroke()
            ctx.setLineDash([])
            return
          }
          ctx.moveTo(first.x, first.y + yOff)
          for (let i = 1; i < stroke.pts.length; i++) {
            const pt = stroke.pts[i]
            const prevPt = stroke.pts[i - 1]
            const len = stroke.lens[i] ?? 0
            const prevLen = stroke.lens[i - 1] ?? 0
            if (!pt || !prevPt) break
            if (len <= target) {
              ctx.lineTo(pt.x, pt.y + yOff)
              continue
            }
            const t = len === prevLen ? 1 : (target - prevLen) / (len - prevLen)
            if (t > 0) {
              ctx.lineTo(prevPt.x + (pt.x - prevPt.x) * t, prevPt.y + (pt.y - prevPt.y) * t + yOff)
            }
            break
          }
          ctx.stroke()
          ctx.setLineDash([])
        }

        /** 画一个主题；elapsed=Infinity 时画完整定格。返回其计划（呼吸相位用） */
        const drawTheme = (slot: number, elapsed: number, alpha: number, breathe: number) => {
          const plan = planOf(slot)
          const t = elapsed / plan.speed
          let pen: Pt | null = null
          let penStart = -1
          for (const stroke of plan.strokes) {
            const local = t - stroke.start
            if (local < 0) continue
            const p = Math.min(1, local / stroke.dur)
            drawPartial(stroke, stroke.fade ? 1 : p, stroke.fade ? p * alpha : alpha, breathe)
            // 笔尖跟「最晚开始且还在画」的那笔（回描重叠时跟随肯定的线）
            if (!stroke.fade && local < stroke.dur && stroke.start >= penStart) {
              penStart = stroke.start
              const target = (stroke.lens[stroke.lens.length - 1] ?? 0) * p
              for (let i = 1; i < stroke.pts.length; i++) {
                const pt = stroke.pts[i]
                const prevPt = stroke.pts[i - 1]
                const len = stroke.lens[i] ?? 0
                const prevLen = stroke.lens[i - 1] ?? 0
                if (!pt || !prevPt || len < target) continue
                const r = len === prevLen ? 1 : (target - prevLen) / (len - prevLen)
                pen = {
                  x: prevPt.x + (pt.x - prevPt.x) * r,
                  y: prevPt.y + (pt.y - prevPt.y) * r + (stroke.figure ? breathe : 0),
                }
                break
              }
            }
          }
          if (pen && alpha > 0.5) {
            ctx.globalAlpha = 0.85 * alpha
            ctx.fillStyle = MOSS
            ctx.beginPath()
            ctx.arc(pen.x, pen.y, 3.2, 0, Math.PI * 2)
            ctx.fill()
          }
          return plan
        }

        const frame = () => {
          if (!alive) return
          const st = stateRef.current
          ctx.setTransform(1, 0, 0, 1, 0, 0)
          ctx.clearRect(0, 0, node.width, node.height)
          ctx.setTransform(fit, 0, 0, fit, offX, offY)
          const elapsed = reduced ? Number.MAX_SAFE_INTEGER / 1e6 : Date.now() - st.startAt
          // 呼吸：人物画完后 3.4s 一拍的轻微起伏
          const current = drawTheme(st.slot, elapsed, 1, 0)
          const breathe =
            elapsed / current.speed > current.aliveAt
              ? Math.sin(((elapsed / current.speed - current.aliveAt) / 3400) * Math.PI * 2) * -2.5
              : 0
          // 前一主题淡出（cross-fade：旧图渐隐、新图开画）
          if (elapsed < CROSSFADE_MS && st.slot > 0) {
            drawTheme(st.slot - 1, Number.MAX_SAFE_INTEGER / 1e6, 1 - elapsed / CROSSFADE_MS, breathe)
          }
          if (st.fading) return // 揭晓递交：定格停笔（canvas 保留最后一帧，外层 CSS 淡出）
          node.requestAnimationFrame(frame)
        }
        if (reduced) {
          frame() // 减动效：静态画完即止，不进循环
        } else {
          node.requestAnimationFrame(frame)
        }
        })
    }
    Taro.nextTick(attach)
    return () => {
      alive = false
    }
  }, [themes, per, reduced])

  const label = themes[index]?.label ?? ''
  return (
    <View className={`dm-sketch ${fading ? 'is-fading' : ''}`}>
      <View className="dm-sketch__grid" />
      <Canvas type="2d" id={canvasId.current} className="dm-sketch__canvas" />
      {seq ? <Text className="dm-sketch__mark">{seq}</Text> : null}
      <View className="dm-sketch__dots">
        {themes.map((theme, i) => (
          <View key={`${theme.theme}-${theme.label}-${i}`} className={`dm-sketch__dot ${i === index ? 'is-on' : ''}`} />
        ))}
      </View>
      <Text className="dm-sketch__label">{label}</Text>
    </View>
  )
}
