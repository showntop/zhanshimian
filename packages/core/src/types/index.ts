// 新 API v1 领域类型 —— JSON snake_case，对照 apps/server/internal/domain/domain.go。
// 字段以后端 JSON tag 为准；前端一律直用 snake_case，不做 camel 转换。

// ---------- 认证与身份 ----------
export type IdentityProvider = 'wechat_miniapp' | 'wechat_app' | 'apple' | 'phone'

export interface User {
  id: string
  nickname: string
}

export interface UserIdentity {
  provider: IdentityProvider
  identifier: string
  created_at: string
}

/** GET /v1/me：账户 + 多端身份列表 */
export interface Account {
  id: string
  nickname: string
  identities: UserIdentity[]
}

export interface Session {
  token: string
  expires_at: string
  user: User
}

/** GET/PUT /v1/me/profile：身高/职业/预算 + 选填体重三围 */
export interface UserProfile {
  height_cm: number
  role: string
  budget: string
  weight_kg?: number
  bust_cm?: number
  waist_cm?: number
  hips_cm?: number
}

// ---------- 统一任务 ----------
export type TaskType = 'analysis' | 'plan_group' | 'plan_look' | 'hair_preview' | 'today_look'
export type TaskStatus = 'queued' | 'processing' | 'completed' | 'failed'

export interface TaskError {
  code: string
  message: string
  /** 照片不合格时逐图中文原因（index 对应上传顺序） */
  photo_reasons?: string[]
}

export interface Task {
  id: string
  type: TaskType
  status: TaskStatus
  progress: number
  stage: string
  error?: TaskError
  result_ref?: string
}

/** 异步创建（202）信封里的任务引用 */
export interface TaskRef {
  id: string
  type: TaskType
}

// ---------- 媒体 ----------
export type MediaKind = 'face' | 'side' | 'body' | 'feedback' | 'outfit' | 'product' | 'wardrobe'

export interface MediaAsset {
  id: string
  kind: MediaKind
  url: string
  /** 服务端内置示例媒体；url 本地化后仍保留来源事实 */
  demo?: boolean
  created_at: string
}

// ---------- 分析与报告 ----------
export type AnalysisStatus = 'queued' | 'processing' | 'completed' | 'failed'

export interface Analysis {
  id: string
  status: AnalysisStatus
  progress: number
  stage: string
  scene?: string
  preview_image_url?: string
  media?: MediaAsset[]
  report_id?: string
  error_message?: string
  created_at: string
  updated_at: string
}

export interface CreateAnalysisInput {
  scene: string
  media_ids: [string, string, string] | string[]
  profile?: Pick<UserProfile, 'height_cm' | 'role' | 'budget'>
}

export interface Finding {
  id: string
  label: string
  category: string
  severity: string
  detail: string
  /** 观察来自哪张分析照（face/side/body）；锚点相对该照片 */
  photo?: string
  anchor_x: number
  anchor_y: number
}

export interface Report {
  id: string
  analysis_id: string
  current_image_url: string
  impression_tags: string[]
  priority_title: string
  priority_copy: string
  findings: Finding[]
  provider_version: string
  generated_at: string
}

// ---------- 方案 ----------
export interface PlanStep {
  id: string
  category: string
  title: string
  summary: string
  /** 结构化细节（JSON 原样透传，按 category 解释） */
  details: unknown
  sort: number
}

export interface Plan {
  id: string
  report_id: string
  scene?: string
  name: string
  slug: string
  image_url: string
  current_image_url?: string
  generated_image_url?: string
  /** 列表内嵌的 look 生成任务状态（页面不再二次查询） */
  look_task?: Task
  /** 生成来源（demo* 前缀 → 前端按示例处理并挂角标） */
  look_provider?: string
  recommended: boolean
  descriptor: string
  why: string
  outcome_tags: string[]
  difference_tags: string[]
  sort: number
  selected: boolean
  steps?: PlanStep[]
}

export interface UpsertPlansInput {
  scene: string
  answers?: Record<string, string>
}

export interface ChecklistItem {
  id: string
  plan_id: string
  category: string
  title: string
  description: string
  meta: string
  completed: boolean
  sort: number
}

export interface PlanFeedbackInput {
  tags: string[]
  comment?: string
  media_id?: string
}

