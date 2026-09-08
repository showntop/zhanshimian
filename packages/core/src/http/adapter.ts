// 平台注入的传输适配器 —— core 零运行时依赖，不 import 任何平台 SDK。
// 小程序端用 Taro.request / Taro.uploadFile 实现；RN 端用 fetch / FormData；
// 测试用 fake adapter。core 只依赖这些接口。

export interface HttpRequest {
  /** API 路径（以 / 开头），客户端负责拼 baseUrl */
  path: string
  method?: 'GET' | 'POST' | 'PUT' | 'PATCH' | 'DELETE' | 'HEAD' | 'OPTIONS'
  /** JSON 请求体（对象；平台层负责序列化） */
  data?: unknown
  header?: Record<string, string>
  /** 每请求超时（ms），透传给平台层 */
  timeout?: number
}

export interface HttpResponse<T = unknown> {
  /** HTTP 状态码；网络层失败由 adapter 以异常形式抛出 */
  statusCode: number
  /** 已解析的响应体（JSON 对象/数组，或原始值）；204 时可为 undefined */
  data: T
  header?: Record<string, string>
}

export interface HttpAdapter {
  request(req: HttpRequest): Promise<HttpResponse>
}

export interface UploadRequest {
  path: string
  /** 本地临时文件路径 */
  filePath: string
  /** multipart 字段名，默认 'file' */
  name?: string
  formData?: Record<string, string | number | boolean>
  header?: Record<string, string>
  timeout?: number
}

export interface UploadAdapter {
  upload(req: UploadRequest): Promise<HttpResponse>
}

/** token 持久化（小程序 Taro.getStorageSync / RN AsyncStorage 各自实现） */
export interface TokenStore {
  get(): string
  set(token: string): void
  clear(): void
}
