// 购买判断：单品图上传 + 同步诊断。空态不堆预览卡；结果是一句判断，不套报告卡。
// 同步请求跨页存活：会话见 services/purchase-session（模块级 inflight + storage 草稿），
// 退回首页再进入可恢复进行中/结论；复访先展示草稿，服务端 latest 只在更新时接管。
import { useCallback, useEffect, useRef, useState } from 'react'
import Taro from '@tarojs/taro'
import { Image, Text, View } from '@tarojs/components'
import { BILLING_COPY, PURCHASE_COPY, type Diagnosis, type DisplayMedia } from '@zsm/core'
import { usePageShell, useShowOnce } from '../../../../hooks/use-page-visibility'
import { peripherals } from '../../../../app/api/peripherals'
import { qualityApi } from '../../../../app/api/quality'
import { mediaUpload } from '../../../../app/api/client'
import { uploadMedia } from '../../../../app/api/media-upload'
import { readLocalImage } from '../../../../features/capture/local-file'
import { handleBillingError } from '../../../../services/billing'
import { billingErrorMessage } from '../../../../services/billing-error'
import {
  isDiagnosticDailyLimit,
  sharedPurchaseSession,
  type PurchaseResumeEvent,
} from '../../../../services/purchase-session'
import { readStorage, writeStorage } from '../../../../services/storage'
import { splitAdviceTitle } from '../../../../services/advice-title'
import AppHeader from '../../../../components/app-header'
import PrimaryButton from '../../../../components/primary-button'
import SourceImage from '../../../../components/source-image'
import ErrorState from '../../../../components/error-state'
import './index.scss'

// 会话单例挂在页面模块上：模块只装载一次，inflight 与草稿因此跨页存活。
const purchaseSession = sharedPurchaseSession({ read: readStorage, write: writeStorage })

