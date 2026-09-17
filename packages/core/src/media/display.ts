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

// http://tmp/ 与 http://usr/ 不是网络来源：微信开发者工具用这两个主机名模拟本地
// 文件系统（临时目录 / USER_DATA_PATH，对应真机的 wxfile://tmp / wxfile://usr），
// 开发者工具的 <image> 能正常渲染它们。真机不会出现这两个主机名——服务端就算
// 下发这种字符串也只是一张裂图，变不成别人的照片，所以放行不开任何口子；
// 其余 http:// 一律按「渲染不了」拒掉。
// data:image/ 同理是自包含的内联图（工具新渲染层拦截 http://tmp/ 后的换出形态，
// 见 miniapp local-file.ts 的 displayableImagePath），不是网络来源，放行。
function isDevtoolsLocalFile(url: string): boolean {
  return url.startsWith('http://tmp/') || url.startsWith('http://usr/')
}

function isInlineImageData(url: string): boolean {
  return url.startsWith('data:image/')
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
    !url.startsWith('file://') &&
    !isInlineImageData(url) &&
    !isDevtoolsLocalFile(url)
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
    // React 列表 key。data URL 自身就是几百 KB 的字符串，不进 key——
    // 它没有独立身份，asset_id 就是身份（同一槽位换图必然换 asset_id 或重挂）
    key: isInlineImageData(url) ? `${media.asset_id}:data` : `${media.asset_id}:${url}`,
    src: url,
    badge: badgeFor(media.source_kind),
    soften:
      media.source_kind === 'demo_example' || media.source_kind === 'bundled_reference',
    sourceKind: media.source_kind,
  }
}
