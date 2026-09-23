// @zsm/design — 唯一手写数据源（纯 JS，零依赖，供 scripts/generate.mjs import）。
// 权威表：appearance-coach-prototype/docs/implementation-plan.md §4.2（品牌值为准）。
// 约定：颜色以 CSS 字符串原样输出；尺寸/圆角/间距/字号一律以 px 数值存储，
// 生成器负责 px×2 → rpx（designWidth: 750）。禁止在此出现 rpx。

// §4.2 品牌色（对齐 brand/README.md，修复 G4 漂移）
// 2026-03 冷调珍珠灰试验：bg 从暖米黄 #F8F5F0 → #F4F5F2，保留苔绿识别，减弱 AI 生活方式暖黄感。
// mossDeep/cream/lineOnDeep：atelier hero 层 —— 深墨绿底 + 冷调浅文字。
export const colors = {
  bg: '#F4F5F2',
  surface: 'rgba(255,255,255,.82)',
  surfaceStrong: '#FFFFFF',
  ink: '#202320',
  ink2: '#626862',
  ink3: '#686D67',
  moss: '#587344',
  mossPressed: '#486238',
  mossSoft: '#EDF1EA',
  mossDeep: '#39492E',
  cream: '#EEF0EC',
  line: 'rgba(55,62,52,.12)',
  lineOnDeep: 'rgba(238,240,236,.24)',
  danger: '#9B4B45',
  warn: '#7E5445',
  badge: 'rgba(30,35,29,.62)',
  // overlay 层：照片遮罩 / 毛玻璃坞 / 图上文字。页面禁止再写散落 rgba。
  scrim: 'rgba(30,35,29,.58)',
  scrimHeavy: 'rgba(30,35,29,.72)',
  glass: 'rgba(244,245,242,.92)',
  glassOnPhoto: 'rgba(24,28,22,.82)',
  onPhoto: 'rgba(255,255,255,.92)',
  onPhotoMuted: 'rgba(255,255,255,.72)',
  dock: 'rgba(244,245,242,.92)',
  disabled: '#CDD3C8',
  creamMuted: 'rgba(238,240,236,.72)'
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
  pageGutter: 16,    // 页面左右留白（32rpx）
  tagWidth: 90,      // 照片标注胶囊（180rpx）
  filmThumbW: 48,    // 胶片缩略宽（96rpx）
  filmThumbH: 64     // 胶片缩略高（128rpx）
}

// 排版字号（px；正文 28rpx / 说明 24rpx / 分区 32rpx / hero 38–50rpx）
export const type = {
  base: 14,
  lede: 14,
  strong: 15,
  sectionTitle: 16,
  title: 25,
  display: 19,
  eyebrow: 12,
  note: 12
}

// 阴影（px；分层体系：raised 卡 / hero 深绿卡 / 主按钮 / 弹层，其余一律发丝描边）
// primaryButton 来自 §4.2 的 12rpx/28rpx（即 6px/14px）；card/sheet 原表即 px。
export const shadows = {
  primaryButton: { y: 6, blur: 14, color: 'rgba(75,100,57,.16)' },
  card: { y: 6, blur: 18, color: 'rgba(52,59,47,.07)' },
  heroCard: { y: 10, blur: 28, color: 'rgba(45,58,35,.24)' },
  sheet: { y: -16, blur: 48, color: 'rgba(0,0,0,.2)' },
  float: { y: 8, blur: 24, color: 'rgba(31,34,30,.12)' },
  annotation: { y: 4, blur: 14, color: 'rgba(31,34,30,.18)' }
}
