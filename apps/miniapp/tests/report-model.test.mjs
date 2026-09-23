// 报告页的两条硬规则：证据必须来自 finding 自己声明的那张照片，终态指针只认服务端的 result_id。
import test from 'node:test'
import assert from 'node:assert/strict'
import {
  findingAnchorPoint,
  reportAvailableRoles,
  defaultReportRole,
  reportFindings,
  reportFindingsOnRole,
  reportPhotoForFinding,
  reportPlanSetHandoff,
  reportPlanSetIdempotencyKey,
  reportPlansCtaState,
  reportPlansIdempotencyKey,
  reportPlanSetRequest,
  reportRouteAfterOperation,
  reportSourcePhoto,
} from '../src/features/report/model.ts'

const report = {
  id: 'r1',
  photo_set_id: 'photos-1',
  source_media: {
    face: { item_id: 'item-face', media: { asset_id: 'face-1' } },
    side: { item_id: 'item-side', media: { asset_id: 'side-1' } },
    body: { item_id: 'item-body', media: { asset_id: 'body-1' } },
  },
}

test('finding always resolves its explicit source role', () => {
  assert.equal(
    reportPhotoForFinding(report, { source_photo: { item_id: 'item-side', role: 'side' } }).asset_id,
    'side-1',
  )
})

test('missing evidence does not fall back to body or bundled media', () => {
  assert.equal(
    reportPhotoForFinding(
      { ...report, source_media: { ...report.source_media, face: null } },
      { source_photo: { item_id: 'item-face', role: 'face' } },
    ),
    null,
  )
})

test('successful assessment uses operation result id', () => {
  assert.equal(
    reportRouteAfterOperation({
      status: 'succeeded',
      result_type: 'report',
      result_id: 'r9',
    }),
    '/pages/report/index?id=r9',
  )
})

test('a finding whose item drifted off its role slot gets no photo', () => {
  // 同一角色下换了 item：锚点是在另一张照片的坐标系里量的，照画就等于伪造证据。
  assert.equal(
    reportPhotoForFinding(report, {
      source_photo: { item_id: 'item-other', role: 'side' },
    }),
    null,
  )
  // 角色槽位存在但缺 media 时同样不回落
  assert.equal(
    reportPhotoForFinding(
      { ...report, source_media: { ...report.source_media, body: { item_id: 'item-body' } } },
      { source_photo: { item_id: 'item-body', role: 'body' } },
    ),
    null,
  )
  assert.equal(reportPhotoForFinding(null, { source_photo: { item_id: 'x', role: 'face' } }), null)
})

test('only a finished report operation yields a report route', () => {
  const base = { id: 'op-1', progress_bps: 5000, public_message: '正在分析', retryable: false }
  const noRoute = {
    // 还没结束：留在分析页继续轮询，不跳走
    working: { ...base, status: 'running' },
    // 结束了但没有结果指针
    noPointer: { ...base, status: 'succeeded' },
    // 结果不是报告（比如其它 kind 的产物）
    otherResult: { ...base, status: 'succeeded', result_type: 'plan_set', result_id: 'p1' },
    // 空 result_id 不算指针
    emptyId: { ...base, status: 'succeeded', result_type: 'report', result_id: '' },
    // 失败、取消、被取代都不是"报告好了"
    failed: { ...base, status: 'failed' },
    cancelled: { ...base, status: 'cancelled' },
    superseded: { ...base, status: 'superseded' },
    idle: null,
  }
  for (const [name, operation] of Object.entries(noRoute)) {
    assert.equal(reportRouteAfterOperation(operation), null, `${name} should not route to a report`)
  }
})

test('view plans asks for one general plan set bound to this report', () => {
  assert.deepEqual(reportPlanSetRequest('r1'), {    report_id: 'r1',
    scene: 'general',
    brief: { focus: 'balanced', preparation: 'closet', impression: 'natural' },
  })
})

