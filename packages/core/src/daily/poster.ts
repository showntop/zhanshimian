// 首页「今天这一条」的海报**容器**规格。
//
// 分工（走过一次弯路，务必分清）：
//   本文件        = 装在哪。底板色调、承载块位置、文字位置、视觉画框。按内容类型给。
//   content.visual = 画什么。色卡 / 位置图 / 对比 / 素材，每条内容自己声明。
//
// 曾经的错误：忽略 content.visual，改用 type 出一套固定方块布局。
// 结果「驼色是个陷阱」声明要画"身体三个位置"，却得到两块灰方块——
// 视觉与内容对不上，观感就是"方框表达力弱"。
// 结论：**表达力来自画对了东西，不来自换了什么材质。**
//
// 硬约束：
//   【画框】stage 的 top 不低于 8%、下沿不超过 38%
//     上界：左上角有编号（09.19）、右上角有竖排标，压到 3%~5% 会叠。
//     下界：承载块名义 top 44%，但有 -24rpx 负 inset，24rpx ÷ 520rpx ≈ 4.6%，
//           实际顶边在 39.4%。落在 38% 以下会被承载块盖掉。
//
//   【收口】plate.top + 承载块高度 ≤ 100%，否则溢出到下方区块。
//     按目前文案量（标题 2 行 + 钩子 2 行 + 按钮）承载块约需 55%。

import type { ContentType } from './types.ts'

export type PosterTone = 'light' | 'dark'

export interface PosterPlate {
  top: string
  rotate: number
  bg: string
  /** 承载块明暗 —— 决定压在它上面的文字用什么色。不填则跟随底板 */
  tone?: PosterTone
}

export interface PosterShell {
  /** 底板渐变，决定整体氛围 */
  base: string
  tone: PosterTone
  /** 左上角竖排小标 */
  vmark: string
  plate: PosterPlate
  /** 视觉画框：内容图形（色卡 / 位置图 / 对比）画在这个范围内 */
  stage: { left: string; top: string; width: string; height: string }
  /** 文字块：水平位置与宽度（纵向由承载块决定） */
  text: { left: string; width: string }
}

