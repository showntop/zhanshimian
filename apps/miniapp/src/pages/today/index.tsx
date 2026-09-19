// 今日：每天一条内容（轻模式）+ 引导去生成今日造型（重模式）。
//
// 美学参照：Aesop / COS 的编辑式极简。
//   一个画面一个主角（视觉），其余全部降权：小字 meta、细线分隔、文字链次级动作。
//   不用色块当容器（曾出现的 moss-soft 底个性化块、浅绿手册卡已全部取消）。
//   编号是小而对齐的序列标记，不是压在标题后的大字残影。
//
// 上一版的「色彩场」已废弃：低饱和品牌色做不出可见的氛围，只会让画面发白，
// 还占掉本该给视觉的空间。背景回到干净底色，让内容视觉自己当主角。
//
// 重模式不在这里并列：它是「看看我穿这样」这个 action 的结果，跳 life 分包的今日造型页。

import { useCallback } from 'react'
import Taro, { useDidShow } from '@tarojs/taro'
import { Text, View } from '@tarojs/components'
import { DAILY_COPY, dailyTypeName } from '@zsm/core'
import { usePageShell } from '../../hooks/use-page-visibility'
import { useDailyPick } from '../../features/daily/use-daily-pick'
import AppHeader from '../../components/app-header'
import DailyVisual from '../../components/daily-visual'
import Skeleton from '../../components/skeleton'
import './index.scss'

const TODAY_PLAN_PATH = '/packages/life/pages/today/index'
const HANDBOOK_PATH = '/packages/life/pages/handbook/index'

export default function Today() {
  const { ctx, saves, loading, pick, bucketName, reloadSaves, saveCurrent } = useDailyPick()
  const { pageClass } = usePageShell(true, '', 'today')

  // 从手册返回时同步一次：那边可以移出，回来要看到最新状态
  useDidShow(() => {
    reloadSaves()
  })

  const goPlan = useCallback(() => {
    void Taro.navigateTo({ url: TODAY_PLAN_PATH })
  }, [])

  const goHandbook = useCallback(() => {
    void Taro.navigateTo({ url: HANDBOOK_PATH })
  }, [])

  const seq = String(saves.length + 1).padStart(2, '0')

  return (
    <View className={pageClass}>
      <AppHeader back />
      {loading ? (
        <Skeleton rows={4} />
      ) : (
        <View className="today">
          <View className="today__ctx">
            <Text className="today__ctx-main">
              {ctx.date.slice(5).replace('-', '月')}日 · {ctx.temperature}° {ctx.condition}
            </Text>
            {ctx.city !== '' ? <Text className="today__ctx-city">{ctx.city}</Text> : null}
          </View>

          {pick ? (
            <View
              className={`today__visual ${
                pick.content.visual.modality === 'poster' ? 'today__visual--tall' : ''
              }`}
            >
              <DailyVisual visual={pick.content.visual} />
            </View>
          ) : null}

          {pick ? (
            <View className="today__body">
              <Text className="today__meta">
                {dailyTypeName(pick.content.type)} · 第 {seq} 条
              </Text>
              <Text className="today__title">{pick.content.topic}</Text>
              <Text className="today__lead">{pick.content.lead}</Text>

              <View className="today__rule" />

              <View className="today__fit">
                <Text className="today__fit-text">{pick.fitText}</Text>
              </View>

              <Text className="today__why">{pick.content.why}</Text>

              <View className="today__cta" onClick={saveCurrent}>
                <Text className="today__cta-text">{DAILY_COPY.saveAction}{bucketName}</Text>
              </View>
              <Text className="today__link" onClick={goPlan}>{DAILY_COPY.seeItAction}</Text>

              <View className="today__rule" />
              <View className="today__handbook" onClick={goHandbook}>
                <Text className="today__handbook-title">{DAILY_COPY.handbookTitle}</Text>
                <Text className="today__handbook-go">{saves.length} ›</Text>
              </View>
            </View>
          ) : (
            <View className="today__empty">
              <Text className="today__empty-title">{DAILY_COPY.emptyTitle}</Text>
              <Text className="today__empty-body">{DAILY_COPY.emptyBody}</Text>
            </View>
          )}
        </View>
      )}
    </View>
  )
}
