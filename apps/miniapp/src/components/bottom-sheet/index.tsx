// 底部弹层：进出场 spring 参数对齐总计划 §4.3（进 .32s / 出更慢更重）。
import { Text, View } from '@tarojs/components'
import './index.scss'

interface BottomSheetProps {
  open: boolean
  title?: string
  description?: string
  onClose: () => void
  children?: React.ReactNode
}

export default function BottomSheet({ open, title, description, onClose, children }: BottomSheetProps) {
  if (!open) return null
  return (
    <View className="bottom-sheet">
      <View className="bottom-sheet__mask" onClick={onClose} catchMove />
      <View className="bottom-sheet__panel" catchMove>
        <View className="bottom-sheet__grabber" onClick={onClose} />
        {title ? <Text className="bottom-sheet__title">{title}</Text> : null}
        {description ? <Text className="bottom-sheet__desc">{description}</Text> : null}
        <View className="bottom-sheet__body">{children}</View>
      </View>
    </View>
  )
}
