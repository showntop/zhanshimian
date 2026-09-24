// 本地存储：只允许 token 与 UI 偏好。业务资源全部归服务端 + src/app/cache，
// 这里再出现业务 id 就是回归。
import Taro from '@tarojs/taro'
import { setLocalLooksResolver } from '@zsm/core'

export const STORAGE_KEYS = {
  token: 'zsm_token',
  uiSchemaVersion: 'zsm_ui_schema_version',
  compareHint: 'zsm_compare_hint',
  // 卡堆手势提示：第一次进决策台时提示「左滑跳过 · 右滑喜欢」，看过一次就收
  deckHint: 'zsm_deck_hint',
  city: 'zsm_city',
  openCreditSheet: 'zsm_open_credit_sheet',
  // 发型方向的性别分段（女士/男士）：只是「上次看的那一侧」，不改变任何业务事实
  hairGender: 'zsm_hair_gender',
  // 每日内容「收下」记录（dedupeKey + 归入哪个资产库）。
  // 服务端「我的手册」接口就绪前暂存本地；就绪后必须移除，改由服务端持有。
  // 不进 UI_PREFERENCE_KEYS：它是用户数据，不该被 UI schema 版本变化清掉。
  dailySaves: 'zsm_daily_saves',
  // 方案集受理在途回执（operation id + 方案集 id + 场景）。服务端 active_operations
  // 不带场景，进程重启后方案 tab 认不出在途受理属于哪个场景。回执带 2h TTL，
  // 只补展示所需的归属，是否在途仍只认服务端（见 app/plan-set-pending）。
  planSetPending: 'zsm_plan_set_pending',
} as const

/** UI 偏好 key：schema 版本变化时只清这些，不迁移任何业务值。 */
const UI_PREFERENCE_KEYS = [
  'compareHint',
  'deckHint',
  'city',
  'openCreditSheet',
  'hairGender',
] as const satisfies readonly (keyof typeof STORAGE_KEYS)[]

/** 与 UI 偏好结构绑定的版本号；改动偏好语义时手动 +1。 */
export const UI_SCHEMA_VERSION = '1'

/** app 启动时调用一次：偏好结构版本不匹配就整组清除（不迁移）。 */
export function syncUiSchemaVersion() {
  try {
    if (readStorage(STORAGE_KEYS.uiSchemaVersion) === UI_SCHEMA_VERSION) return
    for (const key of UI_PREFERENCE_KEYS) removeStorage(STORAGE_KEYS[key])
    writeStorage(STORAGE_KEYS.uiSchemaVersion, UI_SCHEMA_VERSION)
  } catch {
    // 存储不可用（隐私模式/清缓存）：偏好退默认，不阻塞启动
  }
}

/** 读字符串偏好：非字符串值或异常一律回空串（= 未设置）。 */
export function readStorage(key: string): string {
  try {
    const value = Taro.getStorageSync(key)
    return typeof value === 'string' ? value : ''
  } catch {
    return ''
  }
}

/** 写字符串偏好；写空串等于删除。静默失败——偏好不是业务事实。 */
export function writeStorage(key: string, value: string): void {
  try {
    if (value === '') {
      Taro.removeStorageSync(key)
      return
    }
    Taro.setStorageSync(key, value)
  } catch {
    // 同上：偏好可丢
  }
}

export function removeStorage(key: string): void {
  try {
    Taro.removeStorageSync(key)
  } catch {
    // 同上：偏好可丢
  }
}

/** 注销/清数据：除了登录 token 以外全清（含 UI 偏好与城市）。 */
export function clearAllLocalState(): void {
  for (const key of Object.values(STORAGE_KEYS)) {
    if (key !== STORAGE_KEYS.token) removeStorage(key)
  }
}
