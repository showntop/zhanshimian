// 数据真实性契约的 UI 默认实现（AGENTS.md 红线 2 / 3）：
// - 用户照片用 user 模式，无效保持可见空态；
// - 服务端图片用 lookImage 严格校验，绝不回退内置图；
// - 内置素材自动叠角标并弱化；AI 结果即使来自远程 URL 也必须显式叠「AI 风格预览」。
// 钉住 src：Taro 每次把相同 src 再写进原生 <image> 都会让微信重新解码，切 tab 闪一下。
import { memo, useRef } from 'react'
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
  className?: string
  /** 透传 Image onLoad：报告页用它拿照片真实宽高，换算 aspectFit 后的可视区来定位锚点。
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
  mode = 'aspectFill',
  className = '',
  onLoad,
}: ExampleImageProps) {
  const pinnedUrl = useRef('')
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

  return (
    <View
      className={`example-image ${className}`}
      style={url ? { backgroundImage: `url("${url}")` } : undefined}
    >
      <PinnedImage
        className={`example-image__img ${softExample ? 'example-soft' : ''}`}
        src={url}
        mode={mode}
        onLoad={onLoad}
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
  prev.className === next.className &&
  prev.onLoad === next.onLoad
))
