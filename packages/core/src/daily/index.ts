// 每日内容（今天这一条）
//
// 契约与选品进生产；mock 数据仅用于服务端内容接口就绪前的验证，
// 接口就绪后 MOCK_CONTENTS / MOCK_GENES 应整体移除。

export * from './types.ts'
export * from './pick.ts'
export * from './poster.ts'
export * from './visual.ts'
export { MOCK_CONTENTS, MOCK_GENES, MOCK_GENE_DEFAULT } from './mock.ts'
