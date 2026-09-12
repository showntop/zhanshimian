// 发型预览：推荐列表 + 三种照片来源 + 异步生成（900ms 轮询）+ 原图/效果对比 + 保存。
import { useCallback, useEffect, useState } from 'react'
import Taro from '@tarojs/taro'
import { Image, ScrollView, Text, View } from '@tarojs/components'
import { IMAGE_BADGE_COPY, LOCAL_LOOK_SLUGS, POLL_INTERVALS, useTaskPolling, lookImage, userImage, type HairPreview, type HairstyleOption, type LookSlug } from '@zsm/core'
import { usePageShell, useShowOnce } from '../../../../hooks/use-page-visibility'
import { api } from '../../../../services/api'
import { STORAGE_KEYS, readStorage, writeStorage } from '../../../../services/storage'
import AppHeader from '../../../../components/app-header'
import PrimaryButton from '../../../../components/primary-button'
import ExampleImage from '../../../../components/example-image'
import './index.scss'

function asLookSlug(id: string): LookSlug | undefined {
  return (LOCAL_LOOK_SLUGS as readonly string[]).includes(id) ? (id as LookSlug) : undefined
}

const STYLES = [
  { id: 'sharp', name: '锁骨层次发', slug: 'sharp' },
  { id: 'warm', name: '空气微卷', slug: 'warm' },
  { id: 'natural', name: '自然偏分', slug: 'natural' },
] as const

