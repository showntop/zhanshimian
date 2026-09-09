import { Text, View } from '@tarojs/components'
import './index.scss'

interface PrimaryButtonProps {
  text?: string
  loading?: boolean
  disabled?: boolean
  /** solid：常规（moss 底白字）；onDark：hero 深绿卡内（cream 底 moss 字） */
  tone?: 'solid' | 'onDark'
  onClick?: () => void
}

export default function PrimaryButton({
  text = '继续',
  loading,
  disabled,
  tone = 'solid',
  onClick
}: PrimaryButtonProps) {
  const blocked = loading || disabled
  return (
    <View
      className={[
        'primary-button',
        `primary-button--${tone}`,
        blocked ? 'primary-button--disabled' : ''
      ].join(' ')}
      onClick={() => {
        if (!blocked) onClick?.()
      }}
    >
      {loading ? (
        <>
          <View className={`spinner ${tone === 'onDark' ? 'spinner--on-deep' : ''} primary-button__spinner`} />
          <Text>请稍候…</Text>
        </>
      ) : (
        <Text>{text}</Text>
      )}
    </View>
  )
}
