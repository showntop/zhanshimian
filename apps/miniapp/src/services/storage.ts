// 本地存储：key 语义对齐总计划附录 B（zsm_ 前缀，零历史包袱）。
import Taro from '@tarojs/taro'
import { setLocalLooksResolver } from '@zsm/core'

export const STORAGE_KEYS = {
  token: 'zsm_token',
  reportId: 'zsm_report_id',
  planId: 'zsm_plan_id',
  savedPlanId: 'zsm_saved_plan_id',
  activeTaskAnalysis: 'zsm_active_task_analysis',
  activeTaskPlanLook: 'zsm_active_task_plan_look',
  activeTaskHairPreview: 'zsm_active_task_hair_preview',
  outfitSession: 'zsm_outfit_session',
  lastOutfitDiagnosis: 'zsm_last_outfit_diagnosis',
  scenePending: 'zsm_scene_pending',
  sceneBrief: 'zsm_scene_brief',
  advisorConversationId: 'zsm_advisor_conversation_id',
  city: 'zsm_city',
} as const

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
