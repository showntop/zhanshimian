// tokens.data.mjs 的类型声明（同目录邻接声明，供 TS import 使用）
export interface ShadowToken {
  y: number
  blur: number
  color: string
}

export declare const colors: {
  readonly bg: '#F4F5F2'
  readonly surface: 'rgba(255,255,255,.82)'
  readonly surfaceStrong: '#FFFFFF'
  readonly ink: '#202320'
  readonly ink2: '#626862'
  readonly ink3: '#686D67'
  readonly moss: '#587344'
  readonly mossPressed: '#486238'
  readonly mossSoft: '#EDF1EA'
  readonly mossDeep: '#39492E'
  readonly cream: '#EEF0EC'
  readonly line: 'rgba(55,62,52,.12)'
  readonly lineOnDeep: 'rgba(238,240,236,.24)'
  readonly danger: '#9B4B45'
  readonly warn: '#7E5445'
  readonly badge: 'rgba(30,35,29,.62)'
}
export declare const radius: { readonly sm: 8; readonly md: 10; readonly lg: 12; readonly xl: 14; readonly pill: 999 }
export declare const space: { readonly xs: 4; readonly sm: 8; readonly md: 12; readonly lg: 16; readonly xl: 20; readonly xxl: 24; readonly xxxl: 32 }
export declare const sizes: { readonly controlHeight: 48; readonly touchTarget: 44; readonly pageGutter: 16 }
export declare const type: { readonly base: 14; readonly lede: 13; readonly strong: 15; readonly sectionTitle: 16; readonly title: 25; readonly display: 19; readonly eyebrow: 11.5; readonly note: 10 }
export declare const shadows: {
  readonly primaryButton: ShadowToken
  readonly card: ShadowToken
  readonly heroCard: ShadowToken
  readonly sheet: ShadowToken
}
