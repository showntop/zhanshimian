// 我的手册：收下的内容按资产库分组沉淀。
//
// 这里的价值不在「数了多少条」，而在它是结构化的：色卡 / 版型库 / 配色库 / 面料库 / 搭配，
// 每一条都能回看、能对照着买衣服。数字断了是损失，内容攒着是财富。
//
// 放 life 分包：手册是低频查看的资产页，不占主包体积（今日页才是每日必访）。

import { useMemo, useState } from 'react'
import Taro, { useDidShow } from '@tarojs/taro'
import { Text, View } from '@tarojs/components'
import {
  DAILY_COPY,
  MOCK_CONTENTS,
  MOCK_GENE_DEFAULT,
  dailyBucketName,
  dailyTypeName,
  type CollectionCategory,
  type DailyContent,
  type StyleGene,
} from '@zsm/core'
import { usePageShell } from '../../../../hooks/use-page-visibility'
import { readSaves, removeSave, type DailySave } from '../../../../features/daily/saves'
import AppHeader from '../../../../components/app-header'
import DailyVisual from '../../../../components/daily-visual'
import './index.scss'

const BUCKET_ORDER: CollectionCategory[] = [
  'color',
  'fit',
  'proportion',
  'fabric',
  'occasion',
  'howto',
  'outfit',
]

// 服务端 StyleGene 就绪后改为读取用户自己的形象基因
const GENE: StyleGene = MOCK_GENE_DEFAULT

const CONTENT_BY_KEY = new Map<string, DailyContent>(MOCK_CONTENTS.map((c) => [c.dedupeKey, c]))

export default function Handbook() {
  const [saves, setSaves] = useState<DailySave[]>(() => readSaves())
  const { pageClass, enter } = usePageShell(true, '', 'handbook')

  useDidShow(() => {
    setSaves(readSaves())
  })

  const groups = useMemo(() => {
    const map = new Map<CollectionCategory, DailySave[]>()
    for (const bucket of BUCKET_ORDER) map.set(bucket, [])
    for (const save of saves) {
      const list = map.get(save.category)
      if (list) list.push(save)
      // 未知 bucket 直接丢弃，不建隐式分组
    }
    return BUCKET_ORDER.map((bucket) => ({
      bucket,
      name: dailyBucketName(bucket),
      items: map.get(bucket) ?? [],
    })).filter((g) => g.items.length > 0)
  }, [saves])

  const onRemove = (key: string) => {
    setSaves(removeSave(key))
    Taro.showToast({ title: '已移出手册', icon: 'none' })
  }

  const goToday = () => {
    void Taro.navigateTo({ url: '/pages/today/index' })
  }

  return (
    <View className={pageClass}>
      <AppHeader back />
      <View className="hb">
        <View className={`hb__head ${enter()}`}>
          <Text className="hb__title">{DAILY_COPY.handbookTitle}</Text>
          <Text className="hb__count">已收 {saves.length} 条</Text>
        </View>

        {groups.length === 0 ? (
          <View className="hb__empty">
            <Text className="hb__empty-title">{DAILY_COPY.handbookEmptyTitle}</Text>
            <Text className="hb__empty-body">{DAILY_COPY.handbookEmptyBody}</Text>
            <View className="hb__empty-cta" onClick={goToday}>
              <Text className="hb__empty-cta-text">去看今天这一条 ›</Text>
            </View>
          </View>
        ) : (
          groups.map((group) => (
            <View key={group.bucket} className="hb__group">
              <View className="hb__group-head">
                <Text className="hb__group-name">{group.name}</Text>
                <Text className="hb__group-n">{group.items.length}</Text>
              </View>
              {group.items.map((save) => {
                const content = CONTENT_BY_KEY.get(save.key)
                if (!content) return null
                return (
                  <View key={save.key} className="hb__item">
                    <View className="hb__item-visual">
                      <DailyVisual visual={content.visual} />
                    </View>
                    <View className="hb__item-copy">
                      <Text className="hb__item-type">{dailyTypeName(content.type)}</Text>
                      <Text className="hb__item-topic">{content.topic}</Text>
                      <Text className="hb__item-fit">{content.fit(GENE)}</Text>
                    </View>
                    <View className="hb__item-remove" onClick={() => onRemove(save.key)}>
                      <Text className="hb__item-remove-text">移出</Text>
                    </View>
                  </View>
                )
              })}
            </View>
          ))
        )}
      </View>
    </View>
  )
}
