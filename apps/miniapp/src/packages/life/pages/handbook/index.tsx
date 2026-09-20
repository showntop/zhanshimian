// 我的手册：收下的内容按资产库分组沉淀。
//
// 数据源：GET /v1/daily/collection（读服务端固化的副本快照，不再反查本地
// 内容池）。拉取失败 → 显示本地缓存并标注「离线」——手册可看，只是新不到。
//
// 放 life 分包：手册是低频查看的资产页，不占主包体积（今日页才是每日必访）。

import { useCallback, useMemo, useState } from 'react'
import Taro, { useDidShow } from '@tarojs/taro'
import { Text, View } from '@tarojs/components'
import {
  DAILY_COPY,
  dailyBucketName,
  dailyTypeName,
  type CollectionCategory,
  type ContentVisual,
} from '@zsm/core'
import { peripherals } from '../../../../app/api/peripherals'
import { readSaves, removeSave, type DailySave } from '../../../../features/daily/saves'
import { usePageShell } from '../../../../hooks/use-page-visibility'
import AppHeader from '../../../../components/app-header'
import DailyVisual from '../../../../components/daily-visual'
import './index.scss'

// 与服务端 allCategories 同序（含 general）；general 是归不进七格的综合内容，排最后。
const BUCKET_ORDER: CollectionCategory[] = [
  'color',
  'fit',
  'proportion',
  'fabric',
  'occasion',
  'howto',
  'outfit',
  'general',
]

/** 手册条目视图：服务端副本快照（真源）或本地缓存（离线降级）共用 */
interface HandbookItem {
  key: string
  category: CollectionCategory
  type: string
  topic: string
  fitText: string
  visual: ContentVisual
}

function fromCollection(item: {
  id: string
  content_key: string
  category: string
  content_snapshot: {
    type: string
    topic: string
    fitText: string
    visual: { modality: string; spec: Record<string, unknown> | null; alt: string }
  } | null
}): HandbookItem | null {
  const snapshot = item.content_snapshot
  if (!snapshot || !snapshot.visual) return null
  return {
    key: item.content_key,
    category: item.category as CollectionCategory,
    type: snapshot.type,
    topic: snapshot.topic,
    fitText: snapshot.fitText,
    visual: snapshot.visual as unknown as ContentVisual,
  }
}

function fromLocalSave(save: DailySave): HandbookItem | null {
  if (!save.snapshot || !save.snapshot.visual) return null
  return {
    key: save.key,
    category: save.category,
    type: save.snapshot.type,
    topic: save.snapshot.topic,
    fitText: save.snapshot.fitText,
    visual: save.snapshot.visual,
  }
}

export default function Handbook() {
  const [items, setItems] = useState<HandbookItem[]>([])
  const [offline, setOffline] = useState(false)
  const [loading, setLoading] = useState(true)
  const { pageClass, enter } = usePageShell(true, '', 'handbook')

  const load = useCallback(async () => {
    try {
      const collections = await peripherals.listDailyCollection()
      setItems(
        collections
          .map(fromCollection)
          .filter((item): item is HandbookItem => item !== null),
      )
      setOffline(false)
    } catch {
      // 手册拉取失败 → 本地缓存兜底并标注「离线」（方案 §3.3）
      setItems(
        readSaves()
          .map(fromLocalSave)
          .filter((item): item is HandbookItem => item !== null),
      )
      setOffline(true)
    } finally {
      setLoading(false)
    }
  }, [])

  useDidShow(() => {
    void load()
  })

  const groups = useMemo(() => {
    const map = new Map<CollectionCategory, HandbookItem[]>()
    for (const bucket of BUCKET_ORDER) map.set(bucket, [])
    for (const item of items) {
      const list = map.get(item.category)
      if (list) list.push(item)
      // 未知 bucket 直接丢弃，不建隐式分组
    }
    return BUCKET_ORDER.map((bucket) => ({
      bucket,
      name: dailyBucketName(bucket),
      items: map.get(bucket) ?? [],
    })).filter((group) => group.items.length > 0)
  }, [items])

  const onRemove = (item: HandbookItem) => {
    // 本地立即移出 + 服务端尽力删除（幂等 204），失败下次进入对账
    setItems((prev) => prev.filter((prevItem) => prevItem.key !== item.key))
    removeSave(item.key)
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
          <Text className="hb__count">
            {offline ? '离线缓存' : `已收 ${items.length} 条`}
          </Text>
        </View>

        {loading ? null : groups.length === 0 ? (
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
              {group.items.map((item) => (
                <View key={item.key} className="hb__item">
                  <View className="hb__item-visual">
                    <DailyVisual visual={item.visual} />
                  </View>
                  <View className="hb__item-copy">
                    <Text className="hb__item-type">{dailyTypeName(item.type)}</Text>
                    <Text className="hb__item-topic">{item.topic}</Text>
                    <Text className="hb__item-fit">{item.fitText}</Text>
                  </View>
                  <View className="hb__item-remove" onClick={() => onRemove(item)}>
                    <Text className="hb__item-remove-text">移出</Text>
                  </View>
                </View>
              ))}
            </View>
          ))
        )}
      </View>
    </View>
  )
}