// 09-21 重排：八类 = 八个杂志版面，不再是"同一版面换八张皮"。
// 三条变化轴（都在硬约束内）：明暗对半（四浅四深，页面不再闷）、
// 构图镜像（文字块/画框左右换位）、画框宽窄（通栏 / 收窄 / 偏置）。
// 不变的（家族基因）：承载块负 inset + 拱角 + 旋转、幽灵竖排标、衬线编号。
// 同日分格扩充：hair（浅暖灰燕麦）、makeup（浅陶土）、accessory（深橄榄灰）
// 三个版面沿用同一套轴——浅色继续给「纸上的东西」（发型/妆容是贴身主题，
// 落纸更可信），深橄榄灰给配饰（器物感）。
export const SHELL_BY_TYPE: Record<ContentType, PosterShell> = {
  // 配色：米色纸底 = 实体色卡本。色票在纸上比在深底上更可信（配色类是高频类型，
  // 它翻浅色是"页面不闷"的最大杠杆）
  color: {
    base: 'linear-gradient(158deg,#F0EDE2 0%,#E8E4D4 55%,#DDD8C4 100%)',
    tone: 'dark',
    vmark: 'COLOR',
    plate: { top: '44%', rotate: -3, bg: '#1B1F19', tone: 'light' },
    stage: { left: '6%', top: '10%', width: '88%', height: '27%' },
    text: { left: '11%', width: '58%' },
  },
  // 轮廓：燕麦暖灰纸 + 整版右倾（画框与文字块都靠右，唯一右重版面）
  silhouette: {
    base: 'linear-gradient(156deg,#D8D2C2 0%,#CDC6B4 55%,#C0B8A4 100%)',
    tone: 'dark',
    vmark: 'SILHOUETTE',
    plate: { top: '46%', rotate: 2, bg: '#333B2C', tone: 'light' },
    stage: { left: '26%', top: '10%', width: '68%', height: '26%' },
    text: { left: '34%', width: '58%' },
  },
  // 比例：冷调青蓝（全表唯一冷色），左文右图的镜像版面
  proportion: {
    base: 'linear-gradient(162deg,#2C4A50 0%,#264247 55%,#1B3438 100%)',
    tone: 'light',
    vmark: 'PROPORTION',
    plate: { top: '44%', rotate: 2, bg: '#102528' },
    stage: { left: '42%', top: '10%', width: '52%', height: '27%' },
    text: { left: '8%', width: '46%' },
  },
  // 材质：浅灰绿 + 右文左图（与比例互为镜像，一冷一暖）
  fabric: {
    base: 'linear-gradient(158deg,#DCDCCF 0%,#CFD2C2 55%,#C6CABA 100%)',
    tone: 'dark',
    vmark: 'FABRIC',
    plate: { top: '46%', rotate: -2, bg: '#33402D', tone: 'light' },
    stage: { left: '6%', top: '10%', width: '56%', height: '26%' },
    text: { left: '38%', width: '56%' },
  },
  // 场合：暖石墨（不带绿相，与深绿系拉开），画框收窄居中
  occasion: {
    base: 'linear-gradient(155deg,#4A463F 0%,#403C36 55%,#333029 100%)',
    tone: 'light',
    vmark: 'OCCASION',
    plate: { top: '46%', rotate: 2, bg: '#211E1A' },
    stage: { left: '12%', top: '10%', width: '76%', height: '27%' },
    text: { left: '10%', width: '56%' },
  },
  // 技巧：中绿提亮（曾是闷的元凶之一），画框微收、文字微进
  howto: {
    base: 'linear-gradient(160deg,#42604A 0%,#3A5342 55%,#2C4033 100%)',
    tone: 'light',
    vmark: 'HOW-TO',
    plate: { top: '44%', rotate: -2, bg: '#17241B' },
    stage: { left: '10%', top: '10%', width: '84%', height: '27%' },
    text: { left: '12%', width: '60%' },
  },
  // 单品：全表最亮的暖米白 + 浅承载块（浅压浅，靠投影分层），画框居中收窄
  item: {
    base: 'linear-gradient(160deg,#E9E5D8 0%,#DFDACB 100%)',
    tone: 'dark',
    vmark: 'ITEM',
    plate: { top: '46%', rotate: -2, bg: '#F7F5EE', tone: 'dark' },
    stage: { left: '18%', top: '9%', width: '64%', height: '28%' },
    text: { left: '11%', width: '56%' },
  },
  // 综合：蓝石墨，文字块右缩进是它的签名（归不进其余各格的内容，用不带色相倾向的底）
  general: {
    base: 'linear-gradient(158deg,#45484F 0%,#3A3D44 55%,#2E3136 100%)',
    tone: 'light',
    vmark: 'GENERAL',
    plate: { top: '44%', rotate: 2, bg: '#1F2126' },
    stage: { left: '6%', top: '10%', width: '88%', height: '26%' },
    text: { left: '30%', width: '58%' },
  },
  // 发型：浅暖灰燕麦（贴身主题落纸更可信），画框右收窄、文字块靠左——与面料互为镜像
  hair: {
    base: 'linear-gradient(157deg,#E2DAC6 0%,#D6CCB4 55%,#C9BEA2 100%)',
    tone: 'dark',
    vmark: 'HAIR',
    plate: { top: '45%', rotate: 3, bg: '#2A2E24', tone: 'light' },
    stage: { left: '34%', top: '10%', width: '58%', height: '26%' },
    text: { left: '10%', width: '56%' },
  },
  // 妆容：浅陶土（家族里唯一的陶土色相），画框居中收窄、文字块微进
  makeup: {
    base: 'linear-gradient(160deg,#E9D8C6 0%,#DEC7B0 55%,#D2B99F 100%)',
    tone: 'dark',
    vmark: 'MAKEUP',
    plate: { top: '45%', rotate: -2, bg: '#3A2E24', tone: 'light' },
    stage: { left: '16%', top: '10%', width: '68%', height: '26%' },
    text: { left: '14%', width: '58%' },
  },
  // 配饰：深橄榄灰（器物感），画框偏左收窄、文字块靠右——与发型互为镜像
  accessory: {
    base: 'linear-gradient(157deg,#424A3E 0%,#38402F 55%,#2C3226 100%)',
    tone: 'light',
    vmark: 'ACCESSORY',
    plate: { top: '46%', rotate: 2, bg: '#1D2118' },
    stage: { left: '8%', top: '10%', width: '56%', height: '26%' },
    text: { left: '36%', width: '56%' },
  },
}

export function shellFor(type: ContentType): PosterShell {
  return SHELL_BY_TYPE[type] ?? SHELL_BY_TYPE.color
}

/** 压在承载块上的文字用什么色：优先取承载块自己的声明，否则跟随底板 */
export function plateToneFor(shell: PosterShell): PosterTone {
  return shell.plate.tone ?? shell.tone
}