/** 反馈响应含服务端统一产出的个性化承诺文案（G3） */
export interface PlanFeedbackResult {
  message: string
}

// ---------- 诊断与发型 ----------
export type DiagnosisKind = 'outfit' | 'purchase'

export interface CreateDiagnosisInput {
  kind: DiagnosisKind
  media_id: string
  scene?: string
  report_id?: string
}

export interface DiagnosisFinding {
  label: string
  category: string
  tone: string
  anchor_x?: number
  anchor_y?: number
}

export interface DiagnosisOption {
  id: string
  name: string
  image_url: string
  note: string
  reason: string
  tags: string[]
}

export interface Diagnosis {
  id: string
  kind: DiagnosisKind
  scene: string
  conclusion: string
  priority_title: string
  priority_copy: string
  tags: string[]
  findings: DiagnosisFinding[]
  options?: DiagnosisOption[]
  saved: boolean
  provider_version?: string
  media_id?: string
  image_url?: string
  created_at: string
}

export interface HairstyleOption {
  id: string
  name: string
  image_url: string
  note: string
  reason: string
  tags: string[]
}

export interface HairPreview {
  id: string
  status: AnalysisStatus
  progress: number
  stage: string
  style_id: string
  style_name: string
  scene: string
  source_image_url: string
  result_image_url?: string
  provider_version?: string
  saved: boolean
  error_message?: string
  created_at: string
  updated_at: string
}

export interface CreateHairPreviewInput {
  media_id: string
  report_id?: string
  style_id: string
  scene?: string
}

// ---------- 今日 ----------
export interface TodayContext {
  date: string
  city: string
  condition: string
  temperature: number
  day_type: string
  schedule: string
}

export interface TodayPlanStep {
  category: string
  label: string
  title: string
  copy: string
}

export interface TodayPlan {
  id: string
  report_id?: string
  context: TodayContext
  title: string
  summary: string
  /** 生成完成前的内置「风格参考」图 */
  image_url: string
  steps: TodayPlanStep[]
  active: boolean
  feedback?: string
  regenerate_count: number
  generated_image_url?: string
  look_task?: Task
  look_provider?: string
  generation_error?: string
  created_at: string
  updated_at: string
}

export interface CreateTodayPlanInput {
  report_id?: string
  city?: string
  schedule?: string
  refresh?: boolean
}

// ---------- 分享 ----------
export interface CreateShareInput {
  source_type: string
  source_id: string
  include_photo: boolean
}

export interface Share {
  id: string
  token: string
  source_type: string
  source_id: string
  snapshot: unknown
  include_photo: boolean
  revoked: boolean
  expires_at: string
  created_at: string
}

// ---------- 衣橱 ----------
export interface WardrobeItemInput {
  media_id?: string
  name: string
  category: string
  color: string
  season?: string
  formality?: string
  scenes?: string[]
}

export interface WardrobeItem {
  id: string
  media_id?: string
  name: string
  category: string
  color: string
  season: string
  formality: string
  scenes: string[]
  image_url: string
  favorite: boolean
  wear_count: number
  created_at: string
  updated_at: string
}

export interface CreateWardrobeOutfitInput {
  title: string
  note?: string
  item_ids: string[]
  /** 冻结生成时的今日上下文（缺省由服务端现取） */
  context?: TodayContext
}

export interface WardrobeOutfit {
  id: string
  title: string
  note: string
  context: unknown
  item_ids: string[]
  items: WardrobeItem[]
  worn: boolean
  created_at: string
}

// ---------- 顾问 ----------
export interface AdvisorAction {
  id: string
  kind: string
  label: string
  payload: unknown
  applied: boolean
}

export interface AdvisorMessage {
  id: string
  conversation_id: string
  role: string
  content: string
  actions?: AdvisorAction[]
  created_at: string
}

export interface SendAdvisorMessageInput {
  conversation_id?: string
  content: string
  report_id?: string
  today_plan_id?: string
}

// ---------- 首页聚合 ----------
/** GET /v1/home/bootstrap：一屏聚合，替代旧首页 4 次并发请求 */
export interface HomeBootstrap {
  profile_summary: UserProfile | null
  report: Report | null
  today_plan: TodayPlan | null
  active_tasks: Task[]
  recent_plan: Plan | null
}

// ---------- 埋点 ----------
export interface EventInput {
  name: string
  payload?: unknown
}
