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
  readonly scrim: 'rgba(30,35,29,.58)'
  readonly scrimHeavy: 'rgba(30,35,29,.72)'
  readonly glass: 'rgba(244,245,242,.92)'
  readonly glassOnPhoto: 'rgba(24,28,22,.82)'
  readonly onPhoto: 'rgba(255,255,255,.92)'
  readonly onPhotoMuted: 'rgba(255,255,255,.72)'
  readonly dock: 'rgba(244,245,242,.92)'
  readonly disabled: '#CDD3C8'
  readonly creamMuted: 'rgba(238,240,236,.72)'
}
export declare const radius: { readonly sm: 8; readonly md: 10; readonly lg: 12; readonly xl: 14; readonly pill: 999 }
export declare const space: { readonly xs: 4; readonly sm: 8; readonly md: 12; readonly lg: 16; readonly xl: 20; readonly xxl: 24; readonly xxxl: 32 }
export declare const sizes: { readonly controlHeight: 48; readonly touchTarget: 44; readonly pageGutter: 16; readonly tagWidth: 90; readonly filmThumbW: 48; readonly filmThumbH: 64 }
export declare const type: { readonly base: 14; readonly lede: 14; readonly strong: 15; readonly sectionTitle: 16; readonly title: 25; readonly display: 19; readonly eyebrow: 12; readonly note: 12 }
export declare const shadows: {
  readonly primaryButton: ShadowToken
  readonly card: ShadowToken
  readonly heroCard: ShadowToken
  readonly sheet: ShadowToken
  readonly float: ShadowToken
  readonly annotation: ShadowToken
}
