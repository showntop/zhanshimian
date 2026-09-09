// @zsm/design — 唯一手写数据源（纯 JS，零依赖，供 scripts/generate.mjs import）。
// 权威表：appearance-coach-prototype/docs/implementation-plan.md §4.2（品牌值为准）。
// 约定：颜色以 CSS 字符串原样输出；尺寸/圆角/间距/字号一律以 px 数值存储，
// 生成器负责 px×2 → rpx（designWidth: 750）。禁止在此出现 rpx。

// §4.2 品牌色（对齐 brand/README.md，修复 G4 漂移）
// mossDeep/cream/lineOnDeep：atelier hero 层 —— 深墨绿底 + 柔米文字（重设计新增）。
export const colors = {
  bg: '#F8F5F0',
  surface: 'rgba(255,255,255,.78)',
  surfaceStrong: '#FFFFFF',
  ink: '#252725',
  ink2: '#656B64',
  ink3: '#747A73',
  moss: '#587344',
  mossPressed: '#486238',
  mossSoft: '#EEF2E9',
  mossDeep: '#39492E',
  cream: '#F4EFE6',
  line: 'rgba(69,78,64,.14)',
  lineOnDeep: 'rgba(244,239,230,.22)',
  danger: '#9B4B45',
  warn: '#9B6D58',
  badge: 'rgba(30,35,29,.62)'
}

// 圆角（px；胶囊为特殊值，wxss 侧恒为 999rpx，RN 侧给 999 即可近似胶囊）
export const radius = {
  sm: 8,
  md: 10,
  lg: 12,
  xl: 14,
  pill: 999
}

// 8pt 间距体系（px）
export const space = {
  xs: 4,
  sm: 8,
  md: 12,
  lg: 16,
  xl: 20,
  xxl: 24,
  xxxl: 32
}

// 关键控件尺寸（px）
export const sizes = {
  controlHeight: 48, // 主按钮最小高（96rpx）
  touchTarget: 44,   // 触控热区下限（88rpx）
  pageGutter: 16     // 页面左右留白（32rpx）
}

// 排版字号（px；.display 38rpx/.title 50rpx/.section-title 32rpx/.eyebrow 23rpx/.lede 26rpx/正文 28rpx/strong 30rpx/note 20rpx）
export const type = {
  base: 14,
  lede: 13,
  strong: 15,
  sectionTitle: 16,
  title: 25,
  display: 19,
  eyebrow: 11.5,
  note: 10
}

// 阴影（px；分层体系：raised 卡 / hero 深绿卡 / 主按钮 / 弹层，其余一律发丝描边）
// primaryButton 来自 §4.2 的 12rpx/28rpx（即 6px/14px）；card/sheet 原表即 px。
export const shadows = {
  primaryButton: { y: 6, blur: 14, color: 'rgba(75,100,57,.16)' },
  card: { y: 6, blur: 18, color: 'rgba(52,59,47,.07)' },
  heroCard: { y: 10, blur: 28, color: 'rgba(45,58,35,.24)' },
  sheet: { y: -16, blur: 48, color: 'rgba(0,0,0,.2)' }
}
