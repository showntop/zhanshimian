// 执行清单：乐观勾选 + 失败回滚 + check-pop 弹跳动效 + 完成度。
import { useCallback, useEffect, useState } from 'react'
import Taro, { useLoad } from '@tarojs/taro'
import { Text, View } from '@tarojs/components'
import { CHECKLIST_COPY, type ChecklistItem } from '@zsm/core'
import { usePageClass } from '../../hooks/use-page-visibility'
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
  const pageClass = usePageClass(!loading || items.length > 0)

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
  const remain = items.length - done
  const allDone = items.length > 0 && remain === 0

  if (loading) {
    return (
      <View className={pageClass}>
        <AppHeader title={CHECKLIST_COPY.title} back />
        <Skeleton rows={4} />
      </View>
    )
  }

  if (failed) {
    return (
      <View className={pageClass}>
        <AppHeader title={CHECKLIST_COPY.title} back />
        <ErrorState onRetry={load} />
      </View>
    )
  }

  if (items.length === 0) {
    return (
      <View className={pageClass}>
        <AppHeader title={CHECKLIST_COPY.title} back />
        <EmptyState
          title={CHECKLIST_COPY.emptyTitle}
          description={CHECKLIST_COPY.emptyBody}
          actionText={CHECKLIST_COPY.emptyAction}
          onAction={() => Taro.switchTab({ url: '/pages/plans/index' })}
        />
      </View>
    )
  }

  return (
    <View className={pageClass}>
      <AppHeader title={CHECKLIST_COPY.title} back />
      <View className="cklist">
        <View className={`cklist__progress fade-up ${allDone ? 'cklist__progress--done' : ''}`}>
          <View className="cklist__progress-row">
            <Text className="cklist__progress-num">
              {done} / {items.length} {CHECKLIST_COPY.doneOf}
            </Text>
            <Text className="cklist__progress-remain">
              {allDone ? CHECKLIST_COPY.allDoneHint : `${remain} ${CHECKLIST_COPY.remainSuffix}`}
            </Text>
          </View>
          <View className="cklist__progress-track">
            <View
              className="cklist__progress-fill"
              style={{ width: `${(done / items.length) * 100}%` }}
            />
          </View>
          <Text className="cklist__progress-hint">
            {allDone ? CHECKLIST_COPY.celebrate : CHECKLIST_COPY.hint}
          </Text>
        </View>

        <View className="cklist__items fade-up delay-1">
          {items.map((item) => (
            <View
              key={item.id}
              className={`cklist__item pressable ${item.completed ? 'cklist__item--done' : ''}`}
              onClick={() => toggle(item)}
            >
              <View className="cklist__check">
                {item.completed ? <Text className="cklist__check-mark">✓</Text> : null}
              </View>
              <View className="cklist__body">
                <Text className="cklist__cat">{CATEGORY_LABEL[item.category] || item.category}</Text>
                <Text className="cklist__title">{item.title}</Text>
                {item.description ? <Text className="cklist__desc">{item.description}</Text> : null}
                {item.meta ? <Text className="cklist__meta">{item.meta}</Text> : null}
              </View>
            </View>
          ))}
        </View>

        <View className="cklist__foot fade-up delay-2">
          <PrimaryButton
            text={allDone ? CHECKLIST_COPY.ctaDone : CHECKLIST_COPY.cta}
            onClick={() => Taro.navigateTo({ url: `/pages/feedback/index?plan_id=${planId}` })}
          />
          <Text className="cklist__foot-note">{CHECKLIST_COPY.footNote}</Text>
        </View>
      </View>
    </View>
  )
}