test('plan set handoff carries both ids, and only the id when it was reused', () => {
  // 202 受理：异步规划，方案页要靠 operation_id 接着轮询
  assert.deepEqual(
    reportPlanSetHandoff({
      accepted: true,
      data: { id: 'ps-1', state: 'planning' },
      operation: { id: 'op-2' },
    }),
    { planSetId: 'ps-1', operationId: 'op-2' },
  )
  // 200 复用：已发布的方案集，没有任务在跑——operationId 是 null 而不是空串，
  // 方案页对「没有任务」的判断不会因为一个空字符串而走错分支
  assert.deepEqual(
    reportPlanSetHandoff({ accepted: false, planSet: { id: 'ps-1' } }),
    { planSetId: 'ps-1', operationId: null },
  )
})

test('report plans CTA promises 查看 only when the report has something to view', () => {
  // 报告刚出来、一份方案集都没有：按钮必须是「生成」——说「查看」却触发生成
  // 是在骗点击（用户投诉的原案）
  assert.equal(reportPlansCtaState([]), 'generate')
  // 规划在途 / 已就绪：有可看的目标 → 查看
  assert.equal(reportPlansCtaState([{ state: 'planning', variants: [] }]), 'view')
  assert.equal(reportPlansCtaState([{ state: 'ready', variants: [{ steps: [{}] }] }]), 'view')
  // 渲染中 / 失败但文字已发布（ready_partial）：内容可看 → 查看
  assert.equal(reportPlansCtaState([{ state: 'rendering', variants: [{ steps: [{}] }] }]), 'view')
  assert.equal(reportPlansCtaState([{ state: 'failed', variants: [{ steps: [{}] }] }]), 'view')
  // 生成过但全真失败（无文字）：没有可看的东西，回到「生成」语义
  assert.equal(reportPlansCtaState([{ state: 'failed', variants: [] }]), 'retry')
  assert.equal(
    reportPlansCtaState([
      { state: 'failed', variants: [] },
      { state: 'failed', variants: [{ steps: [] }] },
    ]),
    'retry',
  )
})

test('retry mints a fresh idempotency key; first attempt and reuse keep the fixed one', () => {
  // 固定键在服务端会原样重放同一份失败（24h 内）：重试必须换新键——
  // 与方案页 retry marker 是同一条规则
  assert.equal(reportPlansIdempotencyKey('r1', 'generate'), reportPlanSetIdempotencyKey('r1'))
  assert.equal(reportPlansIdempotencyKey('r1', 'view'), reportPlanSetIdempotencyKey('r1'))
  const retryKey = reportPlansIdempotencyKey('r1', 'retry')
  assert.notEqual(retryKey, reportPlanSetIdempotencyKey('r1'))
  assert.ok(retryKey.startsWith(`${reportPlanSetIdempotencyKey('r1')}:`))
})

test('findings render in server position order, never re-sorted by the client', () => {
  const findings = [
    { id: 'f-b', position: 2, source_photo: { item_id: 'item-face', role: 'face' } },
    { id: 'f-a', position: 1, source_photo: { item_id: 'item-side', role: 'side' } },
  ]
  assert.deepEqual(
    reportFindings({ findings }).map((f) => f.id),
    ['f-a', 'f-b'],
  )
  // 原数组不被改动：排序只发生在返回值里
  assert.equal(findings[0].id, 'f-b')
  assert.deepEqual(reportFindings(null), [])
})

test('only findings whose evidence lands on the role get annotated on it', () => {
  const annotated = {
    id: 'r1',
    source_media: {
      face: { item_id: 'item-face', media: { asset_id: 'face-1' } },
      side: { item_id: 'item-side', media: { asset_id: 'side-1' } },
    },
    findings: [
      { id: 'f-1', position: 1, source_photo: { item_id: 'item-face', role: 'face' } },
      // 同角色但 item 漂移：锚点不在这一格的坐标系里，不画
      { id: 'f-2', position: 2, source_photo: { item_id: 'item-other', role: 'face' } },
      // 角色对但那一格没有照片：不画
      { id: 'f-3', position: 3, source_photo: { item_id: 'item-body', role: 'body' } },
    ],
  }
  assert.deepEqual(
    reportFindingsOnRole(annotated, 'face').map((f) => f.id),
    ['f-1'],
  )
  assert.deepEqual(reportFindingsOnRole(annotated, 'side'), [])
})

