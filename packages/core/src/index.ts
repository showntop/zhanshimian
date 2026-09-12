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
export { API_PATHS, createApiEndpoints, type ApiEndpoints, type ApiPathName, type EndpointOptions, type TaskCreated } from './api/endpoints.ts'
export * from './types/index.ts'
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
  shippedAsset,
  userImage,
  type LocalLooksResolver,
  type LookSlug,
  type LookVariant
} from './media/truth.ts'
export {
  MAX_POLL_FAILURES,
  POLL_INTERVALS,
  shouldStopPolling,
  useTaskPolling,
  createTaskPolling,
  type PollScenario,
  type SubscribeVisibility,
  type TaskPollingHandle,
  type TaskPollingOptions
} from './hooks/useTaskPolling.ts'
export {
  PROGRESS_CATCH_UP_MS,
  advanceDisplayProgress,
  useDisplayProgress,
  createDisplayProgress,
  type DisplayProgressHandle,
  type DisplayProgressOptions
} from './hooks/useDisplayProgress.ts'
export { APP_NAME, APP_SLOGAN, DEFAULT_NICKNAME, ERROR_COPY, EMPTY_COPY, FEEDBACK_WORDS, HOME_COPY, HOME_TITLE, PRIVACY_NOTE, PROFILE_SETUP_COPY, REPORT_COPY, SCENES, analysisStageText, analysisTimelineText, ANALYSIS_STAGE_TIMELINE, ANALYSIS_FAIL_COPY, greetingByHour, greetingForNow, IMAGE_BADGE_COPY, LAB_COPY, PRIVACY_SECTION_TITLE, ANALYSIS_STAGE_COPY, OUTFIT_COPY, PURCHASE_COPY, PLAN_DETAIL_COPY, PLANS_COPY, CHECKLIST_COPY, ADVISOR_COPY, BILLING_COPY, FINDING_TONE_COPY, type SceneCopy } from './copy/zh.ts'
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
