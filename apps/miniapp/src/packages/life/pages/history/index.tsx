// 历史建议：系统推送过的每日内容，按 gen_date 倒序回看。
//
// 数据源：GET /v1/daily/history（读 daily_content 本行，行即快照）。
// 与「我的手册」不同源——手册是用户主动收下的，这里是推送过的全部。
//
// 放 life 分包：低频回看页，不挤主包（主包体积已贴近 1.6MB 红线）。
import { useCallback, useEffect, useState } from 'react'
import Taro from '@tarojs/taro'
import { Text, View } from '@tarojs/components'
import {
  DAILY_HISTORY_COPY,
  dailyBucketName,
  type CollectionCategory,
  type ContentVisual,
  type DailyContentDTO,
} from '@zsm/core'
import { peripherals } from '../../../../app/api/peripherals'
import { usePageShell } from '../../../../hooks/use-page-visibility'
import AppHeader from '../../../../components/app-header'
import DailyVisual from '../../../../components/daily-visual'
import EmptyState from '../../../../components/empty-state'
import ErrorState from '../../../../components/error-state'
import Skeleton from '../../../../components/skeleton'
import './index.scss'

interface HistoryItem {
  key: string
  date: string
  category: CollectionCategory
  topic: string
  lead: string
  fitText: string
  visual: ContentVisual
}

/** 契约日期（2026-09-25）→ 界面编号（09.25），与首页海报角落同一语言 */
function dateLabel(genDate: string): string {
  return genDate.slice(5).replace('-', '.')
}

function toItem(row: DailyContentDTO): HistoryItem | null {
  if (!row.visual) return null
  return {
    key: row.id,
    date: dateLabel(row.gen_date),
    category: row.asset as CollectionCategory,
    topic: row.topic,
    lead: row.lead,
    fitText: row.fit_text,
    visual: row.visual as unknown as ContentVisual,
  }
}

export default function History() {
  const [items, setItems] = useState<HistoryItem[]>([])
  const [loading, setLoading] = useState(true)
  const [failed, setFailed] = useState(false)
  const { pageClass, enter } = usePageShell(!loading, '', 'history')

  const load = useCallback(async () => {
    setLoading(true)
    try {
      // 100 是契约上限；数据量天然受「每天一条」限制，一页足够
      const rows = await peripherals.listDailyHistory(100)
      setItems(rows.map(toItem).filter((item): item is HistoryItem => item !== null))
      setFailed(false)
    } catch {
      setFailed(true)
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void load()
  }, [load])

  const goToday = () => {
    void Taro.navigateTo({ url: '/pages/today/index' })
  }

  return (
    <View className={pageClass}>
      <AppHeader title={DAILY_HISTORY_COPY.title} back />
      <View className="dh">
        <View className={`dh__head ${enter()}`}>
          <Text className="dh__title">{DAILY_HISTORY_COPY.title}</Text>
          {!loading && items.length > 0 ? (
            <Text className="dh__count">
              {DAILY_HISTORY_COPY.totalPrefix}
              {items.length}
              {DAILY_HISTORY_COPY.totalSuffix}
            </Text>
          ) : null}
        </View>

        {loading ? (
          <Skeleton rows={5} />
        ) : failed ? (
          // 失败必须单独说：落到空态会被读成「你一条都没收到过」
          <ErrorState
            title={DAILY_HISTORY_COPY.loadFailed}
            onRetry={() => void load()}
          />
        ) : items.length === 0 ? (
          <EmptyState
            title={DAILY_HISTORY_COPY.emptyTitle}
            description={DAILY_HISTORY_COPY.emptyBody}
            actionText={DAILY_HISTORY_COPY.emptyAction}
            onAction={goToday}
          />
        ) : (
          <View className="dh__list">
            {items.map((item, index) => (
              <View key={item.key} className={`dh__item ${enter(index === 0 ? 1 : index === 1 ? 2 : 3)}`}>
                <View className="dh__item-visual">
                  <DailyVisual visual={item.visual} />
                </View>
                <View className="dh__item-copy">
                  <View className="dh__item-meta">
                    <Text className="dh__item-cat">{dailyBucketName(item.category)}</Text>
                    <Text className="dh__item-date serif">{item.date}</Text>
                  </View>
                  <Text className="dh__item-topic">{item.topic}</Text>
                  <Text className="dh__item-lead">{item.lead}</Text>
                  <Text className="dh__item-fit">{item.fitText}</Text>
                </View>
              </View>
            ))}
          </View>
        )}
      </View>
    </View>
  )
}
