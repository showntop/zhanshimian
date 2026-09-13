// 本地存储：key 语义对齐总计划附录 B（zsm_ 前缀，零历史包袱）。
import Taro from '@tarojs/taro'
import { setLocalLooksResolver } from '@zsm/core'

export const STORAGE_KEYS = {
  token: 'zsm_token',
  uiSchemaVersion: 'zsm_ui_schema_version',
  compareHint: 'zsm_compare_hint',
  // 以下外围 key 在 Task 12 随外围页迁移一并删除。主闭环已不再持有任何
  // 业务 id（服务端资源归 src/app/cache，本地只留 UI 偏好）。
  reportId: 'zsm_report_id',
  activeTaskHairPreview: 'zsm_active_task_hair_preview',
  outfitSession: 'zsm_outfit_session',
  lastOutfitDiagnosis: 'zsm_last_outfit_diagnosis',
  purchaseSession: 'zsm_purchase_session',
  lastPurchaseDiagnosis: 'zsm_last_purchase_diagnosis',
  advisorConversationId: 'zsm_advisor_conversation_id',
  city: 'zsm_city',
  openCreditSheet: 'zsm_open_credit_sheet',
} as const

/** UI 偏好 key：schema 版本变化时只清这些，不迁移任何业务值。 */
const UI_PREFERENCE_KEYS = [
  'compareHint',
  'city',
  'openCreditSheet',
] as const satisfies readonly (keyof typeof STORAGE_KEYS)[]

/** 与 UI 偏好结构绑定的版本号；改动偏好语义时手动 +1。 */
export const UI_SCHEMA_VERSION = '1'

/**
 * 结构版本不匹配时只重置 UI 偏好：旧版本留下的值可能语义已经变了，
 * 与其猜怎么迁移，不如回到默认值——业务数据本来就在服务端。
 */
export function syncUiSchemaVersion(): void {
  if (readStorage(STORAGE_KEYS.uiSchemaVersion) === UI_SCHEMA_VERSION) return
  for (const key of UI_PREFERENCE_KEYS) removeStorage(STORAGE_KEYS[key])
  writeStorage(STORAGE_KEYS.uiSchemaVersion, UI_SCHEMA_VERSION)
}

export function readStorage(key: string): string {
  try {
    const value = Taro.getStorageSync(key)
    return typeof value === 'string' ? value : ''
  } catch {
    return ''
  }
}

export function writeStorage(key: string, value: string): void {
  try {
    if (value) Taro.setStorageSync(key, value)
    else Taro.removeStorageSync(key)
  } catch {
    /* 存储失败静默：业务层用空值兜底 */
  }
}

export function removeStorage(key: string): void {
  try {
    Taro.removeStorageSync(key)
  } catch {
    /* 同上 */
  }
}

/** 「删除我的数据」后清空全部本地状态（token 由 401 重登流程处理）。 */
export function clearAllLocalState(): void {
  for (const key of Object.values(STORAGE_KEYS)) {
    if (key !== STORAGE_KEYS.token) removeStorage(key)
  }
}
