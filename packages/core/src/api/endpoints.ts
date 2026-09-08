// 全量类型化端点函数 —— 路径严格按总计划 §一 端点总表（API v1）。
// API_PATHS 供 contracts/check-sync.mjs 与服务端路由表比对。
// 语义保留：原型 runTool 的「404 清 report 引用重试」收敛到 diagnose() 内。
import type { ApiClient, ApiError, Envelope } from '../http/client.ts'
import type {
  Account,
  AdvisorAction,
  AdvisorMessage,
  Analysis,
  ChecklistItem,
  CreateAnalysisInput,
  CreateDiagnosisInput,
  CreateHairPreviewInput,
  CreateShareInput,
  CreateTodayPlanInput,
  CreateWardrobeOutfitInput,
  Diagnosis,
  EventInput,
  HairPreview,
  HairstyleOption,
  HomeBootstrap,
  MediaAsset,
  MediaKind,
  Plan,
  PlanFeedbackInput,
  PlanFeedbackResult,
  Report,
  SendAdvisorMessageInput,
  Session,
  Share,
  Task,
  TodayContext,
  TodayPlan,
  UpsertPlansInput,
  UserProfile,
  WardrobeItem,
  WardrobeItemInput,
  WardrobeOutfit
} from '../types/index.ts'

/** 异步创建结果（202）：{data, task} */
export type TaskCreated<T> = Envelope<T>

/** 端点常量表（check-sync 比对用）：name → 'METHOD path' */
export const API_PATHS = {
  authWechat: 'POST /v1/auth/wechat',
  authDev: 'POST /v1/auth/dev',
  authSmsRequest: 'POST /v1/auth/sms/request',
  authSmsVerify: 'POST /v1/auth/sms/verify',
  authApple: 'POST /v1/auth/apple',
  authWechatApp: 'POST /v1/auth/wechat-app',
  authSession: 'DELETE /v1/auth/session',
  me: 'GET /v1/me',
  meProfile: 'GET /v1/me/profile',
  meProfileUpdate: 'PUT /v1/me/profile',
  task: 'GET /v1/tasks/{id}',
  tasks: 'GET /v1/tasks',
  homeBootstrap: 'GET /v1/home/bootstrap',
  media: 'POST /v1/media',
  mediaDemo: 'POST /v1/media/demo',
  analyses: 'POST /v1/analyses',
  analysis: 'GET /v1/analyses/{id}',
  reportCurrent: 'GET /v1/reports/current',
  report: 'GET /v1/reports/{id}',
  reportPlans: 'GET /v1/reports/{id}/plans',
  reportPlansUpsert: 'PUT /v1/reports/{id}/plans',
  planLookRegenerate: 'POST /v1/plans/{id}/look/regenerate',
  plan: 'GET /v1/plans/{id}',
  planSelect: 'POST /v1/plans/{id}/select',
  planChecklist: 'GET /v1/plans/{id}/checklist',
  planChecklistItem: 'PATCH /v1/plans/{id}/checklist/{itemId}',
  planFeedback: 'POST /v1/plans/{id}/feedback',
  diagnostics: 'POST /v1/diagnostics',
  diagnostic: 'PATCH /v1/diagnostics/{id}',
  hairstyles: 'GET /v1/hairstyles',
  hairPreviews: 'POST /v1/hair-previews',
  hairPreview: 'GET /v1/hair-previews/{id}',
  hairPreviewsSaved: 'GET /v1/hair-previews',
  hairPreviewSave: 'POST /v1/hair-previews/{id}/save',
  todayContext: 'GET /v1/today/context',
  todayPlanCurrent: 'GET /v1/today/plans/current',
  todayPlans: 'POST /v1/today/plans',
  todayPlanActivate: 'POST /v1/today/plans/{id}/activate',
  todayPlanFeedback: 'POST /v1/today/plans/{id}/feedback',
  shares: 'POST /v1/shares',
  shareByToken: 'GET /v1/shares/{token}',
  shareRevoke: 'DELETE /v1/shares/{id}',
  wardrobeItems: 'GET /v1/wardrobe/items',
  wardrobeItemCreate: 'POST /v1/wardrobe/items',
  wardrobeItemDelete: 'DELETE /v1/wardrobe/items/{id}',
  wardrobeOutfits: 'POST /v1/wardrobe/outfits',
  wardrobeOutfitWear: 'POST /v1/wardrobe/outfits/{id}/wear',
  advisorMessages: 'POST /v1/advisor/messages',
  advisorConversationMessages: 'GET /v1/advisor/conversations/{id}/messages',
  advisorActionApply: 'POST /v1/advisor/actions/{id}/apply',
  events: 'POST /v1/events',
  meData: 'DELETE /v1/me/data'
} as const

