// 任务展示与跳转共享工具：首页（入口状态 / Tab badge）与「我的」（任务中心）复用。
import Taro from '@tarojs/taro'
import type { Task } from '@zsm/core'

export function isTaskActive(task: Task): boolean {
  return task.status === 'queued' || task.status === 'processing'
}

export function taskTitle(task: Task): string {
  switch (task.type) {
    case 'analysis':
      return '正在分析你的三张照片'
    case 'plan_group':
      return '正在生成你的三套方案'
    case 'hair_preview':
      return '正在生成发型预览'
    case 'plan_look':
      return '正在生成方案形象图'
    case 'today_look':
      return '正在生成今日搭配图'
    default:
      return '任务进行中'
  }
}

/** 任务完成的轻提醒文案（轮询到终态时 toast） */
export function taskDoneTitle(task: Task): string {
  switch (task.type) {
    case 'analysis':
      return '形象分析完成，去看看报告'
    case 'plan_group':
      return '三套方案已生成，去看看'
    case 'hair_preview':
      return '发型预览已生成'
    case 'plan_look':
      return '方案形象图已生成'
    case 'today_look':
      return '今日搭配图已生成'
    default:
      return '任务已完成'
  }
}

export function openTask(task: Task) {
  switch (task.type) {
    case 'analysis':
      Taro.navigateTo({ url: '/pages/analysis/index' })
      break
    case 'plan_group':
    case 'plan_look':
      Taro.switchTab({ url: '/pages/plans/index' })
      break
    case 'hair_preview':
      Taro.navigateTo({ url: '/packages/tools/pages/hair/index' })
      break
    case 'today_look':
      Taro.navigateTo({ url: '/packages/life/pages/today/index' })
      break
  }
}
