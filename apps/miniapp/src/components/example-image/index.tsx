// 数据真实性契约的 UI 默认实现（AGENTS.md 红线 3）：
// - src 为服务端下发 URL：lookImage 严格校验，无效渲染空态（绝不回退内置图）；
//   isBundledAsset 命中（/assets/looks|plans|...）时叠「风格参考」角标；
// - 显式传 slug：exampleImage 渲染内置模特图，必叠角标 + .example-soft 弱化；
// - user 模式：用户本人照片，无效保持可见的空。
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
  mode?: 'aspectFill' | 'aspectFit' | 'widthFix'
  className?: string
}

export default function ExampleImage({
  src,
  slug,
  variant = 'full',
  user,
  badgeText,
  mode = 'aspectFill',
  className = '',
}: ExampleImageProps) {
  let url = ''
  let isExample = false

  if (slug) {
    url = exampleImage(slug, variant)
    isExample = true
  } else if (user) {
    url = userImage(src)
  } else {
    url = lookImage(src)
    isExample = isBundledAsset(src)
  }

  if (!url) {
    return (
      <View className={`example-image example-image--empty ${className}`}>
        <Text className="example-image__empty-text">{user ? '照片暂不可用' : '图片暂不可用'}</Text>
      </View>
    )
  }

  return (
    <View className={`example-image ${className}`}>
      <Image className={`example-image__img ${isExample ? 'example-soft' : ''}`} src={url} mode={mode} />
      {isExample ? (
        <View className="example-badge">
          <Text>{badgeText || '风格参考'}</Text>
        </View>
      ) : null}
    </View>
  )
}
