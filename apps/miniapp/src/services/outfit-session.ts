// 穿搭诊断会话：同步请求跨页存活；结论以服务端 latest 为准，本地只做即时草稿。
import Taro from '@tarojs/taro'
import type { Diagnosis } from '@zsm/core'
import { STORAGE_KEYS, readStorage, writeStorage } from './storage'

export interface OutfitSession {
  pending: boolean
  scene: string
  photoPath: string
  photoUrl: string
  demoSlug: string
  mediaId: string
  startedAt: number
  result: Diagnosis | null
}

const emptySession = (): OutfitSession => ({
  pending: false,
  scene: 'daily',
  photoPath: '',
  photoUrl: '',
  demoSlug: '',
  mediaId: '',
  startedAt: 0,
  result: null,
})

let memory: OutfitSession | null = null
let inflight: Promise<Diagnosis> | null = null

function persist(next: OutfitSession) {
  memory = next
  writeStorage(STORAGE_KEYS.outfitSession, JSON.stringify(next))
  writeStorage(STORAGE_KEYS.lastOutfitDiagnosis, next.result?.id ?? '')
}

function readStoredSession(): OutfitSession | null {
  try {
    const raw = Taro.getStorageSync(STORAGE_KEYS.outfitSession)
    if (raw && typeof raw === 'object') {
      return { ...emptySession(), ...(raw as OutfitSession) }
    }
    if (typeof raw === 'string' && raw) {
      const parsed = JSON.parse(raw) as OutfitSession
      if (parsed && typeof parsed === 'object') return { ...emptySession(), ...parsed }
    }
  } catch {
    /* 读失败当无会话 */
  }
  return null
}

export function readOutfitSession(): OutfitSession | null {
  if (memory) return memory
  const stored = readStoredSession()
  if (stored) memory = stored
  return stored
}

export function isOutfitPending(): boolean {
  return Boolean(readOutfitSession()?.pending)
}

export function hasOutfitResult(): boolean {
  return Boolean(readOutfitSession()?.result?.id || readStorage(STORAGE_KEYS.lastOutfitDiagnosis))
}

export function writeOutfitDraft(patch: Partial<OutfitSession>) {
  persist({ ...emptySession(), ...readOutfitSession(), ...patch })
}

export function markOutfitPending(patch: Partial<OutfitSession>) {
  persist({
    ...emptySession(),
    ...readOutfitSession(),
    ...patch,
    pending: true,
    startedAt: Date.now(),
    result: null,
  })
}

export function markOutfitDone(result: Diagnosis) {
  persist({
    ...emptySession(),
    ...readOutfitSession(),
    pending: false,
    result,
  })
}

export function markOutfitIdle() {
  const prev = readOutfitSession()
  if (!prev) return
  persist({ ...prev, pending: false })
}

export function clearOutfitResult() {
  const prev = readOutfitSession()
  if (!prev) return
  persist({ ...prev, pending: false, result: null })
}

export function getOutfitInflight(): Promise<Diagnosis> | null {
  return inflight
}

export function runOutfitDiagnose(run: () => Promise<Diagnosis>): Promise<Diagnosis> {
  if (inflight) return inflight
  inflight = run()
    .then((item) => {
      markOutfitDone(item)
      return item
    })
    .catch((error) => {
      markOutfitIdle()
      throw error
    })
    .finally(() => {
      inflight = null
    })
  return inflight
}
