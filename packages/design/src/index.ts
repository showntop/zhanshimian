// @zsm/design 统一导出。
// - tokens / motion：源码直读（两端 alias 源码直引共享包）
// - theme：由 scripts/generate.mjs 生成的 dist/theme.ts（RN 用 px 数值）
// - dist/tokens.wxss 经 `@zsm/design/dist/tokens.wxss` 引入（小程序 app 全局变量）
export * from './tokens'
export * from './motion'
export { theme } from '../dist/theme'
export type { Theme } from '../dist/theme'
