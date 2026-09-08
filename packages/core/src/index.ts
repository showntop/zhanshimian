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
export { localizeDevImages, type ImageDownloader } from './http/images.ts'
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
  type PollScenario,
  type SubscribeVisibility,
  type TaskPollingHandle,
  type TaskPollingOptions
} from './hooks/useTaskPolling.ts'
export {
  PROGRESS_CATCH_UP_MS,
  advanceDisplayProgress,
  useDisplayProgress,
  type DisplayProgressHandle,
  type DisplayProgressOptions
} from './hooks/useDisplayProgress.ts'
export { APP_NAME, ERROR_COPY, EMPTY_COPY, FEEDBACK_WORDS, HOME_TITLE, PRIVACY_NOTE, SCENES, analysisStageText, greetingByHour, greetingForNow, IMAGE_BADGE_COPY, PRIVACY_SECTION_TITLE, ANALYSIS_STAGE_COPY, type SceneCopy } from './copy/zh.ts'
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
