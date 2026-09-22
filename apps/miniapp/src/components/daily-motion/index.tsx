// 每日内容的动画播放器：按服务端下发的脚本播，自己不做任何编排决策。
//
// 三件事它都不管：播什么（stage.kind）、变什么（form）、节奏多快（tempo）。
// 它只负责：按时序切换步骤、维护残影、结束后通知父组件揭晓。
//
// 当前能力注册表：
//   sketch_tour（roam）  → Canvas 草图巡游（sketch.tsx，零素材、可循环）
//   frames（settle）     → 序列帧揭晓（frames.tsx，线稿→成形→定格）
//   roam_tour / converge → 旧 CSS 形态动画（forms.tsx；服务端不再下发，
//                          留作协议兼容与无素材时的服务端兜底）
//
// 查不到 kind / form / 脚本缺失时一律回落（静态形态 + 立即揭晓），
// 绝不因为不认识的协议而空白或卡住。

import { useEffect, useMemo, useRef, useState } from 'react'
import { Text, View } from '@tarojs/components'
import {
  DAILY_COPY,
  planConverge,
  planDuration,
  readConvergeParams,
  readFramesParams,
  reducedPlan,
  roamPerThemeMS,
  roamThemes,
  type ConvergeStep,
  type MotionPresentation,
  type RoamTheme,
} from '@zsm/core'
import { formOf, resolveAxisValue, variantCountsOf, type FormSpec } from './forms'
import SketchTour, { FORM_CATEGORY, type SketchTheme } from './sketch'
import FramesReveal from './frames'
import './index.scss'

interface DailyMotionProps {
  /** 服务端下发的表演脚本（prepare 带巡游、generate 带收敛） */
  presentation?: MotionPresentation | null
  /** waiting=播巡游；settling=播收敛 */
  phase: 'waiting' | 'settling'
  /** gene 色板（客户端注入——服务端脚本是通用的，不携带用户隐私） */
  palette?: string[]
  /** 左上角日期编号（草图巡游的画面锚点，与海报同一语言） */
  seq?: string
  /** 收敛结束（已定格）→ 通知父组件揭晓海报 */
  onSettled?: () => void
  /** 减动效偏好：收敛塌成一步，元素保持可见 */
  reduced?: boolean
}

/** 巡游用各轴的中间变体：形态自然，也不暗示任何"答案" */
function roamValues(spec: FormSpec): number[] {
  return Object.values(spec.axes).map((axis) => Math.floor(axis.variants.length / 2))
}

