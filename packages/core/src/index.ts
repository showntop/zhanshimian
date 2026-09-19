// @zsm/core 统一导出（零运行时依赖）。
export type { HttpRequest, HttpResponse, HttpAdapter, UploadRequest, UploadAdapter, TokenStore } from './http/adapter.ts'
export {
  ApiError,
  createApiClient,
  type ApiClient,
  type ApiClientOptions,
  type Envelope,
  type RequestOptions,
  type ResponseMiddleware,
  type TaskRef
} from './http/client.ts'
export { localizeDevImages, rewriteLoopbackAssetURLs, type ImageDownloader } from './http/images.ts'
export {
  createGeneratedApiClient,
  type GeneratedApiClient
} from './api/client.ts'
export type {
  AcceptedOperationRef,
  Assessment,
  AssessmentAccepted,
  CreateAssessmentRequest,
  CreateExecutionEventRequest,
  CreateExecutionFeedbackRequest,
  CreateGenerationFeedbackRequest,
  CreatePlanSetRequest,
  CreateRenderRunRequest,
  DisplayMedia,
  Execution,
  ExecutionEvent,
  ExecutionEventResult,
  ExecutionFeedback,
  ExecutionStep,
  GenerationFeedback,
  HomeBootstrap,
  // 旧的 types/index.ts 里还留着一个同名但形状完全不同的 MediaAsset（kind/url/demo），
  // 显式导出会盖掉 `export *`。等 Task 12 删掉旧类型后即可改回 MediaAsset。
  MediaAsset as UploadedMediaAsset,
  Operation,
  PlanSet,
  PlanSetAccepted,
  PlanStep,
  PlanVariant,
  PlanVariantDecision,
  PutPlanVariantDecisionRequest,
  PutSelectionRequest,
  RenderRun,
  RenderRunAccepted,
  Report,
  ReportFinding,
  Selection,
  UploadGrant,
  UploadIntent,
  Diagnosis,
  DiagnosisKind,
  HairPreview,
  HairPreviewAccepted,
  BodyPresentation,
  BodyPresentationAccepted,
  BodyPresentationStatus,
  CreateBodyPresentationRequest,
  HairStyle,
  TodayContext,
  TodayPlan,
  TodayPlanAccepted,
  Share,
  ShareView,
  WardrobeItem,
  WardrobeOutfit,
  AdvisorMessage,
  AdvisorAction,
  BillingOrder,
  BillingSKU,
  BillingSummary,
  MeAccount,
  UserProfile
} from './api/types.ts'
export {
  LOCAL_LOOK_SLUGS,
  LOOK_VARIANTS,
  exampleImage,
  getLocalLooksResolver,
  isBundledAsset,
  isDisplayableImage,
  lookImage,
  lookVideo,
  setLocalLooksResolver,
  userImage,
  type LocalLooksResolver,
  type LookSlug,
  type LookVariant
} from './media/truth.ts'
export {
  projectDisplayMedia,
  type DisplayBadge,
  type DisplayImage
} from './media/display.ts'
export {
  CUSTOM_DIRECTION_ID,
  HAIR_DIRECTIONS,
  HAIR_GENDERS,
  hairDirectionViews,
  type HairDirection,
  type HairDirectionView,
  type HairGender,
  type HairGenderTag
} from './hair/directions.ts'
export {
  MAX_OPERATION_FETCH_FAILURES,
  OPERATION_POLL_INTERVAL_MS,
  areOperationsSettled,
  createOperationPolling,
  isOperationSettled,
  type OperationPollingHandle,
  type OperationPollingOptions,
  type SubscribeVisibility
} from './operations/polling.ts'
export {
  PROGRESS_CATCH_UP_MS,
  advanceDisplayProgress,
  useDisplayProgress,
  createDisplayProgress,
  type DisplayProgressHandle,
  type DisplayProgressOptions
} from './hooks/useDisplayProgress.ts'
export { APP_NAME, APP_SLOGAN, DEFAULT_NICKNAME, ERROR_COPY, EMPTY_COPY, FEEDBACK_WORDS, FEEDBACK_ACK_COPY, feedbackAcknowledgement, GENERATION_FEEDBACK_TAGS, EXECUTION_FEEDBACK_TAGS, FEEDBACK_SCREEN_COPY, HOME_COPY, HOME_TITLE, PRIVACY_NOTE, PROFILE_SETUP_COPY, ME_COPY, REPORT_COPY, reportCategoryLabel, SCENES, analysisStageText, ANALYSIS_FAIL_COPY, ASSESSMENT_COPY, ASSESSMENT_STAGE_COPY, assessmentStageText, PLANNING_COPY, PLAN_STAGE_COPY, planStageText, PLANS_COPY, planSlotLabel, SCENE_BRIEF_COPY, sceneIncompleteText, PLAN_DETAIL_COPY, CHECKLIST_COPY, WARDROBE_COPY, greetingByHour, greetingForNow, IMAGE_BADGE_COPY, LAB_COPY, PRIVACY_SECTION_TITLE, SOURCE_IMAGE_COPY, CAPTURE_COPY, captureSelectedText, captureFillMissingText, ANALYSIS_STAGE_COPY, OUTFIT_COPY, PURCHASE_COPY, HAIR_COPY, ADVISOR_COPY, BILLING_COPY, FINDING_TONE_COPY, TASK_DONE_COPY, taskDoneText, DAILY_COPY, dailyBucketName, dailyTypeName, type SceneCopy, type SceneBriefScene, type FeedbackAcknowledgementCode } from './copy/zh.ts'
export * from './daily'

export {
  EVENT_NAME_PATTERN,
  MAX_EVENT_PAYLOAD_BYTES,
  createEventTracker,
  eventPayloadBytes,
  isPayloadWithinLimit,
  isValidEventName,
  setDefaultEventSender,
  trackEvent,
  type EventSender,
  type EventTracker
} from './events.ts'
