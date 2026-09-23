import AsyncStorage from '@react-native-async-storage/async-storage'
import type { TokenStore } from '@zsm/core'

export const STORAGE_KEYS = {
  token: 'zsm_token',
  reportId: 'zsm_report_id',
  planId: 'zsm_plan_id',
  savedPlanId: 'zsm_saved_plan_id',
  activeTaskAnalysis: 'zsm_active_task_analysis',
  city: 'zsm_city',
} as const

const memory = new Map<string, string>()

export async function hydrateStorage(): Promise<void> {
  const pairs = await AsyncStorage.multiGet(Object.values(STORAGE_KEYS))
  for (const [key, value] of pairs) {
    if (key && value) memory.set(key, value)
  }
}

export function readStorage(key: string): string {
  return memory.get(key) ?? ''
}

export function writeStorage(key: string, value: string): void {
  if (value) {
    memory.set(key, value)
    void AsyncStorage.setItem(key, value)
  } else {
    memory.delete(key)
    void AsyncStorage.removeItem(key)
  }
}

export function clearAllLocalState(): void {
  for (const key of Object.values(STORAGE_KEYS)) {
    if (key === STORAGE_KEYS.token) continue
    writeStorage(key, '')
  }
}

export const tokenStore: TokenStore = {
  get: () => readStorage(STORAGE_KEYS.token),
  set: (token) => writeStorage(STORAGE_KEYS.token, token),
  clear: () => writeStorage(STORAGE_KEYS.token, ''),
}
