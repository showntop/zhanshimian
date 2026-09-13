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
