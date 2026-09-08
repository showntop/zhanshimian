import { Text, View } from '@tarojs/components'
import './index.scss'

interface PrimaryButtonProps {
  text?: string
  loading?: boolean
  disabled?: boolean
  onClick?: () => void
}

export default function PrimaryButton({ text = '继续', loading, disabled, onClick }: PrimaryButtonProps) {
  const blocked = loading || disabled
  return (
    <View
      className={`primary-button ${blocked ? 'primary-button--disabled' : ''}`}
      onClick={() => {
        if (!blocked) onClick?.()
      }}
    >
      {loading ? <Text>请稍候…</Text> : <Text>{text}</Text>}
    </View>
  )
}
