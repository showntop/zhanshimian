// 穿搭诊断：场景 3 选 + 单图上传 + 同步诊断（锚点标注）+ 先改哪一处。
// 空态：照片 hero → 导语 → 场景。结果：照片 → 一句建议 → 观察清单，不套报告卡。
// 同步请求跨页存活：会话见 services/outfit-session（模块级 inflight + storage 草稿），
// 退回首页再进入可恢复进行中/结论；复访先展示草稿，服务端 latest 只在更新时接管。
import { useCallback, useEffect, useRef, useState } from 'react'
import Taro from '@tarojs/taro'
import { Image, Text, View } from '@tarojs/components'
import { BILLING_COPY, OUTFIT_COPY, type Diagnosis, type DisplayMedia } from '@zsm/core'
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
  sharedOutfitSession,
  type OutfitResumeEvent,
} from '../../../../services/outfit-session'
import { readStorage, writeStorage } from '../../../../services/storage'
import { splitAdviceTitle } from '../../../../services/advice-title'
import AppHeader from '../../../../components/app-header'
import PrimaryButton from '../../../../components/primary-button'
import SourceImage from '../../../../components/source-image'
import PhotoAnnotationLayer from '../../../../components/photo-annotation'
import Pill from '../../../../components/pill'
import ErrorState from '../../../../components/error-state'
import './index.scss'

// 空态 hero 的拍照示范图（包内资产，JPEG；人物为内置模特，叠「拍照示范」角标）
const OUTFIT_GUIDE_IMAGE = '/assets/capture/outfit-guide.jpg'

// hero 相框比例（rpx，与 index.scss 一致）：锚点按 aspectFit 可视区重映射。
const HERO_W = 686
const HERO_H = 640

const CONTEXTS = [
  { key: 'daily', label: '日常' },
  { key: 'interview', label: '面试' },
  { key: 'date', label: '约会' },
] as const

// 会话单例挂在页面模块上：模块只装载一次，inflight 与草稿因此跨页存活。
const outfitSession = sharedOutfitSession({ read: readStorage, write: writeStorage })

