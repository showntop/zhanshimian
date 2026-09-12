// 穿搭诊断：场景 3 选 + 单图上传 + 同步诊断（锚点标注）+ 先改哪一处。
// 空态：照片 hero → 导语 → 场景。结果：照片 → 一句建议 → 观察清单，不套报告卡。
// 同步请求会跨页存活：模块级 Promise + 本地草稿，退回首页再进入可恢复进行中/结论。
import { useCallback, useEffect, useRef, useState } from 'react'
import Taro from '@tarojs/taro'
import { Image, Text, View } from '@tarojs/components'
import { OUTFIT_COPY, userImage, type Diagnosis } from '@zsm/core'
import { usePageShell, useShowOnce } from '../../../../hooks/use-page-visibility'
import { api } from '../../../../services/api'
import { handleBillingError } from '../../../../services/billing'
import {
  clearOutfitResult,
  getOutfitInflight,
  markOutfitDone,
  markOutfitPending,
  readOutfitSession,
  runOutfitDiagnose,
  writeOutfitDraft,
} from '../../../../services/outfit-session'
import { splitAdviceTitle } from '../../../../services/advice-title'
import { STORAGE_KEYS, readStorage } from '../../../../services/storage'
import AppHeader from '../../../../components/app-header'
import PrimaryButton from '../../../../components/primary-button'
import ExampleImage from '../../../../components/example-image'
import PhotoAnnotationLayer from '../../../../components/photo-annotation'
import Pill from '../../../../components/pill'
import ErrorState from '../../../../components/error-state'
import './index.scss'

// 空态 hero 的拍照示范图（包内资产，JPEG；人物为内置模特，叠「拍照示范」角标）
const OUTFIT_GUIDE_IMAGE = '/assets/capture/outfit-guide.jpg'

// hero 相框比例（rpx，与 index.scss 一致）：锚点按 aspectFit 可视区重映射。
// 照片渲染高落在 [NATIVE_MIN_H, NATIVE_MAX_H] 时改用 widthFix 原生铺满
// （相框高度跟随照片，零裁切），超范围才回落定框 + 模糊衬底。
const HERO_W = 686
const HERO_H = 640
const NATIVE_MIN_H = 480
const NATIVE_MAX_H = 1080

const CONTEXTS = [
  { key: 'daily', label: '日常' },
  { key: 'interview', label: '面试' },
  { key: 'date', label: '约会' },
] as const

