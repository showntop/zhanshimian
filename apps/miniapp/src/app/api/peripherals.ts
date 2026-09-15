// 外围页（工具/生活分包）的类型化窄函数：一个端点一个函数，
// 请求与响应一律来自生成的 schema（经 core 桶文件转出）。这里不出现字符串拼 URL，也不出现旧 DTO。
import type {
  AdvisorAction,
  BodyPresentation,
  BodyPresentationAccepted,
  BodyPresentationStatus,
  CreateBodyPresentationRequest,
  BillingOrder,
  BillingSummary,
  Diagnosis,
  HairPreview,
  HairPreviewAccepted,
  HairStyle,
  MeAccount,
  Share,
  ShareView,
  TodayContext,
  TodayPlan,
  TodayPlanAccepted,
  UserProfile,
  WardrobeItem,
  WardrobeOutfit,
} from '@zsm/core'
import type { AdvisorMessage } from '@zsm/core'
import { client } from './client'
import { bodyOrThrow, dataOrThrow } from './result'
import { createIdempotencyKey } from '../keys'
import { normalizeOutfit } from '../../features/wardrobe/model'

export type DiagnosisKind = 'outfit' | 'purchase'

/** 诊断：同步出结果；404 时由调用方按「无报告上下文」决定是否重试。 */
export const peripherals = {
  diagnose: (input: {
    kind: DiagnosisKind
    media_id: string
    scene?: string
    report_id?: string
  }): Promise<Diagnosis> => client.POST('/v1/diagnostics', { body: input }).then(dataOrThrow),

  getLatestDiagnosis: (kind: DiagnosisKind): Promise<Diagnosis | null> =>
    client
      .GET('/v1/diagnostics/latest', { params: { query: { kind } } })
      .then((result) => (result.response.status === 404 ? null : dataOrThrow(result))),

  updateDiagnosis: (id: string, patch: { saved: boolean }): Promise<Diagnosis> =>
    client.PATCH('/v1/diagnostics/{id}', { params: { path: { id } }, body: patch }).then(dataOrThrow),

  // ---------- 发型 ----------
  listHairstyles: (): Promise<HairStyle[]> =>
    client.GET('/v1/hairstyles').then(dataOrThrow),

  /** 异步生成：返回受理信封，状态只通过公开 Operation 观察。 */
  createHairPreview: (input: {
    media_id: string
    style_id: string
    report_id?: string
    scene?: string
  }): Promise<HairPreviewAccepted> =>
    client
      .POST('/v1/hair-previews', {
        body: input,
        params: { header: { 'Idempotency-Key': createIdempotencyKey(`hair-preview:${input.style_id}`) } },
      })
      .then(bodyOrThrow),

  getHairPreview: (id: string): Promise<HairPreview> =>
    client.GET('/v1/hair-previews/{id}', { params: { path: { id } } }).then(dataOrThrow),

  /** 当前进行中的预览；404 表示没有进行中任务，这不是错误。 */
  getActiveHairPreview: (): Promise<HairPreview | null> =>
    client
      .GET('/v1/hair-previews/active')
      .then((result) => (result.response.status === 404 ? null : dataOrThrow(result))),

  listHairPreviews: (): Promise<HairPreview[]> =>
    client.GET('/v1/hair-previews').then(dataOrThrow),

  saveHairPreview: (id: string): Promise<HairPreview> =>
    client.POST('/v1/hair-previews/{id}/save', { params: { path: { id } } }).then(dataOrThrow),

  // ---------- 今日 ----------
  getTodayContext: (city?: string): Promise<TodayContext> =>
    client.GET('/v1/today/context', { params: { query: { city } } }).then(dataOrThrow),

  getCurrentTodayPlan: (): Promise<TodayPlan | null> =>
    client
      .GET('/v1/today/plans/current')
      .then((result) => (result.response.status === 404 ? null : dataOrThrow(result))),

  /** 异步生成：返回受理信封，状态只通过公开 Operation 观察。 */
  createTodayPlan: (input: {
    report_id?: string
    city?: string
    schedule?: string
    refresh?: boolean
  }): Promise<TodayPlanAccepted> =>
    client
      .POST('/v1/today/plans', {
        body: input,
        params: { header: { 'Idempotency-Key': createIdempotencyKey('today-plan') } },
      })
      .then(bodyOrThrow),

  activateTodayPlan: (id: string): Promise<TodayPlan> =>
    client.POST('/v1/today/plans/{id}/activate', { params: { path: { id } } }).then(dataOrThrow),

  submitTodayPlanFeedback: (id: string, feedback: string): Promise<TodayPlan> =>
    client
      .POST('/v1/today/plans/{id}/feedback', { params: { path: { id } }, body: { feedback } })
      .then(dataOrThrow),

  // ---------- 分享 ----------
  createShare: (input: {
    source_type: 'plan_variant' | 'today_plan'
    source_id: string
    include_photo?: boolean
  }): Promise<Share> => client.POST('/v1/shares', { body: input }).then(dataOrThrow),

  getShareByToken: (shareRef: string): Promise<ShareView> =>
    client.GET('/v1/shares/{share_ref}', { params: { path: { share_ref: shareRef } } }).then(dataOrThrow),

  revokeShare: async (id: string): Promise<void> => {
    await client.DELETE('/v1/shares/{share_ref}', { params: { path: { share_ref: id } } })
  },

  // ---------- 衣橱 ----------
  listWardrobeItems: (): Promise<WardrobeItem[]> =>
    client.GET('/v1/wardrobe/items').then(dataOrThrow),

  createWardrobeItem: (input: {
    media_id?: string
    name: string
    category: string
    color: string
    season?: string
    formality?: string
    scenes?: string[]
  }): Promise<WardrobeItem> =>
    client.POST('/v1/wardrobe/items', { body: input }).then(dataOrThrow),

  deleteWardrobeItem: async (id: string): Promise<void> => {
    await client.DELETE('/v1/wardrobe/items/{id}', { params: { path: { id } } })
  },

  // outfit 的两个写路径服务端都不回填 items（nil slice → JSON null），统一在边界归一
  createWardrobeOutfit: (input: {
    title: string
    note?: string
    item_ids: string[]
  }): Promise<WardrobeOutfit> =>
    client.POST('/v1/wardrobe/outfits', { body: input }).then(dataOrThrow).then(normalizeOutfit),

  wearWardrobeOutfit: (id: string): Promise<WardrobeOutfit> =>
    client
      .POST('/v1/wardrobe/outfits/{id}/wear', { params: { path: { id } } })
      .then(dataOrThrow)
      .then(normalizeOutfit),

  // ---------- 顾问 ----------
  sendAdvisorMessage: (input: {
    conversation_id?: string
    content: string
    report_id?: string
  }): Promise<AdvisorMessage> =>
    client.POST('/v1/advisor/messages', { body: input }).then(dataOrThrow),

  listAdvisorMessages: (): Promise<AdvisorMessage[]> =>
    client.GET('/v1/advisor/conversations/current/messages').then(dataOrThrow),

  applyAdvisorAction: (id: string): Promise<AdvisorAction> =>
    client.POST('/v1/advisor/actions/{id}/apply', { params: { path: { id } } }).then(dataOrThrow),

  // ---------- 3D 形象 Lite ----------
  /** 异步生成：返回受理信封，状态只通过公开 Operation 观察。 */
  createBodyPresentation: (input: CreateBodyPresentationRequest): Promise<BodyPresentationAccepted> =>
    client.POST('/v1/body-presentations', { body: input }).then(bodyOrThrow),

  getBodyPresentationStatus: (): Promise<BodyPresentationStatus> =>
    client.GET('/v1/body-presentations/status').then(dataOrThrow),

  getBodyPresentation: (id: string): Promise<BodyPresentation> =>
    client.GET('/v1/body-presentations/{id}', { params: { path: { id } } }).then(dataOrThrow),

  // ---------- 身份与账单 ----------
  getMe: (): Promise<MeAccount> => client.GET('/v1/me').then(dataOrThrow),

  getMyProfile: (): Promise<UserProfile | null> =>
    client.GET('/v1/me/profile').then(dataOrThrow),

  updateMyProfile: (profile: UserProfile): Promise<UserProfile> =>
    client.PUT('/v1/me/profile', { body: profile }).then(dataOrThrow),

  updateMe: (input: { nickname?: string; avatar_media_id?: string }): Promise<MeAccount> =>
    client.PATCH('/v1/me', { body: input }).then(dataOrThrow),

  getBillingMe: (): Promise<BillingSummary> => client.GET('/v1/billing/me').then(dataOrThrow),

  createBillingOrder: (input: { sku_id: string; code?: string }): Promise<BillingOrder> =>
    client.POST('/v1/billing/orders', { body: input }).then(dataOrThrow),

  syncBillingOrder: (id: string): Promise<BillingOrder> =>
    client.POST('/v1/billing/orders/{id}/sync', { params: { path: { id } } }).then(dataOrThrow),
}
