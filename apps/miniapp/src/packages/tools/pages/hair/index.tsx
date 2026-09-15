// 发型预览：推荐列表 + 三种照片来源 + 受理后唯一轮询 + 原图/效果对比 + 保存。
// 预览是异步受理（202 + 公开 Operation）；恢复不靠本地引用，先问服务端
// /v1/hair-previews/active，端点异常才退回列表里找仍在生成中的那一份。
import { useCallback, useEffect, useState } from 'react'
import Taro from '@tarojs/taro'
import { ScrollView, Text, View } from '@tarojs/components'
import { IMAGE_BADGE_COPY, LOCAL_LOOK_SLUGS, type HairPreview, type HairStyle, type LookSlug } from '@zsm/core'
import { usePageShell, useShowOnce } from '../../../../hooks/use-page-visibility'
import { peripherals } from '../../../../app/api/peripherals'
import { qualityApi } from '../../../../app/api/quality'
import { mediaUpload } from '../../../../app/api/client'
import { uploadMedia } from '../../../../app/api/media-upload'
import { readLocalImage } from '../../../../features/capture/local-file'
import { resourceCache, resourceKey } from '../../../../app/cache/resource-cache'
import { useOperationPolling } from '../../../../app/operations/use-operation-polling'
import { handleBillingError } from '../../../../services/billing'
import AppHeader from '../../../../components/app-header'
import PrimaryButton from '../../../../components/primary-button'
import SourceImage from '../../../../components/source-image'
import './index.scss'

const IN_FLIGHT = new Set(['queued', 'generating', 'checking'])
const STYLES = [
  { id: 'sharp', name: '锁骨层次发' },
  { id: 'warm', name: '空气微卷' },
  { id: 'natural', name: '自然偏分' },
] as const

