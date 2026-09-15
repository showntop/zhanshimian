// 诊断会话（outfit/purchase 双文件同构）的红线：
// inflight 接管不重复发起、recentEnough && sameMedia 防旧盖新、草稿复访先展示+
// 后台校验只在更新时接管、日限 429/402 进体面错误态（不进计费购买链）。
import test from 'node:test'
import assert from 'node:assert/strict'
import { PublicApiError } from '../src/app/api/result.ts'
import {
  createOutfitSession,
  isDiagnosticDailyLimit,
  isNewerDiagnosis,
  shouldAdoptLatest,
} from '../src/services/outfit-session.ts'
import { createPurchaseSession } from '../src/services/purchase-session.ts'

/** 与 services/storage 同语义：写空串即删除。 */
const memStore = () => {
  const map = new Map()
  return {
    read: (key) => map.get(key) ?? '',
    write: (key, value) => {
      if (value) map.set(key, value)
      else map.delete(key)
    },
    map,
  }
}

const media = (assetId) => ({
  asset_id: assetId,
  url: `https://cdn/${assetId}.jpg`,
  url_expires_at: '2026-09-16T00:00:00.000Z',
  mime_type: 'image/jpeg',
  source_kind: 'user_original',
  display_label: '原本',
})

const diagnosis = (over = {}) => ({
  id: 'd-1',
  kind: 'outfit',
  scene: 'daily',
  conclusion: '结论',
  priority_title: '',
  priority_copy: '',
  tags: [],
  findings: [],
  options: [],
  saved: false,
  created_at: '2026-09-15T08:00:00.000Z',
  ...over,
})

const gate = () => {
  let release
  const promise = new Promise((resolve) => {
    release = resolve
  })
  return { promise, release }
}

// ---------- inflight 接管：不重复发起 ----------

test('inflight 期间重复发起接管同一个 Promise，诊断只跑一次', async () => {
  const session = createOutfitSession(memStore())
  session.markPending({ scene: 'daily', photoPath: '/tmp/a.jpg', mediaId: 'm-a' })
  let calls = 0
  const { promise, release } = gate()
  const item = diagnosis({ id: 'd-new', source_media: media('m-a') })
  const run = () => {
    calls += 1
    return promise.then(() => item)
  }
  const first = session.runDiagnose('m-a', run)
  const second = session.runDiagnose('m-a', run)
  assert.strictEqual(first, second)
  assert.strictEqual(session.getInflight(), first)
  release()
  const [r1, r2] = await Promise.all([first, second])
  assert.equal(calls, 1)
  assert.equal(r1.id, 'd-new')
  assert.equal(r2.id, 'd-new')
  assert.equal(session.getInflight(), null)
  assert.equal(session.read().result.id, 'd-new')
  assert.equal(session.read().pending, false)
})

test('复访时接管进行中的请求：不重新发起，完成有结果，busy 包络完整', async () => {
  const session = createOutfitSession(memStore())
  session.markPending({ scene: 'daily', photoPath: '/tmp/a.jpg', mediaId: 'm-a' })
  let diagnoseCalls = 0
  let latestCalls = 0
  const { promise, release } = gate()
  const item = diagnosis({ id: 'd-fresh', source_media: media('m-a') })
  const first = session.runDiagnose('m-a', () => {
    diagnoseCalls += 1
    return promise.then(() => item)
  })
  // 不重置 module，模拟「离开再进」：同一页面模块，inflight 还在
  const events = []
  const resumed = session.resume(
    {
      getLatest: async () => {
        latestCalls += 1
        return null
      },
      diagnose: async () => {
        diagnoseCalls += 1
        return diagnosis()
      },
    },
    (event) => events.push(event),
  )
  release()
  await Promise.all([first, resumed])
  assert.equal(diagnoseCalls, 1)
  assert.equal(latestCalls, 0)
  const results = events.filter((e) => e.type === 'result')
  assert.equal(results.length, 1)
  assert.equal(results[0].item.id, 'd-fresh')
  assert.deepEqual(
    events.filter((e) => e.type === 'busy').map((e) => e.busy),
    [true, false],
  )
})

test('复访接管进行中的请求：失败有错误态，草稿落定不再自动续跑', async () => {
  const session = createOutfitSession(memStore())
  session.markPending({ scene: 'daily', photoPath: '/tmp/a.jpg', mediaId: 'm-a' })
  const { promise, release } = gate()
  const failure = new Error('网络连接不上')
  const first = session.runDiagnose('m-a', () => promise.then(() => Promise.reject(failure)))
  const events = []
  const resumed = session.resume(
    { getLatest: async () => null, diagnose: async () => diagnosis() },
    (event) => events.push(event),
  )
  release()
  await assert.rejects(first, /网络连接不上/)
  await resumed
  const errors = events.filter((e) => e.type === 'error')
  assert.equal(errors.length, 1)
  assert.strictEqual(errors[0].error, failure)
  assert.equal(session.read().pending, false)
  assert.equal(session.read().result, null)
})

