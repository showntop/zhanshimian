import { Text, View } from '@tarojs/components'

interface SectionHeaderProps {
  title: string
  className?: string
}

export default function SectionHeader({ title, className = '' }: SectionHeaderProps) {
  return (
    <View className={`section-head ${className}`.trim()}>
      <Text className="section-title">{title}</Text>
      <View className="section-rule" />
    </View>
  )
}
