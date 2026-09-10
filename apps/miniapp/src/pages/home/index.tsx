// 首页工作台：新用户/回访双状态。
// 请求纪律：单次 /v1/home/bootstrap 聚合；仅当有活跃任务且页面可见时批量轮询(1.5s)。
// 重设计 IA：问候 → 任务轨 → 今日造型/档案 hero → 工具 → 场景 → 最近方案。
import { useCallback, useEffect, useRef, useState } from 'react'
import Taro, { useDidShow } from '@tarojs/taro'
import { ScrollView, Text, View } from '@tarojs/components'
import {
  HOME_COPY,
  HOME_TITLE,
  POLL_INTERVALS,
  SCENES,
  greetingForNow,
  trackEvent,
  type HomeBootstrap,
  type Task,
} from '@zsm/core'
import { api } from '../../services/api'
import { STORAGE_KEYS, readStorage, writeStorage } from '../../services/storage'
import AppHeader from '../../components/app-header'
import PrimaryButton from '../../components/primary-button'
import TaskRail, { type TaskRailItem } from '../../components/task-rail'
import ExampleImage from '../../components/example-image'
import ErrorState from '../../components/error-state'
import Skeleton from '../../components/skeleton'
import './index.scss'

const TOOLS = [
  { key: 'hair', label: '发型预览', desc: '先看效果再决定', path: '/packages/tools/pages/hair/index', badge: '推荐' },
  { key: 'outfit', label: '穿搭诊断', desc: '只指出最值得改的一处', path: '/packages/tools/pages/outfit/index', badge: '' },
  { key: 'purchase', label: '购买判断', desc: '买之前先看适不适合', path: '/packages/tools/pages/purchase/index', badge: '' },
] as const

