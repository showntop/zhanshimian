// 任务展示与跳转共享工具：首页（入口状态 / Tab badge）与「我的」任务中心复用。
import Taro from '@tarojs/taro'
import type { Task } from '@zsm/core'
import { api } from './api'
import { STORAGE_KEYS, readStorage, writeStorage } from './storage'

function isRunningStatus(status?: string): boolean {
  return status === 'queued' || status === 'processing'
}

export function isAnalysisTaskRunning(tasks: readonly Pick<Task, 'type' | 'status'>[]): boolean {
  return tasks.some((task) => task.type === 'analysis' && isRunningStatus(task.status))
}

/** 找回进行中的分析 id，并写回本地。没有进行中的分析返回空。 */
export async function resolveRunningAnalysisId(): Promise<string> {
  const persist = (id: string) => {
    if (id) writeStorage(STORAGE_KEYS.activeTaskAnalysis, id)
    return id
  }

  try {
    const current = await api.getCurrentAnalysis()
    if (current && isRunningStatus(current.status)) return persist(current.id)
  } catch {
    /* 旧服务端无此端点或网络失败，走 bootstrap / 本地 */
  }

  try {
    const boot = await api.getHomeBootstrap()
    if (!isAnalysisTaskRunning(boot.active_tasks ?? [])) return ''
  } catch {
    /* bootstrap 失败时看本地 */
  }

  const local = readStorage(STORAGE_KEYS.activeTaskAnalysis)
  if (!local) return ''
  try {
    const item = await api.getAnalysis(local)
    return isRunningStatus(item.status) ? persist(item.id) : ''
  } catch {
    return ''
  }
}

/** 服务端 queued/processing 才算进行中；失败/完成允许重新建档。 */
export async function isAnalysisRunning(): Promise<boolean> {
  if (await resolveRunningAnalysisId()) return true
  try {
    const boot = await api.getHomeBootstrap()
    return isAnalysisTaskRunning(boot.active_tasks ?? [])
  } catch {
    return false
  }
}

export function analysisPageUrl(id = readStorage(STORAGE_KEYS.activeTaskAnalysis)): string {
  return id ? `/pages/analysis/index?id=${id}` : '/pages/analysis/index'
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
    case 'body_orbit':
      return '正在生成 3D 形象'
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
    case 'body_orbit':
      return '3D 形象已生成'
    default:
      return '任务已完成'
  }
}

export function openTask(task: Task) {
  switch (task.type) {
    case 'analysis':
      Taro.navigateTo({ url: analysisPageUrl() })
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
    case 'body_orbit':
      Taro.navigateTo({ url: '/packages/tools/pages/lab/index' })
      break
  }
}

/** 任务中心共用：按类型聚合同类任务（一次方案生成 = 3 个 plan_look），保持先来先排 */
export function groupTasksByType(tasks: Task[]): Task[][] {
  const groups: Task[][] = []
  for (const task of tasks) {
    const group = groups.find((g) => g[0]?.type === task.type)
    if (group) group.push(task)
    else groups.push([task])
  }
  return groups
}
