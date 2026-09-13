// 静态门禁的单元测试：每条硬规则都用一个内联 fixture 证明它真的会拦。
import test from 'node:test'
import assert from 'node:assert/strict'
import { checkSourceFile } from './check.mjs'

test('static rules reject forbidden miniapp patterns', () => {
  assert.deepEqual(
    checkSourceFile('feature.scss', '.x { width: 10px; }'),
    ['scss 裸 px（须 rpx）: feature.scss:1'],
  )
  assert.deepEqual(
    checkSourceFile('feature.tsx', '{count && <View />}'),
    ['条件渲染可能渲染 0: feature.tsx:1'],
  )
  assert.deepEqual(
    checkSourceFile('feature.tsx', 'items.map((item, index) => <View key={index} />)'),
    ['列表禁止下标 key: feature.tsx:1'],
  )
  assert.deepEqual(
    checkSourceFile('feature.tsx', "const x = '/assets/a.webp'"),
    ['禁止 WebP: feature.tsx:1'],
  )
})

test('boolean guards and string keys are not violations', () => {
  assert.deepEqual(checkSourceFile('feature.tsx', '{ready && <View />}'), [])
  assert.deepEqual(checkSourceFile('feature.tsx', '{items.length > 0 && <List />}'), [])
  assert.deepEqual(checkSourceFile('feature.tsx', '<View key={item.id} />'), [])
  assert.deepEqual(checkSourceFile('feature.scss', '.x { width: 10rpx; box-shadow: 0 2px 4px; }'), [])
  assert.deepEqual(checkSourceFile('feature.tsx', "// 背景 16px 占位"), [])
})

test('pinned images, provider sniffing and business storage are banned', () => {
  assert.match(
    checkSourceFile('feature.tsx', 'const u = pinnedUrl(media)').join(' '),
    /禁止钉住旧图/,
  )
  assert.match(
    checkSourceFile('feature.tsx', "const demo = (item.provider_version ?? '').startsWith('demo')").join(' '),
    /禁止按 provider 判定演示图/,
  )
  assert.match(
    checkSourceFile('feature.tsx', 'if (isBundledAsset(url)) {}').join(' '),
    /禁止 isBundledAsset/,
  )
  assert.match(
    checkSourceFile('feature.tsx', "readStorage(STORAGE_KEYS.reportId)").join(' '),
    /禁止业务 Storage key/,
  )
})

test('task polling is banned everywhere; operation polling only in the wrapper', () => {
  assert.match(
    checkSourceFile('pages/home/index.tsx', 'useTaskPolling({})').join(' '),
    /禁止任务轮询/,
  )
  assert.deepEqual(
    checkSourceFile('app/operations/use-operation-polling.ts', 'createOperationPolling({})'),
    [],
  )
  assert.match(
    checkSourceFile('pages/home/index.tsx', 'createOperationPolling({})').join(' '),
    /轮询只能经 useOperationPolling/,
  )
})

test('main-loop pages may not import api, cache or storage layers', () => {
  assert.match(
    checkSourceFile('pages/report/index.tsx', "import { qualityApi } from '../../app/api/quality'").join(' '),
    /主闭环页面不得/,
  )
  assert.deepEqual(
    checkSourceFile('pages/home/index.tsx', "import { qualityApi } from '../../app/api/quality'"),
    [],
  )
  assert.match(
    checkSourceFile('pages/capture/index.tsx', "import { resourceCache } from '../../app/cache/resource-cache'").join(' '),
    /主闭环页面不得/,
  )
})