// ---------- sameMedia 防旧盖新 ----------

test('shouldAdoptLatest：够新且同一照片才接管（旧线判定）', () => {
  const startedAt = new Date('2026-09-15T08:00:00.000Z').getTime()
  const draft = { startedAt, mediaId: 'm-a' }
  const sameFresh = diagnosis({ created_at: '2026-09-15T08:00:01.000Z', source_media: media('m-a') })
  assert.equal(shouldAdoptLatest(draft, sameFresh), true)
  // 同一照片但结论比发起时刻旧（5s 容忍之外）：别人的旧结论不接管
  const sameStale = diagnosis({ created_at: '2026-09-15T07:00:00.000Z', source_media: media('m-a') })
  assert.equal(shouldAdoptLatest(draft, sameStale), false)
  // 够新但不是同一照片：不接管
  const otherFresh = diagnosis({ created_at: '2026-09-15T08:00:01.000Z', source_media: media('m-b') })
  assert.equal(shouldAdoptLatest(draft, otherFresh), false)
  // latest 没带照片：退回纯时间判定（与旧线 !latest.media_id 的宽容一致）
  const noMedia = diagnosis({ created_at: '2026-09-15T08:00:01.000Z' })
  assert.equal(shouldAdoptLatest(draft, noMedia), true)
})

test('pending 草稿复访：latest 不是同一照片就不接管，用原照片续跑', async () => {
  const store = memStore()
  const seed = createOutfitSession(store)
  seed.writeDraft({
    pending: true,
    scene: 'interview',
    photoPath: '/tmp/a.jpg',
    mediaId: 'm-a',
    startedAt: Date.now(),
    result: null,
  })

  // 冷启动新实例（内存为空）：从 storage 读回 pending 草稿
  const session = createOutfitSession(store)
  const events = []
  const seen = []
  const rerun = diagnosis({ id: 'd-rerun', scene: 'interview', source_media: media('m-a') })
  await session.resume(
    {
      getLatest: async () =>
        diagnosis({ id: 'd-other', created_at: new Date().toISOString(), source_media: media('m-b') }),
      diagnose: async (input) => {
        seen.push(input)
        return rerun
      },
    },
    (event) => events.push(event),
  )
  // 旧结论被忽略：没接管 d-other，用草稿里的 m-a + 原场景续跑
  assert.deepEqual(seen, [{ kind: 'outfit', media_id: 'm-a', scene: 'interview' }])
  const results = events.filter((e) => e.type === 'result')
  assert.equal(results.length, 1)
  assert.equal(results[0].item.id, 'd-rerun')
  assert.equal(session.read().result.id, 'd-rerun')
  assert.equal(session.read().pending, false)
  const hydrate = events.find((e) => e.type === 'hydrate')
  assert.equal(hydrate.draft.scene, 'interview')
  assert.equal(hydrate.draft.photoPath, '/tmp/a.jpg')
})

test('pending 草稿复访：latest 够新且同一照片就接管，不再发起诊断', async () => {
  const store = memStore()
  const seed = createOutfitSession(store)
  seed.writeDraft({ pending: true, scene: 'daily', mediaId: 'm-a', startedAt: Date.now(), result: null })

  const session = createOutfitSession(store)
  let diagnoseCalls = 0
  const events = []
  const latest = diagnosis({ id: 'd-latest', created_at: new Date().toISOString(), source_media: media('m-a') })
  await session.resume(
    {
      getLatest: async () => latest,
      diagnose: async () => {
        diagnoseCalls += 1
        return diagnosis()
      },
    },
    (event) => events.push(event),
  )
  assert.equal(diagnoseCalls, 0)
  const results = events.filter((e) => e.type === 'result')
  assert.equal(results.length, 1)
  assert.equal(results[0].item.id, 'd-latest')
  assert.equal(session.read().result.id, 'd-latest')
  assert.equal(session.read().pending, false)
})

