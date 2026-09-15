// 三图建档的纯逻辑：槽位状态机 + 提交入参 + 幂等键。
// 不碰 Taro、不碰网络、不拼文案——页面和组件只消费这里的判定结果。
//
// 两张不同的「照片集合」在这里被刻意分开，别混：
// - `CaptureSlots`：带状态的槽（可能正在读取/上传/失败），驱动界面；
// - `PhotosByRole`：只有成功的槽投影出的 DisplayMedia，驱动提交。
// 混成一个对象的后果是「上传中」和「已就绪」看起来一样，提交按钮会在错误时机亮起。
import type { CreateAssessmentRequest, DisplayMedia } from '@zsm/core'

/** 三个拍摄角色；顺序即页面从左到右的槽位顺序。 */
export const CAPTURE_ROLES = ['face', 'side', 'body'] as const

export type CaptureRole = (typeof CAPTURE_ROLES)[number]

/**
 * 槽位状态机，任一时刻只允许这六种之一：
 * `empty → selecting → hashing → uploading → ready`，任一环节出错落回 `failed`，
 * `failed` 只能靠用户「重试 / 换一张」回到 `selecting`。
 */
export const CAPTURE_SLOT_PHASES = [
  'empty',
  'selecting',
  'hashing',
  'uploading',
  'ready',
  'failed',
] as const

export type CaptureSlotPhase = (typeof CAPTURE_SLOT_PHASES)[number]

export interface CaptureSlot {
  role: CaptureRole
  phase: CaptureSlotPhase
  /** 服务端下发的 DisplayMedia；只有 ready 槽有值。 */
  media: DisplayMedia | null
  /**
   * 本地临时文件路径。上传途中靠它预览（服务端不回媒体 URL），
   * 也是「重试」要重传的那一份——重试不该让用户再拍一次。
   */
  localPath: string
  /** 失败原因。文案来自 CAPTURE_COPY，不在页面里拼。只有 failed 槽有值。 */
  errorText: string
}

export type CaptureSlots = Record<CaptureRole, CaptureSlot>

/** 角色 → 该角色已就绪的 DisplayMedia。缺槽或缺媒体时该键不存在。 */
export type PhotosByRole = Partial<Record<CaptureRole, DisplayMedia | null | undefined>>

export function createSlots(): CaptureSlots {
  return {
    face: emptySlot('face'),
    side: emptySlot('side'),
    body: emptySlot('body'),
  }
}

function emptySlot(role: CaptureRole): CaptureSlot {
  return { role, phase: 'empty', media: null, localPath: '', errorText: '' }
}

/**
 * 唯一的槽写入入口：把状态机收在一处，顺便守住两条不变量——
 * `ready` 必须有 media（否则 photosByRole 会漏掉它，按钮却亮了），
 * 非 `ready` 一律不保留 media 的「已就绪」语义（老图仍留在槽里给用户看，
 * 但不会再被当成一张选好的照片参与提交）。
 */
export function updateSlot(
  slots: CaptureSlots,
  role: CaptureRole,
  patch: Partial<Omit<CaptureSlot, 'role'>>,
): CaptureSlots {
  if (patch.phase !== undefined && !CAPTURE_SLOT_PHASES.includes(patch.phase)) {
    throw new Error(`capture: unknown slot phase ${String(patch.phase)}`)
  }
  const next: CaptureSlot = { ...slots[role], ...patch }
  if (next.phase === 'ready' && !next.media) {
    throw new Error('capture: ready slot without media')
  }
  return { ...slots, [role]: next }
}

/** 从槽里取「已就绪」的照片。上传中 / 失败 / 空的槽一律不出现在结果里。 */
export function photosByRole(slots: CaptureSlots): PhotosByRole {
  const photos: PhotosByRole = {}
  for (const role of CAPTURE_ROLES) {
    const slot = slots[role]
    if (slot.phase === 'ready' && slot.media) photos[role] = slot.media
  }
  return photos
}

/**
 * Demo 拉取失败时的槽位恢复：进 demo 流程前的槽快照 → 失败后该落回的样子。
 * 之前有 ready 照片就原样回去（媒体与本地路径都还在，与 pick 的 revertTo 同一语义）；
 * 本来就没照片的槽才回 empty。直接清整槽的代价是：用户已有真实照片时，
 * 点一次「先用效果示例体验」失败就把已有照片一起丢掉。
 */
export function demoRestoreSlot(slot: CaptureSlot): Partial<Omit<CaptureSlot, 'role'>> {
  if (slot.media) {
    return { phase: 'ready', media: slot.media, localPath: slot.localPath, errorText: '' }
  }
  return { phase: 'empty', media: null, localPath: '', errorText: '' }
}

/** 三个角色齐备且互不相同——同一张照片占两个槽不算建好档。 */
export function captureReady(photos: PhotosByRole): boolean {
  try {
    const [face, side, body] = requireIds(photos)
    return new Set([face, side, body]).size === 3
  } catch {
    return false
  }
}

/**
 * 提交体只带三个 asset id：补充资料由服务端在受理时自行快照，
 * 客户端多发一份只会造出「页面上的资料」和「档案里的资料」两份真相。
 */
export function toAssessmentInput(photos: PhotosByRole): CreateAssessmentRequest {
  const [face, side, body] = requireIds(photos)
  return {
    photos: {
      face_asset_id: face,
      side_asset_id: side,
      body_asset_id: body,
    },
  }
}

/**
 * 幂等键由三张照片的 asset id 派生，不含时间戳与随机数：
 * 同一个「同一个用户 + 同样三张照片」的重复提交必须复用同一把键，
 * 网络重试才不会在服务端造出第二份档案。
 */
export function assessmentIdempotencyKey(photos: PhotosByRole): string {
  const [face, side, body] = requireIds(photos)
  return `assessment:${face}:${side}:${body}`
}

/** 内部断言：三张都就绪才有 id 可谈。消息只给开发者看，不面向用户。 */
function requireIds(photos: PhotosByRole): [string, string, string] {
  const ids = CAPTURE_ROLES.map((role) => photos[role]?.asset_id ?? '')
  const [face, side, body] = ids
  if (!face || !side || !body) throw new Error('capture: photos not ready')
  return [face, side, body]
}
