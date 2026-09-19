// 每日内容选品的共用逻辑：首页卡片与今日页走同一份，避免两处各算各的。
//
// 语境来自服务端 TodayContext（真实天气）；取不到时用兜底语境继续给内容——
// 内容不该因为一个天气接口失败就整页空掉。

import { useCallback, useEffect, useState } from 'react'
import Taro from '@tarojs/taro'
import {
  DAILY_COPY,
  MOCK_CONTENTS,
  MOCK_GENE_DEFAULT,
  countBuckets,
  dailyBucketName,
  pickDaily,
  type DailyPick,
  type DayType,
  type Season,
  type StyleGene,
  type TodayContext,
} from '@zsm/core'
import { peripherals } from '../../app/api/peripherals'
import { addSave, readSaves, seenKeys, type DailySave } from './saves'

// TODO(服务端 StyleGene 接口就绪后替换)：暂用预设，真实应读用户自己的形象基因
const GENE: StyleGene = MOCK_GENE_DEFAULT

function normalizeDayType(value: string): DayType {
  if (value === 'weekend' || value.includes('周末')) return 'weekend'
  if (value === 'holiday' || value.includes('节') || value.includes('假')) return 'holiday'
  return 'weekday'
}

function seasonOf(date: string): Season {
  const month = Number(date.slice(5, 7))
  if (month >= 3 && month <= 5) return 'spring'
  if (month >= 6 && month <= 8) return 'summer'
  if (month >= 9 && month <= 11) return 'autumn'
  return 'winter'
}

function fallbackContext(): TodayContext {
  const now = new Date()
  const date = `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, '0')}-${String(now.getDate()).padStart(2, '0')}`
  return { date, city: '', condition: '多云', temperature: 18, day_type: 'weekday', schedule: '' }
}

export interface DailyPickState {
  ctx: TodayContext
  saves: DailySave[]
  loading: boolean
  pick: DailyPick | null
  bucketName: string
  reloadSaves: () => void
  saveCurrent: () => void
}

export function useDailyPick(): DailyPickState {
  const [context, setContext] = useState<TodayContext | null>(null)
  const [saves, setSaves] = useState<DailySave[]>(() => readSaves())
  const [loading, setLoading] = useState(true)

  const loadContext = useCallback(async () => {
    setLoading(true)
    const ctx = await peripherals.getTodayContext().catch(() => null)
    setContext(ctx)
    setLoading(false)
  }, [])

  useEffect(() => {
    void loadContext()
  }, [loadContext])

  const reloadSaves = useCallback(() => {
    setSaves(readSaves())
  }, [])

  const ctx = context ?? fallbackContext()

  const pick = pickDaily(MOCK_CONTENTS, {
    gene: GENE,
    temperature: ctx.temperature,
    condition: ctx.condition,
    dayType: normalizeDayType(ctx.day_type),
    season: seasonOf(ctx.date),
    seen: seenKeys(saves),
    bucketCount: countBuckets(saves.map((s) => s.category)),
  })

  const bucketName = pick ? dailyBucketName(pick.content.asset) : ''

  const saveCurrent = useCallback(() => {
    if (!pick) return
    // 副本：fitText 已按该用户基因渲染，内容池后续迭代不影响这条
    const next = addSave({
      key: pick.content.dedupeKey,
      category: pick.content.asset,
      savedAt: new Date().toISOString(),
      status: 'saved',
      note: '',
      snapshot: {
        id: pick.content.id,
        type: pick.content.type,
        topic: pick.content.topic,
        lead: pick.content.lead,
        fitText: pick.fitText,
        why: pick.content.why,
        visual: pick.content.visual,
        category: pick.content.asset,
      },
    })
    setSaves(next)
    Taro.showToast({ title: `${DAILY_COPY.savedToastPrefix}${bucketName}`, icon: 'none' })
  }, [pick, bucketName])

  return { ctx, saves, loading, pick, bucketName, reloadSaves, saveCurrent }
}
