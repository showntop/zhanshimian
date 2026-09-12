// 数据真实性契约的 UI 默认实现（AGENTS.md 红线 2 / 3）：
// - 用户照片用 user 模式，无效保持可见空态；
// - 服务端图片用 lookImage 严格校验，绝不回退内置图；
// - 内置素材自动叠角标并弱化；AI 结果即使来自远程 URL 也必须显式叠「AI 风格预览」。
// 钉住 src：Taro 每次把相同 src 再写进原生 <image> 都会让微信重新解码，切 tab 闪一下。
//
// 人像裁切：微信 aspectFill 只能居中裁，全身照放进矮框会切头。
// anchor="top" 改为 cover + 顶对齐（先按宽铺满裁底；图比相框更扁时改按高铺满裁侧）。
import { memo, useRef, useState } from 'react'
import { Image, Text, View } from '@tarojs/components'
import { exampleImage, isBundledAsset, lookImage, userImage } from '@zsm/core'
import './index.scss'

interface ExampleImageProps {
  /** 服务端下发的图片 URL（方案图/效果图/用户照片） */
  src?: string
  /** 显式指定内置模特图（风格参考场景） */
  slug?: string
  variant?: 'full' | 'portrait' | 'report' | 'hair' | 'plan'
  /** 用户本人照片模式（无效时渲染可见空态，不回退） */
  user?: boolean
  badgeText?: string
  mode?: 'aspectFill' | 'aspectFit' | 'widthFix' | 'heightFix'
  /** 人像短框：顶对齐裁切，避免居中切头。商品图/模糊衬底保持默认 center。 */
  anchor?: 'center' | 'top'
  /** 相框宽/高。仅 anchor=top 且未指定 mode 时，用来在裁底 / 裁侧之间选择。 */
  frameAspect?: number
  className?: string
  /** 透传 Image onLoad：报告页用它拿照片真实宽高，换算可视区来定位锚点。
   *  注意微信平台宽高可能是 string，消费侧自行 Number() 归一。 */
  onLoad?: (event: { detail: { width: number | string; height: number | string } }) => void
}

const PinnedImage = memo(function PinnedImage({
  src,
  className,
  mode,
  onLoad,
}: {
  src: string
  className: string
  mode: NonNullable<ExampleImageProps['mode']>
  onLoad?: ExampleImageProps['onLoad']
}) {
  return <Image className={className} src={src} mode={mode} lazyLoad={false} fadeIn={false} onLoad={onLoad} />
})

function ExampleImage({
  src,
  slug,
  variant = 'full',
  user,
  badgeText,
  mode,
  anchor = 'center',
  frameAspect,
  className = '',
  onLoad,
}: ExampleImageProps) {
  const pinnedUrl = useRef('')
  const [fill, setFill] = useState<'width' | 'height'>('width')
  let url = ''
  let isBundledExample = false

  if (slug) {
    url = exampleImage(slug, variant)
    isBundledExample = true
  } else if (user) {
    url = userImage(src)
  } else {
    url = lookImage(src)
    isBundledExample = isBundledAsset(src)
  }

  if (url) pinnedUrl.current = url
  else if (pinnedUrl.current) url = pinnedUrl.current

  if (!url) {
    return (
      <View className={`example-image example-image--empty ${className}`}>
        <Text className="example-image__empty-text">{user ? '照片暂不可用' : '图片暂不可用'}</Text>
      </View>
    )
  }

  const badge = badgeText || (isBundledExample ? '风格参考' : '')
  // 效果示例可能是服务端 URL，也可能已被开发中间件下载成本地路径；
  // 除显式角标外保持弱化，避免示例照片被误读为用户本人效果。
  const softExample = isBundledExample || badge === '效果示例'

  const resolvedMode = mode ?? (
    anchor === 'top'
      ? (fill === 'height' ? 'heightFix' : 'widthFix')
      : 'aspectFill'
  )
  const fillAxis = resolvedMode === 'heightFix' ? 'height' : 'width'
  const anchorClass = anchor === 'top'
    ? `example-image--anchor-top example-image--fill-${fillAxis}`
    : ''

  const handleLoad: ExampleImageProps['onLoad'] = (event) => {
    if (anchor === 'top' && !mode && frameAspect) {
      const w = Number(event.detail.width)
      const h = Number(event.detail.height)
      if (w > 0 && h > 0) {
        const next = w / h > frameAspect ? 'height' : 'width'
        setFill((prev) => (prev === next ? prev : next))
      }
    }
    onLoad?.(event)
  }

  return (
    <View
      className={`example-image ${anchorClass} ${className}`}
      style={url ? { backgroundImage: `url("${url}")` } : undefined}
    >
      <PinnedImage
        className={`example-image__img ${softExample ? 'example-soft' : ''}`}
        src={url}
        mode={resolvedMode}
        onLoad={handleLoad}
      />
      {badge ? (
        <View className="example-badge">
          <Text>{badge}</Text>
        </View>
      ) : null}
    </View>
  )
}

export default memo(ExampleImage, (prev, next) => (
  prev.src === next.src &&
  prev.slug === next.slug &&
  prev.variant === next.variant &&
  prev.user === next.user &&
  prev.badgeText === next.badgeText &&
  prev.mode === next.mode &&
  prev.anchor === next.anchor &&
  prev.frameAspect === next.frameAspect &&
  prev.className === next.className &&
  prev.onLoad === next.onLoad
))
