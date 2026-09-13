// 首页：一切入口都来自服务端的 HomeBootstrap——报告、方案集、进行中的操作。
// 与旧首页的差别：不再轮询任务接口、不再写业务存储，
// 「当前任务」的记忆归分析页自己（路由 + 资源缓存）。
import { useCallback, useEffect, useRef, useState } from 'react'
import Taro, { useDidShow } from '@tarojs/taro'
import { Text, View } from '@tarojs/components'
import {
  APP_NAME,
  ASSESSMENT_COPY,
  HOME_COPY,
  HOME_TITLE,
  greetingForNow,
} from '@zsm/core'
import type { HomeBootstrap } from '@zsm/core'
import { qualityApi } from '../../app/api/quality'
import { resourceCache, resourceKey } from '../../app/cache/resource-cache'
import { useOperationPolling } from '../../app/operations/use-operation-polling'
import { usePageShell } from '../../hooks/use-page-visibility'
import AppHeader from '../../components/app-header'
import PrimaryButton from '../../components/primary-button'
import Skeleton from '../../components/skeleton'
import ErrorState from '../../components/error-state'
import CurrentReportEntry from '../../features/report/CurrentReportEntry'
import RecentPlanSetEntry from '../../features/planning/RecentPlanSetEntry'
import './index.scss'

const HOME_CACHE_KEY = resourceKey('home', 'current')
const IN_FLIGHT = new Set(['accepted', 'running', 'retrying'])
const TOOLS = [
  { label: '发型预览', url: '/packages/tools/pages/hair/index' },
  { label: '穿搭诊断', url: '/packages/tools/pages/outfit/index' },
  { label: '购买判断', url: '/packages/tools/pages/purchase/index' },
  { label: '顾问', url: '/packages/life/pages/advisor/index' },
  { label: '衣橱', url: '/packages/life/pages/wardrobe/index' },
]

function isActive(operation: HomeBootstrap['active_operations'][number]): boolean {
  return IN_FLIGHT.has(operation.status)
}

export default function Home() {
  const { pageClass, enter } = usePageShell(false, 'page--tab', 'home')
  const [boot, setBoot] = useState<HomeBootstrap | null>(
    () => resourceCache.read<HomeBootstrap>(HOME_CACHE_KEY) ?? null,
  )
  const [loading, setLoading] = useState(!boot)
  const [failed, setFailed] = useState(false)
  const firstShow = useRef(true)

  /** 拉取并整体替换。revalidate 合并发；到达前界面继续显示旧值。 */
  const load = useCallback(async (background: boolean) => {
    if (!background) setLoading(true)
    setFailed(false)
    try {
      const next = await resourceCache.revalidate(HOME_CACHE_KEY, () => qualityApi.getHomeBootstrap())
      setBoot(next)
    } catch {
      setFailed(true)
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void load(false)
  }, [load])

  // 首次 onShow 跳过（挂载时已拉）；此后每次回 tab 静默对账
  useDidShow(() => {
    if (firstShow.current) {
      firstShow.current = false
      return
    }
    void load(true)
  })

  // 有进行中的操作就盯着；全部到终态后只刷新 home
  const activeIds = (boot?.active_operations ?? []).filter(isActive).map((operation) => operation.id)
  useOperationPolling({
    operationIds: activeIds,
    enabled: activeIds.length > 0,
    onSettled: () => void load(true),
  })

  const report = boot?.report ?? null
  const planSet = boot?.plan_set ?? null
  const analyzing = (boot?.active_operations ?? []).find(
    (operation) => operation.kind === 'assessment' && isActive(operation),
  )
  const planning = (boot?.active_operations ?? []).find(
    (operation) => operation.kind === 'plan_set' && isActive(operation),
  )

  return (
    <View className={pageClass}>
      <AppHeader />
      <View className="home">
        <View className={`home__hero ${enter()}`}>
          <Text className="home__greeting">{`${greetingForNow()}，我是${APP_NAME}`}</Text>
          <Text className="home__title">{HOME_TITLE}</Text>
        </View>

        {loading && !boot ? (
          <Skeleton rows={4} />
        ) : failed && !boot ? (
          <ErrorState onRetry={() => void load(false)} />
        ) : (
          <View className={`home__main ${enter(1)}`}>
            {/* 状态即入口：按服务端事实给唯一的主行动 */}
            {analyzing ? (
              <View
                className="home__state-card pressable"
                onClick={() =>
                  void Taro.navigateTo({
                    url:
                      `/pages/analysis/index?operation_id=${encodeURIComponent(analyzing.id)}` +
                      `&assessment_id=`,
                  })
                }
              >
                <View className="spinner home__spin" />
                <View className="home__state-body">
                  <Text className="home__state-title">{HOME_COPY.startAnalysis}</Text>
                  <Text className="home__state-desc">{ASSESSMENT_COPY.stageFallback}</Text>
                </View>
                <Text className="home__state-link">{HOME_COPY.viewProgress}</Text>
              </View>
            ) : report ? (
              <CurrentReportEntry report={report} />
            ) : (
              <View className="home__state-card">
                <View className="home__state-body">
                  <Text className="home__state-title">{HOME_COPY.archiveTitle}</Text>
                  <Text className="home__state-desc">{HOME_COPY.archiveBody}</Text>
                </View>
                <PrimaryButton
                  text={HOME_COPY.startArchive}
                  onClick={() => void Taro.navigateTo({ url: '/pages/capture/index' })}
                />
                <Text className="home__privacy">{HOME_COPY.photoPrivacy}</Text>
              </View>
            )}

            {planSet ? <RecentPlanSetEntry planSet={planSet} /> : null}

            {planning ? (
              <Text
                className="home__planning pressable"
                onClick={() => void Taro.switchTab({ url: '/pages/plans/index' })}
              >
                {HOME_COPY.planningLink}
              </Text>
            ) : null}

            {/* 次数条：只读展示；购买与充值入口归「我的」 */}
            {boot?.billing ? (
              <View className="home__billing">
                <Text className="home__billing-item">
                  {`${HOME_COPY.dailyRemaining} · ${HOME_COPY.analysisLabel} ${boot.billing.daily_remaining.analysis}`}
                </Text>
                <Text className="home__billing-item">
                  {`${HOME_COPY.looksLabel} ${boot.billing.daily_remaining.looks}`}
                </Text>
              </View>
            ) : null}
          </View>
        )}

        <View className={`home__tools ${enter(2)}`}>
          <Text className="home__tools-title">{HOME_COPY.toolsTitle}</Text>
          <View className="home__tools-row">
            {TOOLS.map((tool) => (
              <Text
                key={tool.url}
                className="home__tool pressable"
                onClick={() => void Taro.navigateTo({ url: tool.url })}
              >
                {tool.label}
              </Text>
            ))}
          </View>
        </View>
      </View>
    </View>
  )
}