export default function Hair() {
  const [options, setOptions] = useState<HairstyleOption[]>([])
  const [styleId, setStyleId] = useState<string>('sharp')
  const [photoPath, setPhotoPath] = useState('')
  const [preview, setPreview] = useState<HairPreview | null>(null)
  const [mode, setMode] = useState<'source' | 'result'>('source')
  const [loading, setLoading] = useState(true)
  const [busy, setBusy] = useState(false)
  const { pageClass, enter } = usePageShell(!loading || Boolean(preview) || Boolean(photoPath), '', 'hair')

  const loadOptions = useCallback(async () => {
    setLoading(true)
    try {
      const reportId = readStorage(STORAGE_KEYS.reportId)
      const items = await api.listHairstyles(reportId || undefined)
      setOptions(items)
    } catch {
      setOptions([])
    } finally {
      setLoading(false)
    }
  }, [])

  // 恢复进行中的预览任务：先读本地引用；引用丢失（清缓存/换设备）时
  // 向服务端找回仍在生成中的预览（GET /v1/hair-previews/active，404 = 无进行中任务）
  const resume = useCallback(async () => {
    const id = readStorage(STORAGE_KEYS.activeTaskHairPreview)
    if (id) {
      try {
        const item = await api.getHairPreview(id)
        setPreview(item)
        if (item.style_id) setStyleId(item.style_id)
      } catch {
        writeStorage(STORAGE_KEYS.activeTaskHairPreview, '')
      }
      return
    }
    try {
      const item = await api.getActiveHairPreview()
      setPreview(item)
      if (item.style_id) setStyleId(item.style_id)
      writeStorage(STORAGE_KEYS.activeTaskHairPreview, item.id)
    } catch {
      /* 无进行中任务：保持新任务态 */
    }
  }, [])

  useEffect(() => {
    loadOptions()
    resume()
  }, [loadOptions, resume])

  useShowOnce(() => {
    resume()
  })

  const running =
    preview && (preview.status === 'queued' || preview.status === 'processing')
  const { stop } = useTaskPolling({
    fetcher: async () => {
      if (!preview) throw new Error('no preview')
      const item = await api.getHairPreview(preview.id)
      setPreview(item)
      return {
        id: item.id,
        type: 'hair_preview' as const,
        status: item.status,
        progress: item.progress,
        stage: item.stage,
        created_at: item.created_at,
        updated_at: item.updated_at,
      }
    },
    intervalMs: POLL_INTERVALS.hairPreview,
    enabled: Boolean(running),
    onDone: (result) => {
      if (result.status === 'completed') setMode('result')
    },
  })
  Taro.useDidHide(() => stop())

  const choosePhoto = () => {
    Taro.chooseMedia({
      count: 1,
      mediaType: ['image'],
      sourceType: ['camera', 'album'],
      camera: 'front',
      success: (res) => {
        const file = res.tempFiles[0]
        if (file) setPhotoPath(file.tempFilePath)
      },
    })
  }

  const useDemoPhoto = async () => {
    const asset = await api.createDemoMedia('face')
    setPhotoPath(userImage(asset.url))
    return asset
  }

  const generate = async (demo = false) => {
    setBusy(true)
    try {
      let mediaId: string
      if (demo) {
        mediaId = (await useDemoPhoto()).id
      } else {
        if (!photoPath) {
          Taro.showToast({ title: '先上传一张正脸照', icon: 'none' })
          return
        }
        mediaId = (await api.uploadMedia({ kind: 'face', filePath: photoPath })).id
      }
      const reportId = readStorage(STORAGE_KEYS.reportId) || undefined
      const { data } = await api.createHairPreview({ media_id: mediaId, report_id: reportId, style_id: styleId })
      setPreview(data)
      writeStorage(STORAGE_KEYS.activeTaskHairPreview, data.id)
      setMode('source')
    } catch (e) {
      Taro.showToast({ title: (e as Error).message || '生成没有开始，请重试', icon: 'none' })
    } finally {
      setBusy(false)
    }
  }

  const save = async () => {
    if (!preview) return
    try {
      await api.saveHairPreview(preview.id)
      writeStorage(STORAGE_KEYS.activeTaskHairPreview, '')
      Taro.showToast({ title: '已保存', icon: 'success' })
    } catch (e) {
      Taro.showToast({ title: (e as Error).message || '保存没有成功', icon: 'none' })
    }
  }

  const styleName = preview?.style_name || STYLES.find((s) => s.id === styleId)?.name || ''
  const sourceUrl = userImage(preview?.source_image_url) || photoPath
  const resultUrl = lookImage(preview?.result_image_url)
  const isDemo = (preview?.provider_version ?? '').startsWith('demo')

  return (
    <View className={pageClass}>
      <AppHeader title="发型预览" back />
      <View className="hair">
        <View className={`hair__hero photo-hero photo-hero--bleed ${enter()}`}>
          <View className="hair__hero-frame">
            {mode === 'result' && resultUrl ? (
              <Image className="hair__hero-img" src={resultUrl} mode="aspectFit" />
            ) : sourceUrl ? (
              <Image className="hair__hero-img" src={sourceUrl} mode="aspectFit" />
            ) : (
              <ExampleImage
                className="hair__hero-img"
                slug={asLookSlug(styleId) ?? 'sharp'}
                variant="hair"
                badgeText={IMAGE_BADGE_COPY.bundled}
                anchor="top"
              />
            )}
            {mode === 'result' && resultUrl ? (
              <View className="hair__badge layer-on-photo">
                <Text>{isDemo ? IMAGE_BADGE_COPY.demo : IMAGE_BADGE_COPY.bundled}</Text>
              </View>
            ) : null}
            {preview && (preview.status === 'queued' || preview.status === 'processing') ? (
              <View className="hair__mask">
                <View className="scan-sweep" />
                <View className="hair__mask-spin spinner" />
                <Text className="hair__mask-text">{preview.stage || '正在生成预览'}</Text>
              </View>
            ) : null}
            {mode === 'result' && resultUrl && sourceUrl ? (
              <View className="hair__toggle">
                <Text
                  className={`hair__toggle-seg ${mode !== 'result' ? 'hair__toggle-seg--active' : ''}`}
                  onClick={() => setMode('source')}
                >
                  原图
                </Text>
                <Text
                  className={`hair__toggle-seg ${mode === 'result' ? 'hair__toggle-seg--active' : ''}`}
                  onClick={() => setMode('result')}
                >
                  效果
                </Text>
              </View>
            ) : null}
          </View>
        </View>

        <View className={`hair__hint ${enter(1)}`}>
          <Text className="hair__hint-title">先看效果，再决定剪不剪</Text>
          <View className="section-rule" />
          <Text className="hair__hint-desc">{options.find((o) => o.id === styleId)?.reason || '基于你的正脸照生成，发型轮廓与发色可实时对比。'}</Text>
        </View>

        <ScrollView className={`hair__styles ${enter(2)}`} scrollX enhanced showScrollbar={false}>
          {(options.length > 0 ? options : STYLES.map((s) => ({ id: s.id, name: s.name } as HairstyleOption))).map((opt) => (
            <View
              key={opt.id}
              className={`hair__style ${styleId === opt.id ? 'hair__style--active' : ''} pressable`}
              onClick={() => setStyleId(opt.id)}
            >
              <ExampleImage
                className="hair__style-img"
                src={opt.image_url}
                slug={!opt.image_url ? asLookSlug(opt.id) : undefined}
                variant="hair"
                anchor="top"
              />
              <Text className="hair__style-name">{opt.name}</Text>
            </View>
          ))}
        </ScrollView>

        {preview?.status === 'failed' ? (
          <View className={`hair__failed ${enter()}`}>
            <Text className="hair__failed-text">{preview.error_message || '生成没有完成，请重试'}</Text>
          </View>
        ) : null}

        <View className={`hair__foot ${enter(3)}`}>
          {preview?.status === 'completed' && resultUrl ? (
            <>
              <PrimaryButton text={`保存「${styleName}」`} onClick={save} />
              <Text className="hair__foot-alt pressable" onClick={() => setPreview(null)}>
                换一个方向再试
              </Text>
            </>
          ) : (
            <>
              <PrimaryButton
                text={running ? '正在生成…' : `生成「${styleName}」预览`}
                loading={busy || Boolean(running)}
                onClick={() => generate(false)}
              />
              <View className="hair__foot-row">
                <Text className="hair__foot-alt pressable" onClick={choosePhoto}>
                  {photoPath ? '重选正脸照' : '选择正脸照'}
                </Text>
                <Text className="hair__foot-alt pressable" onClick={() => generate(true)}>
                  用示例照片体验
                </Text>
              </View>
            </>
          )}
        </View>
      </View>
    </View>
  )
}
