// 每日内容的动画播放器：按服务端下发的脚本播，自己不做任何编排决策。
//
// 三件事它都不管：播什么（stage.kind）、变什么（form）、节奏多快（tempo）。
// 它只负责：按时序切换步骤、维护残影、结束后通知父组件揭晓。
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
  reducedPlan,
  roamPerThemeMS,
  roamThemes,
  type ConvergeStep,
  type MotionPresentation,
  type RoamTheme,
} from '@zsm/core'
import { formOf, resolveAxisValue, variantCountsOf, type FormSpec } from './forms'
import './index.scss'

interface DailyMotionProps {
  /** 服务端下发的表演脚本（prepare 带巡游、generate 带收敛） */
  presentation?: MotionPresentation | null
  /** waiting=播巡游；settling=播收敛 */
  phase: 'waiting' | 'settling'
  /** gene 色板（客户端注入——服务端脚本是通用的，不携带用户隐私） */
  palette?: string[]
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
  onSettled,
  reduced = false,
}: DailyMotionProps) {
  const stages = presentation?.stages ?? []
  const roamStage = useMemo(() => stages.find((s) => s.phase === 'roam'), [stages])
  const settleStage = useMemo(() => stages.find((s) => s.phase === 'settle'), [stages])

  const [themeIndex, setThemeIndex] = useState(0)
  const [step, setStep] = useState<ConvergeStep | null>(null)
  const [ghosts, setGhosts] = useState<ConvergeStep[]>([])
  const settled = useRef(false)

  // ---------- 巡游：等待期把几个方向都过一遍 ----------
  // 巡游是通用内容，不该等网络：prepare 没回来（首屏 loading）也要立刻有东西可播，
  // 否则等待期会退成一个圆圈，看起来像卡住。服务端脚本到了就覆盖内置主题。
  const themes = useMemo<RoamTheme[]>(() => {
    const fromServer = roamThemes(roamStage)
    if (fromServer.length > 0) return fromServer
    return DAILY_COPY.motionRoamThemes.map((theme) => ({
      theme: theme.form,
      form: theme.form,
      label: theme.label,
    }))
  }, [roamStage])
  const perTheme = roamPerThemeMS(roamStage)

  useEffect(() => {
    if (phase !== 'waiting' || themes.length === 0 || reduced) return
    const timer = setInterval(() => setThemeIndex((i) => (i + 1) % themes.length), perTheme)
    return () => clearInterval(timer)
  }, [phase, themes.length, perTheme, reduced])

  // ---------- 收敛：从反例乱跳到定格在答案 ----------
  useEffect(() => {
    if (phase !== 'settling') return
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
  }, [phase, settleStage, reduced, onSettled])

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

  if (phase === 'waiting') {
    // 内置主题兜底后 themes 不会为空；真为空时 formOf 也会回落到通用形态。
    const theme = themes[themeIndex % themes.length]
    const spec = formOf(theme?.form)
    return (
      <View className="dm">
        {paint(spec, roamValues(spec), 0, 'lock')}
        <Text className="dm__label">{theme?.label || DAILY_COPY.eyebrow}</Text>
      </View>
    )
  }

  const spec = formOf(settleStage?.form)
  if (!step) return <View className="dm" />
  const ghostOpacity = (amp: number) => Math.max(0, Math.min(0.5, amp * 0.5))
  return (
    <View className="dm">
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
    </View>
  )
}
