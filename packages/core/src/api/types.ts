import type { components } from './generated/schema.ts'

export type Operation = components['schemas']['Operation']
export type DisplayMedia = components['schemas']['DisplayMedia']
export type Assessment = components['schemas']['Assessment']
export type Report = components['schemas']['Report']
export type ReportFinding = components['schemas']['ReportFinding']
export type PlanSet = components['schemas']['PlanSet']
export type PlanVariant = components['schemas']['PlanVariant']
export type PlanVariantDecision = components['schemas']['PlanVariantDecision']
export type PlanStep = components['schemas']['PlanStep']
export type Selection = components['schemas']['Selection']
export type Execution = components['schemas']['Execution']
export type ExecutionStep = components['schemas']['ExecutionStep']
export type HomeBootstrap = components['schemas']['HomeBootstrap']

// 受理信封：异步写入返回 202 + operation 引用，客户端据此进入唯一 polling 路径。
export type AcceptedOperationRef = components['schemas']['AcceptedOperationRef']
export type AssessmentAccepted = components['schemas']['AssessmentAccepted']
export type PlanSetAccepted = components['schemas']['PlanSetAccepted']
export type RenderRunAccepted = components['schemas']['RenderRunAccepted']

// 渲染与执行
export type RenderRun = components['schemas']['RenderRun']
export type ExecutionEvent = components['schemas']['ExecutionEvent']
export type ExecutionEventResult = components['schemas']['ExecutionEventResult']

// 反馈
export type GenerationFeedback = components['schemas']['GenerationFeedback']
export type ExecutionFeedback = components['schemas']['ExecutionFeedback']

// 上传事务：intent 只负责直传授权，complete 之后才拿得到 MediaAsset。
export type UploadIntent = components['schemas']['UploadIntent']
export type UploadGrant = components['schemas']['UploadGrant']
export type MediaAsset = components['schemas']['MediaAsset']

// 请求体：与生成 schema 同源，禁止在小程序里手写第二份。
export type CreateAssessmentRequest = components['schemas']['CreateAssessmentRequest']
export type CreatePlanSetRequest = components['schemas']['CreatePlanSetRequest']
export type PutSelectionRequest = components['schemas']['PutSelectionRequest']
export type PutPlanVariantDecisionRequest = components['schemas']['PutPlanVariantDecisionRequest']
export type CreateRenderRunRequest = Record<string, never>
export type CreateExecutionEventRequest = components['schemas']['CreateExecutionEventRequest']
export type CreateGenerationFeedbackRequest = components['schemas']['CreateGenerationFeedbackRequest']
export type CreateExecutionFeedbackRequest = components['schemas']['CreateExecutionFeedbackRequest']

// 外围（工具/生活分包）响应：同样与生成 schema 同源。
export type Diagnosis = components['schemas']['Diagnosis']
export type DiagnosisKind = 'outfit' | 'purchase'
export type HairPreview = components['schemas']['HairPreview']
export type HairPreviewAccepted = components['schemas']['HairPreviewAccepted']
export type HairStyle = components['schemas']['HairStyle']
export type BodyPresentation = components['schemas']['BodyPresentation']
export type BodyPresentationAccepted = components['schemas']['BodyPresentationAccepted']
export type BodyPresentationStatus = components['schemas']['BodyPresentationStatus']
export type CreateBodyPresentationRequest = components['schemas']['CreateBodyPresentationRequest']
export type TodayContext = components['schemas']['TodayContext']
export type TodayPlan = components['schemas']['TodayPlan']
export type TodayPlanAccepted = components['schemas']['TodayPlanAccepted']

// 每日内容（Daily）：契约类型与生成 schema 同源。
// 注意：DailyContentDTO 叫 DTO 是因为 core/src/daily 的本地内容池类型
// （带 fit 函数的 DailyContent）仍被选品纯函数使用，两者不混用；
// 服务端内容一律走 DTO（fit 已在服务端渲染成 fit_text）。
export type DailyPrepare = components['schemas']['DailyPrepare']
export type DailyGenerateResult = components['schemas']['DailyGenerateResult']
export type DailyContentDTO = components['schemas']['DailyContent']
export type DailyContentVisual = components['schemas']['DailyContentVisual']
export type DailyContentType = components['schemas']['DailyContentType']
export type DailyCollection = components['schemas']['DailyCollection']
export type DailyCollectionStats = components['schemas']['DailyCollectionStats']
export type Share = components['schemas']['Share']
export type ShareView = components['schemas']['ShareView']
export type WardrobeItem = components['schemas']['WardrobeItem']
export type WardrobeOutfit = components['schemas']['WardrobeOutfit']
export type AdvisorMessage = components['schemas']['AdvisorMessage']
export type AdvisorAction = components['schemas']['AdvisorAction']
export type BillingOrder = components['schemas']['BillingOrder']
export type BillingSKU = components['schemas']['BillingSKU']
export type BillingSummary = components['schemas']['BillingSummary']
export type MeAccount = components['schemas']['MeAccount']
export type UserProfile = components['schemas']['UserProfile']
