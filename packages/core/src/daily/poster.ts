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

import type { ContentType } from './types'

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

export const SHELL_BY_TYPE: Record<ContentType, PosterShell> = {
  color: {
    base: 'linear-gradient(155deg,#3A3F38 0%,#2B302A 55%,#22261F 100%)',
    tone: 'light',
    vmark: 'COLOR',
    plate: { top: '44%', rotate: -3, bg: '#161A15' },
    stage: { left: '6%', top: '10%', width: '88%', height: '27%' },
    text: { left: '11%', width: '58%' },
  },
  silhouette: {
    base: 'linear-gradient(155deg,#38452F 0%,#42503A 55%,#2C3826 100%)',
    tone: 'light',
    vmark: 'SILHOUETTE',
    plate: { top: '44%', rotate: 2, bg: '#1C2517' },
    stage: { left: '6%', top: '10%', width: '88%', height: '27%' },
    text: { left: '10%', width: '58%' },
  },
  proportion: {
    base: 'linear-gradient(160deg,#1F3A3E 0%,#28484D 55%,#17302F 100%)',
    tone: 'light',
    vmark: 'PROPORTION',
    plate: { top: '44%', rotate: -3, bg: '#0D2124' },
    stage: { left: '6%', top: '10%', width: '88%', height: '27%' },
    text: { left: '30%', width: '58%' },
  },
  fabric: {
    base: 'linear-gradient(158deg,#DCDCCF 0%,#CFD2C2 55%,#C6CABA 100%)',
    tone: 'dark',
    vmark: 'FABRIC',
    plate: { top: '44%', rotate: -2, bg: '#33402D', tone: 'light' },
    stage: { left: '6%', top: '10%', width: '88%', height: '27%' },
    text: { left: '11%', width: '58%' },
  },
  occasion: {
    base: 'linear-gradient(155deg,#333B34 0%,#3E4740 55%,#2A322B 100%)',
    tone: 'light',
    vmark: 'OCCASION',
    plate: { top: '44%', rotate: 2, bg: '#181E1A' },
    stage: { left: '6%', top: '10%', width: '88%', height: '27%' },
    text: { left: '10%', width: '58%' },
  },
  howto: {
    base: 'linear-gradient(160deg,#2F4436 0%,#3A5342 55%,#26382C 100%)',
    tone: 'light',
    vmark: 'HOW-TO',
    plate: { top: '44%', rotate: -2, bg: '#17241B' },
    stage: { left: '6%', top: '10%', width: '88%', height: '27%' },
    text: { left: '10%', width: '56%' },
  },
  item: {
    base: 'linear-gradient(160deg,#D2D7CA 0%,#C4CBBB 100%)',
    tone: 'dark',
    vmark: 'ITEM',
    plate: { top: '44%', rotate: -2, bg: '#F4F5F0', tone: 'dark' },
    stage: { left: '6%', top: '10%', width: '88%', height: '27%' },
    text: { left: '11%', width: '56%' },
  },
}

export function shellFor(type: ContentType): PosterShell {
  return SHELL_BY_TYPE[type] ?? SHELL_BY_TYPE.color
}

/** 压在承载块上的文字用什么色：优先取承载块自己的声明，否则跟随底板 */
export function plateToneFor(shell: PosterShell): PosterTone {
  return shell.plate.tone ?? shell.tone
}