test('请求飞行中重选照片：迟到的旧结论不落草稿、不上屏', async () => {
  const session = createOutfitSession(memStore())
  session.markPending({ scene: 'daily', photoPath: '/tmp/a.jpg', mediaId: 'm-a' })
  const { promise, release } = gate()
  const stale = diagnosis({ id: 'd-stale', source_media: media('m-a') })
  const events = []
  const first = session.runDiagnose('m-a', () => promise.then(() => stale))
  // 用户重选了照片：草稿换成新照片（尚未发起新诊断）
  session.writeDraft({ pending: false, scene: 'daily', photoPath: '/tmp/b.jpg', mediaId: '', result: null })
  const resumed = session.resume(
    { getLatest: async () => null, diagnose: async () => diagnosis() },
    (event) => events.push(event),
  )
  release()
  await Promise.all([first, resumed])
  assert.equal(session.read().result, null)
  assert.equal(session.read().photoPath, '/tmp/b.jpg')
  assert.equal(events.filter((e) => e.type === 'result').length, 0)
})

// ---------- 草稿恢复 + 后台校验 ----------

test('结果落草稿：新实例复访先展示草稿，服务端同一条不重复接管', async () => {
  const store = memStore()
  const seed = createOutfitSession(store)
  seed.markPending({ scene: 'date', photoPath: '/tmp/a.jpg', mediaId: 'm-a' })
  seed.markDone(diagnosis({ id: 'd-1', created_at: '2026-09-15T08:00:00.000Z' }))

  const session = createOutfitSession(store)
  const events = []
  await session.resume(
    {
      getLatest: async () => diagnosis({ id: 'd-1', created_at: '2026-09-15T08:00:00.000Z' }),
      diagnose: async () => {
        throw new Error('不该发起诊断')
      },
    },
    (event) => events.push(event),
  )
  const hydrate = events.find((e) => e.type === 'hydrate')
  assert.equal(hydrate.draft.result.id, 'd-1')
  assert.equal(hydrate.draft.scene, 'date')
  assert.equal(events.filter((e) => e.type === 'result').length, 0)
  assert.equal(events.filter((e) => e.type === 'busy').length, 0)
})

test('后台校验只在服务端结论更新时接管', async () => {
  const store = memStore()
  const seed = createOutfitSession(store)
  seed.markDone(diagnosis({ id: 'd-1', created_at: '2026-09-15T08:00:00.000Z' }))

  const session = createOutfitSession(store)
  const newer = diagnosis({ id: 'd-2', created_at: '2026-09-15T09:00:00.000Z' })
  const events = []
  await session.resume(
    { getLatest: async () => newer, diagnose: async () => diagnosis() },
    (event) => events.push(event),
  )
  const results = events.filter((e) => e.type === 'result')
  assert.equal(results.length, 1)
  assert.equal(results[0].item.id, 'd-2')
  assert.equal(session.read().result.id, 'd-2')

  // 再来一次 freshStart：不查服务端，旧结论不会立刻盖回来
  let latestCalls = 0
  await session.resume(
    {
      getLatest: async () => {
        latestCalls += 1
        return newer
      },
      diagnose: async () => diagnosis(),
    },
    () => {},
    { freshStart: () => true },
  )
  assert.equal(latestCalls, 0)
})

test('无草稿的复访：服务端 latest 恢复结论；还没有诊断过就保持开始页', async () => {
  const store = memStore()
  const session = createOutfitSession(store)
  const events = []
  await session.resume(
    { getLatest: async () => null, diagnose: async () => diagnosis() },
    (event) => events.push(event),
  )
  assert.deepEqual(events, [])

  const latest = diagnosis({ id: 'd-server', created_at: '2026-09-15T07:00:00.000Z' })
  const events2 = []
  await session.resume(
    { getLatest: async () => latest, diagnose: async () => diagnosis() },
    (event) => events2.push(event),
  )
  const results = events2.filter((e) => e.type === 'result')
  assert.equal(results.length, 1)
  assert.equal(results[0].item.id, 'd-server')
  assert.equal(session.read().result.id, 'd-server')
})

test('isNewerDiagnosis：同一条不算更新，时间更旧也不算更新', () => {
  const current = diagnosis({ id: 'd-1', created_at: '2026-09-15T08:00:00.000Z' })
  assert.equal(isNewerDiagnosis(diagnosis({ id: 'd-1', created_at: '2026-09-15T08:00:00.000Z' }), current), false)
  assert.equal(isNewerDiagnosis(diagnosis({ id: 'd-2', created_at: '2026-09-15T09:00:00.000Z' }), current), true)
  assert.equal(isNewerDiagnosis(diagnosis({ id: 'd-0', created_at: '2026-09-15T07:00:00.000Z' }), current), false)
})

// ---------- 日限错误态 ----------