export default function Outfit() {
  const [scene, setScene] = useState<string>('daily')
  const [photoPath, setPhotoPath] = useState('')
  const [photoUrl, setPhotoUrl] = useState('')
  const [demoSlug, setDemoSlug] = useState('')
  const [result, setResult] = useState<Diagnosis | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  // 所选照片真实宽高（onLoad 采集）：aspectFit 可视区锚点换算
  const [photoDims, setPhotoDims] = useState<{ w: number; h: number } | null>(null)

  const resumingRef = useRef(false)
  // 本页点了「再诊断一次」或重选照片：不要立刻用服务端旧结论盖回去
  const freshStartRef = useRef(false)
  const { pageClass, enter } = usePageShell(true, '', 'outfit')

  const applySession = useCallback((item: Diagnosis) => {
    setResult(item)
    if (item.scene) setScene(item.scene)
    if (item.image_url) setPhotoUrl(item.image_url)
    if ((item.provider_version ?? '').startsWith('demo')) setDemoSlug((prev) => prev || 'natural')
  }, [])

  const hydrateDraft = (stored: ReturnType<typeof readOutfitSession>) => {
    if (!stored) return
    if (stored.scene) setScene(stored.scene)
    if (stored.photoPath) setPhotoPath(stored.photoPath)
    if (stored.photoUrl) setPhotoUrl(stored.photoUrl)
    if (stored.demoSlug) setDemoSlug(stored.demoSlug)
  }

  const resume = useCallback(async () => {
    if (resumingRef.current) return
    resumingRef.current = true
    try {
      const stored = readOutfitSession()
      hydrateDraft(stored)
      if (stored?.result) applySession(stored.result)

      const pending = getOutfitInflight()
      if (pending) {
        setBusy(true)
        setError('')
        try {
          applySession(await pending)
        } catch (e) {
          setError((e as Error).message || '诊断没有成功，请重试')
        } finally {
          setBusy(false)
        }
        return
      }

      if (stored?.pending && stored.mediaId) {
        setBusy(true)
        setError('')
        try {
          try {
            const latest = await api.getLatestDiagnosis('outfit')
            const startedAt = stored.startedAt || 0
            const recentEnough = new Date(latest.created_at).getTime() >= startedAt - 5000
            const sameMedia = !stored.mediaId || !latest.media_id || stored.mediaId === latest.media_id
            if (recentEnough && sameMedia) {
              markOutfitDone(latest)
              applySession(latest)
              return
            }
          } catch {
            /* 服务端还没有这条结论：用同一张照片续跑诊断 */
          }
          const item = await runOutfitDiagnose(() =>
            api.diagnose({
              kind: 'outfit',
              media_id: stored.mediaId,
              scene: stored.scene || scene,
              report_id: readStorage(STORAGE_KEYS.reportId) || undefined,
            }),
          )
          applySession(item)
        } catch (e) {
          setError((e as Error).message || '诊断没有成功，请重试')
        } finally {
          setBusy(false)
        }
        return
      }

      // 本地已有结论就不再打 GET /diagnostics/latest。
      // 线上旧进程只有 PATCH /v1/diagnostics/{id}，GET /latest 会被当成 {id} 回 405。
      if (stored?.result || freshStartRef.current) return

      try {
        const latest = await api.getLatestDiagnosis('outfit')
        markOutfitDone(latest)
        applySession(latest)
      } catch {
        /* 旧服务 405 / 还没有诊断过：保持开始页 */
      }
    } finally {
      resumingRef.current = false
    }
  }, [applySession, scene])

  useEffect(() => {
    resume()
  }, [resume])

  useShowOnce(() => {
    resume()
  })

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
          setPhotoDims(null)
          freshStartRef.current = true
          writeOutfitDraft({
            pending: false,
            scene,
            photoPath: file.tempFilePath,
            photoUrl: '',
            demoSlug: '',
            mediaId: '',
            result: null,
          })
        }
      },
    })
  }

  const analyze = async (demo = false) => {
    if (getOutfitInflight() || busy) return
    setBusy(true)
    setError('')
    try {
      let mediaId: string
      let nextDemo = ''
      let nextUrl = photoUrl
      let nextPath = photoPath
      if (demo) {
        const asset = await api.createDemoMedia('outfit')
        mediaId = asset.id
        nextDemo = 'natural'
        nextUrl = asset.url
        nextPath = ''
        setPhotoDims(null)
        setDemoSlug(nextDemo)
        setPhotoUrl(nextUrl)
        setPhotoPath('')
      } else {
        if (!photoPath && !photoUrl) {
          Taro.showToast({ title: '先上传一张全身照', icon: 'none' })
          return
        }
        if (photoPath) {
          const asset = await api.uploadMedia({ kind: 'outfit', filePath: photoPath })
          mediaId = asset.id
          nextUrl = asset.url
          setPhotoUrl(asset.url)
        } else {
          const stored = readOutfitSession()
          if (!stored?.mediaId) {
            Taro.showToast({ title: '先上传一张全身照', icon: 'none' })
            return
          }
          mediaId = stored.mediaId
        }
      }
      markOutfitPending({
        scene,
        photoPath: nextPath,
        photoUrl: nextUrl,
        demoSlug: nextDemo,
        mediaId,
      })
      const item = await runOutfitDiagnose(() =>
        api.diagnose({ kind: 'outfit', media_id: mediaId, scene, report_id: readReportId() }),
      )
      applySession(item)
    } catch (e) {
      handleBillingError(e)
      setError((e as Error).message || '诊断没有成功，请重试')
    } finally {
      setBusy(false)
    }
  }

  const readReportId = () => {
    return readStorage(STORAGE_KEYS.reportId) || undefined
  }

  const saveResult = async () => {
    if (!result) return
    try {
      await api.updateDiagnosis(result.id, { saved: true })
      Taro.showToast({ title: OUTFIT_COPY.saved, icon: 'success' })
    } catch (e) {
      Taro.showToast({ title: (e as Error).message || '保存没有成功', icon: 'none' })
    }
  }

  const toPlans = () => Taro.switchTab({ url: '/pages/plans/index' })

  const retryFresh = () => {
    freshStartRef.current = true
    clearOutfitResult()
    setResult(null)
    setError('')
  }

  const isDemo = Boolean(demoSlug) || (result?.provider_version ?? '').startsWith('demo')
  const shownUrl = userImage(photoUrl) || userImage(result?.image_url) || photoPath
  const adviceTitle = result?.priority_title || result?.conclusion || OUTFIT_COPY.title
  const { lead: adviceLead, action: adviceAction } = splitAdviceTitle(adviceTitle)
  const adviceBody = result
    ? result.priority_copy || (result.priority_title ? result.conclusion : '')
    : ''
  const keepFindings = (result?.findings ?? []).filter((item) => item.tone === 'positive')
  const liftFindings = (result?.findings ?? []).filter((item) => item.tone !== 'positive')

  // widthFix 原生铺满：相框 = 图框，锚点直接百分比；仅比例适中时启用
  const renderedH = photoDims ? Math.round((HERO_W * photoDims.h) / photoDims.w) : 0
  const nativeFill = Boolean(photoDims) && renderedH >= NATIVE_MIN_H && renderedH <= NATIVE_MAX_H

  return (
    <View className={pageClass}>
      <AppHeader title="穿搭诊断" back />
      <View className={`od${result ? ' od--done' : ''}`}>
        <View className={`od__hero photo-hero photo-hero--bleed ${enter()}`} style={nativeFill ? { height: 'auto' } : undefined}>
          {demoSlug ? (
            nativeFill ? (
              <ExampleImage
                className="od__hero-img od__hero-img--full"
                slug="natural"
                variant="full"
                badgeText="效果示例"
                mode="widthFix"
                onLoad={(e) =>
                  setPhotoDims({ w: Number(e.detail.width), h: Number(e.detail.height) })
                }
              />
            ) : (
              <>
                <ExampleImage className="od__upload-backdrop" slug="natural" variant="full" />
                <ExampleImage className="od__hero-img od__hero-img--fit" slug="natural" variant="full" badgeText="效果示例" mode="aspectFit" />
              </>
            )
          ) : shownUrl ? (
            nativeFill ? (
              <Image
                className="od__hero-img od__hero-img--full"
                src={shownUrl}
                mode="widthFix"
                onLoad={(e) =>
                  setPhotoDims({ w: Number(e.detail.width), h: Number(e.detail.height) })
                }
              />
            ) : (
              <>
                <Image className="od__photo-backdrop" src={shownUrl} mode="aspectFill" />
                <Image
                  className="od__hero-img od__hero-img--fit"
                  src={shownUrl}
                  mode="aspectFit"
                  onLoad={(e) =>
                    setPhotoDims({ w: Number(e.detail.width), h: Number(e.detail.height) })
                  }
                />
              </>
            )
          ) : (
            <View className="od__upload pressable" onClick={choosePhoto}>
              <ExampleImage className="od__upload-backdrop" src={OUTFIT_GUIDE_IMAGE} />
              <ExampleImage className="od__hero-img" src={OUTFIT_GUIDE_IMAGE} badgeText="拍照示范" mode="aspectFit" />
              <View className="od__upload-bar">
                <Text className="od__upload-bar-plus">＋</Text>
                <Text className="od__upload-bar-text">{OUTFIT_COPY.uploadTitle}</Text>
              </View>
            </View>
          )}
          {shownUrl || demoSlug ? (
            <View className="od__hero-actions">
              <Text className="od__hero-alt pressable" onClick={choosePhoto}>{OUTFIT_COPY.reselect}</Text>
            </View>
          ) : null}
          {busy ? (
            <View className="od__mask">
              <View className="od__mask-spin spinner" />
              <Text className="od__mask-text">{OUTFIT_COPY.busyHint}</Text>
            </View>
          ) : null}
          {!busy && result?.findings ? (
            <PhotoAnnotationLayer
              items={result.findings
                .filter((finding) => finding.anchor_x != null && finding.anchor_y != null)
                .slice(0, 1)
                .map((finding) => ({
                  id: `${finding.category}-${finding.label}`,
                  label: finding.label,
                  detail: finding.label,
                  anchorX: finding.anchor_x ?? 0.5,
                  anchorY: finding.anchor_y ?? 0.5,
                }))}
              activeId=""
              frameW={HERO_W}
              frameH={nativeFill && renderedH ? renderedH : HERO_H}
              photoDims={photoDims ?? undefined}
              onTap={() => {}}
              showDrawer={false}
            />
          ) : null}
          {isDemo && !busy ? (
            <View className="od__badge">
              <Text>效果示例</Text>
            </View>
          ) : null}
        </View>

        {result ? (
          <View className={`od__sheet ${enter(1)}`}>
            <View className="od__advice">
              {adviceLead ? <Text className="od__advice-lead">{adviceLead}</Text> : null}
              <Text className="od__advice-title">{adviceAction}</Text>
              {adviceBody ? <Text className="od__advice-body">{adviceBody}</Text> : null}
            </View>

            {keepFindings.length > 0 ? (
              <View className="od__keep">
                <Text className="od__keep-label">{OUTFIT_COPY.findingsKeep}</Text>
                <Text className="od__keep-text">{keepFindings.map((item) => item.label).join('、')}</Text>
              </View>
            ) : null}

            {liftFindings.length > 0 ? (
              <View className="od__lifts">
                <Text className="od__lifts-label">{OUTFIT_COPY.findingsLift}</Text>
                {liftFindings.map((finding) => (
                  <Text key={`${finding.category}-${finding.label}`} className="od__lift">
                    {finding.label}
                  </Text>
                ))}
              </View>
            ) : null}

            {error ? <ErrorState message={error} onRetry={() => analyze(false)} /> : null}

            <View className="od__result-actions">
              <PrimaryButton text={OUTFIT_COPY.toPlans} onClick={toPlans} />
              <View className="od__result-row">
                <Text className="od__result-alt pressable" onClick={saveResult}>{OUTFIT_COPY.save}</Text>
                <Text className="od__result-alt pressable" onClick={retryFresh}>{OUTFIT_COPY.again}</Text>
              </View>
            </View>
          </View>
        ) : (
          <>
            <View className={`od__hint ${enter(1)}`}>
              <Text className="od__hint-title">{OUTFIT_COPY.title}</Text>
              <Text className="od__hint-desc">{OUTFIT_COPY.desc}</Text>
              {!shownUrl && !demoSlug ? (
                <Text className="od__hint-tips">{OUTFIT_COPY.uploadTips.join(' · ')}</Text>
              ) : null}
            </View>
            <View className={`od__context ${enter(2)}`}>
              <Text className="od__context-label">{OUTFIT_COPY.sceneLabel}</Text>
              <View className="od__context-pills">
                {CONTEXTS.map((c) => (
                  <Pill key={c.key} label={c.label} active={scene === c.key} onClick={() => !busy && setScene(c.key)} />
                ))}
              </View>
            </View>
            {error ? <ErrorState message={error} onRetry={() => analyze(false)} /> : null}
          </>
        )}

        {!result ? (
          <View className={`od__foot ${enter(3)}`}>
            <PrimaryButton
              text={busy ? OUTFIT_COPY.busy : OUTFIT_COPY.start}
              loading={busy}
              disabled={!photoPath && !photoUrl && !demoSlug}
              onClick={() => analyze(false)}
            />
            <View className="od__foot-row">
              <Text className="od__foot-alt pressable" onClick={() => analyze(true)}>{OUTFIT_COPY.demo}</Text>
            </View>
          </View>
        ) : null}
      </View>
    </View>
  )
}