export default function DailyMotion({
  presentation,
  phase,
  palette = [],
  seq,
  onSettled,
  reduced = false,
}: DailyMotionProps) {
  const stages = presentation?.stages ?? []
  const roamStage = useMemo(() => stages.find((s) => s.phase === 'roam'), [stages])
  const settleStage = useMemo(() => stages.find((s) => s.phase === 'settle'), [stages])
  const framesParams = useMemo(
    () => (settleStage?.kind === 'frames' ? readFramesParams(settleStage) : null),
    [settleStage],
  )

  const [themeIndex, setThemeIndex] = useState(0)
  const [step, setStep] = useState<ConvergeStep | null>(null)
  const [ghosts, setGhosts] = useState<ConvergeStep[]>([])
  const settled = useRef(false)

  // ---------- 巡游主题 ----------
  // 草图巡游（零素材）：服务端脚本（sketch_tour）优先；脚本没到（首屏
  // loading）用内置主题立即开画，否则等待期会退成一个圆圈，看起来像卡住。
  // 旧 roam_tour 脚本（CSS 形态）没有对应草图分类，走 legacy 分支。
  const sketchThemes = useMemo<SketchTheme[]>(() => {
    const fromServer = roamThemes(roamStage)
    if (roamStage?.kind === 'sketch_tour' && fromServer.length > 0) {
      return fromServer.map((theme) => ({ theme: theme.theme, label: theme.label }))
    }
    if (!roamStage) {
      // 兜底只取七个经典分类：后面 hair/makeup/accessory 没有对应画法，
      // 「在看发型」配一张服装草图是标签与画面打架。
      return DAILY_COPY.motionRoamThemes.slice(0, 7).map((theme) => ({
        theme: FORM_CATEGORY[theme.form] ?? 'outfit',
        label: theme.label,
      }))
    }
    return []
  }, [roamStage])
  const sketchOn = sketchThemes.length > 0
  const perTheme = roamPerThemeMS(roamStage)

  // 等待 → 收敛是同一实例换 props（presentation 从巡游脚本换成收敛脚本），
  // ro 脚本随之消失。递交恰恰需要画布保住最后一笔：这里在等待期快照
  // 主题与节奏，settling 期间 SketchTour 的 props 因此完全稳定，
  // 不会重跑效果、重画 canvas（重画 = 淡出的是一张空图）。
  const frozenRef = useRef<{ themes: SketchTheme[]; per: number } | null>(null)
  if (phase === 'waiting' && sketchOn) {
    frozenRef.current = { themes: sketchThemes, per: perTheme }
  }
  const liveSketchThemes = frozenRef.current?.themes ?? []
  const livePerTheme = frozenRef.current?.per ?? 4000

  // ---------- 巡游（legacy CSS 形态）：等待期把几个方向都过一遍 ----------
  const themesForLegacy = useMemo<RoamTheme[]>(() => {
    const fromServer = roamThemes(roamStage)
    if (fromServer.length > 0) return fromServer
    return DAILY_COPY.motionRoamThemes.map((theme) => ({
      theme: theme.form,
      form: theme.form,
      label: theme.label,
    }))
  }, [roamStage])

  const legacyWaiting = phase === 'waiting' && !sketchOn
  useEffect(() => {
    if (!legacyWaiting || themesForLegacy.length === 0 || reduced) return
    const timer = setInterval(() => setThemeIndex((i) => (i + 1) % themesForLegacy.length), perTheme)
    return () => clearInterval(timer)
  }, [legacyWaiting, themesForLegacy, perTheme, reduced])

  // ---------- 收敛（legacy CSS 形态）：从反例乱跳到定格在答案 ----------
  const legacySettling = phase === 'settling' && !framesParams
  useEffect(() => {
    if (!legacySettling) return
    settled.current = false
    if (!settleStage || settleStage.kind !== 'converge') {
      // 没有收敛脚本也要揭晓：宁可不演，不能卡住不进内容
      onSettled?.()
      return
    }
    const spec = formOf(settleStage.form)
    const params = readConvergeParams(settleStage)
    const counts = variantCountsOf(spec, params.axes)
    const resolve = (axisId: string, value: unknown) => resolveAxisValue(spec.axes[axisId], value)
    const plan = reduced
      ? reducedPlan(planConverge(params, counts, resolve))
      : planConverge(params, counts, resolve)
    if (plan.length === 0) {
      onSettled?.()
      return
    }

    const timers: ReturnType<typeof setTimeout>[] = []
    let history: ConvergeStep[] = []
    const first = plan[0]
    if (!first) {
      onSettled?.()
      return
    }
    setStep(first)
    setGhosts([])
    plan.forEach((entry) => {
      timers.push(
        setTimeout(() => {
          setGhosts(history.slice(-2))
          history = [...history, entry].slice(-3)
          setStep(entry)
        }, entry.at),
      )
    })
    timers.push(
      setTimeout(() => {
        if (settled.current) return
        settled.current = true
        onSettled?.()
      }, planDuration(plan)),
    )
    return () => timers.forEach(clearTimeout)
  }, [legacySettling, settleStage, reduced, onSettled])

  // ---------- 渲染 ----------
  // 位移随幅度衰减（聚拢感）：收敛到 0 时形态自然归位
  const paint = (spec: FormSpec, values: number[], amp: number, phaseName: string) => (
    <View
      className={`dm__shape ${phaseName === 'overshoot' ? 'is-overshoot' : ''} ${phaseName === 'lock' ? 'is-lock' : ''}`}
      style={
        {
          '--dm-dx': `${(amp * 2).toFixed(2)}%`,
          '--dm-dy': `${(-amp * 1.8).toFixed(2)}%`,
        } as never
      }
    >
      {spec.render({ values, palette })}
    </View>
  )

  // 草图在两个相位都渲染在同一个树位置：settle 开始时它停笔定格、
  // 淡出递交（React 复用实例，canvas 内容不丢——硬重挂会闪一帧空白）。
  return (
    <View className="dm">
      {liveSketchThemes.length > 0 ? (
        <SketchTour
          themes={liveSketchThemes}
          perThemeMS={livePerTheme}
          seq={seq}
          fading={phase === 'settling'}
          reduced={reduced}
        />
      ) : null}

      {phase === 'settling' && framesParams ? (
        <FramesReveal params={framesParams} onSettled={onSettled} reduced={reduced} />
      ) : null}

      {legacyWaiting
        ? (() => {
            // 内置主题兜底后 themes 不会为空；真为空时 formOf 也会回落到通用形态。
            const theme = themesForLegacy[themeIndex % Math.max(1, themesForLegacy.length)]
            const spec = formOf(theme?.form)
            return (
              <>
                {paint(spec, roamValues(spec), 0, 'lock')}
                <Text className="dm__label">{theme?.label || DAILY_COPY.eyebrow}</Text>
              </>
            )
          })()
        : null}

      {legacySettling
        ? (() => {
            const spec = formOf(settleStage?.form)
            if (!step) return null
            const ghostOpacity = (amp: number) => Math.max(0, Math.min(0.5, amp * 0.5))
            return (
              <>
                {ghosts.map((ghost, i) => (
                  <View
                    key={`${ghost.at}-${i}`}
                    className={`dm__layer dm__layer--g${ghosts.length - i}`}
                    style={{ opacity: ghostOpacity(ghost.amp) } as never}
                  >
                    {paint(spec, ghost.indices, ghost.amp, ghost.phase)}
                  </View>
                ))}
                <View className="dm__layer dm__layer--main">{paint(spec, step.indices, step.amp, step.phase)}</View>
                <Text className="dm__label">
                  {step.phase === 'lock' ? DAILY_COPY.motionLocked : DAILY_COPY.motionSettling}
                </Text>
              </>
            )
          })()
        : null}
    </View>
  )
}
