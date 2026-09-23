// 质量闭环的确定性状态矩阵：4 个来源 × 16 个公开 UI 状态 = 64 个用例。
// 每个 id 是 `<source-id>:<state-id>`，定位一个「这张图 × 这个状态」的组合。
// 过期签名、空 URL、WebP、错误 label、report 错绑、服务端整体替换等
// 已由 Tasks 3/5/8 的专项测试覆盖，这里不重复。
export const NOW = Date.parse('2026-09-12T08:00:00Z')

interface SourceCase {
  id: string
  media: {
    asset_id: string
    url: string
    url_expires_at: string
    mime_type: string
    source_kind: 'user_original' | 'generated_preview' | 'bundled_reference' | 'demo_example'
    display_label: string
  }
  expected: { badge: string; soften: boolean; sourceKind: string }
}

const SOURCE_CASES: readonly SourceCase[] = [
  {
    id: 'user',
    media: {
      asset_id: 'asset-user',
      url: 'https://cdn.example/user.jpg',
      url_expires_at: '2026-09-13T08:00:00Z',
      mime_type: 'image/jpeg',
      source_kind: 'user_original',
      display_label: '原本',
    },
    expected: { badge: '原本', soften: false, sourceKind: 'user_original' },
  },
  {
    id: 'generated',
    media: {
      asset_id: 'asset-generated',
      url: 'https://cdn.example/generated.jpg',
      url_expires_at: '2026-09-13T08:00:00Z',
      mime_type: 'image/jpeg',
      source_kind: 'generated_preview',
      display_label: '风格参考',
    },
    expected: { badge: '风格参考', soften: false, sourceKind: 'generated_preview' },
  },
  {
    id: 'bundled',
    media: {
      asset_id: 'asset-bundled',
      url: 'https://cdn.example/bundled.jpg',
      url_expires_at: '2026-09-13T08:00:00Z',
      mime_type: 'image/jpeg',
      source_kind: 'bundled_reference',
      display_label: '风格参考',
    },
    expected: { badge: '风格参考', soften: true, sourceKind: 'bundled_reference' },
  },
  {
    id: 'demo',
    media: {
      asset_id: 'asset-demo',
      url: 'https://cdn.example/demo.jpg',
      url_expires_at: '2026-09-13T08:00:00Z',
      mime_type: 'image/jpeg',
      source_kind: 'demo_example',
      display_label: '效果示例',
    },
    expected: { badge: '效果示例', soften: true, sourceKind: 'demo_example' },
  },
]

interface StateCase {
  id: string
  domain: 'plan-set' | 'render' | 'operation'
  input: unknown
  expected: unknown
}

const render = (state: string, overrides: Record<string, unknown> = {}) => ({
  state,
  retryable: false,
  operation_id: 'op-1',
  media: null,
  render_run_id: null,
  publication_id: null,
  ...overrides,
})

const operation = (overrides: Record<string, unknown>) => ({
  id: 'op-1',
  kind: 'render',
  subject_type: 'plan_variant',
  subject_id: 'v1',
  stage_code: 'rendering',
  public_message: '',
  retryable: false,
  created_at: '2026-09-12T07:00:00Z',
  updated_at: '2026-09-12T07:30:00Z',
  ...overrides,
})

const variantWithText = { id: 'v1', steps: [{ id: 's1' }] }

const STATE_CASES: readonly StateCase[] = [
  // PlanSet 五态
  { id: 'plan-planning', domain: 'plan-set', input: { state: 'planning', variants: [] }, expected: { kind: 'planning' } },
  { id: 'plan-rendering', domain: 'plan-set', input: { state: 'rendering', variants: [variantWithText] }, expected: { kind: 'rendering' } },
  { id: 'plan-ready', domain: 'plan-set', input: { state: 'ready', variants: [variantWithText] }, expected: { kind: 'ready' } },
  { id: 'plan-ready-partial', domain: 'plan-set', input: { state: 'ready_partial', variants: [variantWithText] }, expected: { kind: 'ready_partial' } },
  // failed 且无文字：整屏失败
  { id: 'plan-failed', domain: 'plan-set', input: { state: 'failed', variants: [] }, expected: { kind: 'failed' } },
  // Render 六态
  { id: 'render-queued', domain: 'render', input: { steps: [{ id: 's1' }], render: render('queued') }, expected: { kind: 'queued', textAvailable: true, retryable: false, media: null } },
  { id: 'render-generating', domain: 'render', input: { steps: [{ id: 's1' }], render: render('generating') }, expected: { kind: 'generating', textAvailable: true, retryable: false, media: null } },
  { id: 'render-checking', domain: 'render', input: { steps: [{ id: 's1' }], render: render('checking') }, expected: { kind: 'checking', textAvailable: true, retryable: false, media: null } },
  { id: 'render-ready', domain: 'render', input: { steps: [{ id: 's1' }], render: render('ready', { media: { asset_id: 'a1' } }) }, expected: { kind: 'ready', textAvailable: true, retryable: false, media: { asset_id: 'a1' } } },
  { id: 'render-failed', domain: 'render', input: { steps: [{ id: 's1' }], render: render('failed', { retryable: true }) }, expected: { kind: 'failed', textAvailable: true, retryable: true, media: null } },
  { id: 'render-unavailable', domain: 'render', input: { steps: [{ id: 's1' }], render: render('unavailable') }, expected: { kind: 'unavailable', textAvailable: true, retryable: false, media: null } },
  // Operation 五态
  { id: 'operation-accepted', domain: 'operation', input: operation({ status: 'accepted', progress_bps: 0, public_message: '已受理' }), expected: { kind: 'working', progress: 0, message: '已受理', stageCode: 'rendering', retrying: false } },
  { id: 'operation-running', domain: 'operation', input: operation({ status: 'running', progress_bps: 5000, public_message: '生成中' }), expected: { kind: 'working', progress: 50, message: '生成中', stageCode: 'rendering', retrying: false } },
  { id: 'operation-retrying', domain: 'operation', input: operation({ status: 'retrying', progress_bps: 6000, public_message: '正在重试' }), expected: { kind: 'working', progress: 60, message: '正在重试', stageCode: 'rendering', retrying: true } },
  { id: 'operation-succeeded', domain: 'operation', input: operation({ status: 'succeeded', result_type: 'plan_set', result_id: 'ps1' }), expected: { kind: 'succeeded', resultType: 'plan_set', resultId: 'ps1' } },
  { id: 'operation-failed', domain: 'operation', input: operation({ status: 'failed', public_message: '没有生成成功', retryable: true, trace_id: 'trace-1' }), expected: { kind: 'failed', message: '没有生成成功', retryable: true, requestId: 'trace-1' } },
]

export const QUALITY_LOOP_FIXTURES = SOURCE_CASES.flatMap((source) =>
  STATE_CASES.map((state) => ({
    id: `${source.id}:${state.id}`,
    now: NOW,
    media: source.media,
    expectedMedia: {
      key: `${source.media.asset_id}:${source.media.url}`,
      src: source.media.url,
      ...source.expected,
    },
    stateDomain: state.domain,
    stateInput: state.input,
    expectedState: state.expected,
  })),
)
