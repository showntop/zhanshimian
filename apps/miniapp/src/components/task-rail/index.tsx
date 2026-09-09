// 首页任务横轨：进行中任务横向卡（露出下一张提示可滑动）。
import { ScrollView, Text, View } from '@tarojs/components'
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
  return (
    <ScrollView className="task-rail" scrollX enhanced showScrollbar={false}>
      {items.map(({ task, title, open }) => {
        const failed = task.status === 'failed'
        return (
          <View key={task.id} className={`task-card ${failed ? 'task-card--failed' : ''} pressable`} onClick={open}>
            <View className="task-card__head">
              <Text className="task-card__type">{TYPE_LABEL[task.type] || '任务'}</Text>
              {failed ? (
                <Text className="task-card__status task-card__status--failed">未完成</Text>
              ) : (
                <View className="task-card__spinner spinner" />
              )}
            </View>
            <Text className="task-card__title">{title}</Text>
            {!failed ? (
              <View className="task-card__progress">
                <View
                  className="task-card__progress-fill"
                  style={{ transform: `scaleX(${Math.min(100, Math.max(0, task.progress ?? 0)) / 100})` }}
                />
              </View>
            ) : null}
            <Text className="task-card__stage">{task.stage || (failed ? '点击查看原因' : '进行中')}</Text>
          </View>
        )
      })}
    </ScrollView>
  )
}

interface TaskRailProps {
  items: TaskRailItem[]
}
