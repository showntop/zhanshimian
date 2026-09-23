// 唯一允许调用生成客户端的文件。页面和 features 只 import 这里的方法，
// 不得出现字符串拼接的 URL，也不得再出现第二条 polling 或第二个错误类型。
import type {
  AssessmentAccepted,
  CreateAssessmentRequest,
  CreateExecutionEventRequest,
  CreateExecutionFeedbackRequest,
  CreateGenerationFeedbackRequest,
  CreatePlanSetRequest,
  CreateRenderRunRequest,
  DisplayMedia,
  Execution,
  ExecutionEventResult,
  ExecutionFeedback,
  GenerationFeedback,
  HomeBootstrap,
  Operation,
  PlanSet,
  PlanSetAccepted,
  PlanVariantDecision,
  PutPlanVariantDecisionRequest,
  PutSelectionRequest,
  RenderRunAccepted,
  Report,
  Selection,
} from '@zsm/core'
import { CAPTURE_COPY } from '@zsm/core'
import { client } from './client'
import { bodyOrThrow, dataOrThrow, noContentOrThrow, PublicApiError } from './result'

/** 契约里 `/v1/media/demo` 的 role 枚举；与建档页的槽位角色同形但不互相依赖。 */
export type DemoMediaRole = 'face' | 'side' | 'body'

/**
 * 创建方案集有两种成功：200 复用已发布方案集，202 受理异步规划。
 * 归一成判别联合，调用方不必去猜响应体长什么样。
 */
export type PlanSetStart = ({ accepted: true } & PlanSetAccepted) | { accepted: false; planSet: PlanSet }

