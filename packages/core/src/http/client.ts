// createApiClient —— 传输无关 API 客户端（语义移植自原型 services/api.js）。
// - 拼 baseUrl + path、注入 Bearer、content-type json；
// - 2xx 解 {data}（附带 202 的 task 引用）；非 2xx 抛 ApiError；
// - 401 单飞重登重放一次：非 /v1/auth/ 路径且未重试过 → 清 token →
//   模块级 inflight Promise 共享调 relogin() → 重放一次。并发 401 只触发一次登录；
// - uploadFile 同样 401 重试一次；每请求 timeout 透传。
import type { HttpAdapter, HttpRequest, HttpResponse, TokenStore, UploadAdapter, UploadRequest } from './adapter.ts'

/** 成功信封：{data, task?}（异步创建 202 时附任务引用） */
export interface Envelope<T = unknown> {
  data: T
  task?: TaskRef
}

export interface TaskRef {
  id: string
  type: string
}

export class ApiError extends Error {
  readonly code: string
  readonly statusCode: number
  readonly requestId?: string

  constructor(args: { code: string; message: string; statusCode: number; requestId?: string }) {
    super(args.message)
    this.name = 'ApiError'
    this.code = args.code
    this.statusCode = args.statusCode
    this.requestId = args.requestId
  }
}

/** 成功响应中间件（localizeDevImages 即一个实现） */
export type ResponseMiddleware = (data: any) => any | Promise<any>

export interface RequestOptions {
  method?: HttpRequest['method']
  data?: unknown
  header?: Record<string, string>
  timeout?: number
}

export interface ApiClientOptions {
  adapter: HttpAdapter
  upload?: UploadAdapter
  /** API 根，如 https://api.example.com（末尾不带 /） */
  baseUrl: string
  store?: TokenStore
  /** 重新登录（小程序静默 code 登录 / 手机端刷新会话）。返回后必须能取到新 token */
  relogin?: () => Promise<unknown>
  /** 所有失败的可观测钩子（含重试前的失败） */
  onError?: (error: ApiError, context: { path: string; retried: boolean }) => void
  /** 成功 data 的响应中间件链（依序应用） */
  middleware?: ResponseMiddleware[]
}

export interface ApiClient {
  /** 2xx → data；204 → undefined */
  request<T = any>(path: string, options?: RequestOptions): Promise<T>
  /** 2xx → {data, task?}，异步创建端点用 */
  requestEnvelope<T = any>(path: string, options?: RequestOptions): Promise<Envelope<T>>
  /** multipart 上传，2xx → data（字符串响应体会先尝试 JSON.parse） */
  uploadFile<T = any>(req: UploadRequest): Promise<T>
}

const DEFAULT_TIMEOUT = 15000
const AUTH_PATH_PREFIX = '/v1/auth/'

// 模块级单飞：并发 401 只触发一次 relogin（每个进程通常只建一个 client；
// 多 client 共享同一 inflight 是有意的——避免同一进程重复登录风暴）。
let inflightRelogin: Promise<unknown> | null = null

function ensureRelogin(relogin: () => Promise<unknown>): Promise<unknown> {
  if (!inflightRelogin) {
    inflightRelogin = Promise.resolve()
      .then(relogin)
      .finally(() => {
        inflightRelogin = null
      })
  }
  return inflightRelogin
}

interface ErrorBody {
  error?: { code?: string; message?: string; request_id?: string }
}

function parseMaybeJson(body: unknown): unknown {
  if (typeof body === 'string') {
    try {
      return JSON.parse(body)
    } catch {
      return body
    }
  }
  return body
}

function toApiError(res: HttpResponse, path: string): ApiError {
  const body = parseMaybeJson(res.data) as ErrorBody | null
  const err = body && typeof body === 'object' ? body.error : undefined
  return new ApiError({
    code: err?.code || `http_${res.statusCode}`,
    message: err?.message || '服务暂时不可用，请稍后重试',
    statusCode: res.statusCode,
    requestId: err?.request_id
  })
}

