// 衣橱 Lite：单品列表（8 件上限）+ 添加表单（BottomSheet）+ 显式组合生成 + 记录穿着。
import { useCallback, useEffect, useState } from 'react'
import Taro from '@tarojs/taro'
import { Image, Input, ScrollView, Text, View } from '@tarojs/components'
import { lookImage, trackEvent, userImage, type WardrobeItem, type WardrobeOutfit } from '@zsm/core'
import { usePageShell } from '../../../../hooks/use-page-visibility'
import { api } from '../../../../services/api'
import AppHeader from '../../../../components/app-header'
import PrimaryButton from '../../../../components/primary-button'
import BottomSheet from '../../../../components/bottom-sheet'
import Pill from '../../../../components/pill'
import Skeleton from '../../../../components/skeleton'
import EmptyState from '../../../../components/empty-state'
import './index.scss'

const MAX_ITEMS = 8
const CATEGORIES = [
  { key: 'all', label: '全部' },
  { key: 'top', label: '上装' },
  { key: 'bottom', label: '下装' },
  { key: 'outer', label: '外套' },
  { key: 'shoes', label: '鞋' },
  { key: 'bag', label: '包' },
] as const

export default function Wardrobe() {
  const [items, setItems] = useState<WardrobeItem[]>([])
  const [outfit, setOutfit] = useState<WardrobeOutfit | null>(null)
  const [category, setCategory] = useState('all')
  const [loading, setLoading] = useState(true)
  const [busy, setBusy] = useState(false)
  const [adding, setAdding] = useState(false)
  const [form, setForm] = useState({ name: '', category: 'top', color: '' })
  const [formPhoto, setFormPhoto] = useState('')
  const { pageClass, enter } = usePageShell(!loading || items.length > 0, '', 'wardrobe')

  const load = useCallback(async () => {
    setLoading(true)
    try {
      setItems(await api.listWardrobeItems())
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    load()
  }, [load])

  const chooseFormPhoto = () => {
    Taro.chooseMedia({
      count: 1,
      mediaType: ['image'],
      success: (res) => {
        const file = res.tempFiles[0]
        if (file) setFormPhoto(file.tempFilePath)
      },
    })
  }

  const saveItem = async () => {
    if (!form.name.trim()) {
      Taro.showToast({ title: '先给单品起个名字', icon: 'none' })
      return
    }
    setBusy(true)
    try {
      let mediaId: string | undefined
      if (formPhoto) {
        mediaId = (await api.uploadMedia({ kind: 'wardrobe', filePath: formPhoto })).id
      }
      await api.createWardrobeItem({ name: form.name.trim(), category: form.category, color: form.color || '未注明', media_id: mediaId })
      trackEvent('wardrobe_item_add', { category: form.category })
      setAdding(false)
      setForm({ name: '', category: 'top', color: '' })
      setFormPhoto('')
      load()
    } catch (e) {
      Taro.showToast({ title: (e as Error).message || '保存没有成功', icon: 'none' })
    } finally {
      setBusy(false)
    }
  }

  const removeItem = (item: WardrobeItem) => {
    Taro.showModal({
      title: '删除单品',
      content: `删除「${item.name}」？`,
      confirmColor: '#9B4B45',
      success: (res) => {
        if (!res.confirm) return
        api.deleteWardrobeItem(item.id).then(load).catch(() => Taro.showToast({ title: '删除没有成功', icon: 'none' }))
      },
    })
  }

  const createOutfit = async () => {
    if (items.length < 2) {
      Taro.showToast({ title: '先录入至少 2 件单品', icon: 'none' })
      return
    }
    setBusy(true)
    try {
      const ctx = await api.getTodayContext().catch(() => null)
      const ids = items.slice(0, 4).map((i) => i.id)
      const result = await api.createWardrobeOutfit({ title: '今日组合', item_ids: ids, context: ctx ?? undefined })
      setOutfit(result)
      trackEvent('wardrobe_outfit_generate', {})
    } catch (e) {
      Taro.showToast({ title: (e as Error).message || '生成没有成功', icon: 'none' })
    } finally {
      setBusy(false)
    }
  }

  const wear = async () => {
    if (!outfit) return
    try {
      const result = await api.wearWardrobeOutfit(outfit.id)
      setOutfit(result)
      trackEvent('wardrobe_outfit_wear', {})
      Taro.showToast({ title: '已记录', icon: 'success' })
    } catch (e) {
      Taro.showToast({ title: (e as Error).message || '记录没有成功', icon: 'none' })
    }
  }

  const shown = category === 'all' ? items : items.filter((i) => i.category === category)

  if (loading) {
    return (
      <View className={pageClass}>
        <AppHeader title="衣橱" back />
        <Skeleton rows={4} />
      </View>
    )
  }

  return (
    <View className={pageClass}>
      <AppHeader title="衣橱" back />
      <View className="wd">
        <View className={`wd__progress ${enter()}`}>
          <View className="wd__progress-track">
            <View className="wd__progress-fill" style={{ width: `${(items.length / MAX_ITEMS) * 100}%` }} />
          </View>
          <Text className="wd__progress-num">{items.length} / {MAX_ITEMS} 件 · 轻量版</Text>
        </View>

        {items.length === 0 ? (
          <EmptyState
            title="衣橱还是空的"
            description="录几件常穿的单品，就能生成今日组合。"
            actionText="添加第一件"
            onAction={() => setAdding(true)}
          />
        ) : (
          <>
            <ScrollView className="wd__cats" scrollX enhanced showScrollbar={false}>
              {CATEGORIES.map((cat) => (
                <Text
                  key={cat.key}
                  className={`wd__cat ${category === cat.key ? 'wd__cat--active' : ''}`}
                  onClick={() => setCategory(cat.key)}
                >
                  {cat.label}
                </Text>
              ))}
            </ScrollView>

            <View className={`wd__grid ${enter(1)}`}>
              {shown.map((item) => {
                const img = userImage(item.image_url) || lookImage(item.image_url)
                return (
                  <View key={item.id} className="wd__item" onLongPress={() => removeItem(item)}>
                    {img ? (
                      <Image className="wd__item-img" src={img} mode="aspectFill" />
                    ) : (
                      <View className="wd__item-empty">
                        <Text>{item.name.slice(0, 1)}</Text>
                      </View>
                    )}
                    <View className="wd__item-copy">
                      <Text className="wd__item-name">{item.name}</Text>
                      <Text className="wd__item-meta">{item.color} · 穿过 {item.wear_count} 次</Text>
                    </View>
                  </View>
                )
              })}
              {shown.length < MAX_ITEMS && category === 'all' ? (
                <View className="wd__item wd__item--add pressable" onClick={() => setAdding(true)}>
                  <Text className="wd__item-plus">＋</Text>
                  <Text className="wd__item-addtext">添加单品</Text>
                </View>
              ) : null}
            </View>

            <View className={`wd__outfit ${enter(2)}`}>
              <PrimaryButton text="用衣橱生成今日组合" loading={busy} onClick={createOutfit} />
              {outfit ? (
                <View className="wd__outfit-card">
                  <Text className="wd__outfit-title">{outfit.title}</Text>
                  <Text className="wd__outfit-note">{outfit.note}</Text>
                  <View className="wd__outfit-items">
                    {outfit.items.map((item) => (
                      <Text key={item.id} className="wd__outfit-item">{item.name}</Text>
                    ))}
                  </View>
                  <Text className="wd__outfit-wear pressable" onClick={wear}>
                    {outfit.worn ? '已记录今天穿过' : '记录今天穿了'}
                  </Text>
                </View>
              ) : null}
            </View>
          </>
        )}
      </View>

      <BottomSheet open={adding} title="添加单品" description="拍一张或从相册选，30 秒录完" onClose={() => setAdding(false)}>
        <View className="wd__form">
          <View className="wd__form-photo pressable" onClick={chooseFormPhoto}>
            {formPhoto ? (
              <Image className="wd__form-photo-img" src={formPhoto} mode="aspectFill" />
            ) : (
              <Text className="wd__form-photo-hint">＋ 单品照片（可选）</Text>
            )}
          </View>
          <Input
            className="wd__form-input"
            placeholder="名字，如：米白针织衫"
            value={form.name}
            onInput={(e: { detail: { value: string } }) => setForm((f) => ({ ...f, name: e.detail.value }))}
          />
          <Input
            className="wd__form-input"
            placeholder="颜色，如：米白"
            value={form.color}
            onInput={(e: { detail: { value: string } }) => setForm((f) => ({ ...f, color: e.detail.value }))}
          />
          <View className="wd__form-cats">
            {CATEGORIES.filter((c) => c.key !== 'all').map((cat) => (
              <Pill
                key={cat.key}
                label={cat.label}
                active={form.category === cat.key}
                onClick={() => setForm((f) => ({ ...f, category: cat.key }))}
              />
            ))}
          </View>
          <PrimaryButton text="保存" loading={busy} onClick={saveItem} />
        </View>
      </BottomSheet>
    </View>
  )
}
