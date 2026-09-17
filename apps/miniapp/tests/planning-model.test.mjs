// 方案集的两条硬规则：五种状态全都是显式视图（没有"差不多就绪"），
// 对比左图只来自方案集绑定的那一份报告——绑定对不上就报错，不悄悄换图。
import test from 'node:test'
import assert from 'node:assert/strict'
import {
  analyzingAssessmentOperationId,
  boundBodyMedia,
  briefFingerprint,
  createIdempotencyKey,
  inFlightPlanSetOperationIds,
  planProgressView,
  planSetView,
  sceneBriefPrefill,
  sceneBriefRequest,
  sortedVariants,
  stepDetailLines,
  variantRenderView,
} from '../src/features/planning/model.ts'

const variant = (overrides = {}) => ({
  id: 'v1',
  slot: 1,
  key: 'sharp',
  name: '利落',
  descriptor: 'd',
  rationale: 'r',
  recommended: false,
  outcome_tags: [],
  difference_tags: [],
  steps: [],
  render: { state: 'queued', retryable: false, operation_id: null, media: null, render_run_id: null, publication_id: null },
  created_at: '2026-09-13T00:00:00Z',
  ...overrides,
})

test('all plan-set states remain explicit', () => {
  assert.equal(planSetView({ state: 'planning', variants: [] }).kind, 'planning')
  assert.equal(planSetView({ state: 'rendering', variants: [variant()] }).kind, 'rendering')
  assert.equal(planSetView({ state: 'ready', variants: [variant()] }).kind, 'ready')
  assert.equal(planSetView({ state: 'ready_partial', variants: [variant()] }).kind, 'ready_partial')
  assert.equal(planSetView({ state: 'failed', variants: [] }).kind, 'failed')
})

test('a failed set that still carries published text presents as ready_partial', () => {
  // 服务端错误响应里带着已发布文字时按 ready_partial 展示——
  // 客户端不把服务端还在提供的内容藏起来，也不从旧缓存替它拼一份。
  const withText = variant({ steps: [{ id: 's1' }] })
  assert.equal(planSetView({ state: 'failed', variants: [withText] }).kind, 'ready_partial')
  assert.equal(planSetView({ state: 'failed', variants: [variant()] }).kind, 'failed')
})

test('single render failure does not hide text or ready siblings', () => {
  assert.deepEqual(
    variantRenderView({
      steps: [{ id: 's1' }],
      render: { state: 'failed', retryable: true, operation_id: 'op-1', media: null, render_run_id: null, publication_id: null },
    }),
    {
      kind: 'failed',
      textAvailable: true,
      retryable: true,
      media: null,
    },
  )
  // 不可用不是失败：没有"重试"可承诺，retryable 原样带出、由界面决定不给按钮
  assert.equal(
    variantRenderView({
      steps: [{ id: 's1' }],
      render: { state: 'unavailable', retryable: false, operation_id: null, media: null, render_run_id: null, publication_id: null },
    }).kind,
    'unavailable',
  )
})

test('the three in-flight render states all keep text visible and promise no media', () => {
  for (const state of ['queued', 'generating', 'checking']) {
    const view = variantRenderView({
      steps: [],
      render: { state, retryable: false, operation_id: 'op-1', media: null, render_run_id: null, publication_id: null },
    })
    assert.equal(view.kind, state, state)
    assert.equal(view.textAvailable, false)
    assert.equal(view.media, null)
  }
})

test('ready without media is an anomaly, shown as retryable failure rather than an empty image', () => {
  // 契约里 ready 必须带 media；服务端违约时不渲染一张空图，也不假装一切正常
  const view = variantRenderView({
    steps: [{ id: 's1' }],
    render: { state: 'ready', retryable: false, operation_id: null, media: null, render_run_id: null, publication_id: null },
  })
  assert.equal(view.kind, 'failed')
  assert.equal(view.retryable, true)
  assert.equal(view.media, null)
})

test('comparison body is from the plan-set bound report only', () => {
  const planSet = { id: 'ps1', report_id: 'r-old' }
  const bound = {
    id: 'r-old',
    source_media: { body: { item_id: 'item-old', media: { asset_id: 'body-old' } } },
  }
  assert.equal(boundBodyMedia(planSet, bound).asset_id, 'body-old')
  assert.throws(
    () => boundBodyMedia(planSet, { id: 'r-current', source_media: { body: { item_id: 'item-new', media: { asset_id: 'body-new' } } } }),
    /report binding mismatch/,
  )
  // 没有绑定报告 / 报告缺全身照：宁可没有左图，不拿当前报告的图凑
  assert.throws(() => boundBodyMedia(planSet, null), /report binding mismatch/)
  assert.equal(
    boundBodyMedia(planSet, { id: 'r-old', source_media: { body: null } }),
    null,
  )
})