export type ApiPathName = keyof typeof API_PATHS

function query(params: Record<string, string | number | boolean | undefined>): string {
  const pairs = Object.entries(params)
    .filter(([, value]) => value !== undefined && value !== '')
    .map(([key, value]) => `${encodeURIComponent(key)}=${encodeURIComponent(String(value))}`)
  return pairs.length ? `?${pairs.join('&')}` : ''
}

function pathId(prefix: string, id: string): string {
  return `${prefix}/${encodeURIComponent(id)}`
}

function isApiErrorWithStatus(error: unknown, statusCode: number): error is ApiError {
  return typeof error === 'object' && error !== null && (error as ApiError).name === 'ApiError' && (error as ApiError).statusCode === statusCode
}

export interface EndpointOptions {
  /**
   * diagnose() 命中 404（本地残留的 report_id 已在服务端删除）时，
   * 先清掉客户端 report 引用（小程序清 zsm_report_id storage），再无 report_id 重试一次。
   */
  clearReportRef?: () => void
}

export interface ApiEndpoints {
  // ---- 认证与身份（9） ----
  loginWechat(input: { code: string; nickname?: string }): Promise<Session>
  loginDev(input: { user_id?: string; nickname?: string }): Promise<Session>
  requestSmsCode(input: { phone: string }): Promise<void>
  verifySmsCode(input: { phone: string; code: string }): Promise<Session>
  loginApple(input: { identity_token?: string; authorization_code?: string }): Promise<Session>
  loginWechatApp(input: { code: string }): Promise<Session>
  logout(): Promise<void>
  getMe(): Promise<Account>
  getMyProfile(): Promise<UserProfile>
  updateMyProfile(profile: Partial<UserProfile>): Promise<UserProfile>

  // ---- 统一任务（2） ----
  getTask(id: string): Promise<Task>
  getTasks(ids: string[]): Promise<Task[]>

  // ---- 聚合（1） ----
  getHomeBootstrap(): Promise<HomeBootstrap>

  // ---- 媒体（2） ----
  uploadMedia(input: { kind: MediaKind; filePath: string }): Promise<MediaAsset>
  createDemoMedia(kind: MediaKind): Promise<MediaAsset>

  // ---- 分析与报告（4） ----
  createAnalysis(input: CreateAnalysisInput): Promise<TaskCreated<Analysis>>
  getAnalysis(id: string): Promise<Analysis>
  getCurrentReport(): Promise<Report | null>
  getReport(id: string): Promise<Report>

  // ---- 方案（7） ----
  listPlans(reportId: string, scene?: string): Promise<Plan[]>
  upsertPlans(reportId: string, input: UpsertPlansInput): Promise<Plan[]>
  regeneratePlanLook(planId: string): Promise<TaskCreated<Plan>>
  getPlan(id: string): Promise<Plan>
  selectPlan(id: string): Promise<Plan>
  getChecklist(planId: string): Promise<ChecklistItem[]>
  updateChecklistItem(planId: string, itemId: string, patch: { completed: boolean }): Promise<ChecklistItem>
  sendPlanFeedback(planId: string, input: PlanFeedbackInput): Promise<PlanFeedbackResult>

  // ---- 诊断与发型（7） ----
  diagnose(input: CreateDiagnosisInput): Promise<Diagnosis>
  updateDiagnosis(id: string, patch: { saved: boolean }): Promise<Diagnosis>
  listHairstyles(reportId?: string): Promise<HairstyleOption[]>
  createHairPreview(input: CreateHairPreviewInput): Promise<TaskCreated<HairPreview>>
  getHairPreview(id: string): Promise<HairPreview>
  listSavedHairPreviews(): Promise<HairPreview[]>
  saveHairPreview(id: string): Promise<HairPreview>