export default function Outfit() {
  const [scene, setScene] = useState<string>('daily')
  const [photoPath, setPhotoPath] = useState('')
  const [photoMedia, setPhotoMedia] = useState<DisplayMedia | null>(null)
  const [result, setResult] = useState<Diagnosis | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  // 日限（429/402）体面错误态：明日再来 + 查看上次结果，不进计费购买链
  const [limit, setLimit] = useState<{ title: string; body: string } | null>(null)
  // 所选照片真实宽高（onLoad 采集）：aspectFit 可视区锚点换算
  const [photoDims, setPhotoDims] = useState<{ w: number; h: number } | null>(null)

  const resumingRef = useRef(false)
  // 本页点了「再诊断一次」或重选照片：不要立刻用服务端旧结论盖回去
  const freshStartRef = useRef(false)
  const { pageClass, enter } = usePageShell(true, '', 'outfit')

  const applySession = useCallback((item: Diagnosis) => {
    setResult(item)
    if (item.scene) setScene(item.scene)
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
  // 事件在这里翻译成 setState；时序与防旧盖新判定都在 services/outfit-session。
  const resume = useCallback(async () => {
    if (resumingRef.current) return
    resumingRef.current = true
    try {
      await outfitSession.resume(
        {
          getLatest: () => peripherals.getLatestDiagnosis('outfit'),
          diagnose: peripherals.diagnose,
        },
        (event: OutfitResumeEvent) => {
          if (event.type === 'hydrate') {
            const { draft } = event
            if (draft.scene) setScene(draft.scene)
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
          applyError(event.error, '诊断没有成功，请重试')
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
          setPhotoDims(null)
          setError('')
          setLimit(null)
          freshStartRef.current = true
          // 草稿同步换成新照片：进行中的旧请求落定时就不会再盖回来
          outfitSession.writeDraft({
            pending: false,
            scene,
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
    if (outfitSession.getInflight() || busy) return
    setBusy(true)
    setError('')
    setLimit(null)
    // 提到 try 外：catch 要用它判断「草稿是否已换照片」
    let mediaId = ''
    try {
      if (demo) {
        const media = await qualityApi.createDemoMedia('body', `outfit-demo:${Date.now()}`)
        mediaId = media.asset_id
        setPhotoMedia(media)
        setPhotoPath('')
      } else {
        if (!photoPath && !photoMedia) {
          Taro.showToast({ title: '先上传一张全身照', icon: 'none' })
          return
        }
        if (photoPath) {
          const image = await readLocalImage(photoPath)
          mediaId = (await uploadMedia(mediaUpload, image, 'body')).id
        } else {
          mediaId = photoMedia!.asset_id
        }
      }
      outfitSession.markPending({ scene, photoPath: demo ? '' : photoPath, mediaId })
      const item = await outfitSession.runDiagnose(mediaId, () =>
        peripherals.diagnose({ kind: 'outfit', media_id: mediaId, scene }),
      )
      // 飞行中重选了照片：旧结论已被会话拒收，这里也不上屏
      if (outfitSession.read()?.result?.id === item.id) applySession(item)
    } catch (e) {
      // mediaId 为空说明上传/示例图就没走通，错误照常展示；非空但草稿已换照片则静默
      if (mediaId && outfitSession.read()?.mediaId !== mediaId) return
      applyError(e, '诊断没有成功，请重试')
    } finally {
      setBusy(false)
    }
  }

  const saveResult = async () => {
    if (!result) return
    try {
      await peripherals.updateDiagnosis(result.id, { saved: true })
      Taro.showToast({ title: OUTFIT_COPY.saved, icon: 'success' })
    } catch (e) {
      Taro.showToast({ title: (e as Error).message || '保存没有成功', icon: 'none' })
    }
  }

  const toPlans = () => Taro.switchTab({ url: '/pages/plans/index' })

  // 日限错误态的「查看上次结果」：历史进来就替换掉当前未完成的照片位
  const viewHistory = async () => {
    try {
      const latest = await peripherals.getLatestDiagnosis('outfit')
      if (!latest) return
      outfitSession.writeDraft({ photoPath: '' })
      outfitSession.markDone(latest)
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
    outfitSession.clearResult()
    setResult(null)
    setError('')
    setLimit(null)
  }

  const shownMedia: DisplayMedia | null = photoMedia ?? result?.source_media ?? null
  const adviceTitle = result?.priority_title || result?.conclusion || OUTFIT_COPY.title
  const { lead: adviceLead, action: adviceAction } = splitAdviceTitle(adviceTitle)
  const adviceBody = result
    ? result.priority_copy || (result.priority_title ? result.conclusion : '')
    : ''
  const keepFindings = (result?.findings ?? []).filter((item) => item.tone === 'positive')
  const liftFindings = (result?.findings ?? []).filter((item) => item.tone !== 'positive')

  return (
    <View className={pageClass}>
      <AppHeader title="穿搭诊断" back />
      <View className={`od${result ? ' od--done' : ''}`}>
        <View className={`od__hero photo-hero photo-hero--bleed ${enter()}`}>
          {shownMedia ? (
            <>
              <SourceImage className="od__photo-backdrop" media={shownMedia} mode="aspectFill" />
              <SourceImage
                className="od__hero-img od__hero-img--fit"
                media={shownMedia}
                mode="aspectFit"
                onLoad={(e) => setPhotoDims({ w: Number(e.detail.width), h: Number(e.detail.height) })}
              />
            </>
          ) : photoPath ? (
            <>
              <Image className="od__photo-backdrop" src={photoPath} mode="aspectFill" />
              <Image
                className="od__hero-img od__hero-img--fit"
                src={photoPath}
                mode="aspectFit"
                onLoad={(e) => setPhotoDims({ w: Number(e.detail.width), h: Number(e.detail.height) })}
              />
            </>
          ) : (
            <View className="od__upload pressable" onClick={choosePhoto}>
              <SourceImage className="od__upload-backdrop" reference={{ slug: 'natural', variant: 'full' }} />
              <SourceImage className="od__hero-img" reference={{ slug: 'natural', variant: 'full' }} anchor="top" />
              <View className="od__upload-bar">
                <Text className="od__upload-bar-plus">＋</Text>
                <Text className="od__upload-bar-text">{OUTFIT_COPY.uploadTitle}</Text>
              </View>
            </View>
          )}
          {shownMedia || photoPath ? (
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
              frameH={HERO_H}
              photoDims={photoDims ?? undefined}
              onTap={() => {}}
              showDrawer={false}
            />
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

            {error ? <ErrorState message={error} onRetry={() => void analyze(false)} /> : null}

            <View className="od__result-actions">
              <PrimaryButton text={OUTFIT_COPY.toPlans} onClick={toPlans} />
              <View className="od__result-row">
                <Text className="od__result-alt pressable" onClick={() => void saveResult()}>{OUTFIT_COPY.save}</Text>
                <Text className="od__result-alt pressable" onClick={retryFresh}>{OUTFIT_COPY.again}</Text>
              </View>
            </View>
          </View>
        ) : limit ? (
          <>
            <View className={`od__hint ${enter(1)}`}>
              <Text className="od__hint-title">{limit.title}</Text>
              {limit.body ? <Text className="od__hint-desc">{limit.body}</Text> : null}
            </View>
            <View className={`od__foot ${enter(2)}`}>
              <PrimaryButton text={OUTFIT_COPY.lastResult} onClick={() => void viewHistory()} />
            </View>
          </>
        ) : (
          <>
            <View className={`od__hint ${enter(1)}`}>
              <Text className="od__hint-title">{OUTFIT_COPY.title}</Text>
              <Text className="od__hint-desc">{OUTFIT_COPY.desc}</Text>
              {!shownMedia && !photoPath ? (
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
            {error ? <ErrorState message={error} onRetry={() => void analyze(false)} /> : null}
          </>
        )}

        {!result && !limit ? (
          <View className={`od__foot ${enter(3)}`}>
            <PrimaryButton
              text={busy ? OUTFIT_COPY.busy : OUTFIT_COPY.start}
              loading={busy}
              disabled={!photoPath && !photoMedia}
              onClick={() => void analyze(false)}
            />
            <View className="od__foot-row">
              <Text className="od__foot-alt pressable" onClick={() => void analyze(true)}>{OUTFIT_COPY.demo}</Text>
            </View>
          </View>
        ) : null}
      </View>
    </View>
  )
}
