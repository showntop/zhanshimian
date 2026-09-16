// 图片来源的 UI 实现：服务端只下发带类型的 DisplayMedia，这里把它投影成
// 可渲染的图 + 弱化；投影不出来就渲染可见空态，绝不回退内置图。
//
// 角标（2026-09-16 owner 决策）：生成图/示例图/Demo 的「风格参考」「效果示例」
// 不再上屏——AI 生成内容无显式标识的法规风险由 owner 知悉并承担；
// 来源真实性不依赖角标文字，依赖 source_kind 强类型、埋点与 example-soft 弱化。
// 用户本人照片保留「原本」角标（对比语义，非 AI 标识）。
//
// 两种模式互斥（类型层面就互斥，不能同时传）：
// - `media`：服务端 DisplayMedia，走投影；
// - `reference`：包内静态参考位，走 exampleImage。
//
// 人像裁切：微信 aspectFill 只能居中裁，全身照放进矮框会切头。
// anchor="top" 改为 cover + 顶对齐（先按宽铺满裁底；图比相框更扁时改按高铺满裁侧）。
import { memo, useState } from 'react'
import { Image, Text, View } from '@tarojs/components'
import {
  IMAGE_BADGE_COPY,
  SOURCE_IMAGE_COPY,
  exampleImage,
  projectDisplayMedia,
  type DisplayMedia,
  type LookSlug,
  type LookVariant,
} from '@zsm/core'
import './index.scss'

interface SourceImageBaseProps {
  mode?: 'aspectFill' | 'aspectFit' | 'widthFix' | 'heightFix'
  /** 人像短框：顶对齐裁切，避免居中切头。商品图/模糊衬底保持默认 center。 */
  anchor?: 'center' | 'top'
  /** 相框宽/高。仅 anchor=top 且未指定 mode 时，用来在裁底 / 裁侧之间选择。 */
  frameAspect?: number
  className?: string
  /** 透传 Image onLoad：报告页用它拿照片真实宽高，换算可视区来定位锚点。
   *  注意微信平台宽高可能是 string，消费侧自行 Number() 归一。 */
  onLoad?: (event: { detail: { width: number | string; height: number | string } }) => void
  /** 空态下的动作（去拍照 / 重试）。不给就只显示空态文字。 */
  emptyActionText?: string
  onEmptyAction?: () => void
}

export type SourceImageProps = SourceImageBaseProps &
  (
    | { media: DisplayMedia | null | undefined; reference?: never }
    | { reference: { slug: LookSlug | string; variant: LookVariant }; media?: never }
  )

/** 投影结果：可渲染的图，或一个带角标的静态参考位 */
interface ResolvedImage {
  key: string
  src: string
  badge: string
  soften: boolean
  isUserPhoto: boolean
}

function resolve(props: SourceImageProps): ResolvedImage | null {
  if ('reference' in props && props.reference) {
    const { slug, variant } = props.reference
    return {
      key: `reference:${slug}:${variant}`,
      src: exampleImage(slug, variant),
      // 内置参考位一律「风格参考」并弱化，不允许调用方改成别的说法
      badge: IMAGE_BADGE_COPY.bundled,
      soften: true,
      isUserPhoto: false,
    }
  }
  const image = projectDisplayMedia(props.media)
  if (!image) return null
  return {
    key: image.key,
    src: image.src,
    badge: image.badge,
    soften: image.soften,
    isUserPhoto: image.sourceKind === 'user_original',
  }
}

function SourceImage(props: SourceImageProps) {
  const { mode, anchor = 'center', frameAspect, className = '', onLoad } = props
  const [fill, setFill] = useState<'width' | 'height'>('width')
  const image = resolve(props)

  if (!image) {
    // 用户本人照片和方案图分开说：用户照片拍糊了要让他重拍，方案图拿不到要让他重试
    const isUserPhoto = 'media' in props && props.media?.source_kind === 'user_original'
    return (
      <View className={`source-image source-image--empty ${className}`}>
        <Text className="source-image__empty-text">
          {isUserPhoto ? SOURCE_IMAGE_COPY.userPhotoEmpty : SOURCE_IMAGE_COPY.mediaEmpty}
        </Text>
        {props.emptyActionText && props.onEmptyAction ? (
          <Text className="source-image__empty-action" onClick={props.onEmptyAction}>
            {props.emptyActionText}
          </Text>
        ) : null}
      </View>
    )
  }

  const resolvedMode = mode ?? (
    anchor === 'top'
      ? (fill === 'height' ? 'heightFix' : 'widthFix')
      : 'aspectFill'
  )
  const fillAxis = resolvedMode === 'heightFix' ? 'height' : 'width'
  const anchorClass = anchor === 'top'
    ? `source-image--anchor-top source-image--fill-${fillAxis}`
    : ''

  // widthFix / aspectFit 的可见图与 cover 衬底裁法不同，叠在一起会腿部错位。
  // 衬底只留给顶对齐自动铺满（短框裁底），显式 mode 不再铺第二层图。
  const useBackdrop = anchor === 'top' && !mode

  const handleLoad: SourceImageBaseProps['onLoad'] = (event) => {
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
      className={`source-image ${anchorClass} ${className}`}
      style={useBackdrop ? { backgroundImage: `url("${image.src}")` } : undefined}
    >
      <Image
        // key 跟着投影走：换图就重挂，不做"旧 src 还在就复用"的钉图
        key={image.key}
        className={`source-image__img ${image.soften ? 'example-soft' : ''}`}
        src={image.src}
        mode={resolvedMode}
        lazyLoad={false}
        fadeIn={false}
        onLoad={handleLoad}
      />
      {image.badge && image.isUserPhoto ? (
        <View className="example-badge">
          <Text>{image.badge}</Text>
        </View>
      ) : null}
    </View>
  )
}

export default memo(SourceImage)