  // ---- 今日（5） ----
  getTodayContext(city?: string): Promise<TodayContext>
  getCurrentTodayPlan(): Promise<TodayPlan | null>
  createTodayPlan(input: CreateTodayPlanInput): Promise<TaskCreated<TodayPlan>>
  activateTodayPlan(id: string): Promise<TodayPlan>
  sendTodayPlanFeedback(id: string, feedback: string): Promise<TodayPlan>

  // ---- 分享（3） ----
  createShare(input: CreateShareInput): Promise<Share>
  getShareByToken(token: string): Promise<Share>
  revokeShare(id: string): Promise<void>

  // ---- 衣橱（5） ----
  listWardrobeItems(): Promise<WardrobeItem[]>
  createWardrobeItem(input: WardrobeItemInput): Promise<WardrobeItem>
  deleteWardrobeItem(id: string): Promise<void>
  createWardrobeOutfit(input: CreateWardrobeOutfitInput): Promise<WardrobeOutfit>
  wearWardrobeOutfit(id: string): Promise<WardrobeOutfit>

  // ---- 顾问（3） ----
  sendAdvisorMessage(input: SendAdvisorMessageInput): Promise<AdvisorMessage>
  getAdvisorMessages(conversationId: string): Promise<AdvisorMessage[]>
  applyAdvisorAction(id: string): Promise<AdvisorAction>

  // ---- 埋点与隐私（2） ----
  postEvent(body: EventInput): Promise<void>
  deleteMyData(): Promise<void>
}

