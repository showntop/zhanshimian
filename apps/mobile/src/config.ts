import Constants from 'expo-constants'

const extra = (Constants.expoConfig?.extra ?? {}) as { apiBaseURL?: string }

export function resolveBaseURL(): string {
  const fromEnv = process.env.EXPO_PUBLIC_API_BASE_URL
  const base = (fromEnv || extra.apiBaseURL || 'http://127.0.0.1:58000').replace(/\/$/, '')
  if (!base) throw new Error('当前版本尚未配置 API 地址')
  return base
}
