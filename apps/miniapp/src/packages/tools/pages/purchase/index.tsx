// 购买判断：单品图上传 + 同步诊断。空态不堆预览卡；结果是一句判断，不套报告卡。
// 同步请求会跨页存活：模块级 Promise + 本地草稿，退回首页再进入可恢复进行中/结论。
import { useCallback, useEffect, useRef, useState } from 'react'
import Taro from '@tarojs/taro'
import { Image, Text, View } from '@tarojs/components'
import { PURCHASE_COPY, userImage, type Diagnosis } from '@zsm/core'
import { usePageClass, useShowOnce } from '../../../../hooks/use-page-visibility'
import { api } from '../../../../services/api'
import {
  clearPurchaseResult,
  getPurchaseInflight,
  markPurchaseDone,
  markPurchasePending,
  readPurchaseSession,
  runPurchaseDiagnose,
  writePurchaseDraft,
} from '../../../../services/purchase-session'
import { splitAdviceTitle } from '../../../../services/advice-title'
import { STORAGE_KEYS, readStorage } from '../../../../services/storage'
import AppHeader from '../../../../components/app-header'
import PrimaryButton from '../../../../components/primary-button'
import ExampleImage from '../../../../components/example-image'
import ErrorState from '../../../../components/error-state'
import './index.scss'

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
  const [photoDims, setPhotoDims] = useState<{ w: number; h: number } | null>(null)

  const resumingRef = useRef(false)
  const freshStartRef = useRef(false)
  const pageClass = usePageClass(true)

  const applySession = useCallback((item: Diagnosis) => {
    setResult(item)
    if (item.image_url) setPhotoUrl(item.image_url)
    if ((item.provider_version ?? '').startsWith('demo')) setDemoSlug((prev) => prev || 'warm')
  }, [])

  const hydrateDraft = (stored: ReturnType<typeof readPurchaseSession>) => {
    if (!stored) return
    if (stored.photoPath) setPhotoPath(stored.photoPath)
    if (stored.photoUrl) setPhotoUrl(stored.photoUrl)
    if (stored.demoSlug) setDemoSlug(stored.demoSlug)
  }

  const resume = useCallback(async () => {
    if (resumingRef.current) return
    resumingRef.current = true
    try {
      const stored = readPurchaseSession()
      hydrateDraft(stored)
      if (stored?.result) applySession(stored.result)

      const pending = getPurchaseInflight()
      if (pending) {
        setBusy(true)
        setError('')
        try {
          applySession(await pending)
        } catch (e) {
          setError((e as Error).message || '判断没有成功，请重试')
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
            const latest = await api.getLatestDiagnosis('purchase')
            const startedAt = stored.startedAt || 0
            const recentEnough = new Date(latest.created_at).getTime() >= startedAt - 5000
            const sameMedia = !stored.mediaId || !latest.media_id || stored.mediaId === latest.media_id
            if (recentEnough && sameMedia) {
              markPurchaseDone(latest)
              applySession(latest)
              return
            }
          } catch {
            /* 服务端还没有这条结论：用同一张图续跑判断 */
          }
          const item = await runPurchaseDiagnose(() =>
            api.diagnose({
              kind: 'purchase',
              media_id: stored.mediaId,
              report_id: readStorage(STORAGE_KEYS.reportId) || undefined,
            }),
          )
          applySession(item)
        } catch (e) {
          setError((e as Error).message || '判断没有成功，请重试')
        } finally {
          setBusy(false)
        }
        return
      }

      if (stored?.result || freshStartRef.current) return

      try {
        const latest = await api.getLatestDiagnosis('purchase')
        markPurchaseDone(latest)
        applySession(latest)
      } catch {
        /* 旧服务 405 / 还没有判断过：保持开始页 */
      }
    } finally {
      resumingRef.current = false
    }
  }, [applySession])

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
          writePurchaseDraft({
            pending: false,
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
    if (getPurchaseInflight() || busy) return
    setBusy(true)
    setError('')
    try {
      let mediaId: string
      let nextDemo = ''
      let nextUrl = photoUrl
      let nextPath = photoPath
      if (demo) {
        const asset = await api.createDemoMedia('product')
        mediaId = asset.id
        nextDemo = 'warm'
        nextUrl = asset.url
        nextPath = ''
        setPhotoDims(null)
        setDemoSlug(nextDemo)
        setPhotoUrl(nextUrl)
        setPhotoPath('')
      } else {
        if (!photoPath && !photoUrl) {
          Taro.showToast({ title: '先上传商品图', icon: 'none' })
          return
        }
        if (photoPath) {
          const asset = await api.uploadMedia({ kind: 'product', filePath: photoPath })
          mediaId = asset.id
          nextUrl = asset.url
          setPhotoUrl(asset.url)
        } else {
          const stored = readPurchaseSession()
          if (!stored?.mediaId) {
            Taro.showToast({ title: '先上传商品图', icon: 'none' })
            return
          }
          mediaId = stored.mediaId
        }
      }
      markPurchasePending({
        photoPath: nextPath,
        photoUrl: nextUrl,
        demoSlug: nextDemo,
        mediaId,
      })
      const item = await runPurchaseDiagnose(() =>
        api.diagnose({
          kind: 'purchase',
          media_id: mediaId,
          report_id: readStorage(STORAGE_KEYS.reportId) || undefined,
        }),
      )
      applySession(item)
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

  const retryFresh = () => {
    freshStartRef.current = true
    clearPurchaseResult()
    setResult(null)
    setError('')
  }

  const isDemo = Boolean(demoSlug) || (result?.provider_version ?? '').startsWith('demo')
  const shownUrl = userImage(photoUrl) || userImage(result?.image_url) || photoPath
  const adviceTitle = result?.priority_title || result?.conclusion || PURCHASE_COPY.title
  const { lead: adviceLead, action: adviceAction } = splitAdviceTitle(adviceTitle)
  const adviceBody = result
    ? result.priority_copy || (result.priority_title ? result.conclusion : '')
    : ''
  const keepFindings = (result?.findings ?? []).filter((item) => item.tone === 'positive')
  const liftFindings = (result?.findings ?? []).filter((item) => item.tone !== 'positive')
  const renderedH = photoDims ? Math.round((HERO_W * photoDims.h) / photoDims.w) : 0
  const nativeFill = Boolean(photoDims) && renderedH >= NATIVE_MIN_H && renderedH <= NATIVE_MAX_H

  return (
    <View className={pageClass}>
      <AppHeader title="购买判断" back />
      <View className={`pk${result ? ' pk--done' : ''}`}>
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
              <View className="pk__stage" />
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
          {busy ? (
            <View className="pk__mask">
              <View className="pk__mask-spin spinner" />
              <Text className="pk__mask-text">{PURCHASE_COPY.busyHint}</Text>
            </View>
          ) : null}
          {isDemo && !busy ? (
            <View className="pk__badge">
              <Text>效果示例</Text>
            </View>
          ) : null}
        </View>

        {result ? (
          <View className="pk__sheet fade-up delay-1">
            <View className="pk__advice">
              {adviceLead ? <Text className="pk__advice-lead">{adviceLead}</Text> : null}
              <Text className="pk__advice-title">{adviceAction}</Text>
              {adviceBody ? <Text className="pk__advice-body">{adviceBody}</Text> : null}
            </View>

            {keepFindings.length > 0 ? (
              <View className="pk__keep">
                <Text className="pk__keep-label">{PURCHASE_COPY.findingsKeep}</Text>
                <Text className="pk__keep-text">{keepFindings.map((item) => item.label).join('、')}</Text>
              </View>
            ) : null}

            {liftFindings.length > 0 ? (
              <View className="pk__lifts">
                <Text className="pk__lifts-label">{PURCHASE_COPY.findingsLift}</Text>
                {liftFindings.map((finding) => (
                  <Text key={`${finding.category}-${finding.label}`} className="pk__lift">
                    {finding.label}
                  </Text>
                ))}
              </View>
            ) : null}

            {error ? <ErrorState message={error} onRetry={() => analyze(false)} /> : null}

            <View className="pk__result-actions">
              <PrimaryButton text={PURCHASE_COPY.save} onClick={save} />
              <Text className="pk__result-alt pressable" onClick={retryFresh}>{PURCHASE_COPY.again}</Text>
            </View>
          </View>
        ) : (
          <>
            <View className="pk__hint fade-up delay-1">
              <Text className="pk__hint-title">{PURCHASE_COPY.title}</Text>
              <Text className="pk__hint-desc">{PURCHASE_COPY.desc}</Text>
              {!shownUrl && !demoSlug ? (
                <Text className="pk__hint-tips">{PURCHASE_COPY.uploadTips.join(' · ')}</Text>
              ) : null}
            </View>
            {error ? <ErrorState message={error} onRetry={() => analyze(false)} /> : null}
            <View className="pk__foot fade-up delay-2">
              <PrimaryButton
                text={busy ? PURCHASE_COPY.busy : PURCHASE_COPY.start}
                loading={busy}
                disabled={!photoPath && !photoUrl && !demoSlug}
                onClick={() => analyze(false)}
              />
              <View className="pk__foot-row">
                <Text className="pk__foot-alt pressable" onClick={() => analyze(true)}>{PURCHASE_COPY.demo}</Text>
              </View>
            </View>
          </>
        )}
      </View>
    </View>
  )
}
