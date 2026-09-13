// DisplayMedia → 可渲染图片的唯一投影。
//
// 服务端下发的是带类型的判别联合：`source_kind` 有四个取值，`display_label`
// 是跟着它走的常量。角标一律由 `source_kind` 推出，不看 provider、不看 URL、
// 也不看 URL 里有没有 "demo"——这三条正是过去把示例图误当用户效果的根源。
//
// 任何一处不合格（空 URL、非法协议、webp、签名过期、mime 不对）都返回 null，
// 由调用点渲染可见空态；绝不隐式回退内置模特图。
import { IMAGE_BADGE_COPY } from '../copy/zh.ts'
import type { DisplayMedia } from '../api/types.ts'

/** 三种角标：用户原图「原本」、生成图与内置图「风格参考」、Demo「效果示例」 */
export type DisplayBadge =
  | typeof IMAGE_BADGE_COPY.original
  | typeof IMAGE_BADGE_COPY.aiPreview
  | typeof IMAGE_BADGE_COPY.bundled
  | typeof IMAGE_BADGE_COPY.demo

export interface DisplayImage {
  /** React 列表 key：同一资产换签名 URL 就是另一张图，必须带上 url */
  key: string
  src: string
  badge: DisplayBadge
  /** 内置图与 Demo 要弱化，避免被读成用户本人的效果 */
  soften: boolean
  sourceKind: DisplayMedia['source_kind']
}

function badgeFor(sourceKind: DisplayMedia['source_kind']): DisplayBadge {
  switch (sourceKind) {
    case 'user_original':
      return IMAGE_BADGE_COPY.original
    case 'demo_example':
      return IMAGE_BADGE_COPY.demo
    case 'generated_preview':
      return IMAGE_BADGE_COPY.aiPreview
    case 'bundled_reference':
      return IMAGE_BADGE_COPY.bundled
  }
}

export function projectDisplayMedia(
  media: DisplayMedia | null | undefined,
  now = Date.now(),
): DisplayImage | null {
  if (!media?.asset_id) return null
  const url = media.url
  if (
    !url.startsWith('https://') &&
    !url.startsWith('wxfile://') &&
    !url.startsWith('file://')
  ) {
    return null
  }
  if (/\.webp(?:\?|$)/i.test(url)) return null
  // 签名过期的 URL 渲染出来是裂图，不如让调用点显示空态
  const expiresAt = Date.parse(media.url_expires_at)
  if (!Number.isFinite(expiresAt) || expiresAt <= now) return null

  const generated = media.source_kind !== 'user_original'
  if (generated && media.mime_type !== 'image/jpeg') return null
  if (!generated && media.mime_type !== 'image/jpeg' && media.mime_type !== 'image/png') {
    return null
  }

  return {
    key: `${media.asset_id}:${url}`,
    src: url,
    badge: badgeFor(media.source_kind),
    soften:
      media.source_kind === 'demo_example' || media.source_kind === 'bundled_reference',
    sourceKind: media.source_kind,
  }
}