function taskTitle(task: Task): string {
  switch (task.type) {
    case 'analysis':
      return '正在分析你的三张照片'
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

function openTask(task: Task) {
  switch (task.type) {
    case 'analysis':
      Taro.navigateTo({ url: '/pages/analysis/index' })
      break
    case 'hair_preview':
      Taro.navigateTo({ url: '/packages/tools/pages/hair/index' })
      break
    case 'plan_look':
      Taro.switchTab({ url: '/pages/plans/index' })
      break
    case 'today_look':
      Taro.navigateTo({ url: '/packages/life/pages/today/index' })
      break
  }
}

function lookBadge(lookProvider: string | undefined, generatedUrl: string | undefined): string {
  if ((lookProvider ?? '').startsWith('demo')) return '效果示例'
  if (generatedUrl) return 'AI 风格预览'
  return '风格参考'
}

export default function Home() {
  const [bootstrap, setBootstrap] = useState<HomeBootstrap | null>(null)
  const [loading, setLoading] = useState(true)
  const [failed, setFailed] = useState(false)
  const recoveredRef = useRef(false)
  const visibleRef = useRef(true)
  const pollTimer = useRef<ReturnType<typeof setTimeout> | null>(null)

  const load = useCallback(async () => {
    try {
      const data = await api.getHomeBootstrap()
      setBootstrap(data)
      setFailed(false)
      // 聚合返回的最新报告落本地，供方案/清单等页复用
      if (data.report?.id) writeStorage(STORAGE_KEYS.reportId, data.report.id)
    } catch {
      setFailed(true)
    } finally {
      setLoading(false)
    }
  }, [])

  // 缓存被清（换机/重装）时恢复报告引用
  const recoverReport = useCallback(async () => {
    if (readStorage(STORAGE_KEYS.reportId) || recoveredRef.current) return
    recoveredRef.current = true
    try {
      const report = await api.getCurrentReport()
      if (report?.id) writeStorage(STORAGE_KEYS.reportId, report.id)
    } catch {
      /* 无报告：保持新用户态 */
    }
  }, [])

  const scheduleTaskPoll = useCallback(() => {
    if (pollTimer.current) clearTimeout(pollTimer.current)
    const active = bootstrap?.active_tasks ?? []
    if (active.length === 0 || !visibleRef.current) return
    pollTimer.current = setTimeout(async () => {
      try {
        const tasks = await api.getTasks(active.map((t) => t.id))
        setBootstrap((prev) => (prev ? { ...prev, active_tasks: tasks } : prev))
        // 任一任务到达终态：整页聚合刷新一次
        if (tasks.some((t) => t.status === 'completed' || t.status === 'failed')) load()
      } catch {
        /* 轮询失败静默：下一轮重试 */
      }
      scheduleTaskPoll()
    }, POLL_INTERVALS.homeTasks)
  }, [bootstrap?.active_tasks, load])

  useEffect(() => {
    scheduleTaskPoll()
    return () => {
      if (pollTimer.current) clearTimeout(pollTimer.current)
    }
  }, [scheduleTaskPoll])

  useDidShow(() => {
    visibleRef.current = true
    trackEvent('page_view', { page: 'home' })
    recoverReport().then(load)
    scheduleTaskPoll()
  })

  Taro.useDidHide(() => {
    visibleRef.current = false
    if (pollTimer.current) clearTimeout(pollTimer.current)
  })

  const report = bootstrap?.report
  const hasReport = Boolean(report?.id)
  const recentPlan = bootstrap?.recent_plan
  const todayPlan = bootstrap?.today_plan
  const railItems: TaskRailItem[] = (bootstrap?.active_tasks ?? [])
    .filter((t) => t.status === 'queued' || t.status === 'processing' || t.status === 'failed')
    .map((task) => ({ task, title: taskTitle(task), open: () => openTask(task) }))

  return (
    <View className="page page--tab">
      <AppHeader transparent />
      {failed ? (
        <ErrorState onRetry={load} />
      ) : loading ? (
        <Skeleton rows={4} />
      ) : (
        <View className="home">
          <View className="home__greeting fade-up">
            <Text className="home__greeting-kicker">私人形象顾问</Text>
            <Text className="home__greeting-title display">
              {hasReport ? `${greetingForNow()}，${HOME_COPY.returningTitle}` : HOME_TITLE}
            </Text>
          </View>

          {railItems.length > 0 ? (
            <View className="home__section fade-up delay-1">
              <TaskRail items={railItems} />
            </View>
          ) : null}

          {!hasReport ? (
            <View className="fade-up delay-1">
              <View className="home__hero home__hero--onboard card--hero halo">
                <View className="home__hero-top">
                  <View>
                    <Text className="home__hero-eyebrow">{HOME_COPY.startArchive}</Text>
                    <Text className="home__hero-title">{HOME_COPY.archiveTitle}</Text>
                    <Text className="home__hero-desc">{HOME_COPY.archiveBody}</Text>
                  </View>
                  <View className="home__hero-preview">
                    <View className="home__preview-slice home__preview-slice--1" />
                    <View className="home__preview-slice home__preview-slice--2" />
                    <View className="home__preview-slice home__preview-slice--3" />
                  </View>
                </View>
                <View className="home__steps">
                  {HOME_COPY.process.map((item, index) => (
                    <View key={item.title} className="home__step">
                      <Text className="home__step-index">{index + 1}</Text>
                      <View className="home__step-copy">
                        <Text className="home__step-title">{item.title}</Text>
                        <Text className="home__step-desc">{item.desc}</Text>
                      </View>
                    </View>
                  ))}
                </View>
                <PrimaryButton
                  text={HOME_COPY.startAnalysis}
                  tone="onDark"
                  onClick={() => Taro.navigateTo({ url: '/pages/capture/index' })}
                />
                <Text className="home__hero-note">{HOME_COPY.photoPrivacy}</Text>
              </View>
            </View>
          ) : todayPlan ? (
            <View className="fade-up delay-1">
              <View
                className="home__hero home__hero--today card--hero halo pressable"
                onClick={() => Taro.navigateTo({ url: '/packages/life/pages/today/index' })}
              >
                <View className="home__hero-copy">
                  <Text className="home__hero-eyebrow">今日造型</Text>
                  <Text className="home__hero-title">{todayPlan.title}</Text>
                  <Text className="home__hero-desc">{todayPlan.summary}</Text>
                  <Text className="home__hero-link">查看今天怎么穿 ›</Text>
                </View>
                <ExampleImage
                  className="home__hero-img"
                  src={todayPlan.generated_image_url || todayPlan.image_url}
                  badgeText={lookBadge(todayPlan.look_provider, todayPlan.generated_image_url)}
                />
              </View>
            </View>
          ) : (
            <View className="fade-up delay-1">
              <View
                className="home__hero home__hero--report card--hero halo pressable"
                onClick={() => Taro.navigateTo({ url: '/pages/report/index' })}
              >
                <View className="home__hero-copy">
                  <Text className="home__hero-eyebrow">{HOME_COPY.reportReady}</Text>
                  <Text className="home__hero-title">{report?.priority_title}</Text>
                  <Text className="home__hero-desc">{report?.priority_copy}</Text>
                  <View className="home__hero-tags">
                    {(report?.impression_tags ?? []).slice(0, 3).map((tag) => (
                      <Text key={tag} className="home__hero-tag">
                        {tag}
                      </Text>
                    ))}
                  </View>
                  <Text className="home__hero-link">{HOME_COPY.viewReport}</Text>
                </View>
                <View className="home__report-mark">
                  <Text className="home__report-mark-num">{(report?.findings ?? []).length}</Text>
                  <Text className="home__report-mark-label">可提升点</Text>
                </View>
              </View>
            </View>
          )}

          <View className="home__section fade-up delay-2">
            <View className="home__section-head">
              <Text className="section-title">{HOME_COPY.toolsTitle}</Text>
              <View className="section-rule" />
            </View>
            <View className="home__tools">
              {TOOLS.map((tool) => (
                <View
                  key={tool.key}
                  className="home__tool pressable"
                  onClick={() => Taro.navigateTo({ url: tool.path })}
                >
                  {tool.badge ? <Text className="home__tool-badge">{tool.badge}</Text> : null}
                  <Text className="home__tool-name">{tool.label}</Text>
                  <Text className="home__tool-desc">{tool.desc}</Text>
                </View>
              ))}
            </View>
          </View>

          <View className="home__section fade-up delay-3">
            <View className="home__section-head">
              <Text className="section-title">{HOME_COPY.scenesTitle}</Text>
              <View className="section-rule" />
            </View>
            <ScrollView className="home__scenes" scrollX enhanced showScrollbar={false}>
              <View className="home__scene-row">
                {SCENES.map((scene) => (
                  <View
                    key={scene.id}
                    className="home__scene pressable"
                    onClick={() => Taro.navigateTo({ url: `/pages/scene/index?scene=${scene.id}` })}
                  >
                    <Text className="home__scene-label">{scene.label}</Text>
                    <Text className="home__scene-desc">{scene.note}</Text>
                  </View>
                ))}
              </View>
            </ScrollView>
            {hasReport ? <Text className="home__scene-note">{HOME_COPY.sceneReadyNote}</Text> : null}
          </View>

          {recentPlan ? (
            <View className="home__section fade-up delay-3">
              <View className="home__section-head">
                <Text className="section-title">{HOME_COPY.recentTitle}</Text>
                <View className="section-rule" />
              </View>
              <View
                className="home__recent card pressable"
                onClick={() => Taro.navigateTo({ url: `/pages/plan/index?id=${recentPlan.id}` })}
              >
                <ExampleImage
                  className="home__recent-img"
                  src={recentPlan.generated_image_url || recentPlan.image_url}
                  badgeText={lookBadge(recentPlan.look_provider, recentPlan.generated_image_url)}
                />
                <View className="home__recent-copy">
                  <Text className="home__recent-name">{recentPlan.name}</Text>
                  <Text className="home__recent-why">{recentPlan.why}</Text>
                </View>
                <Text className="home__recent-arrow">›</Text>
              </View>
            </View>
          ) : null}
        </View>
      )}
    </View>
  )
}
