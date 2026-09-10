// 错误态：与内容不同屏；必须给下一步动作，文案不甩锅（AGENTS.md 红线 4）。
import { Text, View } from '@tarojs/components'
import PrimaryButton from '../primary-button'
import './index.scss'

interface ErrorStateProps {
  title?: string
  message?: string
  retryText?: string
  onRetry?: () => void
}

export default function ErrorState({ title, message, retryText = '重试', onRetry }: ErrorStateProps) {
  return (
    <View className="error-state fade-up">
      <View className="error-state__dot" />
      <Text className="error-state__title">{title || '加载没有成功'}</Text>
      <Text className="error-state__desc">{message || '网络似乎不太稳定，稍后再试一次。'}</Text>
      {onRetry ? (
        <View className="error-state__action">
          <PrimaryButton text={retryText} onClick={onRetry} />
        </View>
      ) : null}
    </View>
  )
}