test('variants sort by their server slot, not by list order', () => {
  const planSet = {
    variants: [variant({ id: 'b', slot: 2 }), variant({ id: 'a', slot: 1 })],
  }
  assert.deepEqual(
    sortedVariants(planSet).map((v) => v.id),
    ['a', 'b'],
  )
})

test('idempotency keys keep their prefix and never repeat', () => {
  const a = createIdempotencyKey('render:v1')
  const b = createIdempotencyKey('render:v1')
  assert.match(a, /^render:v1:/)
  assert.notEqual(a, b)
})

test('step details narrow by category switch, never by casting', () => {
  const hair = {
    category: 'hair',
    action: 'adjust',
    title: '刘海过渡',
    summary: 's',
    position: 1,
    details: { target: '刘海', intensity: '轻度' },
  }
  assert.deepEqual(stepDetailLines(hair), [
    { label: '部位', value: '刘海' },
    { label: '幅度', value: '轻度' },
  ])

  const outfit = {
    category: 'outfit',
    action: 'keep',
    title: 't',
    summary: 's',
    position: 2,
    details: { silhouette: '直线型', palette: ['藏青', '燕麦'], layers: ['大衣'], avoid: ['logo'], formality: '商务休闲' },
  }
  assert.deepEqual(stepDetailLines(outfit), [
    { label: '版型', value: '直线型' },
    { label: '色板', value: '藏青、燕麦' },
    { label: '层次', value: '大衣' },
    { label: '避开', value: 'logo' },
    { label: '正式度', value: '商务休闲' },
  ])

  // 空值行不进界面
  const sparse = {
    category: 'makeup',
    action: 'keep',
    title: 't',
    summary: '',
    position: 3,
    details: { target: '', intensity: '轻薄' },
  }
  assert.deepEqual(stepDetailLines(sparse), [{ label: '幅度', value: '轻薄' }])
})

test('scene brief answers become a typed request or nothing at all', () => {
  const valid = sceneBriefRequest('r1', 'interview', {
    when: 'today',
    format: 'video',
    preparation: 'closet',
    impression: 'reliable',
  })
  assert.deepEqual(valid, {
    report_id: 'r1',
    scene: 'interview',
    brief: { when: 'today', format: 'video', preparation: 'closet', impression: 'reliable' },
  })

  // 少答一题 / 答了表外的值：宁可不发请求，不发一个服务端要 400 的请求
  assert.equal(
    sceneBriefRequest('r1', 'interview', { when: 'today', format: 'video', preparation: 'closet' }),
    null,
  )
  assert.equal(
    sceneBriefRequest('r1', 'interview', { when: 'yesterday', format: 'video', preparation: 'closet', impression: 'reliable' }),
    null,
  )
  // 未知场景没有问题表，同样拒绝构造
  assert.equal(sceneBriefRequest('r1', 'general', {}), null)
})

test('brief fingerprints key on content, not key order', () => {
  const a = briefFingerprint({ when: 'today', format: 'video', preparation: 'closet', impression: 'reliable' })
  const b = briefFingerprint({ impression: 'reliable', preparation: 'closet', format: 'video', when: 'today' })
  assert.equal(a, b)
  // 改了任何一个答案就是新指纹（409「相同幂等键已被用于不同请求」的修法）
  assert.notEqual(a, briefFingerprint({ when: 'week', format: 'video', preparation: 'closet', impression: 'reliable' }))
  assert.equal(briefFingerprint({}), briefFingerprint({}))
})

test('in-flight plan-set accepts are the only operations worth watching', () => {
  const ops = [
    { id: 'op-1', kind: 'plan_set', status: 'accepted' },
    { id: 'op-2', kind: 'plan_set', status: 'running' },
    { id: 'op-3', kind: 'plan_set', status: 'retrying' },
    { id: 'op-4', kind: 'plan_set', status: 'failed' },
    { id: 'op-5', kind: 'plan_set', status: 'succeeded' },
    { id: 'op-6', kind: 'assessment', status: 'running' },
    { id: 'op-7', kind: 'render', status: 'accepted' },
  ]
  assert.deepEqual(inFlightPlanSetOperationIds(ops), ['op-1', 'op-2', 'op-3'])
  assert.deepEqual(inFlightPlanSetOperationIds([]), [])
})

