// 三环境 API 地址（对齐原型 miniapp/config/runtime.js 语义）：
// - develop 允许本地联调地址（微信开发者工具视 loopback IP 为安全源）；
// - trial/release 必须是已备案 HTTPS 域名，缺失直接抛错，绝不静默回退本地。
// - 第三方平台可通过 extConfig.apiBaseURL 覆盖。
import Taro from '@tarojs/taro'

const PRODUCTION_API = 'https://prompt.wuyill.com/zhanshimian'

const apiBaseURLs: Record<string, string> = {
  develop: PRODUCTION_API,
  trial: PRODUCTION_API,
  release: PRODUCTION_API,
}

function pickBaseURL(): string | undefined {
  try {
    const accountInfo = Taro.getAccountInfoSync()
    const envVersion = accountInfo.miniProgram.envVersion
    if (envVersion && envVersion in apiBaseURLs) return apiBaseURLs[envVersion]
  } catch {
    /* 开发者工具旧基础库：按 develop 处理 */
  }
  try {
    const ext = Taro.getExtConfigSync?.()
    if (ext && typeof ext.apiBaseURL === 'string' && ext.apiBaseURL) return ext.apiBaseURL
  } catch {
    /* 无第三方平台扩展 */
  }
  return undefined
}

export function resolveBaseURL(): string {
  const base = pickBaseURL() ?? apiBaseURLs.develop
  if (!base) throw new Error('当前版本尚未配置 HTTPS API 域名')
  return base.replace(/\/$/, '')
}
