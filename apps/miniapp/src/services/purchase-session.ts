// 购买判断会话：同步请求跨页存活；结论以服务端 latest 为准，本地只做即时草稿。
import Taro from '@tarojs/taro'
import type { Diagnosis } from '@zsm/core'
import { STORAGE_KEYS, readStorage, writeStorage } from './storage'

export interface PurchaseSession {
  pending: boolean
  photoPath: string
  photoUrl: string
  demoSlug: string
  mediaId: string
  startedAt: number
  result: Diagnosis | null
}

const emptySession = (): PurchaseSession => ({
  pending: false,
  photoPath: '',
  photoUrl: '',
  demoSlug: '',
  mediaId: '',
  startedAt: 0,
  result: null,
})

let memory: PurchaseSession | null = null
let inflight: Promise<Diagnosis> | null = null

function persist(next: PurchaseSession) {
  memory = next
  writeStorage(STORAGE_KEYS.purchaseSession, JSON.stringify(next))
  writeStorage(STORAGE_KEYS.lastPurchaseDiagnosis, next.result?.id ?? '')
}

function readStoredSession(): PurchaseSession | null {
  try {
    const raw = Taro.getStorageSync(STORAGE_KEYS.purchaseSession)
    if (raw && typeof raw === 'object') {
      return { ...emptySession(), ...(raw as PurchaseSession) }
    }
    if (typeof raw === 'string' && raw) {
      const parsed = JSON.parse(raw) as PurchaseSession
      if (parsed && typeof parsed === 'object') return { ...emptySession(), ...parsed }
    }
  } catch {
    /* 读失败当无会话 */
  }
  return null
}

export function readPurchaseSession(): PurchaseSession | null {
  if (memory) return memory
  const stored = readStoredSession()
  if (stored) memory = stored
  return stored
}

export function isPurchasePending(): boolean {
  return Boolean(readPurchaseSession()?.pending)
}

export function hasPurchaseResult(): boolean {
  return Boolean(readPurchaseSession()?.result?.id || readStorage(STORAGE_KEYS.lastPurchaseDiagnosis))
}

export function writePurchaseDraft(patch: Partial<PurchaseSession>) {
  persist({ ...emptySession(), ...readPurchaseSession(), ...patch })
}

export function markPurchasePending(patch: Partial<PurchaseSession>) {
  persist({
    ...emptySession(),
    ...readPurchaseSession(),
    ...patch,
    pending: true,
    startedAt: Date.now(),
    result: null,
  })
}

export function markPurchaseDone(result: Diagnosis) {
  persist({
    ...emptySession(),
    ...readPurchaseSession(),
    pending: false,
    result,
  })
}

export function markPurchaseIdle() {
  const prev = readPurchaseSession()
  if (!prev) return
  persist({ ...prev, pending: false })
}

export function clearPurchaseResult() {
  const prev = readPurchaseSession()
  if (!prev) return
  persist({ ...prev, pending: false, result: null })
}

export function getPurchaseInflight(): Promise<Diagnosis> | null {
  return inflight
}

export function runPurchaseDiagnose(run: () => Promise<Diagnosis>): Promise<Diagnosis> {
  if (inflight) return inflight
  inflight = run()
    .then((item) => {
      markPurchaseDone(item)
      return item
    })
    .catch((error) => {
      markPurchaseIdle()
      throw error
    })
    .finally(() => {
      inflight = null
    })
  return inflight
}
