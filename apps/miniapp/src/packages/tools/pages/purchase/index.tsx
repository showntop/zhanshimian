// 购买判断：商品图上传 + 同步诊断（结论/搭配建议/适合与注意）+ 保存。
// 视觉向发型预览页看齐：单品 hero 先行 → 图下 serif 导语 → 单张白卡装结论。
import { useState } from 'react'
import Taro from '@tarojs/taro'
import { Image, Text, View } from '@tarojs/components'
import { FINDING_TONE_COPY, PURCHASE_COPY, userImage, type Diagnosis } from '@zsm/core'
import { api } from '../../../../services/api'
import AppHeader from '../../../../components/app-header'
import PrimaryButton from '../../../../components/primary-button'
import ExampleImage from '../../../../components/example-image'
import ErrorState from '../../../../components/error-state'
import './index.scss'

// 照片渲染高落在 [NATIVE_MIN_H, NATIVE_MAX_H] 时用 widthFix 原生铺满
// （相框高度跟随照片，零裁切），超范围回落定框 + 模糊衬底。
const HERO_W = 686
const NATIVE_MIN_H = 480
const NATIVE_MAX_H = 1080

export default function Purchase() {
  const [photoPath, setPhotoPath] = useState('')
  const [photoUrl, setPhotoUrl] = useState('')
  const [demoSlug, setDemoSlug] = useState('')
  const [result, setResult] = useState<Diagnosis | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  // 所选/示例照片真实宽高：决定是否可 widthFix 原生铺满
  const [photoDims, setPhotoDims] = useState<{ w: number; h: number } | null>(null)

  const choosePhoto = () => {
    Taro.chooseMedia({
      count: 1,
      mediaType: ['image'],
      sourceType: ['camera', 'album'],
      success: (res) => {
        const file = res.tempFiles[0]
        if (file) {
          if (file.width && file.height) setPhotoDims({ w: file.width, h: file.height })
          setPhotoPath(file.tempFilePath)
          setPhotoUrl('')
          setDemoSlug('')
          setResult(null)
        }
      },
    })
  }

  const readReportId = (): string | undefined => {
    try {
      return Taro.getStorageSync('zsm_report_id') || undefined
    } catch {
      return undefined
    }
  }

  const analyze = async (demo = false) => {
    setBusy(true)
    setError('')
    try {
      let mediaId: string
      if (demo) {
        mediaId = (await api.createDemoMedia('product')).id
        setPhotoDims(null)
        setDemoSlug('warm')
        setPhotoUrl('')
        setPhotoPath('')
      } else {
        if (!photoPath) {
          Taro.showToast({ title: '先上传商品图', icon: 'none' })
          return
        }
        const asset = await api.uploadMedia({ kind: 'product', filePath: photoPath })
        mediaId = asset.id
        setPhotoUrl(asset.url)
      }
      const item = await api.diagnose({ kind: 'purchase', media_id: mediaId, report_id: readReportId() })
      setResult(item)
    } catch (e) {
      setError((e as Error).message || '判断没有成功，请重试')
    } finally {
      setBusy(false)
    }
  }

  const save = async () => {
    if (!result) return
    try {
      await api.updateDiagnosis(result.id, { saved: true })
      Taro.showToast({ title: PURCHASE_COPY.saved, icon: 'success' })
    } catch (e) {
      Taro.showToast({ title: (e as Error).message || '保存没有成功', icon: 'none' })
    }
  }

  const shownUrl = userImage(photoUrl) || photoPath
  const renderedH = photoDims ? Math.round((HERO_W * photoDims.h) / photoDims.w) : 0
  const nativeFill = Boolean(photoDims) && renderedH >= NATIVE_MIN_H && renderedH <= NATIVE_MAX_H

  return (
    <View className="page">
      <AppHeader title="购买判断" back />
      <View className="pk">
        <View className="pk__hero fade-up" style={nativeFill ? { height: 'auto' } : undefined}>
          {demoSlug ? (
            nativeFill ? (
              <ExampleImage
                className="pk__hero-img pk__hero-img--full"
                slug="warm"
                variant="full"
                badgeText="效果示例"
                mode="widthFix"
                onLoad={(e) =>
                  setPhotoDims({ w: Number(e.detail.width), h: Number(e.detail.height) })
                }
              />
            ) : (
              <>
                <ExampleImage className="pk__upload-backdrop" slug="warm" variant="full" />
                <ExampleImage className="pk__hero-img pk__hero-img--fit" slug="warm" variant="full" badgeText="效果示例" mode="aspectFit" />
              </>
            )
          ) : shownUrl ? (
            nativeFill ? (
              <Image
                className="pk__hero-img pk__hero-img--full"
                src={shownUrl}
                mode="widthFix"
                onLoad={(e) =>
                  setPhotoDims({ w: Number(e.detail.width), h: Number(e.detail.height) })
                }
              />
            ) : (
              <>
                <Image className="pk__photo-backdrop" src={shownUrl} mode="aspectFill" />
                <Image
                  className="pk__hero-img pk__hero-img--fit"
                  src={shownUrl}
                  mode="aspectFit"
                  onLoad={(e) =>
                    setPhotoDims({ w: Number(e.detail.width), h: Number(e.detail.height) })
                  }
                />
              </>
            )
          ) : (
            <View className="pk__upload pressable" onClick={choosePhoto}>
              <ExampleImage className="pk__upload-backdrop" slug="warm" variant="full" />
              <ExampleImage className="pk__hero-img" slug="warm" variant="full" mode="aspectFit" />
              <View className="pk__upload-bar">
                <Text className="pk__upload-bar-plus">＋</Text>
                <Text className="pk__upload-bar-text">{PURCHASE_COPY.uploadTitle}</Text>
              </View>
            </View>
          )}
          {shownUrl || demoSlug ? (
            <View className="pk__hero-alt-wrap">
              <Text className="pk__hero-alt pressable" onClick={choosePhoto}>{PURCHASE_COPY.reselect}</Text>
            </View>
          ) : null}
        </View>

        {/* 图下导语：与发型预览页同一位置/同一字阶 */}
        <View className="pk__hint fade-up delay-1">
          <Text className="pk__hint-title">{PURCHASE_COPY.title}</Text>
          <Text className="pk__hint-desc">{PURCHASE_COPY.desc}</Text>
          {!shownUrl && !demoSlug ? (
            <Text className="pk__hint-tips">{PURCHASE_COPY.uploadTips.join(' · ')}</Text>
          ) : null}
        </View>

        {error ? <ErrorState message={error} onRetry={() => analyze(false)} /> : null}

        {result ? (
          <View className="pk__result fade-up delay-2">
            <View className="pk__card">
              <Text className="pk__result-kicker">{PURCHASE_COPY.resultKicker}</Text>
              <Text className="pk__conclusion-text display">{result.conclusion}</Text>
              {result.tags.length > 0 ? (
                <View className="pk__tags">
                  {result.tags.map((tag) => (
                    <Text key={tag} className="pk__tag">{tag}</Text>
                  ))}
                </View>
              ) : null}

              <View className="pk__priority">
                <Text className="pk__priority-kicker">{PURCHASE_COPY.priorityKicker}</Text>
                <Text className="pk__priority-title">{result.priority_title}</Text>
                <Text className="pk__priority-copy">{result.priority_copy}</Text>
              </View>

              {(result.findings ?? []).length > 0 ? (
                <View className="pk__findings">
                  <Text className="pk__findings-title">{PURCHASE_COPY.findingsTitle}</Text>
                  {result.findings.map((finding) => (
                    <View key={`${finding.category}-${finding.label}`} className="pk__finding">
                      <Text className={`pk__finding-tone pk__finding-tone--${finding.tone || 'optional'}`}>
                        {FINDING_TONE_COPY[finding.tone] ?? FINDING_TONE_COPY.optional}
                      </Text>
                      <Text className="pk__finding-label">{finding.label}</Text>
                    </View>
                  ))}
                </View>
              ) : null}
            </View>

            <View className="pk__result-actions">
              <PrimaryButton text={PURCHASE_COPY.save} onClick={save} />
            </View>
          </View>
        ) : (
          <View className="pk__foot fade-up delay-2">
            <PrimaryButton text={busy ? PURCHASE_COPY.busy : PURCHASE_COPY.start} loading={busy} disabled={!photoPath} onClick={() => analyze(false)} />
            <View className="pk__foot-row">
              <Text className="pk__foot-alt pressable" onClick={choosePhoto}>{PURCHASE_COPY.choosePhoto}</Text>
              <Text className="pk__foot-alt pressable" onClick={() => analyze(true)}>{PURCHASE_COPY.demo}</Text>
            </View>
            <Text className="pk__foot-note">{PURCHASE_COPY.note}</Text>
          </View>
        )}

        {!result ? (
          <View className="pk__preview fade-up delay-3">
            <Text className="pk__preview-title">{PURCHASE_COPY.previewTitle}</Text>
            {PURCHASE_COPY.previewItems.map((item, index) => (
              <View key={item.title} className="pk__preview-row">
                <Text className="pk__preview-index">{index + 1}</Text>
                <View className="pk__preview-copy">
                  <Text className="pk__preview-name">{item.title}</Text>
                  <Text className="pk__preview-desc">{item.desc}</Text>
                </View>
              </View>
            ))}
          </View>
        ) : null}
      </View>
    </View>
  )
}