export const qualityApi = {
  createAssessment: (body: CreateAssessmentRequest, idempotencyKey: string): Promise<AssessmentAccepted> =>
    client
      .POST('/v1/assessments', {
        body,
        params: { header: { 'Idempotency-Key': idempotencyKey } },
      })
      .then(bodyOrThrow),

  /**
   * Demo 媒体：服务端返回带类型的 DisplayMedia，`source_kind` 必须是 `demo_example`。
   * 别的来源混进来就抛错——「拿内置模特图充当用户照片」正是这道类型闸门要挡的事，
   * 所以校验放在唯一允许碰客户端的这一层，页面拿到的永远是验过的。
   */
  createDemoMedia: (role: DemoMediaRole, idempotencyKey: string): Promise<DisplayMedia> =>
    client
      .POST('/v1/media/demo', {
        body: { role },
        params: { header: { 'Idempotency-Key': idempotencyKey } },
      })
      .then(dataOrThrow)
      .then((media) => {
        if (media.source_kind !== 'demo_example') {
          throw new PublicApiError(
            'demo_media_unexpected_kind',
            CAPTURE_COPY.demoUnavailable,
            0,
            '',
            false,
          )
        }
        return media
      }),

  getOperation: (id: string): Promise<Operation> =>
    client.GET('/v1/operations/{id}', { params: { path: { id } } }).then(dataOrThrow),

  /** 一次问一批 Operation：契约里 ids 是逗号分隔的字符串，不是数组。 */
  listOperations: (ids: readonly string[]): Promise<Operation[]> =>
    client
      .GET('/v1/operations', { params: { query: { ids: ids.join(',') } } })
      .then(dataOrThrow),

  /** 还没有报告时服务端返回 404；这不是错误，是空态。 */
  getCurrentReport: async (): Promise<Report | null> => {
    const result = await client.GET('/v1/reports/current')
    if (result.response.status === 404) return null
    return dataOrThrow(result)
  },

  getReport: (id: string): Promise<Report> =>
    client.GET('/v1/reports/{id}', { params: { path: { id } } }).then(dataOrThrow),

  createPlanSet: async (body: CreatePlanSetRequest, idempotencyKey: string): Promise<PlanSetStart> => {
    const payload = bodyOrThrow(
      await client.POST('/v1/plan-sets', {
        body,
        params: { header: { 'Idempotency-Key': idempotencyKey } },
      }),
    )
    return 'operation' in payload ? { accepted: true, ...payload } : { accepted: false, planSet: payload.data }
  },

  getPlanSet: (id: string): Promise<PlanSet> =>
    client.GET('/v1/plan-sets/{id}', { params: { path: { id } } }).then(dataOrThrow),

  listPlanSets: (reportId: string, scene?: PlanSet['scene']): Promise<PlanSet[]> =>
    client
      .GET('/v1/plan-sets', { params: { query: { report_id: reportId, scene } } })
      .then(dataOrThrow),

  createRenderRun: (planVariantId: string, idempotencyKey: string): Promise<RenderRunAccepted> => {
    const body: CreateRenderRunRequest = {}
    return client
      .POST('/v1/plan-variants/{id}/render-runs', {
        params: { path: { id: planVariantId }, header: { 'Idempotency-Key': idempotencyKey } },
        body,
      })
      .then(bodyOrThrow)
  },

  /** PUT 语义：同一 Idempotency-Key 重放拿回同一条 Selection（200），首次创建为 201。 */
  selectPlanSet: (planSetId: string, body: PutSelectionRequest, idempotencyKey: string): Promise<Selection> =>
    client
      .PUT('/v1/plan-sets/{id}/selection', {
        params: { path: { id: planSetId }, header: { 'Idempotency-Key': idempotencyKey } },
        body,
      })
      .then(dataOrThrow),

  /** 卡堆决策：PUT 语义——首次写入与改主意都是 200；重试换新幂等键。 */
  putVariantDecision: (
    planVariantId: string,
    decision: PutPlanVariantDecisionRequest['decision'],
    idempotencyKey: string,
  ): Promise<PlanVariantDecision> =>
    client
      .PUT('/v1/plan-variants/{id}/decision', {
        params: { path: { id: planVariantId }, header: { 'Idempotency-Key': idempotencyKey } },
        body: { decision },
      })
      .then(dataOrThrow),

  /** 撤销决策：服务端幂等删除，行不存在也回 204。 */
  deleteVariantDecision: async (planVariantId: string): Promise<void> => {
    noContentOrThrow(await client.DELETE('/v1/plan-variants/{id}/decision', { params: { path: { id: planVariantId } } }))
  },

  createExecution: (selectionId: string, idempotencyKey: string): Promise<Execution> =>
    client
      .POST('/v1/selections/{id}/executions', {
        params: { path: { id: selectionId }, header: { 'Idempotency-Key': idempotencyKey } },
        body: {},
      })
      .then(dataOrThrow),

  getExecution: (id: string): Promise<Execution> =>
    client.GET('/v1/executions/{id}', { params: { path: { id } } }).then(dataOrThrow),

  /** 事件必须带当前 Execution version 的强 ETag，服务端据此拒绝过期写入。 */
  createExecutionEvent: (
    executionId: string,
    body: CreateExecutionEventRequest,
    idempotencyKey: string,
    ifMatch: string,
  ): Promise<ExecutionEventResult> =>
    client
      .POST('/v1/executions/{id}/events', {
        params: {
          path: { id: executionId },
          header: { 'Idempotency-Key': idempotencyKey, 'If-Match': ifMatch },
        },
        body,
      })
      .then(dataOrThrow),

  createGenerationFeedback: (
    body: CreateGenerationFeedbackRequest,
    idempotencyKey: string,
  ): Promise<GenerationFeedback> =>
    client
      .POST('/v1/generation-feedback', {
        body,
        params: { header: { 'Idempotency-Key': idempotencyKey } },
      })
      .then(dataOrThrow),

  createExecutionFeedback: (
    body: CreateExecutionFeedbackRequest,
    idempotencyKey: string,
  ): Promise<ExecutionFeedback> =>
    client
      .POST('/v1/execution-feedback', {
        body,
        params: { header: { 'Idempotency-Key': idempotencyKey } },
      })
      .then(dataOrThrow),

  getHomeBootstrap: (): Promise<HomeBootstrap> => client.GET('/v1/home/bootstrap').then(dataOrThrow),

  /** 「删除我的数据」：服务端清空全部业务数据，成功回 204 空体；客户端随后清本地 UI 偏好。 */
  deleteMyData: async (): Promise<void> => {
    noContentOrThrow(await client.DELETE('/v1/me/data'))
  },
}
