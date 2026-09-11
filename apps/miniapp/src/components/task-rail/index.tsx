// 首页任务状态条：进行中任务是瞬时状态，聚合为一条轻量胶囊横幅，
// 不与 hero 主卡争夺视觉重量；完成即消失。同类型任务合并计数
// （一次方案生成 = 3 个 plan_look 任务，平铺三张卡会被误读为 bug）。
import { Text, View } from '@tarojs/components'
import type { Task } from '@zsm/core'
import './index.scss'

export type TaskRailItem = {
  task: Pick<Task, 'id' | 'type' | 'status' | 'progress' | 'stage'>
  title: string
  open: () => void
}

const TYPE_LABEL: Record<string, string> = {
  analysis: '形象分析',
  hair_preview: '发型预览',
  plan_look: '方案形象图',
  today_look: '今日搭配图',
}

export default function TaskRail({ items }: TaskRailProps) {
  if (items.length === 0) return null
  const first = items[0]
  if (!first) return null

  // 按类型聚合同类任务
  const groups: TaskRailItem[][] = []
  for (const item of items) {
    const group = groups.find((g) => g[0]?.task.type === item.task.type)
    if (group) group.push(item)
    else groups.push([item])
  }
  const failedCount = items.filter((i) => i.task.status === 'failed').length
  const activeCount = items.length - failedCount
  const failed = activeCount === 0 && failedCount > 0

  let label: string
  if (groups.length === 1) {
    const typeLabel = TYPE_LABEL[first.task.type] || '任务'
    if (failed) {
      label = `${typeLabel} · ${failedCount} 个未完成`
    } else if (items.length === 1) {
      label = `${typeLabel} · 生成中`
    } else {
      label = `${typeLabel} · ${activeCount} 个生成中`
    }
  } else {
    label = failed ? `${failedCount} 个任务未完成` : `${activeCount} 个任务进行中`
  }

  return (
    <View className={`task-strip fade-up ${failed ? 'task-strip--failed' : ''} pressable`} onClick={first.open}>
      {failed ? (
        <View className="task-strip__dot" />
      ) : (
        <View className="task-strip__spinner spinner" />
      )}
      <Text className="task-strip__label">{label}</Text>
      <Text className="task-strip__arrow">›</Text>
    </View>
  )
}

interface TaskRailProps {
  items: TaskRailItem[]
}
