import { View } from '@tarojs/components'
import './index.scss'

interface SkeletonProps {
  rows?: number
  className?: string
}

/** 骨架屏：加载态首选（shimmer 高光扫过，见 @zsm/design base.scss）。 */
export default function Skeleton({ rows = 3, className = '' }: SkeletonProps) {
  return (
    <View className={`skeleton-block ${className}`}>
      {Array.from({ length: rows }).map((_, i) => (
        <View key={i} className="skeleton skeleton-row" style={{ width: `${100 - i * 12}%` }} />
      ))}
    </View>
  )
}
