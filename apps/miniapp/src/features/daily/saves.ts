// 每日内容收藏的本地读写。
//
// 对应服务端 daily_collection（见 docs/daily-collection-schema.md），字段一一对应，
// 将来换成 API 调用即可，结构不用改。
//
// 关键：收下时存**完整副本**（snapshot）。内容池会迭代（改文案、换素材、甚至下架），
// 但用户手册里的东西不能跟着变 —— 他收下的是"当时的那条建议"。

import type { CollectionCategory, CollectionStatus, ContentVisual } from '@zsm/core'
import { STORAGE_KEYS, readStorage, writeStorage } from '../../services/storage'

/** 内容副本：收下那一刻固化。fit 是函数，落库前必须先渲染成字符串 */
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
  /** 内容去重键（DailyContent.dedupeKey） */
  key: string
  /** 归入手册哪一格 */
  category: CollectionCategory
  savedAt: string
  /** 生命周期：收下 → 试过 → 留下了 */
  status: CollectionStatus
  /** 用户自己的备注 */
  note: string
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
    category,
    savedAt: raw.savedAt,
    status: isStatus(raw.status) ? raw.status : 'saved',
    note: typeof raw.note === 'string' ? raw.note : '',
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

/** 收下一条（同 key 覆盖）；返回最新全量 */
export function addSave(save: DailySave): DailySave[] {
  const next = readSaves().filter((s) => s.key !== save.key)
  next.push(save)
  writeSaves(next)
  return next
}

/** 取消收下；返回最新全量 */
export function removeSave(key: string): DailySave[] {
  const next = readSaves().filter((s) => s.key !== key)
  writeSaves(next)
  return next
}

/** 已收的 dedupeKey 列表，供选品去重 */
export function seenKeys(saves: DailySave[]): string[] {
  return saves.map((s) => s.key)
}

/** 更新状态（收下 → 试过 → 留下了）；返回最新全量 */
export function setStatus(key: string, status: CollectionStatus): DailySave[] {
  const next = readSaves().map((s) => (s.key === key ? { ...s, status } : s))
  writeSaves(next)
  return next
}

/** 更新用户备注；返回最新全量 */
export function setNote(key: string, note: string): DailySave[] {
  const next = readSaves().map((s) => (s.key === key ? { ...s, note } : s))
  writeSaves(next)
  return next
}