test('available roles skip missing photos and the default role follows the first solid finding', () => {
  const partial = {
    source_media: {
      face: { item_id: 'item-face', media: { asset_id: 'face-1' } },
      side: null,
      body: { item_id: 'item-body', media: { asset_id: 'body-1' } },
    },
  }
  // 缺照片的角色直接不出现，不占位
  assert.deepEqual(reportAvailableRoles(partial), ['body', 'face'])
  assert.equal(reportSourcePhoto(partial, 'side'), null)
  assert.equal(reportSourcePhoto(partial, 'face').asset_id, 'face-1')

  // 第一条证据落地的 finding 指向 face：先打开的是它，不是列表第一张 body
  const withFindings = {
    ...partial,
    findings: [
      // position 靠前的这条落不了地（body 缺照片场景里它落在 face？不——它指 body）
      { id: 'f-1', position: 1, source_photo: { item_id: 'item-body', role: 'body' } },
      { id: 'f-2', position: 2, source_photo: { item_id: 'item-face', role: 'face' } },
    ],
  }
  // f-1 指 body，body 可用且证据落地 → 默认打开 body
  assert.equal(defaultReportRole(withFindings), 'body')
  // 把 body 的照片抽走后，f-1 落不了地，轮到 f-2 的 face
  const noBody = {
    ...withFindings,
    source_media: { ...partial.source_media, body: null },
  }
  assert.equal(defaultReportRole(noBody), 'face')
  // 一张可用照片都没有：没有默认角色，页面显示整体空态
  assert.equal(defaultReportRole({ source_media: {} }), null)
  assert.equal(defaultReportRole(null), null)
})

// 锚点语义化：区域框取上下半身的语义边中点，不再用几何中心
// （用例是 9.16 生产报告的真实锚框：T恤→领口、下装鞋履→裤脚、整身框→中心）
test('upper-half anchor box lands on its top edge (collar/hairline)', () => {
  // 黑色圆领T恤：框 y28%~60%，应指领口（顶边中点）而不是胸口正中
  assert.deepEqual(
    findingAnchorPoint({ anchor: { x: 0.24, y: 0.28, w: 0.51, h: 0.32 } }),
    { anchorX: 0.495, anchorY: 0.28 },
  )
  // 顶部蓬松短发：框 y14%~38%，应指发顶而不是额心
  assert.deepEqual(
    findingAnchorPoint({ anchor: { x: 0.27, y: 0.14, w: 0.47, h: 0.24 } }),
    { anchorX: 0.505, anchorY: 0.14 },
  )
})

test('lower-half anchor box lands on its bottom edge (cuffs/shoes)', () => {
  // 全黑下装与鞋履：框 y58%~96%，应指裤脚/鞋而不是大腿中段
  assert.deepEqual(
    findingAnchorPoint({ anchor: { x: 0.31, y: 0.58, w: 0.36, h: 0.38 } }),
    { anchorX: 0.49, anchorY: 0.96 },
  )
})

test('full-body anchor box keeps its geometric center', () => {
  // 整体配色偏深：框高 69% 横跨大半身，留中心
  assert.deepEqual(
    findingAnchorPoint({ anchor: { x: 0.23, y: 0.27, w: 0.53, h: 0.69 } }),
    { anchorX: 0.495, anchorY: 0.615 },
  )
})

// report.v2 起锚点是分析模型直接给的语义关键点，原样采用，不做几何加工
test('report.v2 anchor is the model-provided keypoint, used verbatim', () => {
  assert.deepEqual(
    findingAnchorPoint({ anchor: { x: 0.24, y: 0.28, w: 0.51, h: 0.32 } }, 'report.v2'),
    { anchorX: 0.24, anchorY: 0.28 },
  )
})

// report.v1 的区域矩形仍走上下半身语义边（旧报告兼容路径）
test('report.v1 anchor box falls back to the semantic edge heuristic', () => {
  assert.deepEqual(
    findingAnchorPoint({ anchor: { x: 0.31, y: 0.58, w: 0.36, h: 0.38 } }, 'report.v1'),
    { anchorX: 0.49, anchorY: 0.96 },
  )
})
