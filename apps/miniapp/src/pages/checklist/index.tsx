// 执行清单：乐观勾选 + 失败回滚 + check-pop 弹跳动效 + 完成度。
import { useCallback, useEffect, useState } from 'react'
import Taro, { useLoad } from '@tarojs/taro'
import { Text, View } from '@tarojs/components'
import type { ChecklistItem } from '@zsm/core'
import { api } from '../../services/api'
import { STORAGE_KEYS, readStorage } from '../../services/storage'
import AppHeader from '../../components/app-header'
import PrimaryButton from '../../components/primary-button'
import Skeleton from '../../components/skeleton'
import ErrorState from '../../components/error-state'
import EmptyState from '../../components/empty-state'
import './index.scss'

const CATEGORY_LABEL: Record<string, string> = { hair: '发型', makeup: '妆容', outfit: '穿搭' }

export default function Checklist() {
  const [planId, setPlanId] = useState('')
  const [items, setItems] = useState<ChecklistItem[]>([])
  const [loading, setLoading] = useState(true)
  const [failed, setFailed] = useState(false)

  useLoad((options) => {
    setPlanId(options?.id || readStorage(STORAGE_KEYS.planId))
  })

  const load = useCallback(async () => {
    if (!planId) return
    setLoading(true)
    setFailed(false)
    try {
      setItems(await api.getChecklist(planId))
    } catch {
      setFailed(true)
    } finally {
      setLoading(false)
    }
  }, [planId])

  useEffect(() => {
    load()
  }, [load])

  const toggle = async (item: ChecklistItem) => {
    const next = !item.completed
    // 乐观更新 + 失败回滚
    setItems((prev) => prev.map((it) => (it.id === item.id ? { ...it, completed: next } : it)))
    if (next) Taro.vibrateShort({ type: 'light' })
    try {
      const saved = await api.updateChecklistItem(planId, item.id, { completed: next })
      setItems((prev) => prev.map((it) => (it.id === item.id ? saved : it)))
    } catch {
      setItems((prev) => prev.map((it) => (it.id === item.id ? { ...it, completed: item.completed } : it)))
      Taro.showToast({ title: '同步没有成功，已还原', icon: 'none' })
    }
  }

  const done = items.filter((i) => i.completed).length
  const allDone = items.length > 0 && done === items.length

  if (loading) {
    return (
      <View className="page">
        <AppHeader title="执行清单" back />
        <Skeleton rows={4} />
      </View>
    )
  }

  if (failed) {
    return (
      <View className="page">
        <AppHeader title="执行清单" back />
        <ErrorState onRetry={load} />
      </View>
    )
  }

  if (items.length === 0) {
    return (
      <View className="page">
        <AppHeader title="执行清单" back />
        <EmptyState
          title="清单还是空的"
          description="先在方案详情页选择一套方案。"
          actionText="去看方案"
          onAction={() => Taro.switchTab({ url: '/pages/plans/index' })}
        />
      </View>
    )
  }

  return (
    <View className="page">
      <AppHeader title="执行清单" back />
      <View className="cklist">
        <View className="cklist__progress fade-up">
          <View className="cklist__progress-track">
            <View className="cklist__progress-fill" style={{ width: `${(done / items.length) * 100}%` }} />
          </View>
          <Text className="cklist__progress-num">
            {done} / {items.length} 已完成
          </Text>
        </View>

        {allDone ? (
          <View className="cklist__celebrate fade-up">
            <View className="cklist__celebrate-ring">
              <Text className="cklist__celebrate-mark">✓</Text>
            </View>
            <Text className="cklist__celebrate-title">清单全部完成</Text>
          </View>
        ) : null}

        <View className="cklist__items fade-up delay-1">
          {items.map((item) => (
            <View key={item.id} className={`cklist__item ${item.completed ? 'cklist__item--done' : ''}`}>
              <View className="cklist__check pressable" onClick={() => toggle(item)}>
                {item.completed ? <Text className="cklist__check-mark">✓</Text> : null}
              </View>
              <View className="cklist__body">
                <View className="cklist__head">
                  <Text className="cklist__cat">{CATEGORY_LABEL[item.category] || item.category}</Text>
                  {item.meta ? <Text className="cklist__meta">{item.meta}</Text> : null}
                </View>
                <Text className="cklist__title">{item.title}</Text>
                {item.description ? <Text className="cklist__desc">{item.description}</Text> : null}
              </View>
            </View>
          ))}
        </View>

        <View className="cklist__foot fade-up delay-2">
          <PrimaryButton
            text="完成后回来反馈"
            onClick={() => Taro.navigateTo({ url: `/pages/feedback/index?plan_id=${planId}` })}
          />
          <Text className="cklist__foot-note">你的反馈会让下一次建议更准确</Text>
        </View>
      </View>
    </View>
  )
}
