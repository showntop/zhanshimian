// 公开错误：客户端只能拿到服务端愿意公开的字段。
// 供应商、模型、trace_id 一律留在服务端，界面只用这里的 code/message 决定文案和重试。
export class PublicApiError extends Error {
  code: string
  statusCode: number
  requestId: string
  retryable: boolean

  constructor(
    code: string,
    message: string,
    statusCode: number,
    requestId: string,
    retryable: boolean,
  ) {
    super(message)
    this.name = 'PublicApiError'
    this.code = code
    this.statusCode = statusCode
    this.requestId = requestId
    this.retryable = retryable
  }
}

type ApiResult<T, E> = {
  data?: T
  error?: E
  response: { status: number }
}

type PublicErrorBody = {
  error: {
    code?: string
    message?: string
    request_id?: string
    retryable?: boolean
  }
}

export function bodyOrThrow<T>(result: ApiResult<T, PublicErrorBody>): T {
  if (result.data !== undefined) return result.data
  const error = result.error?.error
  throw new PublicApiError(
    error?.code ?? 'request_failed',
    error?.message ?? '请求没有成功，请重试',
    result.response.status,
    error?.request_id ?? '',
    error?.retryable === true,
  )
}

export function dataOrThrow<T>(result: ApiResult<{ data: T }, PublicErrorBody>): T {
  return bodyOrThrow(result).data
}

/**
 * 成功但按契约没有响应体的状态码（204/205）。
 * 响应中间件据此跳过 JSON 解析——对空体硬解 `JSON.parse('')` 会把一次成功炸成 SyntaxError。
 */
export function isEmptySuccessStatus(status: number): boolean {
  return status === 204 || status === 205
}

/**
 * 「成功即空体」端点的归一：204/205 直接算成功，不要求 data；
 * 其余情况与 bodyOrThrow 同规则（「删除我的数据」就是 204 空体，见契约 deleteMyData）。
 */
export function noContentOrThrow<T>(result: ApiResult<T, PublicErrorBody>): void {
  if (isEmptySuccessStatus(result.response.status)) return
  bodyOrThrow(result)
}
