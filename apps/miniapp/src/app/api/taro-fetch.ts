// 小程序的 Fetch 运行时。
// openapi-fetch 直接构造 Request/Headers/Response 并调用 fetch，而微信小程序运行时
// 不提供这些全局量；本文件把缺失的补齐（已存在的保留原样），并把 fetch 桥接到
// Taro.request。安装必须早于 createGeneratedApiClient()：openapi-fetch 在创建
// 客户端时就把 globalThis.Request / globalThis.fetch 取走了。
import Taro from '@tarojs/taro'
import { ERROR_COPY } from '@zsm/core'

/** 受保护路径前缀：这里的 401 是"没登录"，不是"登录过期"，不触发重登 */
export const AUTH_PATH_PREFIX = '/v1/auth/'

export const DEFAULT_TIMEOUT = 15000

/** 只依赖 entries() 的 headers 来源：TaroHeaders 和运行时原生 Headers 都满足 */
type HeaderEntries = { entries(): Iterable<[string, string]> }

type HeaderInit =
  | TaroHeaders
  | HeaderEntries
  | Record<string, string>
  | Array<[string, string]>
  | null
  | undefined

/** Headers 的最小实现：openapi-fetch 只用到 get/set/delete/append/entries。 */
export class TaroHeaders {
  private store: Map<string, string>

  constructor(init?: HeaderInit) {
    this.store = new Map()
    if (!init) return
    if (init instanceof TaroHeaders) {
      for (const [key, value] of init.entries()) this.store.set(key, value)
      return
    }
    if (Array.isArray(init)) {
      for (const [key, value] of init) this.store.set(key.toLowerCase(), value)
      return
    }
    if (typeof (init as HeaderEntries).entries === 'function') {
      for (const [key, value] of (init as HeaderEntries).entries()) this.store.set(key.toLowerCase(), value)
      return
    }
    const record = init as Record<string, string>
    for (const key of Object.keys(record)) this.store.set(key.toLowerCase(), record[key] ?? '')
  }

  get(name: string): string | null {
    const value = this.store.get(name.toLowerCase())
    return value === undefined ? null : value
  }

  set(name: string, value: string): void {
    this.store.set(name.toLowerCase(), value)
  }

  append(name: string, value: string): void {
    const key = name.toLowerCase()
    const current = this.store.get(key)
    this.store.set(key, current === undefined ? value : `${current}, ${value}`)
  }

  delete(name: string): void {
    this.store.delete(name.toLowerCase())
  }

  has(name: string): boolean {
    return this.store.has(name.toLowerCase())
  }

  entries(): Array<[string, string]> {
    return Array.from(this.store.entries())
  }

  /** 交给 Taro.request 的普通对象 */
  toObject(): Record<string, string> {
    const out: Record<string, string> = {}
    for (const [key, value] of this.store.entries()) out[key] = value
    return out
  }
}

export type TaroRequestInit = {
  method?: string
  headers?: HeaderInit
  body?: unknown
  [key: string]: unknown
}

/** Request 的最小实现：openapi-fetch 会往实例上再挂 redirect 等额外选项。 */
export class TaroRequest {
  url: string
  method: string
  headers: TaroHeaders
  body: unknown

  constructor(input: string | TaroRequest, init?: TaroRequestInit) {
    const source: TaroRequest | null = typeof input === 'string' ? null : input
    this.url = source ? source.url : (input as string)
    this.method = String(init?.method ?? source?.method ?? 'GET').toUpperCase()
    const headerInit: HeaderInit = init?.headers ?? source?.headers
    this.headers = new TaroHeaders(headerInit)
    this.body = init && 'body' in init ? init.body : source?.body
  }
}

/** Response 的最小实现：只需要 status/ok/headers.get()/json()/text()/blob()。 */
export class TaroResponse {
  status: number
  ok: boolean
  headers: TaroHeaders
  private payload: unknown

  constructor(payload: unknown, status: number, headers?: HeaderInit) {
    this.payload = payload
    this.status = status
    this.ok = status >= 200 && status < 300
    this.headers = new TaroHeaders(headers)
  }

