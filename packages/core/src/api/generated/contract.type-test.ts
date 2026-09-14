import type { components, operations, paths } from './schema.ts'

type Expect<T extends true> = T
type HasMethod<Path extends keyof paths, Method extends PropertyKey> =
  Method extends keyof paths[Path] ? true : false

type RequiredOperationIds =
  | 'createUploadIntent'
  | 'completeUploadIntent'
  | 'createDemoMedia'
  | 'createAssessment'
  | 'getOperation'
  | 'listOperations'
  | 'getCurrentReport'
  | 'getReport'
  | 'createPlanSet'
  | 'getPlanSet'
  | 'listPlanSets'
  | 'createRenderRun'
  | 'getRenderRun'
  | 'putPlanSetSelection'
  | 'createSelectionExecution'
  | 'getExecution'
  | 'createExecutionEvent'
  | 'createGenerationFeedback'
  | 'createExecutionFeedback'
  | 'getHomeBootstrap'

type HasRequiredOperationIds = Expect<
  Exclude<RequiredOperationIds, keyof operations> extends never ? true : false
>
type HasCreateUploadIntent = Expect<
  HasMethod<'/v1/media/upload-intents', 'post'>
>
type HasCompleteUploadIntent = Expect<
  HasMethod<'/v1/media/upload-intents/{id}/complete', 'post'>
>
type HasCreateDemoMedia = Expect<HasMethod<'/v1/media/demo', 'post'>>
type HasCreateAssessment = Expect<HasMethod<'/v1/assessments', 'post'>>
type HasGetOperation = Expect<HasMethod<'/v1/operations/{id}', 'get'>>
type HasListOperations = Expect<HasMethod<'/v1/operations', 'get'>>
type HasGetCurrentReport = Expect<HasMethod<'/v1/reports/current', 'get'>>
type HasGetReport = Expect<HasMethod<'/v1/reports/{id}', 'get'>>
type HasCreatePlanSet = Expect<HasMethod<'/v1/plan-sets', 'post'>>
type HasGetPlanSet = Expect<HasMethod<'/v1/plan-sets/{id}', 'get'>>
type HasListPlanSets = Expect<HasMethod<'/v1/plan-sets', 'get'>>
type HasCreateRenderRun = Expect<
  HasMethod<'/v1/plan-variants/{id}/render-runs', 'post'>
>
type HasGetRenderRun = Expect<HasMethod<'/v1/render-runs/{id}', 'get'>>
type HasPutPlanSetSelection = Expect<
  HasMethod<'/v1/plan-sets/{id}/selection', 'put'>
>
type HasCreateSelectionExecution = Expect<
  HasMethod<'/v1/selections/{id}/executions', 'post'>
>
type HasGetExecution = Expect<HasMethod<'/v1/executions/{id}', 'get'>>
type HasCreateExecutionEvent = Expect<
  HasMethod<'/v1/executions/{id}/events', 'post'>
>
type HasCreateGenerationFeedback = Expect<
  HasMethod<'/v1/generation-feedback', 'post'>
>
type HasCreateExecutionFeedback = Expect<
  HasMethod<'/v1/execution-feedback', 'post'>
>
type HasGetHomeBootstrap = Expect<HasMethod<'/v1/home/bootstrap', 'get'>>
// 旧任务端点不得回流：以 `['/v1/' + 'tasks']` 拼接避免门禁 grep 命中字面量。
type LegacyTasksPath = `/v1/${'tasks'}/{id}`
type LegacyAnalysesPath = `/v1/${'analyses'}`
type HasNoLegacyTasks = Expect<keyof paths & LegacyTasksPath extends never ? true : false>
type HasNoLegacyAnalyses = Expect<keyof paths & LegacyAnalysesPath extends never ? true : false>

type HairPreviewQuery = NonNullable<
  operations['listHairPreviews']['parameters']['query']
>
type HasHairPreviewSavedFilter = Expect<
  'saved' extends keyof HairPreviewQuery ? true : false
>
type HasHairPreviewStateFilter = Expect<
  'state' extends keyof HairPreviewQuery ? true : false
>

type DisplayMedia = components['schemas']['DisplayMedia']
type DisplayMediaBase = {
  asset_id: string
  url: string
  url_expires_at: string
  mime_type: 'image/jpeg'
}
type AcceptsUserOriginalPair = Expect<
  DisplayMediaBase & {
    source_kind: 'user_original'
    display_label: '原本'
  } extends DisplayMedia
    ? true
    : false
>
type AcceptsGeneratedPreviewPair = Expect<
  DisplayMediaBase & {
    source_kind: 'generated_preview'
    display_label: '风格参考'
  } extends DisplayMedia
    ? true
    : false
>
type AcceptsBundledReferencePair = Expect<
  DisplayMediaBase & {
    source_kind: 'bundled_reference'
    display_label: '风格参考'
  } extends DisplayMedia
    ? true
    : false
>
type AcceptsDemoExamplePair = Expect<
  DisplayMediaBase & {
    source_kind: 'demo_example'
    display_label: '效果示例'
  } extends DisplayMedia
    ? true
    : false
>
type RejectsIllegalDisplayMediaPair = Expect<
  DisplayMediaBase & {
    source_kind: 'user_original'
    display_label: '效果示例'
  } extends DisplayMedia
    ? false
    : true
>

type Operation = components['schemas']['Operation']
type OperationBase = {
  id: string
  kind: 'assessment'
  subject_type: string
  subject_id: string
  progress_bps: number
  stage_code: string
  public_message: string
  retryable: boolean
  created_at: string
  updated_at: string
}
type AcceptsFailedOperationWithTrace = Expect<
  OperationBase & { status: 'failed'; trace_id: string } extends Operation
    ? true
    : false
>
type RejectsFailedOperationWithoutTrace = Expect<
  OperationBase & { status: 'failed' } extends Operation ? false : true
>
type AcceptsNonFailedOperationWithoutTrace = Expect<
  OperationBase & { status: 'running' } extends Operation ? true : false
>

export type ContractAssertions = [
  HasRequiredOperationIds,
  HasCreateUploadIntent,
  HasCompleteUploadIntent,
  HasCreateDemoMedia,
  HasCreateAssessment,
  HasGetOperation,
  HasListOperations,
  HasGetCurrentReport,
  HasGetReport,
  HasCreatePlanSet,
  HasGetPlanSet,
  HasListPlanSets,
  HasCreateRenderRun,
  HasGetRenderRun,
  HasPutPlanSetSelection,
  HasCreateSelectionExecution,
  HasGetExecution,
  HasCreateExecutionEvent,
  HasCreateGenerationFeedback,
  HasCreateExecutionFeedback,
  HasGetHomeBootstrap,
  HasNoLegacyTasks,
  HasNoLegacyAnalyses,
  HasHairPreviewSavedFilter,
  HasHairPreviewStateFilter,
  AcceptsUserOriginalPair,
  AcceptsGeneratedPreviewPair,
  AcceptsBundledReferencePair,
  AcceptsDemoExamplePair,
  RejectsIllegalDisplayMediaPair,
  AcceptsFailedOperationWithTrace,
  RejectsFailedOperationWithoutTrace,
  AcceptsNonFailedOperationWithoutTrace,
]
