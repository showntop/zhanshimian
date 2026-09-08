import { Text, View } from '@tarojs/components'
import PrimaryButton from '../primary-button'
import './index.scss'

interface EmptyStateProps {
  title: string
  description?: string
  actionText?: string
  onAction?: () => void
}

export default function EmptyState({ title, description, actionText, onAction }: EmptyStateProps) {
  return (
    <View className="empty-state fade-up">
      <Text className="empty-state__title">{title}</Text>
      {description ? <Text className="empty-state__desc">{description}</Text> : null}
      {actionText && onAction ? (
        <View className="empty-state__action">
          <PrimaryButton text={actionText} onClick={onAction} />
        </View>
      ) : null}
    </View>
  )
}
