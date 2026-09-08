// 大图光箱：遮罩淡入 + 图片轻放大进场。
import { Image, Text, View } from '@tarojs/components'
import './index.scss'

interface ImageViewerProps {
  open: boolean
  url: string
  caption?: string
  onClose: () => void
}

export default function ImageViewer({ open, url, caption, onClose }: ImageViewerProps) {
  if (!open || !url) return null
  return (
    <View className="image-viewer" onClick={onClose} catchMove>
      <Image className="image-viewer__img" src={url} mode="aspectFit" />
      {caption ? <Text className="image-viewer__caption">{caption}</Text> : null}
      <Text className="image-viewer__close">轻触关闭</Text>
    </View>
  )
}