test('plan progress view reads only the working accept and maps server stages honestly', () => {
  // 在途快照 → bps 钳成百分比，阶段码定步位，服务端文案优先
  const reading = {
    id: 'op-a',
    status: 'running',
    progress_bps: 1500,
    stage_code: 'plan.reading_report',
    public_message: '正在阅读你的形象报告',
  }
  assert.deepEqual(planProgressView([reading]), {
    percent: 15,
    stepIndex: 0,
    stageLine: '正在阅读你的形象报告',
    retrying: false,
  })

  // 检查阶段；服务端没给文案时查客户端文案表，不认识的码退兜底
  const checking = { id: 'op-b', status: 'running', progress_bps: 6500, stage_code: 'plan.checking', public_message: '' }
  assert.equal(planProgressView([checking]).percent, 65)
  assert.equal(planProgressView([checking]).stepIndex, 1)
  assert.equal(planProgressView([checking]).stageLine, '正在检查三套造型')
  const unknown = { id: 'op-c', status: 'running', progress_bps: 3000, stage_code: 'plan.something_new', public_message: '' }
  assert.equal(planProgressView([unknown]).stepIndex, 0)
  assert.equal(planProgressView([unknown]).stageLine, '正在为你定制三套造型')

  // 服务端自己的阈值（6500）是未知阶段定步的唯一依据，不虚构中间态
  const lateUnknown = { id: 'op-d', status: 'running', progress_bps: 7000, stage_code: 'plan.something_new', public_message: '' }
  assert.equal(planProgressView([lateUnknown]).stepIndex, 1)

  // 到 100 进收尾步；服务端在重试要如实说
  const finishing = { id: 'op-e', status: 'running', progress_bps: 10000, stage_code: 'plan.checking', public_message: '' }
  assert.equal(planProgressView([finishing]).stepIndex, 2)
  const retrying = { id: 'op-f', status: 'retrying', progress_bps: 6500, stage_code: 'plan.checking', public_message: '' }
  assert.equal(planProgressView([retrying]).retrying, true)

  // 没有在途快照就不编进度：null，视图自己退到安静态
  assert.equal(planProgressView([]), null)
  assert.equal(
    planProgressView([{ id: 'op-g', status: 'succeeded', progress_bps: 10000, stage_code: 'plan.ready', public_message: '' }]),
    null,
  )
})

test('analyzingAssessmentOperationId finds the in-flight analysis the plans empty state links to', () => {
  const ops = [
    { id: 'op-1', kind: 'plan_set', status: 'running' },
    { id: 'op-2', kind: 'assessment', status: 'accepted' },
    { id: 'op-3', kind: 'assessment', status: 'running' },
    { id: 'op-4', kind: 'assessment', status: 'failed' },
    { id: 'op-5', kind: 'assessment', status: 'succeeded' },
    { id: 'op-6', kind: 'render', status: 'running' },
  ]
  // 有且只有一个在途分析：空态按钮指向进度页而不是重拍
  assert.equal(analyzingAssessmentOperationId(ops), 'op-2')
  assert.equal(analyzingAssessmentOperationId([]), '')
  // 终态（失败/完成）不算在途：失败允许重拍，完成该去报告
  assert.equal(analyzingAssessmentOperationId(ops.slice(3)), '')
})

test('a published brief prefills the scene brief page, table-checked field by field', () => {
  // 上次提交的 brief 原样预填回来
  assert.deepEqual(
    sceneBriefPrefill('interview', { when: 'today', format: 'video', preparation: 'closet', impression: 'reliable' }),
    { when: 'today', format: 'video', preparation: 'closet', impression: 'reliable' },
  )
  // 表外的值（契约演进丢掉的枚举）丢弃，宁可少填不填错
  assert.deepEqual(
    sceneBriefPrefill('interview', { when: 'yesterday', format: 'video', preparation: 'closet', impression: 'reliable' }),
    { format: 'video', preparation: 'closet', impression: 'reliable' },
  )
  // 缺字段 / 非对象 / general 与未知场景：返回空，不虚构答案
  assert.deepEqual(sceneBriefPrefill('interview', { when: 'today' }), { when: 'today' })
  assert.deepEqual(sceneBriefPrefill('interview', null), {})
  assert.deepEqual(sceneBriefPrefill('general', { focus: 'balanced' }), {})
  assert.deepEqual(sceneBriefPrefill('mars', { when: 'today' }), {})
})