export default function Purchase() {
  const [photoPath, setPhotoPath] = useState('')
  const [photoMedia, setPhotoMedia] = useState<DisplayMedia | null>(null)
  const [result, setResult] = useState<Diagnosis | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  // 日限（429/402）体面错误态：明日再来 + 查看上次结果，不进计费购买链
  const [limit, setLimit] = useState<{ title: string; body: string } | null>(null)

  const resumingRef = useRef(false)
  const freshStartRef = useRef(false)
  const { pageClass, enter } = usePageShell(true, '', 'purchase')

  const applySession = useCallback((item: Diagnosis) => {
    setResult(item)
    if (item.source_media) setPhotoMedia(item.source_media)
  }, [])

  // 日限与通用错误分流：日限进体面错误态（明日再来/查看上次结果），其余照旧。
  const applyError = useCallback((e: unknown, fallback: string) => {
    if (isDiagnosticDailyLimit(e)) {
      const serverMsg = billingErrorMessage(e)
      setLimit({
        title: serverMsg || BILLING_COPY.rateLimited,
        body: serverMsg && serverMsg !== BILLING_COPY.rateLimited ? BILLING_COPY.rateLimited : '',
      })
      setError('')
      return
    }
    handleBillingError(e)
    setError((e as Error)?.message || fallback)
  }, [])

  // 复访恢复：会话统一编排（草稿水合 → inflight 接管 → pending 续跑 → 后台校验），
  // 事件在这里翻译成 setState；时序与防旧盖新判定都在 services/purchase-session。
  const resume = useCallback(async () => {
    if (resumingRef.current) return
    resumingRef.current = true
    try {
      await purchaseSession.resume(
        {
          getLatest: () => peripherals.getLatestDiagnosis('purchase'),
          diagnose: peripherals.diagnose,
        },
        (event: PurchaseResumeEvent) => {
          if (event.type === 'hydrate') {
            const { draft } = event
            if (draft.photoPath) setPhotoPath(draft.photoPath)
            if (draft.result) applySession(draft.result)
            return
          }
          if (event.type === 'busy') {
            setBusy(event.busy)
            if (event.busy) {
              setError('')
              setLimit(null)
            }
            return
          }
          if (event.type === 'result') {
            applySession(event.item)
            return
          }
          applyError(event.error, '判断没有成功，请重试')
        },
        { freshStart: () => freshStartRef.current },
      )
    } finally {
      resumingRef.current = false
    }
  }, [applySession, applyError])

  useEffect(() => {
    void resume()
  }, [resume])

  useShowOnce(() => {
    void resume()
  })

  const choosePhoto = () => {
    Taro.chooseMedia({
      count: 1,
      mediaType: ['image'],
      sizeType: ['compressed'],
      success: (res) => {
        const file = res.tempFiles[0]
        if (file) {
          setPhotoPath(file.tempFilePath)
          setPhotoMedia(null)
          setResult(null)
          setError('')
          setLimit(null)
          freshStartRef.current = true
          // 草稿同步换成新图：进行中的旧请求落定时就不会再盖回来
          purchaseSession.writeDraft({
            pending: false,
            photoPath: file.tempFilePath,
            mediaId: '',
            result: null,
          })
        }
      },
    })
  }

  const analyze = async (demo = false) => {
    // inflight 期间重复发起一律被会话挡下（busy 只是本页实例的第二道闸）
    if (purchaseSession.getInflight() || busy) return
    setBusy(true)
    setError('')
    setLimit(null)
    // 提到 try 外：catch 要用它判断「草稿是否已换图」
    let mediaId = ''
    try {
      if (demo) {
        const media = await qualityApi.createDemoMedia('body', `purchase-demo:${Date.now()}`)
        mediaId = media.asset_id
        setPhotoMedia(media)
        setPhotoPath('')
      } else {
        if (!photoPath && !photoMedia) {
          Taro.showToast({ title: '先上传商品图', icon: 'none' })
          return
        }
        if (photoPath) {
          const image = await readLocalImage(photoPath)
          mediaId = (await uploadMedia(mediaUpload, image, 'wardrobe')).id
        } else {
          mediaId = photoMedia!.asset_id
        }
      }
      purchaseSession.markPending({ photoPath: demo ? '' : photoPath, mediaId })
      const item = await purchaseSession.runDiagnose(mediaId, () =>
        peripherals.diagnose({ kind: 'purchase', media_id: mediaId }),
      )
      // 飞行中重选了图：旧结论已被会话拒收，这里也不上屏
      if (purchaseSession.read()?.result?.id === item.id) applySession(item)
    } catch (e) {
      // mediaId 为空说明上传/示例图就没走通，错误照常展示；非空但草稿已换图则静默
      if (mediaId && purchaseSession.read()?.mediaId !== mediaId) return
      applyError(e, '判断没有成功，请重试')
    } finally {
      setBusy(false)
    }
  }

  const save = async () => {
    if (!result) return
    try {
      await peripherals.updateDiagnosis(result.id, { saved: true })
      Taro.showToast({ title: PURCHASE_COPY.saved, icon: 'success' })
    } catch (e) {
      Taro.showToast({ title: (e as Error).message || '保存没有成功', icon: 'none' })
    }
  }

  // 日限错误态的「查看上次结果」：历史进来就替换掉当前未完成的图片位
  const viewHistory = async () => {
    try {
      const latest = await peripherals.getLatestDiagnosis('purchase')
      if (!latest) return
      purchaseSession.writeDraft({ photoPath: '' })
      purchaseSession.markDone(latest)
      setPhotoPath('')
      applySession(latest)
      setLimit(null)
      setError('')
    } catch {
      /* 拉不到历史：保持日限错误态 */
    }
  }

  const retryFresh = () => {
    freshStartRef.current = true
    purchaseSession.clearResult()
    setResult(null)
    setError('')
    setLimit(null)
  }

  const shownMedia: DisplayMedia | null = photoMedia ?? result?.source_media ?? null
  const adviceTitle = result?.priority_title || result?.conclusion || PURCHASE_COPY.title
  const { lead: adviceLead, action: adviceAction } = splitAdviceTitle(adviceTitle)
  const adviceBody = result
    ? result.priority_copy || (result.priority_title ? result.conclusion : '')
    : ''
  const keepFindings = (result?.findings ?? []).filter((item) => item.tone === 'positive')
  const liftFindings = (result?.findings ?? []).filter((item) => item.tone !== 'positive')

  return (
    <View className={pageClass}>
      <AppHeader title="购买判断" back />
      <View className={`pk${result ? ' pk--done' : ''}`}>
        <View className={`pk__hero photo-hero photo-hero--bleed ${enter()}`}>
          {shownMedia ? (
            <>
              <SourceImage className="pk__photo-backdrop" media={shownMedia} mode="aspectFill" />
              <SourceImage
                className="pk__hero-img pk__hero-img--fit"
                media={shownMedia}
                mode="aspectFit"
              />
            </>
          ) : photoPath ? (
            <Image
              className="pk__hero-img pk__hero-img--fit"
              src={photoPath}
              mode="aspectFit"
            />
          ) : (
            <View className="pk__upload pressable" onClick={choosePhoto}>
              <SourceImage className="pk__upload-backdrop" reference={{ slug: 'warm', variant: 'full' }} />
              <View className="pk__stage" />
              <SourceImage className="pk__hero-img" reference={{ slug: 'warm', variant: 'full' }} mode="aspectFit" />
              <View className="pk__upload-bar">
                <Text className="pk__upload-bar-plus">＋</Text>
                <Text className="pk__upload-bar-text">{PURCHASE_COPY.uploadTitle}</Text>
              </View>
            </View>
          )}
          {shownMedia || photoPath ? (
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
        </View>

        {result ? (
          <View className={`pk__sheet ${enter(1)}`}>
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

            {error ? <ErrorState message={error} onRetry={() => void analyze(false)} /> : null}

            <View className="pk__result-actions">
              <PrimaryButton text={PURCHASE_COPY.save} onClick={() => void save()} />
              <Text className="pk__result-alt pressable" onClick={retryFresh}>{PURCHASE_COPY.again}</Text>
            </View>
          </View>
        ) : limit ? (
          <>
            <View className={`pk__hint ${enter(1)}`}>
              <Text className="pk__hint-title">{limit.title}</Text>
              {limit.body ? <Text className="pk__hint-desc">{limit.body}</Text> : null}
            </View>
            <View className={`pk__foot ${enter(2)}`}>
              <PrimaryButton text={PURCHASE_COPY.lastResult} onClick={() => void viewHistory()} />
            </View>
          </>
        ) : (
          <>
            <View className={`pk__hint ${enter(1)}`}>
              <Text className="pk__hint-title">{PURCHASE_COPY.title}</Text>
              <Text className="pk__hint-desc">{PURCHASE_COPY.desc}</Text>
              {!shownMedia && !photoPath ? (
                <Text className="pk__hint-tips">{PURCHASE_COPY.uploadTips.join(' · ')}</Text>
              ) : null}
            </View>
            {error ? <ErrorState message={error} onRetry={() => void analyze(false)} /> : null}
            <View className={`pk__foot ${enter(2)}`}>
              <PrimaryButton
                text={busy ? PURCHASE_COPY.busy : PURCHASE_COPY.start}
                loading={busy}
                disabled={!photoPath && !photoMedia}
                onClick={() => void analyze(false)}
              />
              <View className="pk__foot-row">
                <Text className="pk__foot-alt pressable" onClick={() => void analyze(true)}>{PURCHASE_COPY.demo}</Text>
              </View>
            </View>
          </>
        )}
      </View>
    </View>
  )
}
