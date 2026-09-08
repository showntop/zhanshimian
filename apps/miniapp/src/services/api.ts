// 唯一 API 门面：core 的类型化端点 + 埋点 sender 接线。
import { createApiEndpoints, setDefaultEventSender, setLocalLooksResolver } from '@zsm/core'
import { client } from './http'
import { localLooksResolver } from './local-looks'
import Taro from '@tarojs/taro'
import { readStorage, STORAGE_KEYS } from './storage'

setLocalLooksResolver(localLooksResolver)
setDefaultEventSender((body) => client.request('/v1/events', { method: 'POST', data: body }))

export const api = createApiEndpoints(client, {
  // diagnose() 命中 404 时清掉本地残留的报告引用再无报告重试一次。
  clearReportRef: () => writeStorageClear(),
})

function writeStorageClear(): void {
  try {
    Taro.removeStorageSync(STORAGE_KEYS.reportId)
  } catch {
    /* 忽略 */
  }
}

export function currentReportId(): string {
  return readStorage(STORAGE_KEYS.reportId)
}
