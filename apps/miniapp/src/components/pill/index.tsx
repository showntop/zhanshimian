import { Text, View } from '@tarojs/components'
import './index.scss'

interface PillProps {
  label: string
  active?: boolean
  onClick?: () => void
}

export default function Pill({ label, active, onClick }: PillProps) {
  return (
    <View className={`pill ${active ? 'pill--active' : ''} pressable`} onClick={() => onClick?.()}>
      <Text>{label}</Text>
    </View>
  )
}
