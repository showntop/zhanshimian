// 试试历史：本地缓存最近试过的今日造型（服务端只有 current 接口，无历史
// 列表——本地留底是唯一出路；卸载/清缓存即失，这是已知取舍）。
//
// 记录时机：今日造型到达 ready（生成成功）时 recordTry；按 plan id 去重，
// 同一条重复生成只刷新快照与时间戳。最多留 7 条，新的顶旧的。
import Taro from '@tarojs/taro'

const TRY_HISTORY_KEY = 'daily_try_history'
const TRY_HISTORY_LIMIT = 7

export interface TryHistoryItem {
  id: string
  title: string
  summary: string
  mediaUrl: string
  triedAt: number
}

interface StoredPlanLike {
  id: string
  title?: string
  summary?: string
  media?: { url?: string } | null
}

export function recordTry(plan: StoredPlanLike): void {
  if (!plan?.id) return
  const list = readTries()
  const next: TryHistoryItem = {
    id: plan.id,
    title: plan.title ?? '',
    summary: plan.summary ?? '',
    mediaUrl: plan.media?.url ?? '',
    triedAt: Date.now(),
  }
  const rest = list.filter((item) => item.id !== plan.id)
  writeTries([next, ...rest].slice(0, TRY_HISTORY_LIMIT))
}

export function listTries(): TryHistoryItem[] {
  return readTries()
}

function readTries(): TryHistoryItem[] {
  try {
    const raw = Taro.getStorageSync(TRY_HISTORY_KEY)
    return Array.isArray(raw) ? (raw as TryHistoryItem[]) : []
  } catch {
    return []
  }
}

function writeTries(list: TryHistoryItem[]): void {
  try {
    Taro.setStorageSync(TRY_HISTORY_KEY, list)
  } catch {
    // 存储失败不阻塞 UI：历史少记一条无伤大雅
  }
}