function toEnvelope(res: HttpResponse): Envelope {
  const body = parseMaybeJson(res.data)
  if (body === null || body === undefined) return { data: undefined as any }
  if (typeof body === 'object' && !Array.isArray(body) && ('data' in (body as Record<string, unknown>) || 'task' in (body as Record<string, unknown>))) {
    const record = body as { data?: unknown; task?: TaskRef }
    return { data: record.data as any, task: record.task }
  }
  // 裸值兜底（健壮性：服务端未包信封时不丢数据）
  return { data: body as any }
}

export function createApiClient(options: ApiClientOptions): ApiClient {
  const { adapter, upload, baseUrl, store, relogin, onError, middleware } = options

  const authHeader = (): Record<string, string> => {
    const token = store?.get() ?? ''
    return token ? { Authorization: `Bearer ${token}` } : {}
  }

  const fire = async (
    path: string,
    requestOptions: RequestOptions | undefined,
    uploadReq: UploadRequest | undefined,
    retried: boolean
  ): Promise<Envelope> => {
    const req: HttpRequest = uploadReq
      ? {
          path,
          method: 'POST',
          header: uploadReq.header,
          timeout: uploadReq.timeout
        }
      : {
          path,
          method: requestOptions?.method ?? 'GET',
          data: requestOptions?.data,
          header: requestOptions?.header,
          timeout: requestOptions?.timeout
        }
    const header: Record<string, string> = {
      'content-type': 'application/json',
      ...authHeader(),
      ...(req.header ?? {})
    }
    req.header = header
    if (!req.timeout) req.timeout = DEFAULT_TIMEOUT

    let res: HttpResponse
    try {
      res = uploadReq ? await requireUpload(upload).upload({ ...uploadReq, header }) : await adapter.request(req)
    } catch (cause) {
      const error = new ApiError({
        code: 'network_error',
        message: cause instanceof Error && cause.message ? cause.message : '网络连接失败，请检查网络后重试',
        statusCode: 0
      })
      onError?.(error, { path, retried })
      throw error
    }

    // 401 单飞重登重放一次（登录/登出等 auth 路径自身绝不重试）
    if (res.statusCode === 401 && !path.startsWith(AUTH_PATH_PREFIX) && !retried) {
      if (!relogin) {
        const error = toApiError(res, path)
        onError?.(error, { path, retried })
        throw error
      }
      store?.clear()
      try {
        await ensureRelogin(relogin)
      } catch (cause) {
        const error = new ApiError({
          code: 'relogin_failed',
          message: cause instanceof Error && cause.message ? cause.message : '登录已过期，请重新进入',
          statusCode: 401
        })
        onError?.(error, { path, retried })
        throw error
      }
      return fire(path, requestOptions, uploadReq, true)
    }

    if (res.statusCode < 200 || res.statusCode >= 300) {
      const error = toApiError(res, path)
      onError?.(error, { path, retried })
      throw error
    }

    const envelope = toEnvelope(res)
    // 中间件失败不阻塞数据（沿用原型：warn 后使用原始 data）
    if (middleware?.length && envelope.data !== undefined && envelope.data !== null) {
      let data = envelope.data
      for (const mw of middleware) {
        try {
          data = await mw(data)
        } catch (cause) {
          console.warn('[api] 响应中间件失败，使用原始数据', path, cause)
        }
      }
      envelope.data = data
    }
    return envelope
  }

  const client: ApiClient = {
    async request<T>(path: string, requestOptions?: RequestOptions): Promise<T> {
      const envelope = await fire(path, requestOptions, undefined, false)
      return envelope.data as T
    },
    async requestEnvelope<T>(path: string, requestOptions?: RequestOptions): Promise<Envelope<T>> {
      return (await fire(path, requestOptions, undefined, false)) as Envelope<T>
    },
    async uploadFile<T>(uploadReq: UploadRequest): Promise<T> {
      const envelope = await fire(uploadReq.path, undefined, uploadReq, false)
      return envelope.data as T
    }
  }
  return client
}

function requireUpload(upload: UploadAdapter | undefined): UploadAdapter {
  if (!upload) throw new ApiError({ code: 'upload_unavailable', message: '当前客户端未配置上传通道', statusCode: 0 })
  return upload
}
