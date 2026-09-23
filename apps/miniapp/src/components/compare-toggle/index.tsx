// 原本/方案 玻璃拟态分段切换（报告与方案页共用）。
import { Text, View } from '@tarojs/components'
import './index.scss'

interface CompareToggleProps {
  value: 'current' | 'plan'
  onChange: (next: 'current' | 'plan') => void
  currentLabel?: string
  planLabel?: string
}

export default function CompareToggle({
  value,
  onChange,
  currentLabel = '原本',
  planLabel = '方案',
}: CompareToggleProps) {
  return (
    <View className="compare-toggle">
      <View
        className={`compare-toggle__seg ${value === 'current' ? 'compare-toggle__seg--active' : ''}`}
        onClick={() => onChange('current')}
      >
        <Text>{currentLabel}</Text>
      </View>
      <View
        className={`compare-toggle__seg ${value === 'plan' ? 'compare-toggle__seg--active' : ''}`}
        onClick={() => onChange('plan')}
      >
        <Text>{planLabel}</Text>
      </View>
    </View>
  )
}
