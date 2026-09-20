// 每日内容收藏：本地为离线缓存 + 乐观更新留底，真源是服务端 daily_collection
// （docs/daily-collection-schema.md，字段一一对应）。
//
// 写穿纪律：先本地后网络。网络失败不阻塞 UI——本地记录带 pendingSync 标记，
// 恢复后由 syncPendingSaves 重放；服务端以 (user_id, content_key) 幂等去重，
// 重放安全。收下时存**完整副本**（snapshot）：内容池会迭代，但用户手册里的
// 东西不能跟着变——他收下的是"当时的那条建议"。

import type { CollectionCategory, CollectionStatus, ContentVisual } from '@zsm/core'
import { peripherals } from '../../app/api/peripherals'
import { STORAGE_KEYS, readStorage, writeStorage } from '../../services/storage'

/** 内容副本：收下那一刻固化。fit 在服务端已渲染成 fitText 字符串 */
export interface SavedSnapshot {
  id: string
  type: string
  topic: string
  lead: string
  /** 已按该用户形象基因渲染后的适配说明 */
  fitText: string
  why: string
  visual: ContentVisual
  category: CollectionCategory
}

export interface DailySave {
  /** 内容去重键（DailyContent.dedupe_key = 服务端 content_key） */
  key: string
  /** 服务端内容行 id（收藏接口的 content_id） */
  contentId?: string
  /** 归入手册哪一格 */
  category: CollectionCategory
  savedAt: string
  /** 生命周期：收下 → 试过 → 留下了 */
  status: CollectionStatus
  /** 用户自己的备注 */
  note: string
  /** 待同步：本地已收、服务端还没确认 */
  pendingSync?: boolean
  snapshot: SavedSnapshot
}

function isStatus(value: unknown): value is CollectionStatus {
  return value === 'saved' || value === 'tried' || value === 'kept'
}

/** 旧数据（只有 key/asset/savedAt）也能读进来，缺字段补默认值 */
function normalize(raw: Record<string, unknown>): DailySave | null {
  if (typeof raw.key !== 'string' || typeof raw.savedAt !== 'string') return null
  const category = (raw.category ?? raw.asset) as CollectionCategory | undefined
  if (typeof category !== 'string') return null
  return {
    key: raw.key,
    contentId: typeof raw.contentId === 'string' ? raw.contentId : undefined,
    category,
    savedAt: raw.savedAt,
    status: isStatus(raw.status) ? raw.status : 'saved',
    note: typeof raw.note === 'string' ? raw.note : '',
    pendingSync: raw.pendingSync === true,
    snapshot: (raw.snapshot as SavedSnapshot) ?? null,
  }
}

/** 读全部收下记录；坏数据整条丢弃，不阻断页面 */
export function readSaves(): DailySave[] {
  const raw = readStorage(STORAGE_KEYS.dailySaves)
  if (raw === '') return []
  try {
    const parsed: unknown = JSON.parse(raw)
    if (!Array.isArray(parsed)) return []
    return parsed
      .map((item) => (typeof item === 'object' && item !== null ? normalize(item as Record<string, unknown>) : null))
      .filter((item): item is DailySave => item !== null)
  } catch {
    return []
  }
}

function writeSaves(saves: DailySave[]): void {
  writeStorage(STORAGE_KEYS.dailySaves, JSON.stringify(saves))
}

function update(predicate: (save: DailySave) => DailySave | null): DailySave[] {
  const next = readSaves()
    .map(predicate)
    .filter((save): save is DailySave => save !== null)
  writeSaves(next)
  return next
}

/**
 * 收下一条（同 key 覆盖）：本地先落（乐观），再写穿服务端。
 * 返回最新全量；服务端结果异步落地，失败留 pendingSync 待重放。
 */
export function addSave(save: DailySave): DailySave[] {
  const next = readSaves().filter((s) => s.key !== save.key)
  next.push(save)
  writeSaves(next)
  if (save.contentId) {
    void peripherals
      .createDailyCollection({ content_id: save.contentId, note: save.note || undefined })
      .then(() => {
        // 同步成功：摘掉 pendingSync 标记
        update((item) => (item.key === save.key ? { ...item, pendingSync: false } : item))
      })
      .catch(() => {
        // 保留 pendingSync，等 syncPendingSaves 重放
      })
  }
  return next
}

/** 取消收下；本地立即生效，服务端尽力删除（幂等 204） */
export function removeSave(key: string): DailySave[] {
  const target = readSaves().find((s) => s.key === key)
  const next = readSaves().filter((s) => s.key !== key)
  writeSaves(next)
  if (target?.contentId) {
    void peripherals.deleteDailyCollection(target.contentId).catch(() => {})
  }
  return next
}

/** 已收的 dedupeKey 列表，供选品去重 */
export function seenKeys(saves: DailySave[]): string[] {
  return saves.map((s) => s.key)
}

/** 更新状态（收下 → 试过 → 留下了） */
export function setStatus(key: string, status: CollectionStatus): DailySave[] {
  const target = readSaves().find((s) => s.key === key)
  const next = update((s) => (s.key === key ? { ...s, status } : s))
  if (target?.contentId) {
    void peripherals.updateDailyCollection(target.contentId, { status }).catch(() => {})
  }
  return next
}

/** 更新用户备注 */
export function setNote(key: string, note: string): DailySave[] {
  const target = readSaves().find((s) => s.key === key)
  const next = update((s) => (s.key === key ? { ...s, note } : s))
  if (target?.contentId) {
    void peripherals.updateDailyCollection(target.contentId, { note }).catch(() => {})
  }
  return next
}

/**
 * 重放待同步的收藏：恢复联网后调用（服务端幂等，重放安全）。
 * 服务端还没有的条目才 POST；已同步过的只是摘标记。
 */
export async function syncPendingSaves(): Promise<void> {
  const pending = readSaves().filter((s) => s.pendingSync)
  for (const save of pending) {
    if (!save.contentId) continue
    try {
      await peripherals.createDailyCollection({ content_id: save.contentId, note: save.note || undefined })
      update((item) => (item.key === save.key ? { ...item, pendingSync: false } : item))
    } catch {
      // 仍不在线：下次再试
    }
  }
}