test('日限 429/402 归类为诊断日限，其余错误照常走通用通道', () => {
  assert.equal(isDiagnosticDailyLimit(new PublicApiError('rate_limited', '今日额度已用完', 429, '', true)), true)
  assert.equal(isDiagnosticDailyLimit(new PublicApiError('daily_limit_exceeded', '今日诊断已达上限', 429, '', true)), true)
  assert.equal(isDiagnosticDailyLimit(new PublicApiError('insufficient_credits', '额度不足', 402, '', false)), true)
  assert.equal(isDiagnosticDailyLimit(new PublicApiError('network_failed', '网络连接不上', 0, '', true)), false)
  assert.equal(isDiagnosticDailyLimit(new PublicApiError('photo_rejected', '照片不满足要求', 422, '', false)), false)
  assert.equal(isDiagnosticDailyLimit(new Error('boom')), false)
  assert.equal(isDiagnosticDailyLimit('rate_limited'), false)
  assert.equal(isDiagnosticDailyLimit(undefined), false)
})

test('pending 续跑撞日限：错误事件上抛且草稿落定，下次复访不再自动发起', async () => {
  const store = memStore()
  const seed = createOutfitSession(store)
  seed.writeDraft({ pending: true, scene: 'daily', mediaId: 'm-a', startedAt: Date.now(), result: null })

  const session = createOutfitSession(store)
  const events = []
  const limitError = new PublicApiError('rate_limited', '今日额度已用完，明天再来', 429, '', true)
  await session.resume(
    {
      getLatest: async () => null,
      diagnose: async () => {
        throw limitError
      },
    },
    (event) => events.push(event),
  )
  const errors = events.filter((e) => e.type === 'error')
  assert.equal(errors.length, 1)
  assert.ok(isDiagnosticDailyLimit(errors[0].error))
  assert.equal(session.read().pending, false)
  assert.equal(session.read().result, null)

  // 草稿已落定：再次复访不再自动续跑（空历史 → 保持开始页）
  let diagnoseCalls = 0
  await session.resume(
    {
      getLatest: async () => null,
      diagnose: async () => {
        diagnoseCalls += 1
        return diagnosis()
      },
    },
    () => {},
  )
  assert.equal(diagnoseCalls, 0)
})

// ---------- purchase 会话（同构，无场景） ----------

test('purchase 会话：独立存储键，pending 复访接管/续跑入参不带场景', async () => {
  const store = memStore()
  const outfitSeed = createOutfitSession(store)
  outfitSeed.markDone(diagnosis({ id: 'd-outfit', created_at: '2026-09-15T08:00:00.000Z' }))
  const purchaseSeed = createPurchaseSession(store)
  purchaseSeed.writeDraft({ pending: true, photoPath: '/tmp/p.jpg', mediaId: 'm-p', startedAt: Date.now(), result: null })

  // 两个会话草稿互不干扰
  assert.ok(store.map.get('zsm_outfit_session').includes('d-outfit'))
  assert.ok(store.map.get('zsm_purchase_session').includes('m-p'))

  const session = createPurchaseSession(store)
  const events = []
  const seen = []
  const rerun = diagnosis({ id: 'd-p-rerun', kind: 'purchase', source_media: media('m-p') })
  await session.resume(
    {
      getLatest: async () =>
        diagnosis({ id: 'd-p-other', kind: 'purchase', created_at: new Date().toISOString(), source_media: media('m-x') }),
      diagnose: async (input) => {
        seen.push(input)
        return rerun
      },
    },
    (event) => events.push(event),
  )
  assert.deepEqual(seen, [{ kind: 'purchase', media_id: 'm-p' }])
  const results = events.filter((e) => e.type === 'result')
  assert.equal(results.length, 1)
  assert.equal(results[0].item.id, 'd-p-rerun')
  // outfit 的草稿没有被 purchase 的复访动过
  assert.ok(store.map.get('zsm_outfit_session').includes('d-outfit'))
})

test('purchase 会话：inflight 期间重复发起接管同一个 Promise', async () => {
  const session = createPurchaseSession(memStore())
  session.markPending({ photoPath: '/tmp/p.jpg', mediaId: 'm-p' })
  let calls = 0
  const { promise, release } = gate()
  const item = diagnosis({ id: 'd-p', kind: 'purchase', source_media: media('m-p') })
  const run = () => {
    calls += 1
    return promise.then(() => item)
  }
  const first = session.runDiagnose('m-p', run)
  const second = session.runDiagnose('m-p', run)
  assert.strictEqual(first, second)
  release()
  await Promise.all([first, second])
  assert.equal(calls, 1)
  assert.equal(session.read().result.id, 'd-p')
})