export default function Hair() {
  const [styles, setStyles] = useState<HairStyle[]>([])
  const [styleId, setStyleId] = useState<string>('sharp')
  const [preview, setPreview] = useState<HairPreview | null>(null)
  const [mode, setMode] = useState<'source' | 'result'>('source')
  const [loading, setLoading] = useState(true)
  const [busy, setBusy] = useState(false)
  const { pageClass, enter } = usePageShell(!loading || Boolean(preview), '', 'hair')

  const loadOptions = useCallback(async () => {
    setLoading(true)
    try {
      const items = await peripherals.listHairstyles()
      setStyles(items)
      if (items[0]) setStyleId(items[0].id)
    } catch {
      setStyles([])
    } finally {
      setLoading(false)
    }
  }, [])

  const loadPreview = useCallback(async (id: string) => {
    const item = await peripherals.getHairPreview(id)
    setPreview(item)
    if (item.style_id) setStyleId(item.style_id)
    return item
  }, [])

  // 恢复：优先问服务端的进行中端点（最准）；它暂时不可用才退回列表里找
  // （无参列表的语义是返回全部，含进行中）。都没有就不装任务态。
  const resume = useCallback(async () => {
    const adopt = (item: HairPreview) => {
      setPreview(item)
      if (item.style_id) setStyleId(item.style_id)
    }
    try {
      const active = await peripherals.getActiveHairPreview()
      // 404 已归一成 null：明确「没有进行中」，不再往列表里翻
      if (active) adopt(active)
      return
    } catch {
      /* 端点异常：退回列表恢复 */
    }
    try {
      const history = await peripherals.listHairPreviews()
      const active = history.find((item) => IN_FLIGHT.has(item.state))
      if (active) adopt(active)
    } catch {
      /* 无历史：保持新任务态 */
    }
  }, [])

  useEffect(() => {
    void loadOptions()
    void resume()
  }, [loadOptions, resume])

  useShowOnce(() => {
    void resume()
  })

  // 受理后的状态只通过公开 Operation 观察；终态后拉一次完整预览
  const running = Boolean(preview && IN_FLIGHT.has(preview.state))
  useOperationPolling({
    operationIds: preview?.operation?.id ? [preview.operation.id] : [],
    enabled: running,
    onSettled: () => {
      if (!preview) return
      void loadPreview(preview.id).then((item) => {
        if (item.state === 'ready' && item.media) setMode('result')
      })
    },
  })

  const choosePhoto = () => {
    Taro.chooseMedia({
      count: 1,
      mediaType: ['image'],
      sourceType: ['camera', 'album'],
      camera: 'front',
      sizeType: ['compressed'],
      success: (res) => {
        const file = res.tempFiles[0]
        if (file) setPendingPath(file.tempFilePath)
      },
    })
  }

  const [pendingPath, setPendingPath] = useState('')

  const generate = async (demo = false) => {
    if (busy) return
    setBusy(true)
    try {
      let mediaId: string
      if (demo) {
        const media = await qualityApi.createDemoMedia('face', `hair-demo:${Date.now()}`)
        mediaId = media.asset_id
      } else {
        if (!pendingPath) {
          Taro.showToast({ title: '先上传一张正脸照', icon: 'none' })
          return
        }
        const image = await readLocalImage(pendingPath)
        mediaId = (await uploadMedia(mediaUpload, image, 'face')).id
      }
      const accepted = await peripherals.createHairPreview({ media_id: mediaId, style_id: styleId })
      resourceCache.write(resourceKey('operation', accepted.operation.id), accepted.operation)
      setPreview(accepted.data)
      setMode('source')
    } catch (e) {
      if (handleBillingError(e)) return
      Taro.showToast({ title: (e as Error).message || '生成没有开始，请重试', icon: 'none' })
    } finally {
      setBusy(false)
    }
  }

  const save = async () => {
    if (!preview) return
    try {
      await peripherals.saveHairPreview(preview.id)
      Taro.showToast({ title: '已保存', icon: 'success' })
    } catch (e) {
      Taro.showToast({ title: (e as Error).message || '保存没有成功', icon: 'none' })
    }
  }

  const styleName = preview?.style_name || STYLES.find((s) => s.id === styleId)?.name || ''
  const hasResult = preview?.state === 'ready' && Boolean(preview.media)
  const generating = running

  return (
    <View className={pageClass}>
      <AppHeader title="发型预览" back />
      <View className="hair">
        <View className={`hair__hero photo-hero photo-hero--bleed ${enter()}`}>
          <View className="hair__hero-frame">
            {mode === 'result' && hasResult ? (
              <SourceImage className="hair__hero-img" media={preview!.media} mode="aspectFit" anchor="top" />
            ) : preview?.source_media ? (
              <SourceImage className="hair__hero-img" media={preview.source_media} mode="aspectFit" anchor="top" />
            ) : (
              <SourceImage
                className="hair__hero-img"
                reference={{ slug: (LOCAL_LOOK_SLUGS as readonly string[]).includes(styleId) ? styleId : 'sharp', variant: 'hair' }}
                anchor="top"
              />
            )}
            {mode === 'result' && hasResult ? (
              <View className="hair__badge layer-on-photo">
                <Text>{IMAGE_BADGE_COPY.bundled}</Text>
              </View>
            ) : null}
            {preview && IN_FLIGHT.has(preview.state) ? (
              <View className="hair__mask">
                <View className="scan-sweep" />
                <View className="hair__mask-spin spinner" />
                <Text className="hair__mask-text">正在生成预览</Text>
              </View>
            ) : null}
            {mode === 'result' && hasResult && preview?.source_media ? (
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
          <Text className="hair__hint-desc">{styles.find((o) => o.id === styleId)?.reason || '基于你的正脸照生成，发型轮廓与发色可实时对比。'}</Text>
        </View>

        <ScrollView className={`hair__styles ${enter(2)}`} scrollX enhanced showScrollbar={false}>
          {(styles.length > 0
            ? styles
            : STYLES.map((s) => ({ id: s.id, name: s.name, media: undefined, reason: undefined } as unknown as HairStyle))
          ).map((opt) => (
            <View
              key={opt.id}
              className={`hair__style ${styleId === opt.id ? 'hair__style--active' : ''} pressable`}
              onClick={() => setStyleId(opt.id)}
            >
              {opt.media ? (
                <SourceImage className="hair__style-img" media={opt.media} anchor="top" />
              ) : (
                <SourceImage
                  className="hair__style-img"
                  reference={{ slug: (LOCAL_LOOK_SLUGS as readonly string[]).includes(opt.id) ? opt.id : 'sharp', variant: 'hair' }}
                  anchor="top"
                />
              )}
              <Text className="hair__style-name">{opt.name}</Text>
            </View>
          ))}
        </ScrollView>

        {preview?.state === 'failed' || preview?.state === 'unavailable' ? (
          <View className={`hair__failed ${enter()}`}>
            <Text className="hair__failed-text">生成没有完成，请重试</Text>
          </View>
        ) : null}

        <View className={`hair__foot ${enter(3)}`}>
          {hasResult ? (
            <>
              <PrimaryButton text={`保存「${styleName}」`} onClick={() => void save()} />
              <Text className="hair__foot-alt pressable" onClick={() => setPreview(null)}>
                换一个方向再试
              </Text>
            </>
          ) : (
            <>
              <PrimaryButton
                text={generating ? '正在生成…' : `生成「${styleName}」预览`}
                loading={busy || generating}
                onClick={() => void generate(false)}
              />
              <View className="hair__foot-row">
                <Text className="hair__foot-alt pressable" onClick={choosePhoto}>
                  {pendingPath ? '重选正脸照' : '选择正脸照'}
                </Text>
                <Text className="hair__foot-alt pressable" onClick={() => void generate(true)}>
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
