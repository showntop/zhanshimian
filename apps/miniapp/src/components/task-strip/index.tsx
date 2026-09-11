// 首页任务轨（IA：问候 → 任务轨 → hero）：进行中任务的统一可见入口。
// 数据来自 /v1/home/bootstrap 的 active_tasks（首页已按 POLL_INTERVALS.homeTasks 轮询），
// 无进行中任务不占位；点击跳到任务所属页面（openTask 单源）。「我的」页任务中心共用分组逻辑。
import { Text, View } from '@tarojs/components'
import type { Task } from '@zsm/core'
import { groupTasksByType, isTaskActive, openTask, taskGroupMeta, taskTitle } from '../../services/task-utils'
import './index.scss'

interface TaskStripProps {
  tasks: Task[]
}

export default function TaskStrip({ tasks }: TaskStripProps) {
  const groups = groupTasksByType(tasks.filter(isTaskActive))
  if (groups.length === 0) return null
  return (
    <View className="task-strip fade-up">
      {groups.map((group) => {
        const first = group[0]
        if (!first) return null
        const progress = Math.min(100, Math.max(0, first.progress ?? 0))
        return (
          <View
            key={first.type}
            className="task-strip__row pressable"
            onClick={() => openTask(first)}
          >
            <View className="task-strip__spin spinner" />
            <View className="task-strip__copy">
              <Text className="task-strip__title">{taskTitle(first)}</Text>
              <View className="task-strip__bar">
                <View className="task-strip__bar-fill" style={{ width: `${progress}%` }} />
              </View>
            </View>
            <Text className="task-strip__meta">{taskGroupMeta(group)}</Text>
          </View>
        )
      })}
    </View>
  )
}