export function createApiEndpoints(client: ApiClient, options: EndpointOptions = {}): ApiEndpoints {
  const { clearReportRef } = options
  return {
    // ---------- 认证与身份 ----------
    loginWechat: (input) => client.request('/v1/auth/wechat', { method: 'POST', data: input }),
    loginDev: (input) => client.request('/v1/auth/dev', { method: 'POST', data: input }),
    requestSmsCode: async (input) => {
      await client.request('/v1/auth/sms/request', { method: 'POST', data: input })
    },
    verifySmsCode: (input) => client.request('/v1/auth/sms/verify', { method: 'POST', data: input }),
    loginApple: (input) => client.request('/v1/auth/apple', { method: 'POST', data: input }),
    loginWechatApp: (input) => client.request('/v1/auth/wechat-app', { method: 'POST', data: input }),
    logout: async () => {
      await client.request('/v1/auth/session', { method: 'DELETE' })
    },
    getMe: () => client.request('/v1/me'),
    getMyProfile: () => client.request('/v1/me/profile'),
    updateMyProfile: (profile) => client.request('/v1/me/profile', { method: 'PUT', data: profile }),

    // ---------- 统一任务 ----------
    getTask: (id) => client.request(pathId('/v1/tasks', id)),
    getTasks: (ids) => client.request(`/v1/tasks${query({ ids: ids.join(',') })}`),

    // ---------- 聚合 ----------
    getHomeBootstrap: () => client.request('/v1/home/bootstrap'),

    // ---------- 媒体 ----------
    uploadMedia: (input) =>
      client.uploadFile({
        path: '/v1/media',
        filePath: input.filePath,
        name: 'file',
        formData: { kind: input.kind },
        timeout: 30000
      }),
    createDemoMedia: (kind) => client.request('/v1/media/demo', { method: 'POST', data: { kind } }),

    // ---------- 分析与报告 ----------
    createAnalysis: (input) => client.requestEnvelope('/v1/analyses', { method: 'POST', data: input, timeout: 30000 }),
    getAnalysis: (id) => client.request(pathId('/v1/analyses', id)),
    getCurrentReport: () => client.request('/v1/reports/current'),
    getReport: (id) => client.request(pathId('/v1/reports', id)),

    // ---------- 方案 ----------
    listPlans: (reportId, scene) => client.request(`${pathId('/v1/reports', reportId)}/plans${query({ scene })}`),
    upsertPlans: (reportId, input) => client.request(`${pathId('/v1/reports', reportId)}/plans`, { method: 'PUT', data: input }),
    regeneratePlanLook: (planId) => client.requestEnvelope(`/v1/plans/${encodeURIComponent(planId)}/look/regenerate`, { method: 'POST' }),
    getPlan: (id) => client.request(pathId('/v1/plans', id)),
    selectPlan: (id) => client.request(`/v1/plans/${encodeURIComponent(id)}/select`, { method: 'POST' }),
    getChecklist: (planId) => client.request(`/v1/plans/${encodeURIComponent(planId)}/checklist`),
    updateChecklistItem: (planId, itemId, patch) =>
      client.request(`/v1/plans/${encodeURIComponent(planId)}/checklist/${encodeURIComponent(itemId)}`, { method: 'PATCH', data: patch }),
    sendPlanFeedback: (planId, input) => client.request(`/v1/plans/${encodeURIComponent(planId)}/feedback`, { method: 'POST', data: input }),

    // ---------- 诊断与发型 ----------
    // 语义移植：旧 runTool 404 时清本地 report 引用后无 report_id 重试一次
    //（本地缓存的分析已被删除，服务端按无报告上下文重新诊断）。
    diagnose: async (input) => {
      try {
        return await client.request('/v1/diagnostics', {
          method: 'POST',
          data: input,
          timeout: input.kind === 'outfit' ? 65000 : 15000
        })
      } catch (error) {
        if (isApiErrorWithStatus(error, 404) && input.report_id) {
          clearReportRef?.()
          const { report_id: _dropped, ...rest } = input
          return client.request('/v1/diagnostics', {
            method: 'POST',
            data: rest,
            timeout: input.kind === 'outfit' ? 65000 : 15000
          })
        }
        throw error
      }
    },
    updateDiagnosis: (id, patch) => client.request(pathId('/v1/diagnostics', id), { method: 'PATCH', data: patch }),
    listHairstyles: (reportId) => client.request(`/v1/hairstyles${query({ report_id: reportId })}`),
    createHairPreview: (input) => client.requestEnvelope('/v1/hair-previews', { method: 'POST', data: input }),
    getHairPreview: (id) => client.request(pathId('/v1/hair-previews', id)),
    listSavedHairPreviews: () => client.request(`/v1/hair-previews${query({ saved: true })}`),
    saveHairPreview: (id) => client.request(`/v1/hair-previews/${encodeURIComponent(id)}/save`, { method: 'POST' }),

    // ---------- 今日 ----------
    getTodayContext: (city) => client.request(`/v1/today/context${query({ city })}`),
    getCurrentTodayPlan: () => client.request('/v1/today/plans/current'),
    createTodayPlan: (input) => client.requestEnvelope('/v1/today/plans', { method: 'POST', data: input }),
    activateTodayPlan: (id) => client.request(`/v1/today/plans/${encodeURIComponent(id)}/activate`, { method: 'POST' }),
    sendTodayPlanFeedback: (id, feedback) =>
      client.request(`/v1/today/plans/${encodeURIComponent(id)}/feedback`, { method: 'POST', data: { feedback } }),

    // ---------- 分享 ----------
    createShare: (input) => client.request('/v1/shares', { method: 'POST', data: input }),
    getShareByToken: (token) => client.request(pathId('/v1/shares', token)),
    revokeShare: async (id) => {
      await client.request(pathId('/v1/shares', id), { method: 'DELETE' })
    },

    // ---------- 衣橱 ----------
    listWardrobeItems: () => client.request('/v1/wardrobe/items'),
    createWardrobeItem: (input) => client.request('/v1/wardrobe/items', { method: 'POST', data: input }),
    deleteWardrobeItem: async (id) => {
      await client.request(pathId('/v1/wardrobe/items', id), { method: 'DELETE' })
    },
    createWardrobeOutfit: (input) => client.request('/v1/wardrobe/outfits', { method: 'POST', data: input }),
    wearWardrobeOutfit: (id) => client.request(`/v1/wardrobe/outfits/${encodeURIComponent(id)}/wear`, { method: 'POST' }),

    // ---------- 顾问 ----------
    sendAdvisorMessage: (input) => client.request('/v1/advisor/messages', { method: 'POST', data: input, timeout: 20000 }),
    getAdvisorMessages: (conversationId) => client.request(`/v1/advisor/conversations/${encodeURIComponent(conversationId)}/messages`),
    applyAdvisorAction: (id) => client.request(`/v1/advisor/actions/${encodeURIComponent(id)}/apply`, { method: 'POST' }),

    // ---------- 埋点与隐私 ----------
    postEvent: async (body) => {
      await client.request('/v1/events', { method: 'POST', data: body })
    },
    deleteMyData: async () => {
      await client.request('/v1/me/data', { method: 'DELETE' })
    }
  }
}
