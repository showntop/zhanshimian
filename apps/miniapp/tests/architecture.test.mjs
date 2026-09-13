// 架构红线：主闭环页面只能是薄壳；轮询只有一个入口；主闭环不再碰业务 Storage。
import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync, readdirSync } from 'node:fs'
import { join } from 'node:path'
import { fileURLToPath } from 'node:url'

const srcRoot = fileURLToPath(new URL('../src/', import.meta.url))
const routeNames = [
  'capture',
  'analysis',
  'report',
  'scene',
  'plans',
  'plan',
  'checklist',
  'feedback',
]

const scan = (dir) =>
  readdirSync(dir, { withFileTypes: true }).flatMap((entry) => {
    const path = join(dir, entry.name)
    return entry.isDirectory() ? scan(path) : [path]
  })

test('main-loop pages are thin and do not own api, storage or polling', () => {
  for (const name of routeNames) {
    const source = readFileSync(join(srcRoot, 'pages', name, 'index.tsx'), 'utf8')
    assert.ok(source.split('\n').length <= 24, `${name} page is not thin`)
    assert.doesNotMatch(source, /qualityApi|resourceCache|Storage|Polling/)
    assert.match(source, /features\//)
  }
})

test('only the operation wrapper may create polling', () => {
  const allowed = join(srcRoot, 'app', 'operations', 'use-operation-polling.ts')
  // 主闭环范围（pages/features/app）不允许旧轮询；外围页（packages/）与
  // services/hooks 里的旧任务轮询归后续迁移完成后纳入全量扫描。
  for (const dir of ['pages', 'features', 'app']) {
    for (const file of scan(join(srcRoot, dir)).filter((path) => /\.(ts|tsx)$/.test(path))) {
      const source = readFileSync(file, 'utf8')
      if (file === allowed) continue
      assert.doesNotMatch(source, /createOperationPolling|useTaskPolling|createTaskPolling/)
    }
  }
})

test('the main loop reads no business storage keys', () => {
  // 业务 id 的本地持久化随主闭环重构全部停用；外围页遗留的 key（reportId、
  // 各 session）随外围迁移一并删除，所以这里断言的是「谁在读」，而不是 storage.ts 全文。
  const businessKey = /STORAGE_KEYS\.(reportId|planId|savedPlanId|activeTask|scene|outfit|purchase|advisor)/
  for (const name of routeNames) {
    const source = readFileSync(join(srcRoot, 'pages', name, 'index.tsx'), 'utf8')
    assert.doesNotMatch(source, businessKey, `${name} still reads business storage`)
  }
  const app = readFileSync(join(srcRoot, 'app.ts'), 'utf8')
  assert.doesNotMatch(app, businessKey)
})

test('no miniapp module imports old api or hand-written dto surfaces', () => {
  for (const file of scan(srcRoot).filter((path) => /\.(ts|tsx)$/.test(path))) {
    const source = readFileSync(file, 'utf8')
    assert.doesNotMatch(source, /services\/api/, file)
    assert.doesNotMatch(source, /API_PATHS|createApiEndpoints/, file)
    assert.doesNotMatch(source, /getTask\(|getTasks\(|active_tasks/, file)
  }
})