  async text(): Promise<string> {
    if (typeof this.payload === 'string') return this.payload
    const encoded: string | undefined = JSON.stringify(this.payload)
    return encoded ?? ''
  }

  async json(): Promise<unknown> {
    return typeof this.payload === 'string' ? JSON.parse(this.payload) : this.payload
  }

  async blob(): Promise<unknown> {
    return this.payload
  }

  async arrayBuffer(): Promise<unknown> {
    return this.payload
  }
}

/**
 * FormData 的最小实现。本文件的 JSON 调用不会用到它，但 openapi-fetch 在
 * 序列化请求体前会执行 `body instanceof FormData`，缺少这个全局量会直接
 * ReferenceError，所以必须存在。
 */
export class TaroFormData {
  private parts: Array<[string, unknown]>

  constructor() {
    this.parts = []
  }

  append(name: string, value: unknown): void {
    this.parts.push([name, value])
  }

  getAll(name: string): unknown[] {
    return this.parts.filter(([key]) => key === name).map(([, value]) => value)
  }

  has(name: string): boolean {
    return this.parts.some(([key]) => key === name)
  }

  entries(): Array<[string, unknown]> {
    return this.parts.slice()
  }
}

export type FetchRuntimeOptions = {
  /** 当前 token；空串表示未登录 */
  resolveToken: () => string
  /** 401（且非 /v1/auth/*）时调用；实现方负责单飞，本文件只保证重放一次 */
  onUnauthorized: () => Promise<void>
  timeout?: number
}

/** 取 URL 的路径部分，不依赖 URL 全局量 */
export function pathnameOf(url: string): string {
  const withoutScheme = url.replace(/^[a-z][a-z0-9+.-]*:\/\/[^/]*/i, '')
  const path = withoutScheme.split(/[?#]/)[0]
  return path && path.startsWith('/') ? path : '/'
}

/** 网络层失败也走同一条公开错误通道：调用方只需要认识 PublicApiError。 */
function networkFailure(): TaroResponse {
  return new TaroResponse(
    { error: { code: 'network_failed', message: ERROR_COPY.network, retryable: true } },
    0,
    new TaroHeaders(),
  )
}

async function sendOnce(request: TaroRequest, token: string, timeout: number): Promise<TaroResponse> {
  const headers = new TaroHeaders(request.headers)
  if (token) headers.set('Authorization', `Bearer ${token}`)
  let res: { statusCode: number; data: unknown; header?: unknown }
  try {
    res = await Taro.request({
      url: request.url,
      method: request.method as never,
      data: request.body as never,
      header: headers.toObject(),
      timeout,
    })
  } catch {
    return networkFailure()
  }
  const raw = (res.header ?? {}) as Record<string, string>
  return new TaroResponse(res.data as unknown, res.statusCode, new TaroHeaders(raw))
}

let installed = false

/**
 * 安装缺失的 Fetch 全局量，并把 fetch 指向 Taro 传输。
 * 幂等：重复调用只覆盖 fetch，不重复构造 Request/Headers/Response。
 */
export function installTaroFetchRuntime(options: FetchRuntimeOptions): void {
  const globals = globalThis as unknown as Record<string, unknown>
  if (!globals.Headers) globals.Headers = TaroHeaders
  if (!globals.Request) globals.Request = TaroRequest
  if (!globals.Response) globals.Response = TaroResponse
  if (!globals.FormData) globals.FormData = TaroFormData

  const timeout = options.timeout ?? DEFAULT_TIMEOUT
  globals.fetch = async (input: string | TaroRequest, init?: TaroRequestInit) => {
    const request = input instanceof TaroRequest ? new TaroRequest(input, init) : new TaroRequest(input, init)
    const response = await sendOnce(request, options.resolveToken(), timeout)
    if (response.status !== 401 || pathnameOf(request.url).startsWith(AUTH_PATH_PREFIX)) return response
    await options.onUnauthorized()
    return sendOnce(request, options.resolveToken(), timeout)
  }
  installed = true
}

export function isTaroFetchRuntimeInstalled(): boolean {
  return installed
}
