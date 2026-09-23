// 动效常量 —— 平台中立纯数据（§4.3 动效系统参数）。
// 小程序侧在 wxss/组件里使用；手机端 Reanimated 3 以同参数复刻。
// 改动任何参数必须同步 appearance-coach-prototype/docs/implementation-plan.md §4.3。

export interface SpringConfig {
  stiffness: number
  damping: number
  mass: number
}

/** 页面 push/pop（手机端 Reanimated；小程序沿用系统转场） */
export const spring: SpringConfig = { stiffness: 360, damping: 38, mass: 0.9 }

/** BottomSheet 进场（更慢更重） */
export const sheetIn: SpringConfig = { stiffness: 500, damping: 43, mass: 0.9 }

/** BottomSheet 出场 */
export const sheetOut: SpringConfig = { stiffness: 250, damping: 30, mass: 1.05 }

/** 边缘右滑返回：起始触点在左缘 28px 内，松手判定阈值 */
export const backGestureThreshold = { edge: 28, x: 92, velocity: 0.45 } as const

/** BottomSheet 下拉关闭阈值 */
export const sheetDismissThreshold = { y: 96, velocity: 0.55 } as const

/** 时长（ms） */
export const durations = {
  fadeUp: 520,
  press: 160,
  select: 200,
  scan: 2000,
  shimmer: 1300,
  checkPop: 250,
  previewFade: 300,
  countUp: 300,
  archReveal: 600,
  drawer: 300,
  sheetInMs: 320,
  sheetOutMs: 240,
  mask: 160,
  halo: 4000,
  stepPulse: 1600
} as const

/** 级联入场步进（报告页 findings chips 等，80ms 递增） */
export const staggerStep = 80

/** 页面入场结束后再钉住，避免中途切走再回来重播 */
export const settlePad = 240

/** 动效时间轴：入场 / 浏览 / 操作 / 结果 错峰，不同时抢注意力 */
export const timelines = {
  enter: 'fade-up',
  browse: 'halo / scan / pulse',
  act: 'press / select / drawer',
  result: 'checkPop / previewFade / archReveal'
} as const

/** 标准曲线（品牌缓动）：入场/转场一律用它 */
export const easing = 'cubic-bezier(.2,.8,.2,1)' as const

/** prefers-reduced-motion: reduce 时动画压到 0.01ms（不许直接 display:none） */
export const reducedMotionDuration = 0.01

/** 扫描线辉光（分析页） */
export const scanGlow = '0 0 18px 5px rgba(111,143,89,.24)' as const

/** 今日卡呼吸辉光 halo（首页，4s 循环） */
export const haloGlow = '0 0 24px 8px rgba(88,115,68,.18)' as const

/** 对比滑块手柄描边（方案页） */
export const compareHandleRing = '0 0 0 6rpx rgba(88,115,68,.15)' as const
